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
	Type          string     `json:"type" bson:"type"`
	Status        string     `json:"status" bson:"status"`
	AutoRestart   bool       `json:"auto_restart" bson:"auto_restart"`
	RestartStatus string     `json:"restart_status" bson:"restart_status,omitempty" gorm:"default:''"`
	LastChecked   *time.Time `json:"last_checked" bson:"last_checked,omitempty"`

	Institution *Institution `json:"-" bson:"-" gorm:"foreignKey:InstitutionID"`
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
