package repository

import (
	"context"
	"errors"
	"time"

	"monitoring-master/internal/models"
)

// ErrNotFound is returned when a queried record does not exist.
var ErrNotFound = errors.New("record not found")

// Repository abstracts all database operations used by handlers, bot, middleware, and seeder.
type Repository interface {
	// Institution
	GetInstitutionByAgentID(ctx context.Context, agentID string) (*models.Institution, error)
	GetInstitutionByID(id string) (*models.Institution, error)
	GetInstitutionByName(name string) (*models.Institution, error)
	GetAllInstitutions() ([]models.Institution, error)
	UpdateInstitutionPing(ctx context.Context, id string, hwInfo *models.HardwareInfo, t time.Time) error
	CountInstitutions() (int64, error)
	CreateInstitution(inst *models.Institution) error
	UpdateInstitution(inst *models.Institution) error
	DeleteInstitution(id string) error

	// Service
	GetServicesByInstitution(instID string, search string, page, limit int) ([]models.Service, int64, error)
	GetService(instID string, name string) (*models.Service, error)
	UpsertService(ctx context.Context, service *models.Service) error
	UpdateServiceStatus(ctx context.Context, instID string, name, status string, t time.Time) error
	CountServicesByStatus(instID string, status string) (int64, error)
	CreateService(service *models.Service) error

	// AuditLog
	CreateAuditLog(ctx context.Context, log *models.AuditLog) error

	// Auth
	GetInstitutionByAgentIDAndSecret(ctx context.Context, agentID, secretKey string) (*models.Institution, error)

	// User
	GetUserByEmail(email string) (*models.User, error)
	CreateUser(user *models.User) error
	GetUserByID(ctx context.Context, id string) (*models.User, error)
	UpdateUserPassword(ctx context.Context, id string, hash string) error
	GetAuditLogs(ctx context.Context, filter map[string]interface{}, page, limit int) ([]models.AuditLog, int64, error)

	// Settings
	GetSettings(ctx context.Context) (*models.GlobalSetting, error)
	UpdateSettings(ctx context.Context, setting *models.GlobalSetting) error
}
