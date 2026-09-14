package handlers_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"monitoring-master/internal/alert"
	"monitoring-master/internal/api/handlers"
	"monitoring-master/internal/models"
	"monitoring-master/internal/repository"
	"monitoring-master/internal/ws"
)

type mockWSRepo struct {
	mu               sync.Mutex
	institution      *models.Institution
	settings         *models.GlobalSetting
	incidentsCreated []*models.IncidentLog
	resolvedTypes    []string
}

func newMockWSRepo(inst *models.Institution) *mockWSRepo {
	return &mockWSRepo{
		institution: inst,
		settings: &models.GlobalSetting{
			TelegramBotToken:     "global-tok",
			TelegramChatID:       "global-chat",
			EnableTelegramAlerts: true,
		},
	}
}

func (m *mockWSRepo) GetInstitutionByID(id string) (*models.Institution, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.institution != nil && m.institution.ID == id {
		return m.institution, nil
	}
	return nil, repository.ErrNotFound
}

func (m *mockWSRepo) GetSettings(_ context.Context) (*models.GlobalSetting, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.settings, nil
}

func (m *mockWSRepo) CreateIncident(_ context.Context, inc *models.IncidentLog) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.incidentsCreated = append(m.incidentsCreated, inc)
	return nil
}

func (m *mockWSRepo) ResolveActiveIncident(_ context.Context, instID, serviceName, incType string, resolvedAt time.Time, isManual bool) (*models.IncidentLog, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resolvedTypes = append(m.resolvedTypes, incType)
	return nil, nil
}

// Stubs for repository.Repository interface
func (m *mockWSRepo) CreateInstitution(_ *models.Institution) error                  { return nil }
func (m *mockWSRepo) UpdateInstitution(_ *models.Institution) error                  { return nil }
func (m *mockWSRepo) GetInstitutionByAgentID(_ context.Context, _ string) (*models.Institution, error) {
	return nil, nil
}
func (m *mockWSRepo) GetInstitutionByName(_ string) (*models.Institution, error)    { return nil, nil }
func (m *mockWSRepo) GetAllInstitutions() ([]models.Institution, error)             { return nil, nil }
func (m *mockWSRepo) GetDashboardSummary(_ context.Context) (*models.DashboardSummaryResponse, error) {
	return nil, nil
}
func (m *mockWSRepo) UpdateInstitutionPing(_ context.Context, _ string, _ *models.HardwareInfo, _ time.Time) error {
	return nil
}
func (m *mockWSRepo) CountInstitutions() (int64, error)              { return 0, nil }
func (m *mockWSRepo) DeleteInstitution(_ string) error               { return nil }
func (m *mockWSRepo) GetServicesByInstitution(_ string, _ string, _ string, _, _ int) ([]models.Service, int64, error) {
	return nil, 0, nil
}
func (m *mockWSRepo) GetService(_ string, _ string) (*models.Service, error)       { return nil, nil }
func (m *mockWSRepo) UpsertService(_ context.Context, _ *models.Service) error      { return nil }
func (m *mockWSRepo) BulkUpsertServices(_ context.Context, _ []models.Service) error { return nil }
func (m *mockWSRepo) UpdateServiceStatus(_ context.Context, _ string, _, _ string, _ time.Time) error {
	return nil
}
func (m *mockWSRepo) UpdateServiceRestartStatus(_ context.Context, _, _, _ string) error {
	return nil
}
func (m *mockWSRepo) UpdateServiceAutoRestart(_ context.Context, _, _ string, _ bool) error {
	return nil
}
func (m *mockWSRepo) CountServicesByStatus(_ string, _ string) (int64, error) { return 0, nil }
func (m *mockWSRepo) CountServicesByStatusBulk(_ []string) (map[string]map[string]int64, error) {
	return nil, nil
}
func (m *mockWSRepo) CreateService(_ *models.Service) error                 { return nil }
func (m *mockWSRepo) DeleteService(_ context.Context, _, _ string) error   { return nil }
func (m *mockWSRepo) Ping(_ context.Context) error                          { return nil }
func (m *mockWSRepo) CreateAuditLog(_ context.Context, _ *models.AuditLog) error {
	return nil
}
func (m *mockWSRepo) PurgeAuditLogs(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}
func (m *mockWSRepo) CreateSecurityLog(_ context.Context, _ *models.SecurityLog) error {
	return nil
}
func (m *mockWSRepo) PurgeSecurityLogs(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}
func (m *mockWSRepo) GetInstitutionByAgentIDAndSecret(_ context.Context, _, _ string) (*models.Institution, error) {
	return nil, nil
}
func (m *mockWSRepo) GetUserByEmail(_ string) (*models.User, error)     { return nil, nil }
func (m *mockWSRepo) CreateUser(_ *models.User) error                   { return nil }
func (m *mockWSRepo) GetUserByID(_ context.Context, _ string) (*models.User, error) {
	return nil, nil
}
func (m *mockWSRepo) UpdateUserPassword(_ context.Context, _, _ string) error { return nil }
func (m *mockWSRepo) GetAuditLogs(_ context.Context, _ map[string]interface{}, _, _ int) ([]models.AuditLog, int64, error) {
	return nil, 0, nil
}
func (m *mockWSRepo) GetAuditLogsAll(_ context.Context, _ map[string]interface{}) ([]models.AuditLog, error) {
	return nil, nil
}
func (m *mockWSRepo) GetSecurityLogs(_ context.Context, _, _ int) ([]models.SecurityLog, int64, error) {
	return nil, 0, nil
}
func (m *mockWSRepo) UpdateSettings(_ context.Context, _ *models.GlobalSetting) error { return nil }
func (m *mockWSRepo) BlacklistIP(_ context.Context, _, _ string) error                { return nil }
func (m *mockWSRepo) IsIPBlacklisted(_ context.Context, _ string) (bool, error)      { return false, nil }
func (m *mockWSRepo) GetBlacklistedIPs(_ context.Context) ([]string, error)          { return nil, nil }
func (m *mockWSRepo) ScheduleRepo() repository.ScheduleRepository                    { return nil }
func (m *mockWSRepo) PurgeScheduleRuns(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}
func (m *mockWSRepo) GetActiveIncident(_ context.Context, _, _, _ string) (*models.IncidentLog, error) {
	return nil, nil
}
func (m *mockWSRepo) GetIncidentsByInstitution(_ context.Context, _ string, _, _ int) ([]models.IncidentLog, int64, error) {
	return nil, 0, nil
}
func (m *mockWSRepo) GetUptimeStats(_ context.Context, _ string, _ int) (*models.UptimeResponse, error) {
	return &models.UptimeResponse{SLAPercentage: 100.0, TotalDays: 30, Days: []models.UptimeDayBucket{}}, nil
}
func (m *mockWSRepo) GetRecentIncidentsCount(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}
func (m *mockWSRepo) GetRecentIncidentsCountByInstitution(_ context.Context, _ string, _ time.Time) (int64, error) {
	return 0, nil
}
func (m *mockWSRepo) PurgeIncidents(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}

func setupTestWSHandler(inst *models.Institution) (*handlers.WSHandler, *mockWSRepo, *[]string, *[]string) {
	repo := newMockWSRepo(inst)
	registry := ws.NewRegistry()
	alertMgr := alert.NewManager(repo, 2*time.Minute)
	h := handlers.NewWSHandler(registry, repo, alertMgr)

	var sentAlerts []string
	var editedAlerts []string
	var mu sync.Mutex

	h.SetAlertHooks(
		func(token, chatID, topicID, text string) (int, error) {
			mu.Lock()
			defer mu.Unlock()
			sentAlerts = append(sentAlerts, text)
			return len(sentAlerts), nil
		},
		func(token, chatID, topicID string, messageID int, text string) error {
			mu.Lock()
			defer mu.Unlock()
			editedAlerts = append(editedAlerts, text)
			return nil
		},
	)

	return h, repo, &sentAlerts, &editedAlerts
}

// 1. Test Silent Reconnect within Grace Period
func TestNodeAgentSilentReconnectWithinGracePeriod(t *testing.T) {
	inst := &models.Institution{
		ID:                   "inst-1",
		Name:                 "Lembaga Grace",
		AgentID:              "agent-1",
		NotifyNodeOffline:    true,
		EnableTelegramAlerts: true,
	}
	h, _, sentAlerts, editedAlerts := setupTestWSHandler(inst)
	h.SetTimingConfig(50*time.Millisecond, 10*time.Second, 10*time.Second, 3)

	// Agent disconnects
	h.OnAgentDisconnected(inst)

	// Reconnect quickly (after 10ms, well within 50ms grace period)
	time.Sleep(10 * time.Millisecond)
	h.OnAgentConnected(inst)

	// Wait past the original grace period
	time.Sleep(60 * time.Millisecond)

	if len(*sentAlerts) != 0 {
		t.Fatalf("expected 0 alerts for silent reconnect, got %d: %+v", len(*sentAlerts), *sentAlerts)
	}
	if len(*editedAlerts) != 0 {
		t.Fatalf("expected 0 edited alerts, got %d", len(*editedAlerts))
	}
}

// 2. Test True Offline After Grace Period Expires and Recovery with Downtime
func TestNodeAgentTrueOfflineAfterGracePeriod(t *testing.T) {
	inst := &models.Institution{
		ID:                   "inst-2",
		Name:                 "Lembaga Offline",
		AgentID:              "agent-2",
		NotifyNodeOffline:    true,
		EnableTelegramAlerts: true,
	}
	h, repo, sentAlerts, editedAlerts := setupTestWSHandler(inst)
	h.SetTimingConfig(30*time.Millisecond, 10*time.Second, 10*time.Second, 3)

	// Agent disconnects
	h.OnAgentDisconnected(inst)

	// Wait past grace period so alert fires
	time.Sleep(50 * time.Millisecond)

	if len(*sentAlerts) != 1 {
		t.Fatalf("expected 1 critical alert, got %d: %+v", len(*sentAlerts), *sentAlerts)
	}
	if !strings.Contains((*sentAlerts)[0], "CRITICAL ALERT") {
		t.Errorf("alert text should contain CRITICAL ALERT, got %s", (*sentAlerts)[0])
	}
	if len(repo.incidentsCreated) != 1 {
		t.Errorf("expected 1 incident created, got %d", len(repo.incidentsCreated))
	}

	// Now agent reconnects
	time.Sleep(20 * time.Millisecond)
	h.OnAgentConnected(inst)

	if len(*editedAlerts) != 1 {
		t.Fatalf("expected 1 edited alert on recovery, got %d", len(*editedAlerts))
	}
	if !strings.Contains((*editedAlerts)[0], "NODE RECOVERED") {
		t.Errorf("expected edited alert to contain NODE RECOVERED, got %s", (*editedAlerts)[0])
	}
	if !strings.Contains((*editedAlerts)[0], "Durasi Offline") {
		t.Errorf("expected recovery message to include Durasi Offline, got %s", (*editedAlerts)[0])
	}
}

// 3. Test Flapping Detection and Cooldown
func TestNodeAgentFlappingDetection(t *testing.T) {
	inst := &models.Institution{
		ID:                   "inst-3",
		Name:                 "Lembaga Flapping",
		AgentID:              "agent-3",
		NotifyNodeOffline:    true,
		EnableTelegramAlerts: true,
	}
	h, _, sentAlerts, _ := setupTestWSHandler(inst)
	// Threshold = 3 disconnects within 1s window, cooldown = 500ms, grace period = 200ms
	h.SetTimingConfig(200*time.Millisecond, 1*time.Second, 500*time.Millisecond, 3)

	// Disconnect 1
	h.OnAgentDisconnected(inst)
	h.OnAgentConnected(inst)

	// Disconnect 2
	h.OnAgentDisconnected(inst)
	h.OnAgentConnected(inst)

	if len(*sentAlerts) != 0 {
		t.Fatalf("expected 0 alerts before reaching threshold, got %d", len(*sentAlerts))
	}

	// Disconnect 3 -> Should trigger Flapping Alert!
	h.OnAgentDisconnected(inst)

	if len(*sentAlerts) != 1 {
		t.Fatalf("expected 1 flapping alert on 3rd disconnect, got %d: %+v", len(*sentAlerts), *sentAlerts)
	}
	if !strings.Contains((*sentAlerts)[0], "FLAPPING") {
		t.Errorf("alert text should contain FLAPPING, got %s", (*sentAlerts)[0])
	}

	// Disconnect 4 during cooldown -> Should be suppressed (muted)
	h.OnAgentDisconnected(inst)

	if len(*sentAlerts) != 1 {
		t.Fatalf("expected no extra alert during cooldown, got %d", len(*sentAlerts))
	}
}

// 4. Test Disabled NotifyNodeOffline
func TestNodeAgentDisabledNotifyNodeOffline(t *testing.T) {
	inst := &models.Institution{
		ID:                   "inst-4",
		Name:                 "Lembaga Muted",
		AgentID:              "agent-4",
		NotifyNodeOffline:    false, // Disabled!
		EnableTelegramAlerts: true,
	}
	h, _, sentAlerts, _ := setupTestWSHandler(inst)
	h.SetTimingConfig(20*time.Millisecond, 1*time.Second, 500*time.Millisecond, 3)

	h.OnAgentDisconnected(inst)
	time.Sleep(40 * time.Millisecond)

	if len(*sentAlerts) != 0 {
		t.Fatalf("expected 0 alerts when NotifyNodeOffline is false, got %d: %+v", len(*sentAlerts), *sentAlerts)
	}
}
