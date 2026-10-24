package alert_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"monitoring-master/internal/alert"
	"monitoring-master/internal/models"
	"monitoring-master/internal/repository"
	"monitoring-master/internal/services"
)

type mockAlertRepo struct {
	repository.Repository
	incidents    []models.IncidentLog
	institutions map[string]*models.Institution
	settings     *models.GlobalSetting
	services     map[string]map[string]*models.Service // instID -> name -> Service
	batchStates  map[string]*models.AlertBatchState    // instID -> persisted batch alert
	mu           sync.Mutex
}

func (m *mockAlertRepo) CreateIncident(_ context.Context, inc *models.IncidentLog) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.incidents = append(m.incidents, *inc)
	return nil
}

func (m *mockAlertRepo) GetActiveIncident(_ context.Context, instID, serviceName, incType string) (*models.IncidentLog, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.incidents) - 1; i >= 0; i-- {
		inc := &m.incidents[i]
		if inc.InstitutionID == instID && inc.Type == incType && (serviceName == "" || inc.ServiceName == serviceName) && inc.ResolvedAt == nil {
			return inc, nil
		}
	}
	return nil, nil
}

func (m *mockAlertRepo) ResolveActiveIncident(_ context.Context, instID, serviceName, incType string, resolvedAt time.Time, resolvedByAuto bool) (*models.IncidentLog, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.incidents) - 1; i >= 0; i-- {
		inc := &m.incidents[i]
		if inc.InstitutionID == instID && inc.Type == incType && (serviceName == "" || inc.ServiceName == serviceName) && inc.ResolvedAt == nil {
			inc.ResolvedAt = &resolvedAt
			inc.ResolvedByAuto = resolvedByAuto
			inc.DurationSec = int64(resolvedAt.Sub(inc.StartedAt).Seconds())
			return inc, nil
		}
	}
	return nil, nil
}

func newMockAlertRepo() *mockAlertRepo {
	return &mockAlertRepo{
		institutions: make(map[string]*models.Institution),
		services:     make(map[string]map[string]*models.Service),
		settings: &models.GlobalSetting{
			TelegramBotToken:     "test-token",
			TelegramChatID:       "123456",
			EnableTelegramAlerts: true,
		},
	}
}

func (m *mockAlertRepo) GetInstitutionByID(id string) (*models.Institution, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	inst, ok := m.institutions[id]
	if !ok {
		return nil, fmt.Errorf("institution not found")
	}
	return inst, nil
}

func (m *mockAlertRepo) GetAlertBatchState(_ context.Context, instID string) (*models.AlertBatchState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, ok := m.batchStates[instID]
	if !ok {
		return nil, nil
	}
	cp := *state
	cp.Messages = append([]models.AlertBatchMessage(nil), state.Messages...)
	cp.DownServices = append([]string(nil), state.DownServices...)
	return &cp, nil
}

func (m *mockAlertRepo) SaveAlertBatchState(_ context.Context, state *models.AlertBatchState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.batchStates == nil {
		m.batchStates = make(map[string]*models.AlertBatchState)
	}
	cp := *state
	cp.Messages = append([]models.AlertBatchMessage(nil), state.Messages...)
	cp.DownServices = append([]string(nil), state.DownServices...)
	m.batchStates[state.InstitutionID] = &cp
	return nil
}

func (m *mockAlertRepo) DeleteAlertBatchState(_ context.Context, instID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.batchStates, instID)
	return nil
}

func (m *mockAlertRepo) GetSettings(_ context.Context) (*models.GlobalSetting, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.settings, nil
}

func (m *mockAlertRepo) GetService(instID, name string) (*models.Service, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if instMap, ok := m.services[instID]; ok {
		if svc, ok := instMap[name]; ok {
			return svc, nil
		}
	}
	return nil, fmt.Errorf("service not found")
}

func (m *mockAlertRepo) GetServicesByInstitution(instID, _search, _status string, _page, _limit int) ([]models.Service, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var list []models.Service
	if instMap, ok := m.services[instID]; ok {
		for _, svc := range instMap {
			list = append(list, *svc)
		}
	}
	return list, int64(len(list)), nil
}
func (m *mockAlertRepo) UpdateServiceStatus(_ context.Context, instID, name, status string, t time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.services[instID]; !ok {
		m.services[instID] = make(map[string]*models.Service)
	}
	if svc, ok := m.services[instID][name]; ok {
		svc.Status = status
		svc.LastChecked = &t
	} else {
		m.services[instID][name] = &models.Service{
			InstitutionID: instID,
			Name:          name,
			Status:        status,
			LastChecked:   &t,
		}
	}
	return nil
}

func TestBatchAlert_UpDalamDebounceAdaDiBatchTetapEdit(t *testing.T) {
	repo := newMockAlertRepo()
	repo.institutions["inst-1"] = &models.Institution{
		ID:   "inst-1",
		Name: "Test Institution",
	}

	// Telegram mock server
	var editedMessages []string
	var editMu sync.Mutex
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/bottest-token/sendMessage" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"ok":true,"result":{"message_id":101}}`))
			return
		}
		if r.URL.Path == "/bottest-token/editMessageText" {
			editMu.Lock()
			editedMessages = append(editedMessages, r.URL.Path)
			editMu.Unlock()
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"ok":true,"result":{"message_id":101}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	// Teardown order matters: cancel manager BEFORE closing mock server
	// (defer LIFO: cancel registered last runs first).
	defer ts.Close()
	services.SetTelegramBaseURLForTest(ts.URL)
	defer services.ResetTelegramBaseURLForTest()
	mgr := alert.NewManager(repo, 30*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go mgr.Start(ctx)

	// svc1 goes DOWN -> timer fires -> batch created with svc1.
	// Poll until tracked (timer + HTTP mock can be slow on loaded CI).
	mgr.AddEvent("inst-1", "svc1", "DOWN", "died")
	deadline := time.Now().Add(2 * time.Second)
	for {
		if bs := mgr.GetBatchState("inst-1"); bs != nil {
			if _, ok := bs.DownService["svc1"]; ok {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("batch for svc1 never tracked")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Now svc1 flaps: goes DOWN again (starts debounce timer)
	mgr.AddEvent("inst-1", "svc1", "DOWN", "died again")
	time.Sleep(10 * time.Millisecond)

	// svc1 goes UP while debounce timer is ACTIVE, but svc1 is already in active batch!
	mgr.AddEvent("inst-1", "svc1", "UP", "back up")
	time.Sleep(50 * time.Millisecond)

	editMu.Lock()
	count := len(editedMessages)
	editMu.Unlock()

	if count == 0 {
		t.Fatalf("expected batch message to be edited when service in active batch goes UP during debounce, but was not edited")
	}
}

func TestBatchAlert_BatchKeduaMerge(t *testing.T) {
	repo := newMockAlertRepo()
	repo.institutions["inst-1"] = &models.Institution{
		ID:   "inst-1",
		Name: "Test Institution",
	}

	var sentMessages []string
	var sendMu sync.Mutex
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/bottest-token/sendMessage" {
			sendMu.Lock()
			sentMessages = append(sentMessages, r.URL.Path)
			sendMu.Unlock()
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"ok":true,"result":{"message_id":102}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true,"result":{"message_id":102}}`))
	}))
	// Teardown order matters: cancel manager BEFORE closing mock server.
	defer ts.Close()
	services.SetTelegramBaseURLForTest(ts.URL)
	defer services.ResetTelegramBaseURLForTest()

	mgr := alert.NewManager(repo, 30*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go mgr.Start(ctx)

	// svc1 goes DOWN -> timer fires -> batch created with svc1.
	// Poll until tracked before firing the second batch.
	mgr.AddEvent("inst-1", "svc1", "DOWN", "down1")
	deadline := time.Now().Add(2 * time.Second)
	for {
		if bs := mgr.GetBatchState("inst-1"); bs != nil {
			if _, ok := bs.DownService["svc1"]; ok {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("batch for svc1 never tracked")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// svc2 goes DOWN -> timer fires -> second batch fires. Should merge DownService, not overwrite svc1.
	mgr.AddEvent("inst-1", "svc2", "DOWN", "down2")
	deadline2 := time.Now().Add(2 * time.Second)
	for {
		if bs := mgr.GetBatchState("inst-1"); bs != nil {
			if _, ok := bs.DownService["svc2"]; ok {
				break
			}
		}
		if time.Now().After(deadline2) {
			t.Fatalf("batch for svc2 never tracked")
		}
		time.Sleep(10 * time.Millisecond)
	}

	bs := mgr.GetBatchState("inst-1")
	if bs == nil {
		t.Fatalf("expected active batch state for inst-1")
	}
	if _, ok := bs.DownService["svc1"]; !ok {
		t.Errorf("expected svc1 still in batch state DownService after second batch")
	}
	if _, ok := bs.DownService["svc2"]; !ok {
		t.Errorf("expected svc2 in batch state DownService after second batch")
	}
}

func TestServices_EditTelegramMessageRetry(t *testing.T) {
	attempts := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"ok":false,"description":"Internal error"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true,"result":{"message_id":101}}`))
	}))
	defer ts.Close()

	services.SetTelegramBaseURLForTest(ts.URL)
	defer services.ResetTelegramBaseURLForTest()

	err := services.EditTelegramMessage("test-token", "123", "", 101, "new text")
	if err != nil {
		t.Fatalf("expected EditTelegramMessage to succeed after retry, got: %v", err)
	}
	if attempts < 2 {
		t.Fatalf("expected at least 2 attempts, got %d", attempts)
	}
}

// waitForBatchService polls until the alert manager tracks svc in the active batch.
func waitForBatchService(t *testing.T, mgr *alert.Manager, instID, svc string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if bs := mgr.GetBatchState(instID); bs != nil {
			if _, ok := bs.DownService[svc]; ok {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("batch for %s never tracked", svc)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestBatchAlert_DownBaruEditPesanLamaBukanKirimBaru(t *testing.T) {
	repo := newMockAlertRepo()
	repo.institutions["inst-1"] = &models.Institution{ID: "inst-1", Name: "Test Institution"}

	var sends, edits int
	var mu sync.Mutex
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mu.Lock()
		switch r.URL.Path {
		case "/bottest-token/sendMessage":
			sends++
		case "/bottest-token/editMessageText":
			edits++
		}
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true,"result":{"message_id":101}}`))
	}))
	defer ts.Close()
	services.SetTelegramBaseURLForTest(ts.URL)
	defer services.ResetTelegramBaseURLForTest()

	mgr := alert.NewManager(repo, 30*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go mgr.Start(ctx)

	mgr.AddEvent("inst-1", "svc1", "DOWN", "down1")
	waitForBatchService(t, mgr, "inst-1", "svc1")

	mgr.AddEvent("inst-1", "svc2", "DOWN", "down2")
	waitForBatchService(t, mgr, "inst-1", "svc2")

	mu.Lock()
	defer mu.Unlock()
	if sends != 1 {
		t.Fatalf("expected exactly 1 sendMessage (existing batch edited afterwards), got %d", sends)
	}
	if edits == 0 {
		t.Fatalf("expected editMessageText when a new service joins an existing batch, got 0")
	}
}

func TestBatchAlert_DownBerulangTidakKirimUlang(t *testing.T) {
	repo := newMockAlertRepo()
	repo.institutions["inst-1"] = &models.Institution{ID: "inst-1", Name: "Test Institution"}

	var sends int
	var mu sync.Mutex
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mu.Lock()
		if r.URL.Path == "/bottest-token/sendMessage" {
			sends++
		}
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true,"result":{"message_id":101}}`))
	}))
	defer ts.Close()
	services.SetTelegramBaseURLForTest(ts.URL)
	defer services.ResetTelegramBaseURLForTest()

	mgr := alert.NewManager(repo, 30*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go mgr.Start(ctx)

	mgr.AddEvent("inst-1", "svc1", "DOWN", "down1")
	waitForBatchService(t, mgr, "inst-1", "svc1")

	// Service keeps reporting DOWN (agent auto-restart keeps failing).
	for i := 0; i < 3; i++ {
		mgr.AddEvent("inst-1", "svc1", "DOWN", "still down")
		time.Sleep(60 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if sends != 1 {
		t.Fatalf("expected repeated DOWN for an already-batched service to send nothing new, got %d sendMessage calls", sends)
	}
}

func TestBatchAlert_EditGagalKirimUlangHanyaKeTargetItu(t *testing.T) {
	repo := newMockAlertRepo()
	repo.institutions["inst-1"] = &models.Institution{ID: "inst-1", Name: "Test Institution"}

	var sends, edits int
	var mu sync.Mutex
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mu.Lock()
		switch r.URL.Path {
		case "/bottest-token/sendMessage":
			sends++
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"ok":true,"result":{"message_id":900}}`))
			return
		case "/bottest-token/editMessageText":
			edits++
			mu.Unlock()
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"ok":false,"description":"Bad Request: message to edit not found"}`))
			return
		}
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true,"result":{"message_id":900}}`))
	}))
	defer ts.Close()
	services.SetTelegramBaseURLForTest(ts.URL)
	defer services.ResetTelegramBaseURLForTest()

	mgr := alert.NewManager(repo, 30*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go mgr.Start(ctx)

	mgr.AddEvent("inst-1", "svc1", "DOWN", "down1")
	waitForBatchService(t, mgr, "inst-1", "svc1")

	// Second wave: the tracked message no longer exists, so it must be resent.
	mgr.AddEvent("inst-1", "svc2", "DOWN", "down2")
	waitForBatchService(t, mgr, "inst-1", "svc2")

	mu.Lock()
	defer mu.Unlock()
	if edits == 0 {
		t.Fatalf("expected an edit attempt for the existing batch message")
	}
	if sends != 2 {
		t.Fatalf("expected 1 initial send + 1 resend after the failed edit, got %d", sends)
	}
	bs := mgr.GetBatchState("inst-1")
	if bs == nil || len(bs.SentMessages) != 1 || bs.SentMessages[0].MessageID != 900 {
		t.Fatalf("expected the batch to track the resent message 900, got %+v", bs)
	}
}

// Simulates a master restart: a new Manager over the same repository must pick up
// the persisted batch and edit the existing Telegram message instead of resending.
func TestBatchAlert_SetelahRestartEditPesanLama(t *testing.T) {
	repo := newMockAlertRepo()
	repo.institutions["inst-1"] = &models.Institution{ID: "inst-1", Name: "Test Institution"}

	var sends, edits int
	var mu sync.Mutex
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mu.Lock()
		switch r.URL.Path {
		case "/bottest-token/sendMessage":
			sends++
		case "/bottest-token/editMessageText":
			edits++
		}
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true,"result":{"message_id":555}}`))
	}))
	defer ts.Close()
	services.SetTelegramBaseURLForTest(ts.URL)
	defer services.ResetTelegramBaseURLForTest()

	// --- Process 1 ---
	ctx1, stop1 := context.WithCancel(context.Background())
	mgr1 := alert.NewManager(repo, 30*time.Millisecond)
	go mgr1.Start(ctx1)
	mgr1.AddEvent("inst-1", "svc1", "DOWN", "down1")
	waitForBatchService(t, mgr1, "inst-1", "svc1")
	stop1()

	if stored, _ := repo.GetAlertBatchState(context.Background(), "inst-1"); stored == nil || len(stored.Messages) != 1 {
		t.Fatalf("expected batch state persisted before restart, got %+v", stored)
	}

	// --- Process 2 (fresh in-memory state) ---
	ctx2, stop2 := context.WithCancel(context.Background())
	defer stop2()
	mgr2 := alert.NewManager(repo, 30*time.Millisecond)
	go mgr2.Start(ctx2)
	if bs := mgr2.GetBatchState("inst-1"); bs != nil {
		t.Fatalf("fresh manager should start with empty in-memory state, got %+v", bs)
	}

	mgr2.AddEvent("inst-1", "svc2", "DOWN", "down2")
	waitForBatchService(t, mgr2, "inst-1", "svc2")

	mu.Lock()
	if sends != 1 {
		t.Fatalf("expected no new message after restart (edit only), got %d sendMessage calls", sends)
	}
	if edits != 1 {
		t.Fatalf("expected the persisted batch message to be edited, got %d edits", edits)
	}
	mu.Unlock()

	// Recovery after restart must also edit, not send a standalone recovery.
	mgr2.AddEvent("inst-1", "svc1", "UP", "auto-restart successful")
	deadline := time.Now().Add(2 * time.Second)
	for {
		if bs := mgr2.GetBatchState("inst-1"); bs != nil {
			if _, still := bs.DownService["svc1"]; !still {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("svc1 was never removed from the restored batch")
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if sends != 1 {
		t.Fatalf("recovery must edit, not send; got %d sendMessage calls", sends)
	}
	if edits != 2 {
		t.Fatalf("expected 2 edits (svc2 join + svc1 recovery), got %d", edits)
	}
}

func TestBatchAlert_StateTerhapusSetelahSemuaPulih(t *testing.T) {
	repo := newMockAlertRepo()
	repo.institutions["inst-1"] = &models.Institution{ID: "inst-1", Name: "Test Institution"}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true,"result":{"message_id":777}}`))
	}))
	defer ts.Close()
	services.SetTelegramBaseURLForTest(ts.URL)
	defer services.ResetTelegramBaseURLForTest()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr := alert.NewManager(repo, 30*time.Millisecond)
	go mgr.Start(ctx)

	mgr.AddEvent("inst-1", "svc1", "DOWN", "down1")
	waitForBatchService(t, mgr, "inst-1", "svc1")
	mgr.AddEvent("inst-1", "svc1", "UP", "recovered")

	deadline := time.Now().Add(2 * time.Second)
	for {
		stored, _ := repo.GetAlertBatchState(context.Background(), "inst-1")
		if stored == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("persisted batch state should be deleted after full recovery, got %+v", stored)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
