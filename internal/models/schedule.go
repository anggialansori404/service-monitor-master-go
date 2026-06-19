package models

import (
	"time"
)

// Schedule defines a cron-based monitoring schedule for an institution.
type Schedule struct {
	ID             string     `json:"id" bson:"_id" gorm:"primaryKey;type:char(36)"`
	InstitutionID  string     `json:"institution_id" bson:"institution_id" gorm:"index;type:char(36)"`
	Name          string     `json:"name" bson:"name" gorm:"not null"`
	CronExpr      string     `json:"cron_expr" bson:"cron_expr" gorm:"not null"`
	Timezone      string     `json:"timezone" bson:"timezone" gorm:"not null;default:Asia/Jakarta"`
	ServiceNames  []string   `json:"service_names" bson:"service_names" gorm:"serializer:json"`
	MissedPolicy  string     `json:"missed_policy" bson:"missed_policy" gorm:"not null;default:skip"`
	Enabled       bool       `json:"enabled" bson:"enabled" gorm:"not null;default:true"`
	LastRunAt     *time.Time `json:"last_run_at,omitempty" bson:"last_run_at,omitempty" gorm:"type:datetime"`
	NextRunAt     *time.Time `json:"next_run_at,omitempty" bson:"next_run_at,omitempty" gorm:"type:datetime"`
	CreatedAt     time.Time  `json:"created_at" bson:"created_at" gorm:"autoCreateTime"`
	UpdatedAt     time.Time  `json:"updated_at" bson:"updated_at" gorm:"autoUpdateTime"`
}

// ScheduleRun records a single execution of a schedule.
type ScheduleRun struct {
	ID            string    `json:"id" bson:"_id" gorm:"primaryKey;type:char(36)"`
	ScheduleID    string    `json:"schedule_id" bson:"schedule_id" gorm:"index;type:char(36)"`
	ScheduleName  string    `json:"schedule_name" bson:"schedule_name" gorm:"not null"`
	InstitutionID string    `json:"institution_id" bson:"institution_id" gorm:"index;type:char(36)"`
	TriggeredBy   string    `json:"triggered_by" bson:"triggered_by" gorm:"not null"`
	ServiceNames  []string  `json:"service_names" bson:"service_names" gorm:"serializer:json"`
	Status        string    `json:"status" bson:"status" gorm:"not null"`
	ResultMessage string    `json:"result_message,omitempty" bson:"result_message,omitempty" gorm:"type:text"`
	RunAt         time.Time `json:"run_at" bson:"run_at" gorm:"not null"`
	CatchUp       bool      `json:"catch_up" bson:"catch_up" gorm:"not null;default:false"`
	CreatedAt     time.Time `json:"created_at" bson:"created_at" gorm:"autoCreateTime"`
}
