package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"monitoring-master/internal/models"
	"monitoring-master/internal/repository"

	"github.com/gin-gonic/gin"
)

type mockSettingRepo struct {
	auditPurged    int64
	securityPurged int64
	schedPurged    int64
}

func (m *mockSettingRepo) PurgeAuditLogs(_ context.Context, _ time.Time) (int64, error) {
	return m.auditPurged, nil
}

func (m *mockSettingRepo) PurgeSecurityLogs(_ context.Context, _ time.Time) (int64, error) {
	return m.securityPurged, nil
}

func (m *mockSettingRepo) PurgeScheduleRuns(_ context.Context, _ time.Time) (int64, error) {
	return m.schedPurged, nil
}

// Dummy stubs
func (m *mockSettingRepo) GetInstitutionByID(_ string) (*models.Institution, error) { return nil, nil }
func (m *mockSettingRepo) GetInstitutionByAgentID(_ context.Context, _ string) (*models.Institution, error) {
	return nil, nil
}
func (m *mockSettingRepo) GetInstitutionByName(_ string) (*models.Institution, error) { return nil, nil }
func (m *mockSettingRepo) GetAllInstitutions() ([]models.Institution, error)         { return nil, nil }
func (m *mockSettingRepo) GetDashboardSummary(_ context.Context) (*models.DashboardSummaryResponse, error) {
	return nil, nil
}
func (m *mockSettingRepo) UpdateInstitutionPing(_ context.Context, _ string, _ *models.HardwareInfo, _ time.Time) error {
	return nil
}
func (m *mockSettingRepo) CountInstitutions() (int64, error)                            { return 0, nil }
func (m *mockSettingRepo) CreateInstitution(_ *models.Institution) error                 { return nil }
func (m *mockSettingRepo) UpdateInstitution(_ *models.Institution) error                 { return nil }
func (m *mockSettingRepo) DeleteInstitution(_ string) error                             { return nil }
func (m *mockSettingRepo) GetServicesByInstitution(_ string, _ string, _ string, _, _ int) ([]models.Service, int64, error) {
	return nil, 0, nil
}
func (m *mockSettingRepo) GetService(_ string, _ string) (*models.Service, error)       { return nil, nil }
func (m *mockSettingRepo) UpsertService(_ context.Context, _ *models.Service) error      { return nil }
func (m *mockSettingRepo) BulkUpsertServices(_ context.Context, _ []models.Service) error { return nil }
func (m *mockSettingRepo) UpdateServiceStatus(_ context.Context, _ string, _, _ string, _ time.Time) error {
	return nil
}
func (m *mockSettingRepo) UpdateServiceRestartStatus(_ context.Context, _, _, _ string) error {
	return nil
}
func (m *mockSettingRepo) UpdateServiceAutoRestart(_ context.Context, _, _ string, _ bool) error {
	return nil
}
func (m *mockSettingRepo) CountServicesByStatus(_ string, _ string) (int64, error)       { return 0, nil }
func (m *mockSettingRepo) CountServicesByStatusBulk(_ []string) (map[string]map[string]int64, error) {
	return nil, nil
}
func (m *mockSettingRepo) CreateService(_ *models.Service) error                          { return nil }
func (m *mockSettingRepo) DeleteService(_ context.Context, _, _ string) error             { return nil }
func (m *mockSettingRepo) Ping(_ context.Context) error                                   { return nil }
func (m *mockSettingRepo) CreateAuditLog(_ context.Context, _ *models.AuditLog) error     { return nil }
func (m *mockSettingRepo) CreateSecurityLog(_ context.Context, _ *models.SecurityLog) error {
	return nil
}
func (m *mockSettingRepo) GetInstitutionByAgentIDAndSecret(_ context.Context, _, _ string) (*models.Institution, error) {
	return nil, nil
}
func (m *mockSettingRepo) GetUserByEmail(_ string) (*models.User, error)                 { return nil, nil }
func (m *mockSettingRepo) CreateUser(_ *models.User) error                               { return nil }
func (m *mockSettingRepo) GetUserByID(_ context.Context, _ string) (*models.User, error) { return nil, nil }
func (m *mockSettingRepo) UpdateUserPassword(_ context.Context, _ string, _ string) error {
	return nil
}
func (m *mockSettingRepo) GetAuditLogs(_ context.Context, _ map[string]interface{}, _, _ int) ([]models.AuditLog, int64, error) {
	return nil, 0, nil
}
func (m *mockSettingRepo) GetAuditLogsAll(_ context.Context, _ map[string]interface{}) ([]models.AuditLog, error) {
	return nil, nil
}
func (m *mockSettingRepo) GetSecurityLogs(_ context.Context, _, _ int) ([]models.SecurityLog, int64, error) {
	return nil, 0, nil
}
func (m *mockSettingRepo) GetSettings(_ context.Context) (*models.GlobalSetting, error)  { return nil, nil }
func (m *mockSettingRepo) UpdateSettings(_ context.Context, _ *models.GlobalSetting) error { return nil }
func (m *mockSettingRepo) BlacklistIP(_ context.Context, _ string, _ string) error       { return nil }
func (m *mockSettingRepo) IsIPBlacklisted(_ context.Context, _ string) (bool, error)     { return false, nil }
func (m *mockSettingRepo) GetBlacklistedIPs(_ context.Context) ([]string, error)          { return nil, nil }
func (m *mockSettingRepo) ScheduleRepo() repository.ScheduleRepository                  { return nil }

func TestSettingHandler_PurgeLogs(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockRepo := &mockSettingRepo{
		auditPurged:    25,
		securityPurged: 10,
		schedPurged:    5,
	}

	h := NewSettingHandler(mockRepo)
	r := gin.New()
	r.POST("/api/v1/settings/purge-logs", h.PurgeLogs)

	reqBody, _ := json.Marshal(PurgeLogsRequest{
		LogType: "all",
		Days:    30,
	})

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/settings/purge-logs", bytes.NewBuffer(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json unmarshal error: %v", err)
	}

	if int(resp["total_deleted"].(float64)) != 40 {
		t.Errorf("expected total_deleted = 40, got %v", resp["total_deleted"])
	}
}
