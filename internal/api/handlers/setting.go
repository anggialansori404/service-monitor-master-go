package handlers

import (
	"net/http"

	"monitoring-master/internal/models"
	"monitoring-master/internal/repository"

	"github.com/gin-gonic/gin"
)

type SettingHandler struct {
	Repo repository.Repository
}

func NewSettingHandler(repo repository.Repository) *SettingHandler {
	return &SettingHandler{Repo: repo}
}

// GetSettings retrieves the global settings. Returns empty/default if none exist.
func (h *SettingHandler) GetSettings(c *gin.Context) {
	setting, err := h.Repo.GetSettings(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch settings"})
		return
	}

	c.JSON(http.StatusOK, setting)
}

// UpdateSettings updates or creates the global settings.
func (h *SettingHandler) UpdateSettings(c *gin.Context) {
	var input struct {
		TelegramBotToken     string `json:"telegram_bot_token"`
		TelegramChatID       string `json:"telegram_chat_id"`
		EnableTelegramAlerts bool   `json:"enable_telegram_alerts"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	setting := models.GlobalSetting{
		TelegramBotToken:     input.TelegramBotToken,
		TelegramChatID:       input.TelegramChatID,
		EnableTelegramAlerts: input.EnableTelegramAlerts,
	}

	err := h.Repo.UpdateSettings(c.Request.Context(), &setting)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save settings"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Settings updated successfully"})
}
