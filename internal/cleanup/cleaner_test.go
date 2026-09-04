package cleanup

import (
	"context"
	"testing"
	"time"

	"monitoring-master/internal/config"
	"monitoring-master/internal/models"
	"monitoring-master/internal/repository"
)

type mockCleanupRepo struct {
	auditCount    int64
	securityCount int64
	schedRunCount int64
}

func (m *mockCleanupRepo) PurgeAuditLogs(_ context.Context, _ time.Time) (int64, error) {
	c := m.auditCount
	m.auditCount = 0
	return c, nil
}

func (m *mockCleanupRepo) PurgeSecurityLogs(_ context.Context, _ time.Time) (int64, error) {
	c := m.securityCount
	m.securityCount = 0
	return c, nil
}

func (m *mockCleanupRepo) PurgeScheduleRuns(_ context.Context, _ time.Time) (int64, error) {
	c := m.schedRunCount
	m.schedRunCount = 0
	return c, nil
}

// Full interface stubs
func (m *mockCleanupRepo) GetInstitutionByID(_ string) (*models.Institution, error) { return nil, nil }
func (m *mockCleanupRepo) GetInstitutionByAgentID(_ context.Context, _ string) (*models.Institution, error) {
	return nil, nil
}
func (m *mockCleanupRepo) GetInstitutionByName(_ string) (*models.Institution, error) { return nil, nil }
func (m *mockCleanupRepo) GetAllInstitutions() ([]models.Institution, error)         { return nil, nil }
func (m *mockCleanupRepo) GetDashboardSummary(_ context.Context) (*models.DashboardSummaryResponse, error) {
	return nil, nil
}
func (m *mockCleanupRepo) UpdateInstitutionPing(_ context.Context, _ string, _ *models.HardwareInfo, _ time.Time) error {
	return nil
}
func (m *mockCleanupRepo) CountInstitutions() (int64, error)                            { return 0, nil }
func (m *mockCleanupRepo) CreateInstitution(_ *models.Institution) error                 { return nil }
func (m *mockCleanupRepo) UpdateInstitution(_ *models.Institution) error                 { return nil }
func (m *mockCleanupRepo) DeleteInstitution(_ string) error                             { return nil }
func (m *mockCleanupRepo) GetServicesByInstitution(_ string, _ string, _ string, _, _ int) ([]models.Service, int64, error) {
	return nil, 0, nil
}
func (m *mockCleanupRepo) GetService(_ string, _ string) (*models.Service, error)       { return nil, nil }
func (m *mockCleanupRepo) UpsertService(_ context.Context, _ *models.Service) error      { return nil }
func (m *mockCleanupRepo) BulkUpsertServices(_ context.Context, _ []models.Service) error { return nil }
func (m *mockCleanupRepo) UpdateServiceStatus(_ context.Context, _ string, _, _ string, _ time.Time) error {
	return nil
}
func (m *mockCleanupRepo) UpdateServiceRestartStatus(_ context.Context, _, _, _ string) error {
	return nil
}
func (m *mockCleanupRepo) UpdateServiceAutoRestart(_ context.Context, _, _ string, _ bool) error {
	return nil
}
func (m *mockCleanupRepo) CountServicesByStatus(_ string, _ string) (int64, error)       { return 0, nil }
func (m *mockCleanupRepo) CountServicesByStatusBulk(_ []string) (map[string]map[string]int64, error) {
	return nil, nil
}
func (m *mockCleanupRepo) CreateService(_ *models.Service) error                          { return nil }
func (m *mockCleanupRepo) DeleteService(_ context.Context, _, _ string) error             { return nil }
func (m *mockCleanupRepo) Ping(_ context.Context) error                                   { return nil }
func (m *mockCleanupRepo) CreateAuditLog(_ context.Context, _ *models.AuditLog) error     { return nil }
func (m *mockCleanupRepo) CreateSecurityLog(_ context.Context, _ *models.SecurityLog) error {
	return nil
}
func (m *mockCleanupRepo) GetInstitutionByAgentIDAndSecret(_ context.Context, _, _ string) (*models.Institution, error) {
	return nil, nil
}
func (m *mockCleanupRepo) GetUserByEmail(_ string) (*models.User, error)                 { return nil, nil }
func (m *mockCleanupRepo) CreateUser(_ *models.User) error                               { return nil }
func (m *mockCleanupRepo) GetUserByID(_ context.Context, _ string) (*models.User, error) { return nil, nil }
func (m *mockCleanupRepo) UpdateUserPassword(_ context.Context, _ string, _ string) error {
	return nil
}
func (m *mockCleanupRepo) GetAuditLogs(_ context.Context, _ map[string]interface{}, _, _ int) ([]models.AuditLog, int64, error) {
	return nil, 0, nil
}
func (m *mockCleanupRepo) GetAuditLogsAll(_ context.Context, _ map[string]interface{}) ([]models.AuditLog, error) {
	return nil, nil
}
func (m *mockCleanupRepo) GetSecurityLogs(_ context.Context, _, _ int) ([]models.SecurityLog, int64, error) {
	return nil, 0, nil
}
func (m *mockCleanupRepo) GetSettings(_ context.Context) (*models.GlobalSetting, error)  { return nil, nil }
func (m *mockCleanupRepo) UpdateSettings(_ context.Context, _ *models.GlobalSetting) error { return nil }
func (m *mockCleanupRepo) BlacklistIP(_ context.Context, _ string, _ string) error       { return nil }
func (m *mockCleanupRepo) IsIPBlacklisted(_ context.Context, _ string) (bool, error)     { return false, nil }
func (m *mockCleanupRepo) GetBlacklistedIPs(_ context.Context) ([]string, error)          { return nil, nil }
func (m *mockCleanupRepo) ScheduleRepo() repository.ScheduleRepository                  { return nil }
func (m *mockCleanupRepo) CreateIncident(_ context.Context, _ *models.IncidentLog) error { return nil }
func (m *mockCleanupRepo) GetActiveIncident(_ context.Context, _, _, _ string) (*models.IncidentLog, error) { return nil, nil }
func (m *mockCleanupRepo) ResolveActiveIncident(_ context.Context, _, _, _ string, _ time.Time, _ bool) (*models.IncidentLog, error) {
	return nil, nil
}
func (m *mockCleanupRepo) GetIncidentsByInstitution(_ context.Context, _ string, _, _ int) ([]models.IncidentLog, int64, error) {
	return nil, 0, nil
}
func (m *mockCleanupRepo) GetUptimeStats(_ context.Context, _ string, _ int) (*models.UptimeResponse, error) {
	return nil, nil
}
func (m *mockCleanupRepo) GetRecentIncidentsCount(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}
func (m *mockCleanupRepo) PurgeIncidents(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}

func TestExecutePurge(t *testing.T) {
	repo := &mockCleanupRepo{
		auditCount:    50,
		securityCount: 20,
		schedRunCount: 10,
	}

	// Purge only audit
	res, err := ExecutePurge(context.Background(), repo, PurgeOptions{LogType: "audit", Days: 30})
	if err != nil {
		t.Fatalf("ExecutePurge audit error: %v", err)
	}
	if res.DeletedAudit != 50 || res.TotalDeleted != 50 {
		t.Errorf("expected 50 deleted audit logs, got %v", res)
	}

	// Purge all
	repo.auditCount = 100
	repo.securityCount = 40
	repo.schedRunCount = 25
	resAll, err := ExecutePurge(context.Background(), repo, PurgeOptions{LogType: "all", Days: 14})
	if err != nil {
		t.Fatalf("ExecutePurge all error: %v", err)
	}
	if resAll.TotalDeleted != 165 {
		t.Errorf("expected 165 total deleted, got %d", resAll.TotalDeleted)
	}
}

func TestRunDailyCleanup(t *testing.T) {
	repo := &mockCleanupRepo{
		auditCount:    30,
		securityCount: 15,
		schedRunCount: 5,
	}

	cfg := &config.Config{
		AuditLogRetentionDays:    30,
		SecurityLogRetentionDays: 30,
		ScheduleRunRetentionDays: 30,
	}

	RunDailyCleanup(context.Background(), repo, cfg)

	if repo.auditCount != 0 || repo.securityCount != 0 || repo.schedRunCount != 0 {
		t.Error("expected all counts to be purged to 0")
	}
}
