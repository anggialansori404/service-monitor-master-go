package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"

	"monitoring-master/internal/models"
	"monitoring-master/internal/repository"
)

// --- Mock ScheduleRepository ---

type mockScheduleRepo struct {
	schedules map[string]*models.Schedule
	runs      map[string][]models.ScheduleRun
	mu        sync.RWMutex
}

func newMockScheduleRepo() *mockScheduleRepo {
	return &mockScheduleRepo{
		schedules: make(map[string]*models.Schedule),
		runs:      make(map[string][]models.ScheduleRun),
	}
}

func (m *mockScheduleRepo) CreateSchedule(_ context.Context, s *models.Schedule) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.schedules[s.ID] = s
	return nil
}

func (m *mockScheduleRepo) GetSchedule(_ context.Context, id string) (*models.Schedule, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.schedules[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return s, nil
}

func (m *mockScheduleRepo) ListSchedules(_ context.Context, institutionID string, enabledOnly bool) ([]models.Schedule, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []models.Schedule
	for _, s := range m.schedules {
		if institutionID != "" && s.InstitutionID != institutionID {
			continue
		}
		if enabledOnly && !s.Enabled {
			continue
		}
		result = append(result, *s)
	}
	return result, nil
}

func (m *mockScheduleRepo) UpdateSchedule(_ context.Context, s *models.Schedule) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.schedules[s.ID] = s
	return nil
}

func (m *mockScheduleRepo) DeleteSchedule(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.schedules[id]; !ok {
		return repository.ErrNotFound
	}
	delete(m.schedules, id)
	return nil
}

func (m *mockScheduleRepo) GetEnabledSchedules(_ context.Context) ([]models.Schedule, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []models.Schedule
	for _, s := range m.schedules {
		if s.Enabled {
			result = append(result, *s)
		}
	}
	return result, nil
}

func (m *mockScheduleRepo) CreateScheduleRun(_ context.Context, run *models.ScheduleRun) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runs[run.ScheduleID] = append(m.runs[run.ScheduleID], *run)
	return nil
}

func (m *mockScheduleRepo) ListScheduleRuns(_ context.Context, scheduleID string, page, limit int) ([]models.ScheduleRun, int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	runs := m.runs[scheduleID]
	total := int64(len(runs))
	start := (page - 1) * limit
	if start >= len(runs) {
		return []models.ScheduleRun{}, total, nil
	}
	end := start + limit
	if end > len(runs) {
		end = len(runs)
	}
	return runs[start:end], total, nil
}

func (m *mockScheduleRepo) UpdateScheduleRunTimes(_ context.Context, _ string, _, _ time.Time) error {
	return nil
}

func (m *mockScheduleRepo) PurgeScheduleRuns(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}

// --- Mock Repository ---

type mockRepo struct {
	scheduleRepo *mockScheduleRepo
}

func newMockRepo() *mockRepo {
	return &mockRepo{
		scheduleRepo: newMockScheduleRepo(),
	}
}

func (m *mockRepo) ScheduleRepo() repository.ScheduleRepository { return m.scheduleRepo }
func (m *mockRepo) GetInstitutionByID(_ string) (*models.Institution, error) {
	return &models.Institution{ID: "inst-1", AgentID: "agent-1"}, nil
}

// Stubs for remaining Repository methods.
func (m *mockRepo) GetInstitutionByAgentID(_ context.Context, _ string) (*models.Institution, error) {
	return nil, repository.ErrNotFound
}
func (m *mockRepo) GetInstitutionByName(_ string) (*models.Institution, error) {
	return nil, repository.ErrNotFound
}
func (m *mockRepo) GetAllInstitutions() ([]models.Institution, error)         { return nil, nil }
func (m *mockRepo) GetDashboardSummary(_ context.Context) (*models.DashboardSummaryResponse, error) {
	return nil, nil
}
func (m *mockRepo) UpdateInstitutionPing(_ context.Context, _ string, _ *models.HardwareInfo, _ time.Time) error {
	return nil
}
func (m *mockRepo) CountInstitutions() (int64, error)                            { return 0, nil }
func (m *mockRepo) CreateInstitution(_ *models.Institution) error                 { return nil }
func (m *mockRepo) UpdateInstitution(_ *models.Institution) error                 { return nil }
func (m *mockRepo) DeleteInstitution(_ string) error                             { return nil }
func (m *mockRepo) GetServicesByInstitution(_ string, _ string, _ string, _, _ int) ([]models.Service, int64, error) {
	return nil, 0, nil
}
func (m *mockRepo) GetService(_ string, _ string) (*models.Service, error)       { return nil, nil }
func (m *mockRepo) UpsertService(_ context.Context, _ *models.Service) error      { return nil }
func (m *mockRepo) BulkUpsertServices(_ context.Context, _ []models.Service) error { return nil }
func (m *mockRepo) UpdateServiceStatus(_ context.Context, _ string, _, _ string, _ time.Time) error {
	return nil
}
func (m *mockRepo) UpdateServiceRestartStatus(_ context.Context, _, _, _ string) error { return nil }
func (m *mockRepo) UpdateServiceAutoRestart(_ context.Context, _, _ string, _ bool) error {
	return nil
}
func (m *mockRepo) CountServicesByStatus(_ string, _ string) (int64, error)       { return 0, nil }
func (m *mockRepo) CountServicesByStatusBulk(_ []string) (map[string]map[string]int64, error) {
	return nil, nil
}
func (m *mockRepo) CreateService(_ *models.Service) error                          { return nil }
func (m *mockRepo) DeleteService(_ context.Context, _, _ string) error             { return nil }
func (m *mockRepo) Ping(_ context.Context) error                                   { return nil }
func (m *mockRepo) CreateAuditLog(_ context.Context, _ *models.AuditLog) error     { return nil }
func (m *mockRepo) CreateSecurityLog(_ context.Context, _ *models.SecurityLog) error {
	return nil
}
func (m *mockRepo) GetInstitutionByAgentIDAndSecret(_ context.Context, _, _ string) (*models.Institution, error) {
	return nil, repository.ErrNotFound
}
func (m *mockRepo) GetUserByEmail(_ string) (*models.User, error)                 { return nil, nil }
func (m *mockRepo) CreateUser(_ *models.User) error                               { return nil }
func (m *mockRepo) GetUserByID(_ context.Context, _ string) (*models.User, error) { return nil, nil }
func (m *mockRepo) UpdateUserPassword(_ context.Context, _ string, _ string) error { return nil }
func (m *mockRepo) GetAuditLogs(_ context.Context, _ map[string]interface{}, _, _ int) ([]models.AuditLog, int64, error) {
	return nil, 0, nil
}
func (m *mockRepo) GetAuditLogsAll(_ context.Context, _ map[string]interface{}) ([]models.AuditLog, error) {
	return nil, nil
}
func (m *mockRepo) GetSecurityLogs(_ context.Context, _, _ int) ([]models.SecurityLog, int64, error) {
	return nil, 0, nil
}
func (m *mockRepo) GetSettings(_ context.Context) (*models.GlobalSetting, error)  { return nil, nil }
func (m *mockRepo) UpdateSettings(_ context.Context, _ *models.GlobalSetting) error { return nil }
func (m *mockRepo) BlacklistIP(_ context.Context, _ string, _ string) error       { return nil }
func (m *mockRepo) IsIPBlacklisted(_ context.Context, _ string) (bool, error)     { return false, nil }
func (m *mockRepo) GetBlacklistedIPs(_ context.Context) ([]string, error)          { return []string{}, nil }
func (m *mockRepo) PurgeAuditLogs(_ context.Context, _ time.Time) (int64, error)   { return 0, nil }
func (m *mockRepo) PurgeSecurityLogs(_ context.Context, _ time.Time) (int64, error) { return 0, nil }
func (m *mockRepo) PurgeScheduleRuns(_ context.Context, _ time.Time) (int64, error) { return 0, nil }
func (m *mockRepo) CreateIncident(_ context.Context, _ *models.IncidentLog) error { return nil }
func (m *mockRepo) GetActiveIncident(_ context.Context, _, _, _ string) (*models.IncidentLog, error) { return nil, nil }
func (m *mockRepo) ResolveActiveIncident(_ context.Context, _, _, _ string, _ time.Time, _ bool) (*models.IncidentLog, error) {
	return nil, nil
}
func (m *mockRepo) GetIncidentsByInstitution(_ context.Context, _ string, _, _ int) ([]models.IncidentLog, int64, error) {
	return []models.IncidentLog{}, 0, nil
}
func (m *mockRepo) GetUptimeStats(_ context.Context, _ string, _ int) (*models.UptimeResponse, error) {
	return &models.UptimeResponse{SLAPercentage: 100.0, TotalDays: 30, Days: []models.UptimeDayBucket{}}, nil
}
func (m *mockRepo) GetRecentIncidentsCount(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}
func (m *mockRepo) GetRecentIncidentsCountByInstitution(_ context.Context, _ string, _ time.Time) (int64, error) {
	return 0, nil
}
func (m *mockRepo) PurgeIncidents(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}

// --- Helpers ---

func seedSchedule(repo *mockRepo, id, name, cron string, enabled bool) *models.Schedule {
	s := &models.Schedule{
		ID:            id,
		InstitutionID: "inst-1",
		Name:          name,
		CronExpr:      cron,
		Timezone:      "Asia/Jakarta",
		ServiceNames:  []string{"nginx"},
		MissedPolicy:  "skip",
		Enabled:       enabled,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	repo.scheduleRepo.schedules[id] = s
	return s
}

func countJobs(s *ServiceScheduler) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.jobs)
}

// --- Tests ---

func TestSchedulerRecovery(t *testing.T) {
	repo := newMockRepo()
	seedSchedule(repo, "sched-1", "Daily Restart", "0 0 * * *", true)
	seedSchedule(repo, "sched-2", "Disabled One", "0 12 * * *", false)

	sched1, err := New(repo, nil)
	if err != nil {
		t.Fatalf("first scheduler New() failed: %v", err)
	}
	defer sched1.Stop()

	if n := countJobs(sched1); n != 1 {
		t.Fatalf("expected 1 job loaded, got %d", n)
	}
	if _, ok := sched1.jobs["sched-1"]; !ok {
		t.Error("expected sched-1 to be registered")
	}
	if _, ok := sched1.jobs["sched-2"]; ok {
		t.Error("sched-2 should NOT be registered (disabled)")
	}

	// Simulate crash: stop first scheduler.
	sched1.Stop()

	// Simulate restart: create new scheduler with same repo (DB state unchanged).
	sched2, err := New(repo, nil)
	if err != nil {
		t.Fatalf("second scheduler New() failed: %v", err)
	}
	defer sched2.Stop()

	if n := countJobs(sched2); n != 1 {
		t.Fatalf("after recovery expected 1 job, got %d", n)
	}
	if _, ok := sched2.jobs["sched-1"]; !ok {
		t.Error("sched-1 should be recovered after restart")
	}
}

func TestSchedulerDisabledSchedule(t *testing.T) {
	repo := newMockRepo()
	seedSchedule(repo, "enabled-1", "Active", "0 0 * * *", true)
	seedSchedule(repo, "disabled-1", "Inactive", "0 6 * * *", false)
	seedSchedule(repo, "disabled-2", "Also Inactive", "0 12 * * *", false)

	sched, err := New(repo, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer sched.Stop()

	if n := countJobs(sched); n != 1 {
		t.Fatalf("expected 1 enabled job loaded, got %d", n)
	}
	if _, ok := sched.jobs["enabled-1"]; !ok {
		t.Error("enabled-1 should be registered")
	}
	if _, ok := sched.jobs["disabled-1"]; ok {
		t.Error("disabled-1 should NOT be registered")
	}
	if _, ok := sched.jobs["disabled-2"]; ok {
		t.Error("disabled-2 should NOT be registered")
	}
}

func TestSchedulerDeletedSchedule(t *testing.T) {
	repo := newMockRepo()
	seedSchedule(repo, "sched-del", "ToDelete", "0 0 * * *", true)

	sched, err := New(repo, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer sched.Stop()

	if n := countJobs(sched); n != 1 {
		t.Fatalf("expected 1 job, got %d", n)
	}

	// Simulate schedule deleted from DB between restarts.
	repo.scheduleRepo.mu.Lock()
	delete(repo.scheduleRepo.schedules, "sched-del")
	repo.scheduleRepo.mu.Unlock()

	// Reload — should not crash even though schedule disappeared from DB.
	if err := sched.LoadSchedules(); err != nil {
		t.Fatalf("LoadSchedules() after delete should not error, got: %v", err)
	}
}

func TestNextRunCalculation(t *testing.T) {
	repo := newMockRepo()
	s := seedSchedule(repo, "sched-test", "2 AM WITA Test", "0 2 * * *", true)
	s.Timezone = "WITA" // test abbreviation alias

	sched, err := New(repo, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer sched.Stop()

	job, ok := sched.jobs["sched-test"]
	if !ok {
		t.Fatal("expected sched-test to be in jobs map")
	}

	nextRun, err := job.NextRun()
	if err != nil {
		t.Fatalf("NextRun error: %v", err)
	}

	loc, err := ParseLocation("WITA")
	if err != nil {
		t.Fatalf("ParseLocation(WITA) failed: %v", err)
	}

	nextInLoc := nextRun.In(loc)
	if nextInLoc.Hour() != 2 || nextInLoc.Minute() != 0 {
		t.Errorf("expected next run to be at 02:00 in WITA, got %s", nextInLoc.Format("15:04:05 MST"))
	}
}

func TestParseLocationAliases(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"WIB", "Asia/Jakarta"},
		{"Asia/Jakarta", "Asia/Jakarta"},
		{"WITA", "Asia/Makassar"},
		{"Asia/Makassar", "Asia/Makassar"},
		{"WIT", "Asia/Jayapura"},
		{"Asia/Jayapura", "Asia/Jayapura"},
		{"UTC", "UTC"},
		{"GMT", "UTC"},
		{"", "Asia/Jakarta"},
	}

	for _, tc := range cases {
		loc, err := ParseLocation(tc.input)
		if err != nil {
			t.Errorf("ParseLocation(%q) returned error: %v", tc.input, err)
			continue
		}
		if loc.String() != tc.expected {
			t.Errorf("ParseLocation(%q) = %s, expected %s", tc.input, loc.String(), tc.expected)
		}
	}
}
