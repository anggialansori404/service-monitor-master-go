package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"monitoring-master/internal/models"
	"monitoring-master/internal/repository"

	"github.com/gin-gonic/gin"
)

type mockBlacklistRepo struct {
	mu          sync.RWMutex
	blacklisted map[string]string
}

func newMockBlacklistRepo() *mockBlacklistRepo {
	return &mockBlacklistRepo{
		blacklisted: make(map[string]string),
	}
}

func (m *mockBlacklistRepo) BlacklistIP(_ context.Context, ip, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blacklisted[ip] = reason
	return nil
}

func (m *mockBlacklistRepo) IsIPBlacklisted(_ context.Context, ip string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.blacklisted[ip]
	return ok, nil
}

func (m *mockBlacklistRepo) GetBlacklistedIPs(_ context.Context) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var ips []string
	for ip := range m.blacklisted {
		ips = append(ips, ip)
	}
	return ips, nil
}

func (m *mockBlacklistRepo) CreateSecurityLog(_ context.Context, _ *models.SecurityLog) error {
	return nil
}

// Dummy stubs to satisfy repository.Repository interface if needed
type fullMockRepo struct {
	*mockBlacklistRepo
}

func (f *fullMockRepo) GetInstitutionByID(_ string) (*models.Institution, error) { return nil, nil }
func (f *fullMockRepo) GetInstitutionByAgentID(_ context.Context, _ string) (*models.Institution, error) {
	return nil, nil
}
func (f *fullMockRepo) GetInstitutionByName(_ string) (*models.Institution, error) { return nil, nil }
func (f *fullMockRepo) GetAllInstitutions() ([]models.Institution, error)         { return nil, nil }
func (f *fullMockRepo) GetDashboardSummary(_ context.Context) (*models.DashboardSummaryResponse, error) {
	return nil, nil
}
func (f *fullMockRepo) UpdateInstitutionPing(_ context.Context, _ string, _ *models.HardwareInfo, _ time.Time) error {
	return nil
}
func (f *fullMockRepo) CountInstitutions() (int64, error)                            { return 0, nil }
func (f *fullMockRepo) CreateInstitution(_ *models.Institution) error                 { return nil }
func (f *fullMockRepo) UpdateInstitution(_ *models.Institution) error                 { return nil }
func (f *fullMockRepo) DeleteInstitution(_ string) error                             { return nil }
func (f *fullMockRepo) GetServicesByInstitution(_ string, _ string, _ string, _, _ int) ([]models.Service, int64, error) {
	return nil, 0, nil
}
func (f *fullMockRepo) GetService(_ string, _ string) (*models.Service, error)       { return nil, nil }
func (f *fullMockRepo) UpsertService(_ context.Context, _ *models.Service) error      { return nil }
func (f *fullMockRepo) BulkUpsertServices(_ context.Context, _ []models.Service) error { return nil }
func (f *fullMockRepo) UpdateServiceStatus(_ context.Context, _ string, _, _ string, _ time.Time) error {
	return nil
}
func (f *fullMockRepo) UpdateServiceStatusWithDisplayName(_ context.Context, _ string, _, _, _ string, _ time.Time) error {
	return nil
}
func (f *fullMockRepo) UpdateServiceRestartStatus(_ context.Context, _, _, _ string) error { return nil }
func (f *fullMockRepo) UpdateServiceAutoRestart(_ context.Context, _, _ string, _ bool) error {
	return nil
}
func (f *fullMockRepo) CountServicesByStatus(_ string, _ string) (int64, error)       { return 0, nil }
func (f *fullMockRepo) CountServicesByStatusBulk(_ []string) (map[string]map[string]int64, error) {
	return nil, nil
}
func (f *fullMockRepo) CreateService(_ *models.Service) error                          { return nil }
func (f *fullMockRepo) DeleteService(_ context.Context, _, _ string) error             { return nil }
func (f *fullMockRepo) Ping(_ context.Context) error                                   { return nil }
func (f *fullMockRepo) CreateAuditLog(_ context.Context, _ *models.AuditLog) error     { return nil }
func (f *fullMockRepo) GetInstitutionByAgentIDAndSecret(_ context.Context, _, _ string) (*models.Institution, error) {
	return nil, nil
}
func (f *fullMockRepo) GetUserByEmail(_ string) (*models.User, error)                 { return nil, nil }
func (f *fullMockRepo) CreateUser(_ *models.User) error                               { return nil }
func (f *fullMockRepo) GetUserByID(_ context.Context, _ string) (*models.User, error) { return nil, nil }
func (f *fullMockRepo) UpdateUserPassword(_ context.Context, _ string, _ string) error { return nil }
func (f *fullMockRepo) GetAuditLogs(_ context.Context, _ map[string]interface{}, _, _ int) ([]models.AuditLog, int64, error) {
	return nil, 0, nil
}
func (f *fullMockRepo) GetAuditLogsAll(_ context.Context, _ map[string]interface{}) ([]models.AuditLog, error) {
	return nil, nil
}
func (f *fullMockRepo) GetSecurityLogs(_ context.Context, _, _ int) ([]models.SecurityLog, int64, error) {
	return nil, 0, nil
}
func (f *fullMockRepo) PurgeAuditLogs(_ context.Context, _ time.Time) (int64, error)   { return 0, nil }
func (f *fullMockRepo) PurgeSecurityLogs(_ context.Context, _ time.Time) (int64, error) { return 0, nil }
func (f *fullMockRepo) PurgeScheduleRuns(_ context.Context, _ time.Time) (int64, error) { return 0, nil }
func (f *fullMockRepo) GetSettings(_ context.Context) (*models.GlobalSetting, error)  { return nil, nil }
func (f *fullMockRepo) UpdateSettings(_ context.Context, _ *models.GlobalSetting) error { return nil }
func (f *fullMockRepo) ScheduleRepo() repository.ScheduleRepository                  { return nil }
func (f *fullMockRepo) CreateIncident(_ context.Context, _ *models.IncidentLog) error { return nil }
func (f *fullMockRepo) GetActiveIncident(_ context.Context, _, _, _ string) (*models.IncidentLog, error) { return nil, nil }
func (f *fullMockRepo) ResolveActiveIncident(_ context.Context, _, _, _ string, _ time.Time, _ bool) (*models.IncidentLog, error) {
	return nil, nil
}
func (f *fullMockRepo) GetIncidentsByInstitution(_ context.Context, _ string, _, _ int) ([]models.IncidentLog, int64, error) {
	return nil, 0, nil
}
func (f *fullMockRepo) GetUptimeStats(_ context.Context, _ string, _ int) (*models.UptimeResponse, error) {
	return &models.UptimeResponse{SLAPercentage: 100.0, TotalDays: 30, Days: []models.UptimeDayBucket{}}, nil
}
func (f *fullMockRepo) GetRecentIncidentsCount(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}
func (f *fullMockRepo) GetRecentIncidentsCountByInstitution(_ context.Context, _ string, _ time.Time) (int64, error) {
	return 0, nil
}
func (f *fullMockRepo) PurgeIncidents(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}

func TestBlacklistManager(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mock := newMockBlacklistRepo()
	mock.blacklisted["192.168.1.100"] = "Initial bad IP"

	repo := &fullMockRepo{mockBlacklistRepo: mock}
	bm := NewBlacklistManager(repo)
	defer bm.Stop()

	// Initial load should contain the preloaded IP
	if !bm.IsBlacklisted("192.168.1.100") {
		t.Error("expected 192.168.1.100 to be blacklisted from initial load")
	}
	if bm.IsBlacklisted("192.168.1.200") {
		t.Error("did not expect 192.168.1.200 to be blacklisted initially")
	}

	// Dynamically blacklist a new IP
	bm.Blacklist("192.168.1.200", "Spam attempt")
	if !bm.IsBlacklisted("192.168.1.200") {
		t.Error("expected 192.168.1.200 to be blacklisted in-memory immediately")
	}

	// Test Gin Middleware
	r := gin.New()
	r.Use(IPBlacklist(bm))
	r.Use(SuspiciousPathLogger(bm, repo))
	r.GET("/api/test", func(c *gin.Context) {
		c.String(http.StatusOK, "OK")
	})

	// 1. Request from clean IP should succeed
	req, _ := http.NewRequest(http.MethodGet, "/api/test", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 OK for clean IP, got %d", w.Code)
	}

	// 2. Request hitting honeypot path (.env) should trigger blacklist & return 403
	req2, _ := http.NewRequest(http.MethodGet, "/.env", nil)
	req2.RemoteAddr = "10.0.0.2:12345"
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for suspicious path, got %d", w2.Code)
	}

	// 3. Now subsequent request from 10.0.0.2 to clean path should be blocked by IPBlacklist
	req3, _ := http.NewRequest(http.MethodGet, "/api/test", nil)
	req3.RemoteAddr = "10.0.0.2:12345"
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for banned IP, got %d", w3.Code)
	}

	// 4. Localhost should never be blocked
	req4, _ := http.NewRequest(http.MethodGet, "/api/test", nil)
	req4.RemoteAddr = "127.0.0.1:12345"
	w4 := httptest.NewRecorder()
	r.ServeHTTP(w4, req4)
	if w4.Code != http.StatusOK {
		t.Errorf("expected 200 OK for localhost, got %d", w4.Code)
	}
}
