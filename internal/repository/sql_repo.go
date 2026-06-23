package repository

import (
	"context"
	"errors"
	"strings"
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

func (r *SQLRepository) GetDashboardSummary(ctx context.Context) (*models.DashboardSummaryResponse, error) {
	var summary models.DashboardSummaryResponse
	
	if err := r.db.WithContext(ctx).Model(&models.Institution{}).Count(&summary.TotalInstitutions).Error; err != nil {
		return nil, err
	}
	if err := r.db.WithContext(ctx).Model(&models.Service{}).Count(&summary.TotalServices).Error; err != nil {
		return nil, err
	}
	if err := r.db.WithContext(ctx).Model(&models.Service{}).Where("status = ?", "UP").Count(&summary.TotalUp).Error; err != nil {
		return nil, err
	}
	if err := r.db.WithContext(ctx).Model(&models.Service{}).Where("status IN ?", []string{"DOWN", "ERROR"}).Count(&summary.TotalDown).Error; err != nil {
		return nil, err
	}
	
	return &summary, nil
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

func (r *SQLRepository) GetServicesByInstitution(instID string, search string, status string, page, limit int) ([]models.Service, int64, error) {
	var services []models.Service
	var total int64

	query := r.db.Model(&models.Service{}).Where("institution_id = ?", instID)

	if search != "" {
		query = query.Where("name LIKE ?", "%"+search+"%")
	}

	// Inject Status Filter - Created by Kang Anggi
	if status != "" && strings.ToUpper(status) != "ALL" {
		query = query.Where("status = ?", strings.ToUpper(status))
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

func (r *SQLRepository) BulkUpsertServices(ctx context.Context, services []models.Service) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, svc := range services {
			var existing models.Service
			err := tx.Where("institution_id = ? AND name = ?", svc.InstitutionID, svc.Name).First(&existing).Error

			if errors.Is(err, gorm.ErrRecordNotFound) {
				if svc.Status == "" {
					svc.Status = "UP"
				}
				if svc.ID == "" {
					svc.ID = generateID()
				}
				if err := tx.Create(&svc).Error; err != nil {
					return err
				}
				continue
			} else if err != nil {
				return err
			}

			statusStr := svc.Status
			if statusStr == "" {
				statusStr = "UP"
			}

			err = tx.Model(&existing).Updates(map[string]interface{}{
				"type":         svc.Type,
				"status":       statusStr,
				"auto_restart": svc.AutoRestart,
				"last_checked": svc.LastChecked,
			}).Error
			if err != nil {
				return err
			}
		}
		return nil
	})
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

func (r *SQLRepository) UpdateServiceAutoRestart(ctx context.Context, instID, name string, autoRestart bool) error {
	result := r.db.WithContext(ctx).Model(&models.Service{}).
		Where("institution_id = ? AND name = ?", instID, name).
		Update("auto_restart", autoRestart)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *SQLRepository) UpdateServiceRestartStatus(ctx context.Context, instID, name, restartStatus string) error {
	return r.db.WithContext(ctx).Model(&models.Service{}).
		Where("institution_id = ? AND name = ?", instID, name).
		Update("restart_status", restartStatus).Error
}

func (r *SQLRepository) CountServicesByStatus(instID string, status string) (int64, error) {
	var count int64
	err := r.db.Model(&models.Service{}).Where("institution_id = ? AND status = ?", instID, status).Count(&count).Error
	return count, err
}

func (r *SQLRepository) CountServicesByStatusBulk(instIDs []string) (map[string]map[string]int64, error) {
	if len(instIDs) == 0 {
		return map[string]map[string]int64{}, nil
	}

	type row struct {
		InstitutionID string
		Status        string
		Cnt           int64
	}

	var rows []row
	err := r.db.Model(&models.Service{}).
		Select("institution_id, status, COUNT(*) as cnt").
		Where("institution_id IN ?", instIDs).
		Group("institution_id, status").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}

	result := make(map[string]map[string]int64)
	for _, r := range rows {
		if result[r.InstitutionID] == nil {
			result[r.InstitutionID] = make(map[string]int64)
		}
		result[r.InstitutionID][r.Status] = r.Cnt
	}
	return result, nil
}

func (r *SQLRepository) CreateService(service *models.Service) error {
	if service.ID == "" {
		service.ID = generateID()
	}
	return r.db.Create(service).Error
}

func (r *SQLRepository) DeleteService(ctx context.Context, instID, name string) error {
	result := r.db.WithContext(ctx).Where("institution_id = ? AND name = ?", instID, name).Delete(&models.Service{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *SQLRepository) Ping(ctx context.Context) error {
	sqlDB, err := r.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
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
	var logs []models.AuditLog
	var total int64

	query := r.db.WithContext(ctx).
		Table("audit_logs").
		Select("audit_logs.*, institutions.name as institution_name").
		Joins("LEFT JOIN institutions ON institutions.id = audit_logs.institution_id")

	countQuery := r.db.WithContext(ctx).Table("audit_logs")

	for key, val := range filter {
		switch key {
		case "action", "status", "institution_id":
			query = query.Where("audit_logs."+key+" = ?", val)
			countQuery = countQuery.Where(key+" = ?", val)
		case "timestamp":
			if tsMap, ok := val.(map[string]interface{}); ok {
				if gte, ok := tsMap["$gte"]; ok {
					query = query.Where("audit_logs.timestamp >= ?", gte)
					countQuery = countQuery.Where("timestamp >= ?", gte)
				}
				if lte, ok := tsMap["$lte"]; ok {
					query = query.Where("audit_logs.timestamp <= ?", lte)
					countQuery = countQuery.Where("timestamp <= ?", lte)
				}
			}
		case "$search":
			if s, ok := val.(string); ok && s != "" {
				like := "%" + s + "%"
				clause := "audit_logs.service_name LIKE ? OR audit_logs.triggered_by LIKE ? OR audit_logs.details LIKE ? OR audit_logs.action LIKE ?"
				query = query.Where(clause, like, like, like, like)
				countQuery = countQuery.Where(clause, like, like, like, like)
			}
		}
	}

	if err := countQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	sortField := "audit_logs.timestamp"
	sortDir := "DESC"
	if sf, ok := filter["$sort_field"].(string); ok && sf != "" {
		switch sf {
		case "service_name":
			sortField = "audit_logs.service_name"
		case "action":
			sortField = "audit_logs.action"
		case "status":
			sortField = "audit_logs.status"
		case "triggered_by":
			sortField = "audit_logs.triggered_by"
		case "institution_name":
			sortField = "institutions.name"
		}
	}
	if sd, ok := filter["$sort_dir"].(string); ok && strings.ToUpper(sd) == "ASC" {
		sortDir = "ASC"
	}

	offset := (page - 1) * limit
	if err := query.Order(sortField + " " + sortDir).Offset(offset).Limit(limit).Find(&logs).Error; err != nil {
		return nil, 0, err
	}

	if logs == nil {
		logs = []models.AuditLog{}
	}
	return logs, total, nil
}

func (r *SQLRepository) GetAuditLogsAll(ctx context.Context, filter map[string]interface{}) ([]models.AuditLog, error) {
	var logs []models.AuditLog

	query := r.db.WithContext(ctx).
		Table("audit_logs").
		Select("audit_logs.*, institutions.name as institution_name").
		Joins("LEFT JOIN institutions ON institutions.id = audit_logs.institution_id").
		Limit(50000)

	for key, val := range filter {
		switch key {
		case "action", "status", "institution_id":
			query = query.Where("audit_logs."+key+" = ?", val)
		case "timestamp":
			if tsMap, ok := val.(map[string]interface{}); ok {
				if gte, ok := tsMap["$gte"]; ok {
					query = query.Where("audit_logs.timestamp >= ?", gte)
				}
				if lte, ok := tsMap["$lte"]; ok {
					query = query.Where("audit_logs.timestamp <= ?", lte)
				}
			}
		case "$search":
			if s, ok := val.(string); ok && s != "" {
				like := "%" + s + "%"
				clause := "audit_logs.service_name LIKE ? OR audit_logs.triggered_by LIKE ? OR audit_logs.details LIKE ? OR audit_logs.action LIKE ?"
				query = query.Where(clause, like, like, like, like)
			}
		}
	}

	if err := query.Order("audit_logs.timestamp DESC").Find(&logs).Error; err != nil {
		return nil, err
	}

	if logs == nil {
		logs = []models.AuditLog{}
	}
	return logs, nil
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
		setting.UpdatedAt = time.Now().Local()
		return r.db.WithContext(ctx).Create(setting).Error
	} else if err != nil {
		return err
	}

	existing.TelegramBotToken = setting.TelegramBotToken
	existing.TelegramChatID = setting.TelegramChatID
	existing.EnableTelegramAlerts = setting.EnableTelegramAlerts
	existing.UpdatedAt = time.Now().Local()

	return r.db.WithContext(ctx).Save(&existing).Error
}

// --- Blacklist ---

func (r *SQLRepository) BlacklistIP(ctx context.Context, ip string, reason string) error {
	blacklisted := models.BlacklistedIP{
		ID:        generateID(),
		IP:        ip,
		Reason:    reason,
		CreatedAt: time.Now().Local(),
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

// --- Schedule ---

// ScheduleRepo returns the ScheduleRepository interface implemented by SQLRepository.
func (r *SQLRepository) ScheduleRepo() ScheduleRepository {
	return r
}

func (r *SQLRepository) CreateSchedule(ctx context.Context, schedule *models.Schedule) error {
	return r.db.WithContext(ctx).Create(schedule).Error
}

func (r *SQLRepository) GetSchedule(ctx context.Context, id string) (*models.Schedule, error) {
	var schedule models.Schedule
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&schedule).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &schedule, nil
}

func (r *SQLRepository) ListSchedules(ctx context.Context, institutionID string, enabledOnly bool) ([]models.Schedule, error) {
	query := r.db.WithContext(ctx).Model(&models.Schedule{})

	if institutionID != "" {
		query = query.Where("institution_id = ?", institutionID)
	}
	if enabledOnly {
		query = query.Where("enabled = ?", true)
	}

	var schedules []models.Schedule
	if err := query.Order("created_at DESC").Find(&schedules).Error; err != nil {
		return nil, err
	}
	if schedules == nil {
		schedules = []models.Schedule{}
	}
	return schedules, nil
}

func (r *SQLRepository) UpdateSchedule(ctx context.Context, schedule *models.Schedule) error {
	return r.db.WithContext(ctx).Save(schedule).Error
}

func (r *SQLRepository) DeleteSchedule(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&models.Schedule{}, "id = ?", id).Error
}

func (r *SQLRepository) CreateScheduleRun(ctx context.Context, run *models.ScheduleRun) error {
	return r.db.WithContext(ctx).Create(run).Error
}

func (r *SQLRepository) ListScheduleRuns(ctx context.Context, scheduleID string, page, limit int) ([]models.ScheduleRun, int64, error) {
	query := r.db.WithContext(ctx).Model(&models.ScheduleRun{}).Where("schedule_id = ?", scheduleID)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * limit
	var runs []models.ScheduleRun
	if err := query.Order("run_at DESC").Offset(offset).Limit(limit).Find(&runs).Error; err != nil {
		return nil, 0, err
	}
	if runs == nil {
		runs = []models.ScheduleRun{}
	}
	return runs, total, nil
}

func (r *SQLRepository) GetEnabledSchedules(ctx context.Context) ([]models.Schedule, error) {
	var schedules []models.Schedule
	if err := r.db.WithContext(ctx).Where("enabled = ?", true).Find(&schedules).Error; err != nil {
		return nil, err
	}
	if schedules == nil {
		schedules = []models.Schedule{}
	}
	return schedules, nil
}

func (r *SQLRepository) UpdateScheduleRunTimes(ctx context.Context, scheduleID string, lastRunAt, nextRunAt time.Time) error {
	return r.db.WithContext(ctx).Model(&models.Schedule{}).Where("id = ?", scheduleID).Updates(map[string]interface{}{
		"last_run_at": lastRunAt,
		"next_run_at": nextRunAt,
	}).Error
}
