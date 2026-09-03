package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"monitoring-master/internal/api/handlers"
	"monitoring-master/internal/models"
	"monitoring-master/internal/repository"
	"monitoring-master/internal/ws"

	"github.com/gin-gonic/gin"
)

type mockInstRepo struct {
	createdInst *models.Institution
	updatedInst *models.Institution
	getInst     *models.Institution
	settings    *models.GlobalSetting
}

func (m *mockInstRepo) CreateInstitution(inst *models.Institution) error {
	m.createdInst = inst
	return nil
}

func (m *mockInstRepo) UpdateInstitution(inst *models.Institution) error {
	m.updatedInst = inst
	return nil
}

func (m *mockInstRepo) GetInstitutionByID(id string) (*models.Institution, error) {
	if m.getInst != nil && m.getInst.ID == id {
		return m.getInst, nil
	}
	return nil, repository.ErrNotFound
}

func (m *mockInstRepo) GetSettings(_ context.Context) (*models.GlobalSetting, error) {
	if m.settings != nil {
		return m.settings, nil
	}
	return &models.GlobalSetting{}, nil
}

// Stubs for repository.Repository
func (m *mockInstRepo) GetInstitutionByAgentID(_ context.Context, _ string) (*models.Institution, error) {
	return nil, nil
}
func (m *mockInstRepo) GetInstitutionByName(_ string) (*models.Institution, error) { return nil, nil }
func (m *mockInstRepo) GetAllInstitutions() ([]models.Institution, error)         { return nil, nil }
func (m *mockInstRepo) GetDashboardSummary(_ context.Context) (*models.DashboardSummaryResponse, error) {
	return nil, nil
}
func (m *mockInstRepo) UpdateInstitutionPing(_ context.Context, _ string, _ *models.HardwareInfo, _ time.Time) error {
	return nil
}
func (m *mockInstRepo) CountInstitutions() (int64, error)              { return 0, nil }
func (m *mockInstRepo) DeleteInstitution(_ string) error               { return nil }
func (m *mockInstRepo) GetServicesByInstitution(_ string, _ string, _ string, _, _ int) ([]models.Service, int64, error) {
	return nil, 0, nil
}
func (m *mockInstRepo) GetService(_ string, _ string) (*models.Service, error)       { return nil, nil }
func (m *mockInstRepo) UpsertService(_ context.Context, _ *models.Service) error      { return nil }
func (m *mockInstRepo) BulkUpsertServices(_ context.Context, _ []models.Service) error { return nil }
func (m *mockInstRepo) UpdateServiceStatus(_ context.Context, _ string, _, _ string, _ time.Time) error {
	return nil
}
func (m *mockInstRepo) UpdateServiceRestartStatus(_ context.Context, _, _, _ string) error {
	return nil
}
func (m *mockInstRepo) UpdateServiceAutoRestart(_ context.Context, _, _ string, _ bool) error {
	return nil
}
func (m *mockInstRepo) CountServicesByStatus(_ string, _ string) (int64, error) { return 0, nil }
func (m *mockInstRepo) CountServicesByStatusBulk(_ []string) (map[string]map[string]int64, error) {
	return nil, nil
}
func (m *mockInstRepo) CreateService(_ *models.Service) error                 { return nil }
func (m *mockInstRepo) DeleteService(_ context.Context, _, _ string) error   { return nil }
func (m *mockInstRepo) Ping(_ context.Context) error                          { return nil }
func (m *mockInstRepo) CreateAuditLog(_ context.Context, _ *models.AuditLog) error {
	return nil
}
func (m *mockInstRepo) PurgeAuditLogs(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}
func (m *mockInstRepo) CreateSecurityLog(_ context.Context, _ *models.SecurityLog) error {
	return nil
}
func (m *mockInstRepo) PurgeSecurityLogs(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}
func (m *mockInstRepo) GetInstitutionByAgentIDAndSecret(_ context.Context, _, _ string) (*models.Institution, error) {
	return nil, nil
}
func (m *mockInstRepo) GetUserByEmail(_ string) (*models.User, error)     { return nil, nil }
func (m *mockInstRepo) CreateUser(_ *models.User) error                   { return nil }
func (m *mockInstRepo) GetUserByID(_ context.Context, _ string) (*models.User, error) {
	return nil, nil
}
func (m *mockInstRepo) UpdateUserPassword(_ context.Context, _, _ string) error { return nil }
func (m *mockInstRepo) GetAuditLogs(_ context.Context, _ map[string]interface{}, _, _ int) ([]models.AuditLog, int64, error) {
	return nil, 0, nil
}
func (m *mockInstRepo) GetAuditLogsAll(_ context.Context, _ map[string]interface{}) ([]models.AuditLog, error) {
	return nil, nil
}
func (m *mockInstRepo) GetSecurityLogs(_ context.Context, _, _ int) ([]models.SecurityLog, int64, error) {
	return nil, 0, nil
}
func (m *mockInstRepo) UpdateSettings(_ context.Context, _ *models.GlobalSetting) error { return nil }
func (m *mockInstRepo) BlacklistIP(_ context.Context, _, _ string) error                { return nil }
func (m *mockInstRepo) IsIPBlacklisted(_ context.Context, _ string) (bool, error)      { return false, nil }
func (m *mockInstRepo) GetBlacklistedIPs(_ context.Context) ([]string, error)          { return nil, nil }
func (m *mockInstRepo) ScheduleRepo() repository.ScheduleRepository                    { return nil }
func (m *mockInstRepo) PurgeScheduleRuns(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}
func (m *mockInstRepo) CreateIncident(_ context.Context, _ *models.IncidentLog) error { return nil }
func (m *mockInstRepo) ResolveActiveIncident(_ context.Context, _, _, _ string, _ time.Time, _ bool) (*models.IncidentLog, error) {
	return nil, nil
}
func (m *mockInstRepo) GetIncidentsByInstitution(_ context.Context, _ string, _ int) ([]models.IncidentLog, error) {
	return nil, nil
}
func (m *mockInstRepo) GetUptimeStats(_ context.Context, _ string, _ int) (*models.UptimeResponse, error) {
	return &models.UptimeResponse{SLAPercentage: 100.0, TotalDays: 30, Days: []models.UptimeDayBucket{}}, nil
}
func (m *mockInstRepo) GetRecentIncidentsCount(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}
func (m *mockInstRepo) PurgeIncidents(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}

func TestCreateAndUpdateInstitutionTelegramFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &mockInstRepo{
		getInst: &models.Institution{
			ID:   "inst-abc",
			Name: "Initial Name",
		},
	}
	registry := ws.NewRegistry()
	h := handlers.NewFrontendHandler(repo, registry, "secret", "captcha")

	r := gin.New()
	r.POST("/institutions", h.CreateInstitution)
	r.PUT("/institutions/:id", h.UpdateInstitution)

	// 1. Test Create
	payload := map[string]interface{}{
		"name":                   "Lembaga Baru",
		"use_custom_telegram":    true,
		"telegram_bot_token":     "tok-123",
		"telegram_chat_id":       "chat-456",
		"telegram_topic_id":      "topic-789",
		"enable_telegram_alerts": true,
		"mirror_to_global":       true,
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", "/institutions", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", w.Code, w.Body.String())
	}
	if repo.createdInst == nil || !repo.createdInst.UseCustomTelegram || repo.createdInst.TelegramBotToken != "tok-123" {
		t.Errorf("expected created institution to have custom telegram fields, got %+v", repo.createdInst)
	}

	// 2. Test Update
	updatePayload := map[string]interface{}{
		"telegram_chat_id": "new-chat-999",
		"mirror_to_global": false,
	}
	bodyUpdate, _ := json.Marshal(updatePayload)
	reqUpdate, _ := http.NewRequest("PUT", "/institutions/inst-abc", bytes.NewBuffer(bodyUpdate))
	reqUpdate.Header.Set("Content-Type", "application/json")
	wUpdate := httptest.NewRecorder()
	r.ServeHTTP(wUpdate, reqUpdate)

	if wUpdate.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", wUpdate.Code, wUpdate.Body.String())
	}
	if repo.updatedInst == nil || repo.updatedInst.TelegramChatID != "new-chat-999" || repo.updatedInst.MirrorToGlobal != false {
		t.Errorf("expected updated institution to have updated telegram fields, got %+v", repo.updatedInst)
	}
}
