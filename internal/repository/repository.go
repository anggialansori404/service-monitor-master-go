package repository

import (
	"context"
	"errors"
	"time"

	"monitoring-master/internal/models"
)

// ErrNotFound is returned when a queried record does not exist.
var ErrNotFound = errors.New("record not found")

// ScheduleRepository abstracts schedule and schedule-run database operations.
type ScheduleRepository interface {
	CreateSchedule(ctx context.Context, schedule *models.Schedule) error
	GetSchedule(ctx context.Context, id string) (*models.Schedule, error)
	ListSchedules(ctx context.Context, institutionID string, enabledOnly bool) ([]models.Schedule, error)
	UpdateSchedule(ctx context.Context, schedule *models.Schedule) error
	DeleteSchedule(ctx context.Context, id string) error
	CreateScheduleRun(ctx context.Context, run *models.ScheduleRun) error
	ListScheduleRuns(ctx context.Context, scheduleID string, page, limit int) ([]models.ScheduleRun, int64, error)
	GetEnabledSchedules(ctx context.Context) ([]models.Schedule, error)
	UpdateScheduleRunTimes(ctx context.Context, scheduleID string, lastRunAt, nextRunAt time.Time) error
	PurgeScheduleRuns(ctx context.Context, olderThan time.Time) (int64, error)
}

// Repository abstracts all database operations used by handlers, bot, middleware, and seeder.
type Repository interface {
	// Institution
	GetInstitutionByAgentID(ctx context.Context, agentID string) (*models.Institution, error)
	GetInstitutionByID(id string) (*models.Institution, error)
	GetInstitutionByName(name string) (*models.Institution, error)
	GetAllInstitutions() ([]models.Institution, error)
	GetDashboardSummary(ctx context.Context) (*models.DashboardSummaryResponse, error)
	UpdateInstitutionPing(ctx context.Context, id string, hwInfo *models.HardwareInfo, t time.Time) error
	CountInstitutions() (int64, error)
	CreateInstitution(inst *models.Institution) error
	UpdateInstitution(inst *models.Institution) error
	DeleteInstitution(id string) error

	// Service
	GetServicesByInstitution(instID string, search string, status string, page, limit int) ([]models.Service, int64, error)
	GetService(instID string, name string) (*models.Service, error)
	UpsertService(ctx context.Context, service *models.Service) error
	BulkUpsertServices(ctx context.Context, services []models.Service) error
	UpdateServiceStatus(ctx context.Context, instID string, name, status string, t time.Time) error
	UpdateServiceRestartStatus(ctx context.Context, instID, name, restartStatus string) error
	UpdateServiceAutoRestart(ctx context.Context, instID, name string, autoRestart bool) error
	CountServicesByStatus(instID string, status string) (int64, error)
	CountServicesByStatusBulk(instIDs []string) (map[string]map[string]int64, error)
	CreateService(service *models.Service) error
	DeleteService(ctx context.Context, instID, name string) error
	Ping(ctx context.Context) error

	// AuditLog
	CreateAuditLog(ctx context.Context, log *models.AuditLog) error
	PurgeAuditLogs(ctx context.Context, olderThan time.Time) (int64, error)
	// SecurityLog
	CreateSecurityLog(ctx context.Context, log *models.SecurityLog) error
	PurgeSecurityLogs(ctx context.Context, olderThan time.Time) (int64, error)

	// Auth
	GetInstitutionByAgentIDAndSecret(ctx context.Context, agentID, secretKey string) (*models.Institution, error)

	// User
	GetUserByEmail(email string) (*models.User, error)
	CreateUser(user *models.User) error
	GetUserByID(ctx context.Context, id string) (*models.User, error)
	UpdateUserPassword(ctx context.Context, id string, hash string) error
	GetAuditLogs(ctx context.Context, filter map[string]interface{}, page, limit int) ([]models.AuditLog, int64, error)
	GetAuditLogsAll(ctx context.Context, filter map[string]interface{}) ([]models.AuditLog, error)
	GetSecurityLogs(ctx context.Context, page, limit int) ([]models.SecurityLog, int64, error)

	// Settings
	GetSettings(ctx context.Context) (*models.GlobalSetting, error)
	UpdateSettings(ctx context.Context, setting *models.GlobalSetting) error
	// Blacklist
	BlacklistIP(ctx context.Context, ip string, reason string) error
	IsIPBlacklisted(ctx context.Context, ip string) (bool, error)
	GetBlacklistedIPs(ctx context.Context) ([]string, error)

	// Schedule
	ScheduleRepo() ScheduleRepository
	PurgeScheduleRuns(ctx context.Context, olderThan time.Time) (int64, error)
}
