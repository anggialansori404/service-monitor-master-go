package models

import (
	"time"
)

// Institution represents a monitored institution/organization.
type Institution struct {
	ID           string        `json:"id" bson:"_id,omitempty"` // Will handle manual map decode if needed
	Name         string        `json:"name" bson:"name"`
	AgentID      string        `json:"agent_id" bson:"agent_id"`
	SecretKey    string        `json:"secret_key" bson:"secret_key"`
	IsActive     bool          `json:"is_active" bson:"is_active"`
	LastPing     *time.Time    `json:"last_ping" bson:"last_ping,omitempty"`
	HardwareInfo *HardwareInfo `json:"hardware_info" bson:"hardware_info,omitempty" gorm:"serializer:json"`
	Services     []Service     `json:"services" bson:"services,omitempty" gorm:"foreignKey:InstitutionID;references:ID"`

	// Telegram Alert Configurations per Institution
	UseCustomTelegram    bool   `json:"use_custom_telegram" bson:"use_custom_telegram" gorm:"default:false"`
	TelegramBotToken     string `json:"telegram_bot_token" bson:"telegram_bot_token" gorm:"default:''"`
	TelegramChatID       string `json:"telegram_chat_id" bson:"telegram_chat_id" gorm:"default:''"`
	TelegramTopicID      string `json:"telegram_topic_id" bson:"telegram_topic_id" gorm:"default:''"`
	EnableTelegramAlerts bool   `json:"enable_telegram_alerts" bson:"enable_telegram_alerts" gorm:"default:true"`
	MirrorToGlobal       bool   `json:"mirror_to_global" bson:"mirror_to_global" gorm:"default:false"`
	NotifyNodeOffline    bool   `json:"notify_node_offline" bson:"notify_node_offline" gorm:"default:true"`

	// Maintenance Mode
	InMaintenance    bool       `json:"in_maintenance" bson:"in_maintenance" gorm:"default:false"`
	MaintenanceUntil *time.Time `json:"maintenance_until" bson:"maintenance_until,omitempty"`
}

// HardwareInfo represents the server health metrics of a node.
type HardwareInfo struct {
	OSPlatform string  `json:"os_platform" bson:"os_platform"`
	CPUUsage   float64 `json:"cpu_usage" bson:"cpu_usage"`
	RAMUsedMB  float64 `json:"ram_used_mb" bson:"ram_used_mb"`
	RAMTotalMB float64 `json:"ram_total_mb" bson:"ram_total_mb"`
}

// Service represents a monitored service.
type Service struct {
	ID            string     `json:"id" bson:"_id,omitempty" gorm:"primaryKey"`
	InstitutionID string     `json:"institution_id" bson:"institution_id"`
	Name          string     `json:"name" bson:"name"`
	DisplayName   string     `json:"display_name" bson:"display_name,omitempty" gorm:"default:''"`
	Type          string     `json:"type" bson:"type"`
	Status        string     `json:"status" bson:"status"`
	AutoRestart   bool       `json:"auto_restart" bson:"auto_restart"`
	RestartStatus string     `json:"restart_status" bson:"restart_status,omitempty" gorm:"default:''"`
	LastChecked   *time.Time `json:"last_checked" bson:"last_checked,omitempty"`
	IsStale       bool       `json:"is_stale" bson:"-" gorm:"-"`
	Stale         bool       `json:"stale" bson:"-" gorm:"-"`

	Institution *Institution `json:"-" bson:"-" gorm:"foreignKey:InstitutionID"`
}

// GetDisplayName returns DisplayName if non-empty, otherwise falls back to Name.
func (s Service) GetDisplayName() string {
	if s.DisplayName != "" {
		return s.DisplayName
	}
	return s.Name
}

// DashboardSummaryResponse represents the aggregated dashboard data.
type DashboardSummaryResponse struct {
	TotalInstitutions int64 `json:"total_institutions"`
	TotalServices     int64 `json:"total_services"`
	TotalUp           int64 `json:"total_up"`
	TotalDown         int64 `json:"total_down"`
}

// AuditLog records actions performed on services.
type AuditLog struct {
	ID              string    `json:"id" bson:"_id,omitempty" gorm:"primaryKey"`
	InstitutionID   string    `json:"institution_id" bson:"institution_id"`
	InstitutionName string    `json:"institution_name,omitempty" bson:"institution_name,omitempty"`
	ServiceName     string    `json:"service_name" bson:"service_name"`
	Action          string    `json:"action" bson:"action"`
	Status          string    `json:"status" bson:"status"`
	TriggeredBy     string    `json:"triggered_by" bson:"triggered_by"`
	Details         string    `json:"details,omitempty" bson:"details,omitempty"`
	Timestamp       time.Time `json:"timestamp" bson:"timestamp"`
	CreatedAt       time.Time `json:"created_at" bson:"created_at"`
}

// User represents an authenticated dashboard user.
type User struct {
	ID           string     `json:"id" bson:"_id,omitempty" gorm:"primaryKey"`
	Email        string     `json:"email" bson:"email"`
	PasswordHash string     `json:"-" bson:"password_hash"`
	Role         string     `json:"role" bson:"role"`
	CreatedAt    *time.Time `json:"created_at" bson:"created_at,omitempty"`
}

// GlobalSetting stores single global configurations like Telegram alerts.
type GlobalSetting struct {
	ID                   string    `json:"id" bson:"_id,omitempty" gorm:"primaryKey"`
	TelegramBotToken     string    `json:"telegram_bot_token" bson:"telegram_bot_token"`
	TelegramChatID       string    `json:"telegram_chat_id" bson:"telegram_chat_id"`
	TelegramTopicID      string    `json:"telegram_topic_id" bson:"telegram_topic_id"`
	EnableTelegramAlerts bool      `json:"enable_telegram_alerts" bson:"enable_telegram_alerts"`
	EnableDailyDigest    bool      `json:"enable_daily_digest" bson:"enable_daily_digest" gorm:"default:false"`
	DailyDigestTime      string    `json:"daily_digest_time" bson:"daily_digest_time" gorm:"default:'08:00'"`
	UpdatedAt            time.Time `json:"updated_at" bson:"updated_at"`
}
// SecurityLog records suspicious access attempts.
type SecurityLog struct {
	ID        string    `json:"id" bson:"_id,omitempty" gorm:"primaryKey"`
	IP        string    `json:"ip" bson:"ip"`
	Path      string    `json:"path" bson:"path"`
	Method    string    `json:"method" bson:"method"`
	UserAgent string    `json:"user_agent" bson:"user_agent"`
	Timestamp time.Time `json:"timestamp" bson:"timestamp"`
}

// BlacklistedIP represents an IP address that is blocked from accessing the system.
type BlacklistedIP struct {
	ID        string    `json:"id" bson:"_id,omitempty" gorm:"primaryKey"`
	IP        string    `json:"ip" bson:"ip"`
	Reason    string    `json:"reason" bson:"reason"`
	CreatedAt time.Time `json:"created_at" bson:"created_at"`
}

// AlertBatchMessage identifies one Telegram message carrying a batch alert.
// The bot token is deliberately not stored: it is resolved from the current
// institution/global settings when the message needs editing, so no extra copy
// of a credential lives in this table.
type AlertBatchMessage struct {
	ChatID    string `json:"chat_id" bson:"chat_id"`
	TopicID   string `json:"topic_id" bson:"topic_id"`
	IsGlobal  bool   `json:"is_global" bson:"is_global"`
	MessageID int    `json:"message_id" bson:"message_id"`
}

// AlertBatchState persists the active critical batch alert of one institution so
// a master restart edits the existing Telegram message instead of posting a new one.
type AlertBatchState struct {
	InstitutionID string              `json:"institution_id" bson:"_id" gorm:"primaryKey"`
	Messages      []AlertBatchMessage `json:"messages" bson:"messages" gorm:"serializer:json"`
	DownServices  []string            `json:"down_services" bson:"down_services" gorm:"serializer:json"`
	UpdatedAt     time.Time           `json:"updated_at" bson:"updated_at"`
}
