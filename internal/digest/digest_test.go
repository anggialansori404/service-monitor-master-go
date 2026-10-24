package digest_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"monitoring-master/internal/digest"
	"monitoring-master/internal/models"
	"monitoring-master/internal/repository"
	"monitoring-master/internal/services"
)

func TestBuildGlobalDigestMessage(t *testing.T) {
	now := time.Date(2026, 9, 4, 8, 0, 0, 0, time.UTC)
	msg := digest.BuildGlobalDigestMessage(10, 10, 50, 50, 0, 0, now)

	if !strings.Contains(msg, "LAPORAN MONITORING HARIAN") {
		t.Errorf("expected title in message, got:\n%s", msg)
	}
	if !strings.Contains(msg, "Semua Node Online") {
		t.Errorf("expected all nodes online, got:\n%s", msg)
	}
	if !strings.Contains(msg, "50 Running, 0 Down") {
		t.Errorf("expected 50 running services, got:\n%s", msg)
	}
	if !strings.Contains(msg, "Tidak ada insiden") {
		t.Errorf("expected 0 incidents text, got:\n%s", msg)
	}

	// Degraded status
	degMsg := digest.BuildGlobalDigestMessage(10, 9, 50, 48, 2, 3, now)
	if !strings.Contains(degMsg, "9/10 Node Online") {
		t.Errorf("expected 9/10 nodes online, got:\n%s", degMsg)
	}
	if !strings.Contains(degMsg, "2 Service Mengalami Masalah") {
		t.Errorf("expected 2 service down, got:\n%s", degMsg)
	}
	if !strings.Contains(degMsg, "Terjadi 3 insiden") {
		t.Errorf("expected 3 incidents, got:\n%s", degMsg)
	}
}

func TestBuildInstitutionDigestMessage(t *testing.T) {
	now := time.Date(2026, 9, 4, 8, 0, 0, 0, time.UTC)

	// 0 incidents
	msg := digest.BuildInstitutionDigestMessage("RSUD Cisarua", true, 8, 8, 0, 0, now)
	if !strings.Contains(msg, "RSUD Cisarua") {
		t.Errorf("expected institution name in message, got:\n%s", msg)
	}
	if !strings.Contains(msg, "Online (Tersambung)") {
		t.Errorf("expected online status, got:\n%s", msg)
	}
	if !strings.Contains(msg, "8/8 Aktif") {
		t.Errorf("expected 8/8 active, got:\n%s", msg)
	}
	if !strings.Contains(msg, "✅ *Insiden 24 Jam:* 0 insiden") {
		t.Errorf("expected 0 incidents checkmark, got:\n%s", msg)
	}

	// With incidents and down service
	incMsg := digest.BuildInstitutionDigestMessage("CUKK Branchless", false, 10, 8, 2, 5, now)
	if !strings.Contains(incMsg, "Offline (Terputus)") {
		t.Errorf("expected offline status, got:\n%s", incMsg)
	}
	if !strings.Contains(incMsg, "⚠️ 2 Service DOWN") {
		t.Errorf("expected 2 service down, got:\n%s", incMsg)
	}
	if !strings.Contains(incMsg, "🚨 *Insiden 24 Jam:* 5 insiden") {
		t.Errorf("expected 5 incidents alert, got:\n%s", incMsg)
	}
}

type mockDigestRepo struct {
	repository.Repository
	settings             *models.GlobalSetting
	institutions         []models.Institution
	services             map[string][]models.Service
	instIncidentsCount   map[string]int64
	globalIncidentsCount int64
	calledInstIncidents  map[string]bool
}

func (m *mockDigestRepo) GetSettings(_ context.Context) (*models.GlobalSetting, error) {
	return m.settings, nil
}

func (m *mockDigestRepo) GetAllInstitutions() ([]models.Institution, error) {
	return m.institutions, nil
}

func (m *mockDigestRepo) GetServicesByInstitution(instID, _, _ string, _, _ int) ([]models.Service, int64, error) {
	return m.services[instID], int64(len(m.services[instID])), nil
}

func (m *mockDigestRepo) GetRecentIncidentsCountByInstitution(_ context.Context, instID string, _ time.Time) (int64, error) {
	if m.calledInstIncidents == nil {
		m.calledInstIncidents = make(map[string]bool)
	}
	m.calledInstIncidents[instID] = true
	return m.instIncidentsCount[instID], nil
}

func (m *mockDigestRepo) GetRecentIncidentsCount(_ context.Context, _ time.Time) (int64, error) {
	return m.globalIncidentsCount, nil
}

func TestSendDailyDigest(t *testing.T) {
	ctx := context.Background()

	// Test case 1: Disabled
	disabledRepo := &mockDigestRepo{
		settings: &models.GlobalSetting{
			EnableDailyDigest: false,
		},
	}
	err := digest.SendDailyDigest(ctx, disabledRepo)
	if err != nil {
		t.Fatalf("expected nil when daily digest is disabled, got: %v", err)
	}

	// Test case 2: Enabled - verifies institution incident counts are retrieved
	repo := &mockDigestRepo{
		settings: &models.GlobalSetting{
			EnableDailyDigest: true,
			// Leave Telegram credentials empty so no network requests are sent
		},
		institutions: []models.Institution{
			{ID: "inst-1", Name: "CUKK Branchless"},
			{ID: "inst-2", Name: "RSUD Cisarua"},
		},
		services: map[string][]models.Service{
			"inst-1": {{Name: "Core Banking", Status: "UP"}},
			"inst-2": {{Name: "HIS", Status: "UP"}},
		},
		instIncidentsCount: map[string]int64{
			"inst-1": 12,
			"inst-2": 0,
		},
		globalIncidentsCount: 12,
	}

	err = digest.SendDailyDigest(ctx, repo)
	if err == nil {
		t.Fatalf("expected an error when no telegram destination is configured, got nil")
	}
	if !strings.Contains(err.Error(), "no telegram destination") {
		t.Fatalf("expected a no-destination error, got: %v", err)
	}

	if !repo.calledInstIncidents["inst-1"] {
		t.Errorf("expected GetRecentIncidentsCountByInstitution called for inst-1")
	}
	if !repo.calledInstIncidents["inst-2"] {
		t.Errorf("expected GetRecentIncidentsCountByInstitution called for inst-2")
	}
}

func TestShouldSendDigest(t *testing.T) {
	jakarta := time.FixedZone("WIB", 7*3600)
	at := func(hour, minute int) time.Time {
		return time.Date(2026, 10, 24, hour, minute, 0, 0, jakarta)
	}

	cases := []struct {
		name         string
		now          time.Time
		digestTime   string
		lastSentDate string
		want         bool
	}{
		{"before target", at(7, 59), "08:00", "", false},
		{"exactly at target", at(8, 0), "08:00", "", true},
		{"inside catch-up window", at(8, 12), "08:00", "", true},
		{"after catch-up window", at(8, 16), "08:00", "", false},
		{"already sent today", at(8, 5), "08:00", "2026-10-24", false},
		{"sent yesterday", at(8, 5), "08:00", "2026-10-23", true},
		{"empty time falls back to 08:00", at(8, 1), "", "", true},
		{"unpadded hour", at(8, 1), "8:00", "", true},
		{"with seconds", at(8, 1), "08:00:00", "", true},
		{"malformed never fires", at(8, 1), "8 pagi", "", false},
		{"out of range never fires", at(8, 1), "25:00", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := digest.ShouldSendDigest(tc.now, tc.digestTime, tc.lastSentDate); got != tc.want {
				t.Fatalf("ShouldSendDigest(%s, %q, %q) = %v, want %v",
					tc.now.Format("15:04"), tc.digestTime, tc.lastSentDate, got, tc.want)
			}
		})
	}
}

func TestSendDailyDigestDispatchesGlobalMessage(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	var bodies []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		paths = append(paths, r.URL.Path)
		bodies = append(bodies, string(body))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true,"result":{"message_id":42}}`))
	}))
	defer ts.Close()
	services.SetTelegramBaseURLForTest(ts.URL)
	defer services.ResetTelegramBaseURLForTest()

	repo := &mockDigestRepo{
		settings: &models.GlobalSetting{
			EnableDailyDigest: true,
			TelegramBotToken:  "123:abc",
			TelegramChatID:    "-100200",
			TelegramTopicID:   "7",
		},
		institutions: []models.Institution{{ID: "inst-1", Name: "CUKK Branchless"}},
		services: map[string][]models.Service{
			"inst-1": {{Name: "Core Banking", Status: "DOWN"}},
		},
		instIncidentsCount:   map[string]int64{"inst-1": 2},
		globalIncidentsCount: 2,
	}

	if err := digest.SendDailyDigest(context.Background(), repo); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(paths) != 1 {
		t.Fatalf("expected exactly 1 telegram request, got %d (%v)", len(paths), paths)
	}
	if paths[0] != "/bot123:abc/sendMessage" {
		t.Fatalf("unexpected telegram path: %s", paths[0])
	}
	if !strings.Contains(bodies[0], "LAPORAN MONITORING HARIAN") {
		t.Fatalf("expected global digest body, got: %s", bodies[0])
	}
	if !strings.Contains(bodies[0], "-100200") || !strings.Contains(bodies[0], "message_thread_id") {
		t.Fatalf("expected chat id and topic in payload, got: %s", bodies[0])
	}
}
