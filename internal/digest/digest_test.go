package digest_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"monitoring-master/internal/digest"
	"monitoring-master/internal/models"
	"monitoring-master/internal/repository"
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
	if err != nil {
		t.Fatalf("unexpected error running SendDailyDigest: %v", err)
	}

	if !repo.calledInstIncidents["inst-1"] {
		t.Errorf("expected GetRecentIncidentsCountByInstitution called for inst-1")
	}
	if !repo.calledInstIncidents["inst-2"] {
		t.Errorf("expected GetRecentIncidentsCountByInstitution called for inst-2")
	}
}
