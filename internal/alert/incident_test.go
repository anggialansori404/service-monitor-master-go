package alert_test

import (
	"context"
	"testing"
	"time"

	"monitoring-master/internal/models"
)

// MockIncidentRepo tests the logic of incident tracking and uptime calculation
type mockIncidentRepo struct {
	incidents []models.IncidentLog
}

func (m *mockIncidentRepo) CreateIncident(_ context.Context, inc *models.IncidentLog) error {
	m.incidents = append(m.incidents, *inc)
	return nil
}

func (m *mockIncidentRepo) ResolveActiveIncident(_ context.Context, instID, serviceName, incType string, resolvedAt time.Time, resolvedByAuto bool) (*models.IncidentLog, error) {
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

func (m *mockIncidentRepo) GetUptimeStats(_ context.Context, instID string, days int) (*models.UptimeResponse, error) {
	now := time.Now().UTC()
	buckets := make([]models.UptimeDayBucket, days)
	var totalDowntimeSec int64

	for i := 0; i < days; i++ {
		dayDate := now.AddDate(0, 0, -(days - 1 - i))
		dateStr := dayDate.Format("2006-01-02")
		startOfDay := time.Date(dayDate.Year(), dayDate.Month(), dayDate.Day(), 0, 0, 0, 0, time.UTC)
		endOfDay := startOfDay.Add(24 * time.Hour)

		var dayIncidents int
		var dayDowntimeSec int64

		for _, inc := range m.incidents {
			if inc.InstitutionID != instID {
				continue
			}
			incStart := inc.StartedAt
			incEnd := now
			if inc.ResolvedAt != nil {
				incEnd = *inc.ResolvedAt
			}
			if incStart.Before(endOfDay) && incEnd.After(startOfDay) {
				dayIncidents++
				overlapStart := incStart
				if overlapStart.Before(startOfDay) {
					overlapStart = startOfDay
				}
				overlapEnd := incEnd
				if overlapEnd.After(endOfDay) {
					overlapEnd = endOfDay
				}
				overlapSec := int64(overlapEnd.Sub(overlapStart).Seconds())
				if overlapSec > 0 {
					dayDowntimeSec += overlapSec
				}
			}
		}

		if dayDowntimeSec > 86400 {
			dayDowntimeSec = 86400
		}
		uptimePct := 100.0 - (float64(dayDowntimeSec) / 86400.0 * 100.0)
		if uptimePct < 0 {
			uptimePct = 0
		}

		buckets[i] = models.UptimeDayBucket{
			Date:          dateStr,
			UptimePercent: float64(int(uptimePct*100)) / 100,
			Incidents:     dayIncidents,
			DowntimeSec:   dayDowntimeSec,
		}
		totalDowntimeSec += dayDowntimeSec
	}

	totalSeconds := int64(days) * 86400
	overallUptime := 100.0 - (float64(totalDowntimeSec) / float64(totalSeconds) * 100.0)
	if overallUptime < 0 {
		overallUptime = 0
	}

	return &models.UptimeResponse{
		SLAPercentage: float64(int(overallUptime*100)) / 100,
		TotalDays:     days,
		Days:          buckets,
	}, nil
}

func TestIncidentLifecycleAndUptimeCalculation(t *testing.T) {
	ctx := context.Background()
	repo := &mockIncidentRepo{}

	instID := "inst-uptime-test"

	// 1. Initial uptime (no incidents)
	stats, err := repo.GetUptimeStats(ctx, instID, 30)
	if err != nil {
		t.Fatalf("failed to get uptime stats: %v", err)
	}
	if stats.SLAPercentage != 100.0 {
		t.Errorf("expected initial SLA to be 100.0, got %f", stats.SLAPercentage)
	}
	if len(stats.Days) != 30 {
		t.Errorf("expected 30 days buckets, got %d", len(stats.Days))
	}

	// 2. Open incident 1 hour ago
	startTime := time.Now().UTC().Add(-1 * time.Hour)
	inc := &models.IncidentLog{
		InstitutionID: instID,
		ServiceName:   "nginx",
		Type:          "service_down",
		StartedAt:     startTime,
		Cause:         "Process died",
		CreatedAt:     time.Now().UTC(),
	}
	if err := repo.CreateIncident(ctx, inc); err != nil {
		t.Fatalf("failed to create incident: %v", err)
	}

	// 3. Resolve incident after 10 minutes (600s)
	resolveTime := startTime.Add(10 * time.Minute)
	resolved, err := repo.ResolveActiveIncident(ctx, instID, "nginx", "service_down", resolveTime, true)
	if err != nil {
		t.Fatalf("failed to resolve incident: %v", err)
	}
	if resolved == nil {
		t.Fatalf("expected resolved incident, got nil")
	}
	if resolved.DurationSec != 600 {
		t.Errorf("expected duration to be 600 seconds, got %d", resolved.DurationSec)
	}
	if !resolved.ResolvedByAuto {
		t.Errorf("expected resolved_by_auto to be true")
	}

	// 4. Verify updated SLA reflects downtime
	updatedStats, err := repo.GetUptimeStats(ctx, instID, 30)
	if err != nil {
		t.Fatalf("failed to get updated uptime stats: %v", err)
	}
	if updatedStats.SLAPercentage >= 100.0 {
		t.Errorf("expected SLA to be < 100.0 after 10m downtime, got %f", updatedStats.SLAPercentage)
	}

	// Last bucket (today) should have 1 incident and 600s downtime
	todayBucket := updatedStats.Days[len(updatedStats.Days)-1]
	if todayBucket.Incidents != 1 || todayBucket.DowntimeSec != 600 {
		t.Errorf("expected today bucket to have 1 incident and 600s downtime, got %+v", todayBucket)
	}
}
