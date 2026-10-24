package handlers

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"monitoring-master/internal/cleanup"
	"monitoring-master/internal/digest"
	"monitoring-master/internal/models"
	"monitoring-master/internal/repository"

	"github.com/gin-gonic/gin"
)

var tokenRegex = regexp.MustCompile(`^\d+:[A-Za-z0-9\-_]+$`)

type SettingHandler struct {
	Repo repository.Repository
	Bot  interface{ SendTestAlert(chatID, topicID string) error }
}

func NewSettingHandler(repo repository.Repository) *SettingHandler {
	return &SettingHandler{Repo: repo}
}

func (h *SettingHandler) SetBot(bot interface{ SendTestAlert(chatID, topicID string) error }) {
	h.Bot = bot
}

// GetSettings retrieves the global settings. Masks token for UI safety.
func (h *SettingHandler) GetSettings(c *gin.Context) {
	setting, err := h.Repo.GetSettings(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch settings"})
		return
	}

	// Mask token: show first 10 chars then ****
	masked := setting.TelegramBotToken
	if len(masked) > 10 {
		masked = masked[:10] + "••••••"
	}

	c.JSON(http.StatusOK, gin.H{
		"telegram_bot_token":      masked,
		"telegram_bot_token_full": setting.TelegramBotToken,
		"telegram_chat_id":        setting.TelegramChatID,
		"telegram_topic_id":       setting.TelegramTopicID,
		"enable_telegram_alerts":  setting.EnableTelegramAlerts,
		"enable_daily_digest":     setting.EnableDailyDigest,
		"daily_digest_time":       setting.DailyDigestTime,
		"updated_at":              setting.UpdatedAt,
	})
}

// UpdateSettings updates or creates the global settings with token validation.
func (h *SettingHandler) UpdateSettings(c *gin.Context) {
	var input struct {
		TelegramBotToken     string `json:"telegram_bot_token"`
		TelegramChatID       string `json:"telegram_chat_id"`
		TelegramTopicID      string `json:"telegram_topic_id"`
		EnableTelegramAlerts bool   `json:"enable_telegram_alerts"`
		EnableDailyDigest    bool   `json:"enable_daily_digest"`
		DailyDigestTime      string `json:"daily_digest_time"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate bot token format if provided
	if input.TelegramBotToken != "" && !tokenRegex.MatchString(input.TelegramBotToken) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid bot token format. Expected: <numbers>:<alphanumeric>"})
		return
	}

	if input.DailyDigestTime == "" {
		input.DailyDigestTime = "08:00"
	}
	if _, _, ok := digest.ParseDigestTime(input.DailyDigestTime); !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid daily_digest_time format. Expected HH:MM"})
		return
	}

	setting := models.GlobalSetting{
		TelegramBotToken:     input.TelegramBotToken,
		TelegramChatID:       input.TelegramChatID,
		TelegramTopicID:      input.TelegramTopicID,
		EnableTelegramAlerts: input.EnableTelegramAlerts,
		EnableDailyDigest:    input.EnableDailyDigest,
		DailyDigestTime:      input.DailyDigestTime,
		UpdatedAt:            time.Now(),
	}

	err := h.Repo.UpdateSettings(c.Request.Context(), &setting)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save settings"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Settings updated successfully", "updated_at": setting.UpdatedAt})
}

// TestAlert sends a test Telegram message to verify configuration.
func (h *SettingHandler) TestAlert(c *gin.Context) {
	var input struct {
		TelegramBotToken string `json:"telegram_bot_token"`
		TelegramChatID   string `json:"telegram_chat_id"`
		TelegramTopicID  string `json:"telegram_topic_id"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if input.TelegramBotToken == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Bot token is required"})
		return
	}
	if input.TelegramChatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Chat ID is required"})
		return
	}

	// If masked token (contains •), reject — user must save first to reveal full
	if containsMaskChar(input.TelegramBotToken) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cannot test with masked token. Save settings first."})
		return
	}

	// Send test via bot package
	// We use a direct HTTP call to avoid dependency on full bot lifecycle
	err := sendTestMessage(input.TelegramBotToken, input.TelegramChatID, input.TelegramTopicID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to send test message: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Test alert sent successfully"})
}

// TestDailyDigest triggers an immediate test run of the daily health digest.
func (h *SettingHandler) TestDailyDigest(c *gin.Context) {
	setting, err := h.Repo.GetSettings(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch settings"})
		return
	}
	if !setting.EnableDailyDigest {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Laporan harian sedang dinonaktifkan. Aktifkan switch Laporan Harian lalu simpan dulu."})
		return
	}

	err = digest.SendDailyDigest(c.Request.Context(), h.Repo)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Gagal mengirim daily digest: %v", err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Laporan harian (Daily Digest) berhasil dikirim ke Telegram."})
}

// GetSecurityLogs returns paginated security logs.
func (h *SettingHandler) GetSecurityLogs(c *gin.Context) {
	page := 1
	if p := c.Query("page"); p != "" {
		fmt.Sscanf(p, "%d", &page)
	}
	if page < 1 {
		page = 1
	}

	limit := 20
	if l := c.Query("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}
	if limit < 1 {
		limit = 20
	}

	logs, total, err := h.Repo.GetSecurityLogs(c.Request.Context(), page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch security logs"})
		return
	}

	if logs == nil {
		logs = []models.SecurityLog{}
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

func containsMaskChar(s string) bool {
	for _, r := range s {
		if r == '•' {
			return true
		}
	}
	return false
}

// sendTestMessage sends a test alert via Telegram HTTP API.
func sendTestMessage(token, chatID, topicID string) error {
	text := "🧪 *Test Alert*\n\nService Monitor configuration is working correctly."
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)

	var reqBody string
	if topicID != "" {
		reqBody = fmt.Sprintf(
			`{"chat_id":%q,"message_thread_id":%s,"text":%q,"parse_mode":"Markdown"}`,
			chatID, topicID, text,
		)
	} else {
		reqBody = fmt.Sprintf(
			`{"chat_id":%q,"text":%q,"parse_mode":"Markdown"}`,
			chatID, text,
		)
	}

	req, err := http.NewRequest("POST", url, strings.NewReader(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram API error: %s (status %d)", string(respBody), resp.StatusCode)
	}

	return nil
}

type PurgeLogsRequest struct {
	LogType string `json:"log_type"` // "audit", "security", "schedule_run", "all"
	Days    int    `json:"days"`     // 0 = all, >0 = older than N days
}

// PurgeLogs manually deletes log records matching criteria.
func (h *SettingHandler) PurgeLogs(c *gin.Context) {
	var input PurgeLogsRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	if input.LogType == "" {
		input.LogType = "all"
	}
	if input.LogType != "audit" && input.LogType != "security" && input.LogType != "schedule_run" && input.LogType != "all" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid log_type. Must be audit, security, schedule_run, or all"})
		return
	}

	res, err := cleanup.ExecutePurge(c.Request.Context(), h.Repo, cleanup.PurgeOptions{
		LogType: input.LogType,
		Days:    input.Days,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to purge logs: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":               "Logs successfully purged",
		"deleted_audit":         res.DeletedAudit,
		"deleted_security":      res.DeletedSecurity,
		"deleted_schedule_runs": res.DeletedScheduleRuns,
		"total_deleted":         res.TotalDeleted,
	})
}

