package digest_test

import (
	"strings"
	"testing"
	"time"

	"monitoring-master/internal/digest"
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
}
