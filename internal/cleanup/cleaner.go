package cleanup

import (
	"context"
	"fmt"
	"log"
	"time"

	"monitoring-master/internal/config"
	"monitoring-master/internal/repository"
)

// PurgeResult contains counts of records deleted during a purge operation.
type PurgeResult struct {
	DeletedAudit        int64 `json:"deleted_audit"`
	DeletedSecurity     int64 `json:"deleted_security"`
	DeletedScheduleRuns int64 `json:"deleted_schedule_runs"`
	TotalDeleted        int64 `json:"total_deleted"`
}

// PurgeOptions specifies parameters for a manual or automated purge.
type PurgeOptions struct {
	LogType string `json:"log_type"` // "audit", "security", "schedule_run", "all"
	Days    int    `json:"days"`     // records older than N days (0 means delete all)
}

// ExecutePurge performs log purging based on provided options.
func ExecutePurge(ctx context.Context, repo repository.Repository, opts PurgeOptions) (*PurgeResult, error) {
	var cutoff time.Time
	if opts.Days > 0 {
		cutoff = time.Now().AddDate(0, 0, -opts.Days)
	} else {
		// If 0 or negative days specified, purge all up to current moment
		cutoff = time.Now()
	}

	result := &PurgeResult{}

	// Purge Audit Logs
	if opts.LogType == "all" || opts.LogType == "audit" {
		count, err := repo.PurgeAuditLogs(ctx, cutoff)
		if err != nil {
			return nil, fmt.Errorf("PurgeAuditLogs: %w", err)
		}
		result.DeletedAudit = count
		result.TotalDeleted += count
	}

	// Purge Security Logs
	if opts.LogType == "all" || opts.LogType == "security" {
		count, err := repo.PurgeSecurityLogs(ctx, cutoff)
		if err != nil {
			return nil, fmt.Errorf("PurgeSecurityLogs: %w", err)
		}
		result.DeletedSecurity = count
		result.TotalDeleted += count
	}

	// Purge Schedule Runs
	if opts.LogType == "all" || opts.LogType == "schedule_run" {
		count, err := repo.PurgeScheduleRuns(ctx, cutoff)
		if err != nil {
			return nil, fmt.Errorf("PurgeScheduleRuns: %w", err)
		}
		result.DeletedScheduleRuns = count
		result.TotalDeleted += count
	}

	return result, nil
}

// RunDailyCleanup executes the retention policy based on environment configurations.
func RunDailyCleanup(ctx context.Context, repo repository.Repository, cfg *config.Config) {
	log.Println("[CLEANUP] Starting daily log retention cleanup...")

	var totalDeleted int64

	// Audit Logs retention
	if cfg.AuditLogRetentionDays > 0 {
		cutoff := time.Now().AddDate(0, 0, -cfg.AuditLogRetentionDays)
		count, err := repo.PurgeAuditLogs(ctx, cutoff)
		if err != nil {
			log.Printf("[CLEANUP ERROR] Failed to purge audit logs older than %d days: %v", cfg.AuditLogRetentionDays, err)
		} else if count > 0 {
			log.Printf("[CLEANUP] Purged %d audit logs older than %d days", count, cfg.AuditLogRetentionDays)
			totalDeleted += count
		}
	}

	// Security Logs retention
	if cfg.SecurityLogRetentionDays > 0 {
		cutoff := time.Now().AddDate(0, 0, -cfg.SecurityLogRetentionDays)
		count, err := repo.PurgeSecurityLogs(ctx, cutoff)
		if err != nil {
			log.Printf("[CLEANUP ERROR] Failed to purge security logs older than %d days: %v", cfg.SecurityLogRetentionDays, err)
		} else if count > 0 {
			log.Printf("[CLEANUP] Purged %d security logs older than %d days", count, cfg.SecurityLogRetentionDays)
			totalDeleted += count
		}
	}

	// Schedule Runs retention
	if cfg.ScheduleRunRetentionDays > 0 {
		cutoff := time.Now().AddDate(0, 0, -cfg.ScheduleRunRetentionDays)
		count, err := repo.PurgeScheduleRuns(ctx, cutoff)
		if err != nil {
			log.Printf("[CLEANUP ERROR] Failed to purge schedule runs older than %d days: %v", cfg.ScheduleRunRetentionDays, err)
		} else if count > 0 {
			log.Printf("[CLEANUP] Purged %d schedule runs older than %d days", count, cfg.ScheduleRunRetentionDays)
			totalDeleted += count
		}
	}

	log.Printf("[CLEANUP] Daily log retention cleanup finished. Total deleted records: %d", totalDeleted)
}

// StartDailyCleanup launches a background goroutine that runs daily at 03:00 AM local time.
func StartDailyCleanup(repo repository.Repository, cfg *config.Config) {
	go func() {
		// Run initial cleanup check 10 seconds after server startup
		time.Sleep(10 * time.Second)
		RunDailyCleanup(context.Background(), repo, cfg)

		for {
			now := time.Now()
			// Calculate next 03:00 AM
			nextRun := time.Date(now.Year(), now.Month(), now.Day(), 3, 0, 0, 0, now.Location())
			if !now.Before(nextRun) {
				nextRun = nextRun.Add(24 * time.Hour)
			}

			waitDuration := time.Until(nextRun)
			log.Printf("[CLEANUP] Next daily log cleanup scheduled at: %s (in %v)", nextRun.Format("2006-01-02 15:04:05 MST"), waitDuration.Round(time.Minute))

			time.Sleep(waitDuration)
			RunDailyCleanup(context.Background(), repo, cfg)
		}
	}()
}
