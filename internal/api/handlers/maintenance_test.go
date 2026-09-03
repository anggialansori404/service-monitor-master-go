package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"monitoring-master/internal/alert"
	"monitoring-master/internal/api/handlers"
	"monitoring-master/internal/models"
	"monitoring-master/internal/ws"

	"github.com/gin-gonic/gin"
)

func TestMaintenanceModeAlertSuppression(t *testing.T) {
	future := time.Now().Add(1 * time.Hour)
	past := time.Now().Add(-1 * time.Hour)

	global := &models.GlobalSetting{
		TelegramBotToken:     "global-tok",
		TelegramChatID:       "global-chat",
		EnableTelegramAlerts: true,
	}

	// 1. Institution in active maintenance -> targets should be EMPTY
	instActiveMaint := &models.Institution{
		ID:                   "inst-1",
		Name:                 "Active Maint",
		EnableTelegramAlerts: true,
		UseCustomTelegram:    true,
		TelegramBotToken:     "tok-1",
		TelegramChatID:       "chat-1",
		InMaintenance:        true,
		MaintenanceUntil:     &future,
	}
	targets := alert.ResolveTelegramTargets(instActiveMaint, global)
	if len(targets) != 0 {
		t.Errorf("expected 0 targets during active maintenance, got %d", len(targets))
	}

	// 2. Institution in maintenance with nil until (indefinite) -> targets should be EMPTY
	instIndefMaint := &models.Institution{
		ID:                   "inst-2",
		Name:                 "Indef Maint",
		EnableTelegramAlerts: true,
		UseCustomTelegram:    true,
		TelegramBotToken:     "tok-2",
		TelegramChatID:       "chat-2",
		InMaintenance:        true,
		MaintenanceUntil:     nil,
	}
	targets = alert.ResolveTelegramTargets(instIndefMaint, global)
	if len(targets) != 0 {
		t.Errorf("expected 0 targets during indefinite maintenance, got %d", len(targets))
	}

	// 3. Institution maintenance has EXPIRED -> targets should NOT be empty
	instExpiredMaint := &models.Institution{
		ID:                   "inst-3",
		Name:                 "Expired Maint",
		EnableTelegramAlerts: true,
		UseCustomTelegram:    true,
		TelegramBotToken:     "tok-3",
		TelegramChatID:       "chat-3",
		InMaintenance:        true,
		MaintenanceUntil:     &past,
	}
	targets = alert.ResolveTelegramTargets(instExpiredMaint, global)
	if len(targets) != 1 {
		t.Errorf("expected 1 target after maintenance expired, got %d", len(targets))
	}
}

func TestSetMaintenanceModeAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &mockInstRepo{
		getInst: &models.Institution{
			ID:   "inst-abc",
			Name: "Hospital Node",
		},
	}
	registry := ws.NewRegistry()
	h := handlers.NewFrontendHandler(repo, registry, "secret", "captcha")

	r := gin.New()
	r.POST("/institutions/:id/maintenance", func(c *gin.Context) {
		c.Set("userID", "admin-1")
		h.SetMaintenanceMode(c)
	})

	payload := map[string]interface{}{
		"enabled":          true,
		"duration_minutes": 60,
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", "/institutions/inst-abc/maintenance", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	if repo.updatedInst == nil || !repo.updatedInst.InMaintenance || repo.updatedInst.MaintenanceUntil == nil {
		t.Fatalf("expected institution to be in maintenance with future expiration timestamp, got %+v", repo.updatedInst)
	}
}
