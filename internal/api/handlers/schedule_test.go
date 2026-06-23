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
	"monitoring-master/internal/scheduler"
	"monitoring-master/internal/ws"

	"github.com/gin-gonic/gin"
)

// --- Mock ScheduleRepository ---

type mockScheduleRepo struct {
	schedules map[string]*models.Schedule
	runs      map[string][]models.ScheduleRun
}

func newMockScheduleRepo() *mockScheduleRepo {
	return &mockScheduleRepo{
		schedules: make(map[string]*models.Schedule),
		runs:      make(map[string][]models.ScheduleRun),
	}
}

func (m *mockScheduleRepo) CreateSchedule(_ context.Context, s *models.Schedule) error {
	m.schedules[s.ID] = s
	return nil
}

func (m *mockScheduleRepo) GetSchedule(_ context.Context, id string) (*models.Schedule, error) {
	s, ok := m.schedules[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return s, nil
}

func (m *mockScheduleRepo) ListSchedules(_ context.Context, institutionID string, enabledOnly bool) ([]models.Schedule, error) {
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
	m.schedules[s.ID] = s
	return nil
}

func (m *mockScheduleRepo) DeleteSchedule(_ context.Context, id string) error {
	if _, ok := m.schedules[id]; !ok {
		return repository.ErrNotFound
	}
	delete(m.schedules, id)
	return nil
}

func (m *mockScheduleRepo) GetEnabledSchedules(_ context.Context) ([]models.Schedule, error) {
	var result []models.Schedule
	for _, s := range m.schedules {
		if s.Enabled {
			result = append(result, *s)
		}
	}
	return result, nil
}

func (m *mockScheduleRepo) CreateScheduleRun(_ context.Context, run *models.ScheduleRun) error {
	m.runs[run.ScheduleID] = append(m.runs[run.ScheduleID], *run)
	return nil
}

func (m *mockScheduleRepo) ListScheduleRuns(_ context.Context, scheduleID string, page, limit int) ([]models.ScheduleRun, int64, error) {
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

// --- Mock Repository ---

type mockRepo struct {
	scheduleRepo *mockScheduleRepo
	institutions map[string]*models.Institution
}

func newMockRepo() *mockRepo {
	return &mockRepo{
		scheduleRepo: newMockScheduleRepo(),
		institutions: make(map[string]*models.Institution),
	}
}

func (m *mockRepo) ScheduleRepo() repository.ScheduleRepository { return m.scheduleRepo }
func (m *mockRepo) GetInstitutionByID(id string) (*models.Institution, error) {
	inst, ok := m.institutions[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return inst, nil
}

// Stub implementations for other Repository methods (unused in schedule tests).
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
func (m *mockRepo) GetSettings(_ context.Context) (*models.GlobalSetting, error)  { return nil, nil }
func (m *mockRepo) UpdateSettings(_ context.Context, _ *models.GlobalSetting) error { return nil }
func (m *mockRepo) BlacklistIP(_ context.Context, _ string, _ string) error       { return nil }
func (m *mockRepo) IsIPBlacklisted(_ context.Context, _ string) (bool, error)     { return false, nil }

// --- Mock Scheduler ---

type mockScheduler struct {
	*scheduler.ServiceScheduler
}

// We only need a no-op scheduler for most tests. Since ServiceScheduler
// requires gocron internals, we wrap a nil-safe approach.
func newNoOpHandler(repo *mockRepo, registry *ws.Registry) *ScheduleHandler {
	// Create a real scheduler but don't start it — AddSchedule/RemoveSchedule
	// will be called but we accept any error silently.
	sched, _ := scheduler.New(repo, registry)
	return &ScheduleHandler{
		Repo:      repo,
		Registry:  registry,
		Scheduler: sched,
	}
}

// --- Helpers ---

func setupRouter(handler *ScheduleHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/schedules", handler.CreateSchedule)
	r.GET("/schedules", handler.ListSchedules)
	r.GET("/schedules/:id", handler.GetSchedule)
	r.PATCH("/schedules/:id", handler.UpdateSchedule)
	r.DELETE("/schedules/:id", handler.DeleteSchedule)
	r.POST("/schedules/:id/enable", handler.EnableSchedule)
	r.POST("/schedules/:id/disable", handler.DisableSchedule)
	r.POST("/schedules/:id/trigger", handler.TriggerSchedule)
	return r
}

func seedSchedule(repo *mockRepo, id, institutionID, name, cron string, enabled bool) *models.Schedule {
	s := &models.Schedule{
		ID:            id,
		InstitutionID: institutionID,
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

// --- Tests ---

func TestCreateSchedule_Valid(t *testing.T) {
	repo := newMockRepo()
	repo.institutions["inst-1"] = &models.Institution{ID: "inst-1", Name: "Test", AgentID: "agent-1"}
	handler := newNoOpHandler(repo, nil)
	router := setupRouter(handler)

	body := createScheduleReq{
		InstitutionID: "inst-1",
		Name:          "Daily Restart",
		CronExpr:      "0 0 * * *",
		Timezone:      "Asia/Jakarta",
		ServiceNames:  []string{"nginx", "redis"},
		MissedPolicy:  "skip",
	}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/schedules", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp models.Schedule
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if resp.Name != "Daily Restart" {
		t.Errorf("expected name 'Daily Restart', got %q", resp.Name)
	}
	if resp.ID == "" {
		t.Error("expected non-empty ID")
	}
}

func TestCreateSchedule_InvalidCron(t *testing.T) {
	repo := newMockRepo()
	repo.institutions["inst-1"] = &models.Institution{ID: "inst-1"}
	handler := newNoOpHandler(repo, nil)
	router := setupRouter(handler)

	body := createScheduleReq{
		InstitutionID: "inst-1",
		Name:          "Bad Cron",
		CronExpr:      "not-a-cron",
		ServiceNames:  []string{"nginx"},
	}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/schedules", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateSchedule_InvalidTimezone(t *testing.T) {
	repo := newMockRepo()
	repo.institutions["inst-1"] = &models.Institution{ID: "inst-1"}
	handler := newNoOpHandler(repo, nil)
	router := setupRouter(handler)

	body := createScheduleReq{
		InstitutionID: "inst-1",
		Name:          "Bad TZ",
		CronExpr:      "0 0 * * *",
		Timezone:      "Mars/Colony",
		ServiceNames:  []string{"nginx"},
	}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/schedules", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateSchedule_EmptyServices(t *testing.T) {
	repo := newMockRepo()
	repo.institutions["inst-1"] = &models.Institution{ID: "inst-1"}
	handler := newNoOpHandler(repo, nil)
	router := setupRouter(handler)

	body := createScheduleReq{
		InstitutionID: "inst-1",
		Name:          "No Services",
		CronExpr:      "0 0 * * *",
		ServiceNames:  []string{},
	}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/schedules", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetSchedule_Exists(t *testing.T) {
	repo := newMockRepo()
	seedSchedule(repo, "sched-1", "inst-1", "Morning Restart", "0 6 * * *", true)
	handler := newNoOpHandler(repo, nil)
	router := setupRouter(handler)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/schedules/sched-1", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp models.Schedule
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if resp.Name != "Morning Restart" {
		t.Errorf("expected name 'Morning Restart', got %q", resp.Name)
	}
}

func TestGetSchedule_NotFound(t *testing.T) {
	repo := newMockRepo()
	handler := newNoOpHandler(repo, nil)
	router := setupRouter(handler)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/schedules/nonexistent", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListSchedules_FilterByInstitution(t *testing.T) {
	repo := newMockRepo()
	seedSchedule(repo, "s1", "inst-1", "S1", "* * * * *", true)
	seedSchedule(repo, "s2", "inst-2", "S2", "* * * * *", true)
	seedSchedule(repo, "s3", "inst-1", "S3", "* * * * *", false)
	handler := newNoOpHandler(repo, nil)
	router := setupRouter(handler)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/schedules?institution_id=inst-1", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp []models.Schedule
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if len(resp) != 2 {
		t.Errorf("expected 2 schedules for inst-1, got %d", len(resp))
	}
}

func TestUpdateSchedule_PartialFields(t *testing.T) {
	repo := newMockRepo()
	seedSchedule(repo, "sched-1", "inst-1", "Old Name", "0 0 * * *", true)
	handler := newNoOpHandler(repo, nil)
	router := setupRouter(handler)

	newName := "New Name"
	body := updateScheduleReq{Name: &newName}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PATCH", "/schedules/sched-1", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp models.Schedule
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if resp.Name != "New Name" {
		t.Errorf("expected name 'New Name', got %q", resp.Name)
	}
	if resp.CronExpr != "0 0 * * *" {
		t.Errorf("cron should be unchanged, got %q", resp.CronExpr)
	}
}

func TestDeleteSchedule_Success(t *testing.T) {
	repo := newMockRepo()
	seedSchedule(repo, "sched-1", "inst-1", "ToDelete", "* * * * *", true)
	handler := newNoOpHandler(repo, nil)
	router := setupRouter(handler)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("DELETE", "/schedules/sched-1", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Verify deleted.
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", "/schedules/sched-1", nil)
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusNotFound {
		t.Errorf("expected 404 after delete, got %d", w2.Code)
	}
}

func TestEnableSchedule_Toggle(t *testing.T) {
	repo := newMockRepo()
	seedSchedule(repo, "sched-1", "inst-1", "ToggleMe", "* * * * *", false)
	handler := newNoOpHandler(repo, nil)
	router := setupRouter(handler)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/schedules/sched-1/enable", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp models.Schedule
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if !resp.Enabled {
		t.Error("expected enabled=true after enable")
	}
}

func TestDisableSchedule_Toggle(t *testing.T) {
	repo := newMockRepo()
	seedSchedule(repo, "sched-1", "inst-1", "ToggleMe", "* * * * *", true)
	handler := newNoOpHandler(repo, nil)
	router := setupRouter(handler)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/schedules/sched-1/disable", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp models.Schedule
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if resp.Enabled {
		t.Error("expected enabled=false after disable")
	}
}

func TestTriggerSchedule_AgentOffline(t *testing.T) {
	repo := newMockRepo()
	repo.institutions["inst-1"] = &models.Institution{ID: "inst-1", AgentID: "agent-1"}
	seedSchedule(repo, "sched-1", "inst-1", "Trigger", "* * * * *", true)

	// nil registry → TriggerSchedule will panic on h.Registry.PendingRequests.
	// Instead, use an empty registry with no connected agents.
	registry := ws.NewRegistry()
	handler := newNoOpHandler(repo, registry)
	router := setupRouter(handler)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/schedules/sched-1/trigger", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if resp["status"] != "FAILED" {
		t.Errorf("expected status FAILED, got %v", resp["status"])
	}
}
