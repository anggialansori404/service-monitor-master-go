package models

import "time"

// IncidentLog tracks downtime incidents for services and nodes.
type IncidentLog struct {
	ID             string     `json:"id" bson:"_id,omitempty" gorm:"primaryKey"`
	InstitutionID  string     `json:"institution_id" bson:"institution_id" gorm:"index"`
	ServiceName    string     `json:"service_name" bson:"service_name" gorm:"index"`
	Type           string     `json:"type" bson:"type"` // "service_down" or "node_offline"
	StartedAt      time.Time  `json:"started_at" bson:"started_at" gorm:"index"`
	ResolvedAt     *time.Time `json:"resolved_at" bson:"resolved_at"`
	DurationSec    int64      `json:"duration_sec" bson:"duration_sec"` // Duration in seconds
	Cause          string     `json:"cause" bson:"cause"`
	ResolvedByAuto bool       `json:"resolved_by_auto" bson:"resolved_by_auto"`
	CreatedAt      time.Time  `json:"created_at" bson:"created_at"`
}

// UptimeDayBucket represents a single day's uptime stats for visual uptime bars.
type UptimeDayBucket struct {
	Date          string  `json:"date"`           // "YYYY-MM-DD"
	UptimePercent float64 `json:"uptime_percent"` // 0.0 - 100.0
	Incidents     int     `json:"incidents"`
	DowntimeSec   int64   `json:"downtime_sec"`
}

// UptimeResponse encapsulates SLA and daily bucket metrics.
type UptimeResponse struct {
	SLAPercentage float64           `json:"sla_percentage"` // e.g. 99.85
	TotalDays     int               `json:"total_days"`
	Days          []UptimeDayBucket `json:"days"`
}
