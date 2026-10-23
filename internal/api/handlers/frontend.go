package handlers

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"monitoring-master/internal/alert"
	"monitoring-master/internal/logutil"
	"monitoring-master/internal/models"
	"monitoring-master/internal/repository"
	"monitoring-master/internal/services"
	"monitoring-master/internal/ws"

	"crypto/rand"
	"math/big"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type FrontendHandler struct {
	Repo          repository.Repository
	Registry      *ws.Registry
	WSHandler     *WSHandler
	JWTSecret     string
	CaptchaSecret string
}

// NewFrontendHandler creates a new FrontendHandler.
func NewFrontendHandler(repo repository.Repository, registry *ws.Registry, jwtSecret, captchaSecret string) *FrontendHandler {
	return &FrontendHandler{Repo: repo, Registry: registry, JWTSecret: jwtSecret, CaptchaSecret: captchaSecret}
}

// loginRequest is the expected payload for POST /auth/login.
type loginRequest struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
	CfToken  string `json:"cf_token"`
}

// Login handles POST /auth/login.
// Authenticates against bcrypt-hashed passwords in the users table.
func (h *FrontendHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// --- CAPTCHA validation ---
	captchaProvider := os.Getenv("CAPTCHA_PROVIDER")
	if captchaProvider == "" {
		captchaProvider = "auto"
	}

	switch captchaProvider {
	case "local":
		if req.CfToken == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "cf_token is required"})
			return
		}
		_, err := verifyLocalCaptchaToken(req.CfToken, h.CaptchaSecret)
		if err != nil {
			log.Printf("CAPTCHA FAILED (local): %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid or expired captcha token"})
			return
		}

	case "turnstile":
		if req.CfToken == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "cf_token is required"})
			return
		}
		secretKey := os.Getenv("TURNSTILE_SECRET_KEY")
		ok, err := verifyTurnstileToken(req.CfToken, secretKey)
		if err != nil || !ok {
			log.Printf("CAPTCHA FAILED (turnstile): ok=%v err=%v", ok, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "turnstile verification failed"})
			return
		}

	case "auto", "both":
		// Auto-detect: accept either local or turnstile token
		if req.CfToken == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "cf_token is required"})
			return
		}
		// Try local first (fast, no network)
		if _, err := verifyLocalCaptchaToken(req.CfToken, h.CaptchaSecret); err == nil {
			// Valid local token
		} else {
			// Not a local token, try turnstile
			secretKey := os.Getenv("TURNSTILE_SECRET_KEY")
			if secretKey == "" {
				log.Printf("CAPTCHA FAILED (auto): not a local token and TURNSTILE_SECRET_KEY not set")
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid captcha token"})
				return
			}
			ok, err := verifyTurnstileToken(req.CfToken, secretKey)
			if err != nil || !ok {
				log.Printf("CAPTCHA FAILED (auto): local=%v turnstile=(ok=%v, err=%v)", false, ok, err)
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid or expired captcha token"})
				return
			}
		}

	case "none":
		// No CAPTCHA — rate limiter already protects this route
	}

	user, err := h.Repo.GetUserByEmail(req.Email)
	if err != nil {
		log.Printf("LOGIN FAILED: User %s not found in DB or error. Error: %v", req.Email, err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		log.Printf("LOGIN FAILED: Password mismatch for %s. Error: %v", req.Email, err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	// 1. Buat custom claims dengan tipe MapClaims
	expiryStr := os.Getenv("JWT_EXPIRY_MINUTES")
	if expiryStr == "" {
		expiryStr = "1440" // 24 hours
	}
	duration, err := strconv.Atoi(expiryStr)
	if err != nil {
		duration = 1440
	}

	claims := jwt.MapClaims{
		"email": user.Email, // SANGAT PENTING: Sisipkan Email ke Token!
		"sub":   user.ID,    // ID string user
		"role":  user.Role,  // "admin"
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(time.Minute * time.Duration(duration)).Unix(),
	}

	// 2. Buat token dengan algoritma HS256
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	// 3. Tanda tangani token dengan JWT Secret
	tokenString, err := token.SignedString([]byte(h.JWTSecret))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}

	// 4. Return token ke Frontend
	c.JSON(http.StatusOK, gin.H{
		"token": tokenString,
		"user": gin.H{
			"email": user.Email,
			"role":  user.Role,
		},
	})
}

// institutionStats is the response shape for the institutions list.
type institutionStats struct {
	models.Institution
	UpCount   int64 `json:"up_count"`
	DownCount int64 `json:"down_count"`
	Connected bool  `json:"connected"`
}

// ListInstitutions handles GET /institutions.
// Returns all institutions with UP/DOWN service counts and connection status.
func (h *FrontendHandler) ListInstitutions(c *gin.Context) {
	institutions, err := h.Repo.GetAllInstitutions()
	if err != nil {
		log.Printf("ERROR GET INSTITUTIONS: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch institutions"})
		return
	}

	var instIDs []string
	for _, inst := range institutions {
		instIDs = append(instIDs, inst.ID)
	}

	countsMap, err := h.Repo.CountServicesByStatusBulk(instIDs)
	if err != nil {
		log.Printf("ERROR COUNT SERVICES BULK: %v", err)
		countsMap = make(map[string]map[string]int64) // fallback to empty
	}

	var stats []institutionStats
	for _, inst := range institutions {
		counts := countsMap[inst.ID]
		var up, down int64
		if counts != nil {
			up = counts["UP"]
			down = counts["DOWN"] + counts["ERROR"]
		}

		connected := false
		if inst.LastPing != nil && time.Since(*inst.LastPing) < 5*time.Minute {
			connected = true
		}

		stats = append(stats, institutionStats{
			Institution: inst,
			UpCount:     up,
			DownCount:   down,
			Connected:   connected,
		})
	}
	c.JSON(http.StatusOK, stats)
}

// GetDashboardSummary handles GET /institutions/summary.
// Returns an aggregated summary of institutions and services.
func (h *FrontendHandler) GetDashboardSummary(c *gin.Context) {
	summary, err := h.Repo.GetDashboardSummary(c.Request.Context())
	if err != nil {
		log.Printf("ERROR GET DASHBOARD SUMMARY: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch dashboard summary"})
		return
	}
	c.JSON(http.StatusOK, summary)
}

// ListServices handles GET /institutions/:id/services.
// Returns all services for the given institution.
func (h *FrontendHandler) ListServices(c *gin.Context) {
	institutionID := c.Param("id")
	if institutionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid institution id"})
		return
	}

	_, err := h.Repo.GetInstitutionByID(institutionID)
	if err != nil {
		if err == repository.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "institution not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	// Pagination
	page := 1
	if p := c.Query("page"); p != "" {
		fmt.Sscanf(p, "%d", &page)
	}
	if page < 1 {
		page = 1
	}

	limit := 10
	if l := c.Query("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}
	if limit < 1 {
		limit = 10
	}

	search := c.Query("search")
	status := c.Query("status")

	// Direct DB Integration (Redis Removed) - Created by Kang Anggi
	services, total, err := h.Repo.GetServicesByInstitution(institutionID, search, status, page, limit)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch services"})
		return
	}
	if services == nil {
		services = []models.Service{}
	}

	// Stale = no successful check within 90s (~6x the 15s agent interval).
	// Old 3-minute window let dead agents show fresh UP for too long.
	staleThreshold := 90 * time.Second
	now := time.Now()
	for i := range services {
		if services[i].LastChecked == nil || now.Sub(*services[i].LastChecked) > staleThreshold {
			services[i].IsStale = true
			services[i].Stale = true
		}
	}

	totalPages := int(total) / limit
	if int(total)%limit != 0 {
		totalPages++
	}

	c.JSON(http.StatusOK, gin.H{
		"data": services,
		"pagination": gin.H{
			"total_count":  total,
			"total_pages":  totalPages,
			"current_page": page,
			"limit":        limit,
		},
	})

}

// RestartService handles POST /institutions/:id/services/:name/restart.
// Sends a restart command to the connected agent via WebSocket synchronously
// and returns the agent's response.
func (h *FrontendHandler) RestartService(c *gin.Context) {
	instID := c.Param("id")
	if instID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid institution id"})
		return
	}

	serviceName := c.Param("name")
	if serviceName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "service name is required"})
		return
	}

	// Fetch institution to get AgentID.
	institution, err := h.Repo.GetInstitutionByID(instID)
	if err != nil {
		if err == repository.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "institution not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	// Verify service exists.
	_, err = h.Repo.GetService(instID, serviceName)
	if err != nil {
		if err == repository.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "service not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	// 1. Ambil titipan dari Middleware
	userEmail := c.GetString("userEmail") // Sesuai dengan c.Set di Middleware

	// 2. LOG HASIL GET
	logutil.Debugf("RESTART CONTROLLER: Extracted email from context='%s'", userEmail)

	if userEmail == "" {
		if uidStr, exists := c.Get("userID"); exists {
			uid, ok := uidStr.(string)
			if ok && uid != "" {
				if user, err := h.Repo.GetUserByID(c.Request.Context(), uid); err == nil {
					userEmail = user.Email
				}
			}
		}
	}
	if userEmail == "" {
		userEmail = "Unknown Admin"
	}

	agentResp, err := h.Registry.RequestRestart(institution.AgentID, serviceName, 10*time.Second)
	if err != nil {
		auditLog := models.AuditLog{
			InstitutionID: instID,
			ServiceName:   serviceName,
			Action:        "RESTART_COMMAND",
			Status:        "FAILED",
			TriggeredBy:   userEmail,
			Timestamp:     time.Now().Local(),
			Details:       err.Error(),
		}
		_ = h.Repo.CreateAuditLog(c.Request.Context(), &auditLog)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	statusVal := "FAILED"
	success, ok := agentResp["success"].(bool)
	if ok && success {
		statusVal = "SUCCESS"
		// Mark restart outcome. Do NOT claim UP/DOWN here:
		// real status arrives with the next agent sync. last_known_status
		// is the pre-restart DB value, not a fresh check.
		if err := h.Repo.UpdateServiceRestartStatus(c.Request.Context(), instID, serviceName, "SUCCESS"); err != nil {
			log.Printf("RestartService: failed to update restart status for %s/%s: %v", instID, serviceName, err)
		}
		if refreshedSvc, err := h.Repo.GetService(instID, serviceName); err == nil && refreshedSvc != nil {
			agentResp["display_name"] = refreshedSvc.GetDisplayName()
			agentResp["last_known_status"] = refreshedSvc.Status
			agentResp["note"] = "restart accepted; fresh status arrives with next agent sync"
		}
	}
	detailsStr, _ := agentResp["message"].(string)

	auditLog := models.AuditLog{
		InstitutionID: instID,
		ServiceName:   serviceName,
		Action:        "RESTART_COMMAND",
		Status:        statusVal,
		TriggeredBy:   userEmail,
		Timestamp:     time.Now().Local(),
		Details:       detailsStr,
	}
	if err := h.Repo.CreateAuditLog(c.Request.Context(), &auditLog); err != nil {
		agentResp["warning"] = "failed to record audit log"
	}

	c.JSON(http.StatusOK, agentResp)
}

// UpdateServiceAutoRestart handles PATCH /institutions/:id/services/:name.
// Updates the auto_restart field for a service.
func (h *FrontendHandler) UpdateServiceAutoRestart(c *gin.Context) {
	instID := c.Param("id")
	serviceName := c.Param("name")
	
	var req struct {
		AutoRestart bool `json:"auto_restart" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	
	if err := h.Repo.UpdateServiceAutoRestart(c.Request.Context(), instID, serviceName, req.AutoRestart); err != nil {
		if err == repository.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "service not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update service"})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{"message": "auto_restart updated", "auto_restart": req.AutoRestart})
}

// generateRandomString returns a cryptographically random string of length n.
func generateRandomString(n int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		idx, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		b[i] = charset[idx.Int64()]
	}
	return string(b)
}

// createInstitutionReq is the expected payload for POST /institutions.
type createInstitutionReq struct {
	Name                 string `json:"name" binding:"required"`
	UseCustomTelegram    bool   `json:"use_custom_telegram"`
	TelegramBotToken     string `json:"telegram_bot_token"`
	TelegramChatID       string `json:"telegram_chat_id"`
	TelegramTopicID      string `json:"telegram_topic_id"`
	EnableTelegramAlerts *bool  `json:"enable_telegram_alerts"`
	MirrorToGlobal       bool   `json:"mirror_to_global"`
	NotifyNodeOffline    *bool  `json:"notify_node_offline"`
}

// CreateInstitution handles POST /institutions.
// Creates a new institution with a generated AgentID and SecretKey.
func (h *FrontendHandler) CreateInstitution(c *gin.Context) {
	var req createInstitutionReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	enableAlerts := true
	if req.EnableTelegramAlerts != nil {
		enableAlerts = *req.EnableTelegramAlerts
	}

	notifyNodeOffline := true
	if req.NotifyNodeOffline != nil {
		notifyNodeOffline = *req.NotifyNodeOffline
	}

	inst := models.Institution{
		Name:                 req.Name,
		AgentID:              uuid.New().String(),
		SecretKey:            generateRandomString(32),
		IsActive:             true,
		UseCustomTelegram:    req.UseCustomTelegram,
		TelegramBotToken:     req.TelegramBotToken,
		TelegramChatID:       req.TelegramChatID,
		TelegramTopicID:      req.TelegramTopicID,
		EnableTelegramAlerts: enableAlerts,
		MirrorToGlobal:       req.MirrorToGlobal,
		NotifyNodeOffline:    notifyNodeOffline,
	}

	if err := h.Repo.CreateInstitution(&inst); err != nil {
		log.Printf("CreateInstitution error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create institution: " + err.Error()})
		return
	}

	c.JSON(http.StatusCreated, inst)
}

// updateInstitutionReq is the expected payload for PUT /institutions/:id.
type updateInstitutionReq struct {
	Name                 string  `json:"name"`
	IsActive             *bool   `json:"is_active"`
	UseCustomTelegram    *bool   `json:"use_custom_telegram"`
	TelegramBotToken     *string `json:"telegram_bot_token"`
	TelegramChatID       *string `json:"telegram_chat_id"`
	TelegramTopicID      *string `json:"telegram_topic_id"`
	EnableTelegramAlerts *bool   `json:"enable_telegram_alerts"`
	MirrorToGlobal       *bool   `json:"mirror_to_global"`
	NotifyNodeOffline    *bool   `json:"notify_node_offline"`
}

// GetInstitution handles GET /institutions/:id.
// Returns a single institution by ID.
func (h *FrontendHandler) GetInstitution(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid institution id"})
		return
	}

	inst, err := h.Repo.GetInstitutionByID(id)
	if err != nil {
		if err == repository.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "institution not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	c.JSON(http.StatusOK, inst)
}

// GetInstitutionCredentials handles GET /institutions/:id/credentials.
// Returns only the agent_id and secret_key for the given institution.
func (h *FrontendHandler) GetInstitutionCredentials(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid institution id"})
		return
	}

	inst, err := h.Repo.GetInstitutionByID(id)
	if err != nil {
		if err == repository.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "institution not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"agent_id":   inst.AgentID,
		"secret_key": inst.SecretKey,
	})
}

// UpdateInstitution handles PUT /institutions/:id.
// Updates an existing institution's fields.
func (h *FrontendHandler) UpdateInstitution(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid institution id"})
		return
	}

	inst, err := h.Repo.GetInstitutionByID(id)
	if err != nil {
		if err == repository.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "institution not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	var req updateInstitutionReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Name != "" {
		inst.Name = req.Name
	}
	if req.IsActive != nil {
		inst.IsActive = *req.IsActive
	}
	if req.UseCustomTelegram != nil {
		inst.UseCustomTelegram = *req.UseCustomTelegram
	}
	if req.TelegramBotToken != nil {
		inst.TelegramBotToken = *req.TelegramBotToken
	}
	if req.TelegramChatID != nil {
		inst.TelegramChatID = *req.TelegramChatID
	}
	if req.TelegramTopicID != nil {
		inst.TelegramTopicID = *req.TelegramTopicID
	}
	if req.EnableTelegramAlerts != nil {
		inst.EnableTelegramAlerts = *req.EnableTelegramAlerts
	}
	if req.MirrorToGlobal != nil {
		inst.MirrorToGlobal = *req.MirrorToGlobal
	}
	if req.NotifyNodeOffline != nil {
		inst.NotifyNodeOffline = *req.NotifyNodeOffline
	}

	if err := h.Repo.UpdateInstitution(inst); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update institution"})
		return
	}

	c.JSON(http.StatusOK, inst)
}

type testInstitutionAlertReq struct {
	UseCustomTelegram    *bool   `json:"use_custom_telegram"`
	TelegramBotToken     *string `json:"telegram_bot_token"`
	TelegramChatID       *string `json:"telegram_chat_id"`
	TelegramTopicID      *string `json:"telegram_topic_id"`
	EnableTelegramAlerts *bool   `json:"enable_telegram_alerts"`
	MirrorToGlobal       *bool   `json:"mirror_to_global"`
}

// TestInstitutionAlert handles POST /institutions/:id/test-alert.
// Sends a test message to the institution's resolved Telegram destination(s).
func (h *FrontendHandler) TestInstitutionAlert(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid institution id"})
		return
	}

	inst, err := h.Repo.GetInstitutionByID(id)
	if err != nil {
		if err == repository.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "institution not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	// Override fields if provided in request body (e.g. testing directly from modal inputs)
	var req testInstitutionAlertReq
	if err := c.ShouldBindJSON(&req); err == nil {
		if req.UseCustomTelegram != nil {
			inst.UseCustomTelegram = *req.UseCustomTelegram
		}
		if req.TelegramBotToken != nil {
			inst.TelegramBotToken = *req.TelegramBotToken
		}
		if req.TelegramChatID != nil {
			inst.TelegramChatID = *req.TelegramChatID
		}
		if req.TelegramTopicID != nil {
			inst.TelegramTopicID = *req.TelegramTopicID
		}
		if req.EnableTelegramAlerts != nil {
			inst.EnableTelegramAlerts = *req.EnableTelegramAlerts
		}
		if req.MirrorToGlobal != nil {
			inst.MirrorToGlobal = *req.MirrorToGlobal
		}
	}

	if inst.UseCustomTelegram {
		if inst.TelegramBotToken == "" || inst.TelegramChatID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Bot Token dan Chat ID / Grup ID lembaga wajib diisi untuk menguji bot khusus lembaga."})
			return
		}
	}

	setting, _ := h.Repo.GetSettings(c.Request.Context())
	targets := alert.ResolveTelegramTargets(inst, setting)
	if len(targets) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Belum ada konfigurasi Telegram yang aktif atau alert dinonaktifkan untuk lembaga ini."})
		return
	}

	testMsg := fmt.Sprintf("🔔 *TEST NOTIFIKASI MONITORING*\n\n🏢 *Lembaga:* %s\n⏰ *Waktu:* %s\n\n_Konfigurasi bot Telegram lembaga ini berhasil terhubung dan siap menerima notifikasi alert!_",
		inst.Name, time.Now().Format("02 Jan 2006 15:04:05"))

	successCount := 0
	var customErr error
	var globalErr error

	for _, target := range targets {
		_, err := services.SendTelegramAlert(target.Token, target.ChatID, target.TopicID, testMsg)
		if err == nil {
			successCount++
		} else {
			if target.IsGlobal {
				globalErr = err
			} else {
				customErr = err
			}
			log.Printf("Failed to send test alert to target %+v: %v", target, err)
		}
	}

	// If custom bot target failed, report failure specifically for custom bot
	if customErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("Gagal mengirim ke Bot Lembaga (Chat ID: %s): %v. Pastikan Bot Token valid, Bot telah dimasukkan ke grup/channel sebagai Admin, dan Chat ID benar.", inst.TelegramChatID, customErr),
		})
		return
	}

	if successCount == 0 {
		errMsg := "Gagal mengirim notifikasi ke Telegram."
		if globalErr != nil {
			errMsg = fmt.Sprintf("Gagal mengirim ke Telegram Global: %v", globalErr)
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": errMsg})
		return
	}

	msg := fmt.Sprintf("Berhasil mengirim test notifikasi ke %d tujuan Telegram.", successCount)
	if inst.UseCustomTelegram && inst.MirrorToGlobal {
		msg = fmt.Sprintf("Berhasil mengirim test notifikasi ke Bot Lembaga (%s) dan Grup Global Admin.", inst.TelegramChatID)
	} else if inst.UseCustomTelegram {
		msg = fmt.Sprintf("Berhasil mengirim test notifikasi ke Bot Lembaga (%s).", inst.TelegramChatID)
	}

	c.JSON(http.StatusOK, gin.H{
		"message": msg,
	})
}

type setMaintenanceModeReq struct {
	Enabled         *bool `json:"enabled"`
	InMaintenance   *bool `json:"in_maintenance"`
	DurationMinutes int   `json:"duration_minutes"` // e.g. 30, 60, 120; 0 means indefinite until manual disable
}

// SetMaintenanceMode handles POST /institutions/:id/maintenance.
// Enables or disables maintenance mode with optional duration.
func (h *FrontendHandler) SetMaintenanceMode(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid institution id"})
		return
	}

	var req setMaintenanceModeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var isEnabled bool
	if req.InMaintenance != nil {
		isEnabled = *req.InMaintenance
	} else if req.Enabled != nil {
		isEnabled = *req.Enabled
	}

	inst, err := h.Repo.GetInstitutionByID(id)
	if err != nil {
		if err == repository.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "institution not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	inst.InMaintenance = isEnabled
	if isEnabled && req.DurationMinutes > 0 {
		until := time.Now().Add(time.Duration(req.DurationMinutes) * time.Minute)
		inst.MaintenanceUntil = &until
	} else {
		inst.MaintenanceUntil = nil
	}

	if err := h.Repo.UpdateInstitution(inst); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update maintenance mode"})
		return
	}

	// Audit log
	action := "DISABLE_MAINTENANCE_MODE"
	details := fmt.Sprintf("Disabled maintenance mode for %s", inst.Name)
	if isEnabled {
		action = "ENABLE_MAINTENANCE_MODE"
		if inst.MaintenanceUntil != nil {
			details = fmt.Sprintf("Enabled maintenance mode for %s until %s (%d mins)", inst.Name, inst.MaintenanceUntil.Format(time.RFC3339), req.DurationMinutes)
		} else {
			details = fmt.Sprintf("Enabled indefinite maintenance mode for %s", inst.Name)
		}
	}
	_ = h.Repo.CreateAuditLog(c.Request.Context(), &models.AuditLog{
		InstitutionID: inst.ID,
		Action:        action,
		Status:        "SUCCESS",
		TriggeredBy:   c.GetString("userEmail"),
		Details:       details,
		Timestamp:     time.Now(),
		CreatedAt:     time.Now(),
	})

	c.JSON(http.StatusOK, gin.H{
		"message":           "Maintenance mode updated successfully",
		"in_maintenance":    inst.InMaintenance,
		"enabled":           inst.InMaintenance,
		"maintenance_until": inst.MaintenanceUntil,
	})
}

// GetInstitutionUptime handles GET /institutions/:id/uptime.
// Returns SLA percentage and daily uptime buckets for visual uptime bars.
func (h *FrontendHandler) GetInstitutionUptime(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid institution id"})
		return
	}

	daysStr := c.DefaultQuery("days", "30")
	days, err := strconv.Atoi(daysStr)
	if err != nil || days <= 0 || days > 90 {
		days = 30
	}

	res, err := h.Repo.GetUptimeStats(c.Request.Context(), id, days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to calculate uptime stats"})
		return
	}

	c.JSON(http.StatusOK, res)
}

// GetInstitutionIncidents handles GET /institutions/:id/incidents.
// Returns list of recent downtime incidents for an institution with pagination (limit 10).
func (h *FrontendHandler) GetInstitutionIncidents(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid institution id"})
		return
	}

	page := 1
	if p := c.Query("page"); p != "" {
		fmt.Sscanf(p, "%d", &page)
	}
	if page < 1 {
		page = 1
	}

	limit := 10
	if l := c.Query("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}
	if limit <= 0 || limit > 100 {
		limit = 10
	}

	incidents, total, err := h.Repo.GetIncidentsByInstitution(c.Request.Context(), id, page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch incidents"})
		return
	}

	if incidents == nil {
		incidents = []models.IncidentLog{}
	}

	totalPages := int(total) / limit
	if int(total)%limit != 0 || totalPages == 0 {
		totalPages++
	}

	c.JSON(http.StatusOK, gin.H{
		"data": incidents,
		"pagination": gin.H{
			"total_count":  total,
			"total_pages":  totalPages,
			"current_page": page,
			"limit":        limit,
		},
	})
}

// DeleteInstitution handles DELETE /institutions/:id.
// Deletes an institution and its associated data.
func (h *FrontendHandler) DeleteInstitution(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid institution id"})
		return
	}

	// Fetch institution before deletion to clean up active WebSocket connections and timers
	inst, err := h.Repo.GetInstitutionByID(id)
	if err != nil && err != repository.ErrNotFound {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	if inst != nil {
		if h.WSHandler != nil {
			h.WSHandler.CleanupInstitution(inst.ID, inst.AgentID)
		} else if h.Registry != nil && inst.AgentID != "" {
			h.Registry.Unregister(inst.AgentID)
		}
	}

	if err := h.Repo.DeleteInstitution(id); err != nil {
		if err == repository.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "institution not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete institution"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "institution deleted"})
}

// RegenerateKeys handles POST /institutions/:id/regenerate-keys.
// Generates a new random 32-char SecretKey for the institution.
func (h *FrontendHandler) RegenerateKeys(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid institution id"})
		return
	}

	inst, err := h.Repo.GetInstitutionByID(id)
	if err != nil {
		if err == repository.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "institution not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	inst.SecretKey = generateRandomString(32)

	if err := h.Repo.UpdateInstitution(inst); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to regenerate keys"})
		return
	}

	c.JSON(http.StatusOK, inst)
}

func (h *FrontendHandler) ChangePassword(c *gin.Context) {
	var req struct {
		OldPassword string `json:"old_password" binding:"required"`
		NewPassword string `json:"new_password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	userIDStr, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	userID := userIDStr.(string)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid user id"})
		return
	}

	user, err := h.Repo.GetUserByID(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.OldPassword)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "incorrect old password"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
		return
	}

	if err := h.Repo.UpdateUserPassword(c.Request.Context(), userID, string(hash)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update password"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "password updated successfully"})
}

func (h *FrontendHandler) GetAuditLogs(c *gin.Context) {
	page := 1
	if p := c.Query("page"); p != "" {
		fmt.Sscanf(p, "%d", &page)
	}
	if page < 1 {
		page = 1
	}

	limit := 10
	if l := c.Query("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}
	if limit < 1 {
		limit = 10
	}

	filter := map[string]interface{}{}
	if startDate := c.Query("start_date"); startDate != "" {
		if t, err := time.Parse(time.RFC3339, startDate); err == nil {
			if filter["timestamp"] == nil {
				filter["timestamp"] = map[string]interface{}{}
			}
			filter["timestamp"].(map[string]interface{})["$gte"] = t
		}
	}
	if endDate := c.Query("end_date"); endDate != "" {
		if t, err := time.Parse(time.RFC3339, endDate); err == nil {
			if filter["timestamp"] == nil {
				filter["timestamp"] = map[string]interface{}{}
			}
			filter["timestamp"].(map[string]interface{})["$lte"] = t
		}
	}
	if action := c.Query("action"); action != "" {
		filter["action"] = action
	}
	if status := c.Query("status"); status != "" {
		filter["status"] = status
	}
	if institutionID := c.Query("institution_id"); institutionID != "" {
		filter["institution_id"] = institutionID
	}
	if search := c.Query("search"); search != "" {
		filter["$search"] = search
	}
	if sortField := c.Query("sort_field"); sortField != "" {
		filter["$sort_field"] = sortField
	}
	if sortDir := c.Query("sort_dir"); sortDir != "" {
		filter["$sort_dir"] = sortDir
	}

	logs, total, err := h.Repo.GetAuditLogs(c.Request.Context(), filter, page, limit)
	if err != nil {
		log.Printf("Failed to get audit logs: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch audit logs"})
		return
	}

	if logs == nil {
		logs = []models.AuditLog{}
	}

	totalPages := int(total) / limit
	if int(total)%limit != 0 {
		totalPages++
	}

	c.JSON(http.StatusOK, gin.H{
		"data": logs,
		"pagination": gin.H{
			"total_count":  total,
			"total_pages":  totalPages,
			"current_page": page,
			"limit":        limit,
		},
	})

}

// ExportAuditLogs returns ALL matching audit logs without pagination for CSV export.
func (h *FrontendHandler) ExportAuditLogs(c *gin.Context) {
	filter := map[string]interface{}{}
	if startDate := c.Query("start_date"); startDate != "" {
		if t, err := time.Parse(time.RFC3339, startDate); err == nil {
			if filter["timestamp"] == nil {
				filter["timestamp"] = map[string]interface{}{}
			}
			filter["timestamp"].(map[string]interface{})["$gte"] = t
		}
	}
	if endDate := c.Query("end_date"); endDate != "" {
		if t, err := time.Parse(time.RFC3339, endDate); err == nil {
			if filter["timestamp"] == nil {
				filter["timestamp"] = map[string]interface{}{}
			}
			filter["timestamp"].(map[string]interface{})["$lte"] = t
		}
	}
	if action := c.Query("action"); action != "" {
		filter["action"] = action
	}
	if status := c.Query("status"); status != "" {
		filter["status"] = status
	}
	if institutionID := c.Query("institution_id"); institutionID != "" {
		filter["institution_id"] = institutionID
	}
	if search := c.Query("search"); search != "" {
		filter["$search"] = search
	}

	// Fetch all matching records (max 50000 for safety)
	logs, err := h.Repo.GetAuditLogsAll(c.Request.Context(), filter)
	if err != nil {
		log.Printf("Failed to export audit logs: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to export audit logs"})
		return
	}

	if logs == nil {
		logs = []models.AuditLog{}
	}

	c.JSON(http.StatusOK, gin.H{"data": logs})
}
