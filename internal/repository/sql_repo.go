package repository

import (
	"context"
	"errors"
	"time"

	"monitoring-master/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// SQLRepository implements Repository using GORM.
type SQLRepository struct {
	db *gorm.DB
}

// NewSQLRepository creates a new SQLRepository.
func NewSQLRepository(db *gorm.DB) *SQLRepository {
	return &SQLRepository{db: db}
}

// --- Helper Functions ---

func generateID() string {
	return uuid.New().String()
}

// --- Institution ---

func (r *SQLRepository) GetInstitutionByAgentID(ctx context.Context, agentID string) (*models.Institution, error) {
	var inst models.Institution
	err := r.db.WithContext(ctx).Where("agent_id = ?", agentID).First(&inst).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &inst, nil
}

func (r *SQLRepository) GetInstitutionByID(id string) (*models.Institution, error) {
	var inst models.Institution
	err := r.db.Where("id = ?", id).First(&inst).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &inst, nil
}

func (r *SQLRepository) GetInstitutionByName(name string) (*models.Institution, error) {
	var inst models.Institution
	err := r.db.Where("name = ?", name).First(&inst).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &inst, nil
}

func (r *SQLRepository) GetAllInstitutions() ([]models.Institution, error) {
	var institutions []models.Institution
	// Preload services
	err := r.db.Preload("Services").Find(&institutions).Error
	if err != nil {
		return nil, err
	}
	return institutions, nil
}

func (r *SQLRepository) UpdateInstitutionPing(ctx context.Context, id string, hwInfo *models.HardwareInfo, t time.Time) error {
	updateFields := map[string]interface{}{
		"last_ping": t,
	}

	if hwInfo != nil {
		updateFields["hardware_info"] = hwInfo
	}

	result := r.db.WithContext(ctx).Model(&models.Institution{}).Where("id = ?", id).Updates(updateFields)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *SQLRepository) CountInstitutions() (int64, error) {
	var count int64
	err := r.db.Model(&models.Institution{}).Count(&count).Error
	return count, err
}

func (r *SQLRepository) CreateInstitution(inst *models.Institution) error {
	if inst.ID == "" {
		inst.ID = generateID()
	}
	return r.db.Create(inst).Error
}

func (r *SQLRepository) UpdateInstitution(inst *models.Institution) error {
	if inst.ID == "" {
		return ErrNotFound
	}
	// Save will save all fields. To only update changed fields, we can use Updates
	result := r.db.Model(&models.Institution{}).Where("id = ?", inst.ID).Updates(inst)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *SQLRepository) DeleteInstitution(id string) error {
	// First delete associated services and audit logs to handle foreign keys if not cascaded
	r.db.Where("institution_id = ?", id).Delete(&models.Service{})
	r.db.Where("institution_id = ?", id).Delete(&models.AuditLog{})

	result := r.db.Where("id = ?", id).Delete(&models.Institution{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Service ---

func (r *SQLRepository) GetServicesByInstitution(instID string, search string, page, limit int) ([]models.Service, int64, error) {
	var services []models.Service
	var total int64

	query := r.db.Model(&models.Service{}).Where("institution_id = ?", instID)

	if search != "" {
		query = query.Where("name LIKE ?", "%"+search+"%")
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * limit
	err := query.Offset(offset).Limit(limit).Find(&services).Error
	if err != nil {
		return nil, 0, err
	}
	return services, total, nil
}

func (r *SQLRepository) GetService(instID string, name string) (*models.Service, error) {
	var service models.Service
	err := r.db.Where("institution_id = ? AND name = ?", instID, name).First(&service).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &service, nil
}

func (r *SQLRepository) UpsertService(ctx context.Context, service *models.Service) error {
	var existing models.Service
	err := r.db.WithContext(ctx).Where("institution_id = ? AND name = ?", service.InstitutionID, service.Name).First(&existing).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		if service.Status == "" {
			service.Status = "UP"
		}
		if service.ID == "" {
			service.ID = generateID()
		}
		return r.db.WithContext(ctx).Create(service).Error
	} else if err != nil {
		return err
	}

	statusStr := service.Status
	if statusStr == "" {
		statusStr = "UP"
	}

	return r.db.WithContext(ctx).Model(&existing).Updates(map[string]interface{}{
		"type":         service.Type,
		"status":       statusStr,
		"auto_restart": service.AutoRestart,
		"last_checked": service.LastChecked,
	}).Error
}

func (r *SQLRepository) UpdateServiceStatus(ctx context.Context, instID string, name, status string, t time.Time) error {
	result := r.db.WithContext(ctx).Model(&models.Service{}).Where("institution_id = ? AND name = ?", instID, name).Updates(map[string]interface{}{
		"status":       status,
		"last_checked": t,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *SQLRepository) CountServicesByStatus(instID string, status string) (int64, error) {
	var count int64
	err := r.db.Model(&models.Service{}).Where("institution_id = ? AND status = ?", instID, status).Count(&count).Error
	return count, err
}

func (r *SQLRepository) CreateService(service *models.Service) error {
	if service.ID == "" {
		service.ID = generateID()
	}
	return r.db.Create(service).Error
}

// --- AuditLog ---

func (r *SQLRepository) CreateAuditLog(ctx context.Context, log *models.AuditLog) error {
	if log.ID == "" {
		log.ID = generateID()
	}
	return r.db.WithContext(ctx).Create(log).Error
}
// --- SecurityLog ---

func (r *SQLRepository) CreateSecurityLog(ctx context.Context, log *models.SecurityLog) error {
	if log.ID == "" {
		log.ID = generateID()
	}
	return r.db.WithContext(ctx).Create(log).Error
}

// --- Auth ---

func (r *SQLRepository) GetInstitutionByAgentIDAndSecret(ctx context.Context, agentID, secretKey string) (*models.Institution, error) {
	var inst models.Institution
	err := r.db.WithContext(ctx).Where("agent_id = ? AND secret_key = ?", agentID, secretKey).First(&inst).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &inst, nil
}

// --- User ---

func (r *SQLRepository) GetUserByEmail(email string) (*models.User, error) {
	var user models.User
	err := r.db.Where("email = ?", email).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &user, nil
}

func (r *SQLRepository) CreateUser(user *models.User) error {
	if user.ID == "" {
		user.ID = generateID()
	}
	return r.db.Create(user).Error
}

func (r *SQLRepository) GetUserByID(ctx context.Context, id string) (*models.User, error) {
	var user models.User
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &user, nil
}

func (r *SQLRepository) UpdateUserPassword(ctx context.Context, id string, hash string) error {
	return r.db.WithContext(ctx).Model(&models.User{}).Where("id = ?", id).Update("password_hash", hash).Error
}

func (r *SQLRepository) GetAuditLogs(ctx context.Context, filter map[string]interface{}, page, limit int) ([]models.AuditLog, int64, error) {
	query := r.db.WithContext(ctx).Model(&models.AuditLog{})

	// Apply filters safely translating MongoDB style to GORM
	if filter != nil {
		if tsFilter, ok := filter["timestamp"].(map[string]interface{}); ok {
			if gte, ok := tsFilter["$gte"].(time.Time); ok {
				query = query.Where("timestamp >= ?", gte)
			}
			if lte, ok := tsFilter["$lte"].(time.Time); ok {
				query = query.Where("timestamp <= ?", lte)
			}
		}
		if action, ok := filter["action"].(string); ok {
			query = query.Where("action = ?", action)
		}
		if status, ok := filter["status"].(string); ok {
			query = query.Where("status = ?", status)
		}
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * limit
	var logs []models.AuditLog

	// Instead of lookup via query, we could join or pre-fill, but we'll run a loop for simplicity
	if err := query.Order("timestamp desc").Offset(offset).Limit(limit).Find(&logs).Error; err != nil {
		return nil, 0, err
	}

	// Quick manual lookup for institution_name
	for i, log := range logs {
		var inst models.Institution
		if r.db.WithContext(ctx).Where("id = ?", log.InstitutionID).First(&inst).Error == nil {
			logs[i].InstitutionName = inst.Name
		}
	}

	if logs == nil {
		logs = []models.AuditLog{}
	}
	return logs, total, nil
}

// --- Settings ---

func (r *SQLRepository) GetSettings(ctx context.Context) (*models.GlobalSetting, error) {
	var setting models.GlobalSetting
	err := r.db.WithContext(ctx).First(&setting).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &models.GlobalSetting{}, nil
		}
		return nil, err
	}
	return &setting, nil
}

func (r *SQLRepository) UpdateSettings(ctx context.Context, setting *models.GlobalSetting) error {
	var existing models.GlobalSetting
	err := r.db.WithContext(ctx).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if setting.ID == "" {
			setting.ID = generateID()
		}
		setting.UpdatedAt = time.Now()
		return r.db.WithContext(ctx).Create(setting).Error
	} else if err != nil {
		return err
	}

	existing.TelegramBotToken = setting.TelegramBotToken
	existing.TelegramChatID = setting.TelegramChatID
	existing.EnableTelegramAlerts = setting.EnableTelegramAlerts
	existing.UpdatedAt = time.Now()

	return r.db.WithContext(ctx).Save(&existing).Error
}

// --- Blacklist ---

func (r *SQLRepository) BlacklistIP(ctx context.Context, ip string, reason string) error {
	blacklisted := models.BlacklistedIP{
		ID:        generateID(),
		IP:        ip,
		Reason:    reason,
		CreatedAt: time.Now(),
	}
	return r.db.WithContext(ctx).Create(&blacklisted).Error
}

func (r *SQLRepository) IsIPBlacklisted(ctx context.Context, ip string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.BlacklistedIP{}).Where("ip = ?", ip).Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
