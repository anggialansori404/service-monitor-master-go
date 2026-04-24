package handlers

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"monitoring-master/internal/models"
	"monitoring-master/internal/repository"
	"monitoring-master/internal/ws"

	"crypto/rand"
	"math/big"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type FrontendHandler struct {
	Repo        repository.Repository
	Registry    *ws.Registry
	JWTSecret   string
}

// NewFrontendHandler creates a new FrontendHandler.
func NewFrontendHandler(repo repository.Repository, registry *ws.Registry, jwtSecret string) *FrontendHandler {
	return &FrontendHandler{Repo: repo, Registry: registry, JWTSecret: jwtSecret}
}

// loginRequest is the expected payload for POST /auth/login.
type loginRequest struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// Login handles POST /auth/login.
// Authenticates against bcrypt-hashed passwords in the users table.
func (h *FrontendHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
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
		expiryStr = "15"
	}
	duration, err := strconv.Atoi(expiryStr)
	if err != nil {
		duration = 15
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

	var stats []institutionStats
	for _, inst := range institutions {
		up, _ := h.Repo.CountServicesByStatus(inst.ID, "UP")
		down, _ := h.Repo.CountServicesByStatus(inst.ID, "DOWN")

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

	// 1. Generate ID Unik
	reqID := fmt.Sprintf("req-%d", time.Now().UnixNano())

	// 2. Buat Channel untuk menunggu balasan
	responseChan := make(chan ws.WsResponse)
	h.Registry.PendingRequests.Store(reqID, responseChan)
	defer h.Registry.PendingRequests.Delete(reqID)

	// 3. Susun Payload dengan map (agar JSON key-nya PASTI huruf kecil 'request_id')
	payload := map[string]interface{}{
		"request_id": reqID,
		"action":     "restart",
		"service":    serviceName, // dari parameter URL
	}

	// 4. Kirim ke WebSocket Agent
	err = h.Registry.SendToAgent(institution.AgentID, payload)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	// 1. Ambil titipan dari Middleware
	userEmail := c.GetString("userEmail") // Sesuai dengan c.Set di Middleware

	// 2. LOG HASIL GET
	log.Printf("DEBUG RESTART CONTROLLER: Extracted email from context='%s'", userEmail)

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

	var agentResp ws.WsResponse
	select {
	case res := <-responseChan:
		agentResp = res

		statusVal := "FAILED"
		success, ok := res["success"].(bool)
		if ok && success {
			statusVal = "SUCCESS"
		}

		detailsStr, _ := res["message"].(string)

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
			// Log failure but still return agent response.
			agentResp["warning"] = "failed to record audit log"
		}

		c.JSON(http.StatusOK, agentResp)

	case <-time.After(10 * time.Second):
		auditLog := models.AuditLog{
			InstitutionID: instID,
			ServiceName:   serviceName,
			Action:        "RESTART_COMMAND",
			Status:        "FAILED",
			TriggeredBy:   userEmail,
			Timestamp:     time.Now().Local(),
			Details:       "Timeout waiting for agent response",
		}
		_ = h.Repo.CreateAuditLog(c.Request.Context(), &auditLog)

		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "timeout waiting for agent response"})
	}
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
	Name string `json:"name" binding:"required"`
}

// CreateInstitution handles POST /institutions.
// Creates a new institution with a generated AgentID and SecretKey.
func (h *FrontendHandler) CreateInstitution(c *gin.Context) {
	var req createInstitutionReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	inst := models.Institution{
		Name:      req.Name,
		AgentID:   uuid.New().String(),
		SecretKey: generateRandomString(32),
		IsActive:  true,
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
	Name     string `json:"name"`
	IsActive *bool  `json:"is_active"`
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

	if err := h.Repo.UpdateInstitution(inst); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update institution"})
		return
	}

	c.JSON(http.StatusOK, inst)
}

// DeleteInstitution handles DELETE /institutions/:id.
// Deletes an institution and its associated data.
func (h *FrontendHandler) DeleteInstitution(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid institution id"})
		return
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
