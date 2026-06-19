package handlers

import (
	"log"
	"net/http"
	"regexp"
	"time"

	"monitoring-master/internal/models"
	"monitoring-master/internal/repository"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ScheduleHandler handles CRUD operations for schedules.
type ScheduleHandler struct {
	Repo      repository.Repository
	Registry  interface{} // placeholder, typed later (ws.Registry)
	Scheduler interface{} // placeholder, typed later (gocron scheduler)
}

// NewScheduleHandler creates a new ScheduleHandler.
func NewScheduleHandler(repo repository.Repository) *ScheduleHandler {
	return &ScheduleHandler{Repo: repo}
}

// cronRegex validates standard 5-field cron expressions.
// Supports: * wildcards, ranges (1-5), steps (*/5), lists (1,3,5), and combinations.
var cronRegex = regexp.MustCompile(
	`^(` +
		`(\*|([0-5]?\d)(-([0-5]?\d))?(,([0-5]?\d)(-([0-5]?\d))?)*)(\/\d+)?` +
		`)\s+` +
		`(\*|([01]?\d|2[0-3])(-([01]?\d|2[0-3]))?(,([01]?\d|2[0-3])(-([01]?\d|2[0-3]))?)*)(\/\d+)?` +
		`\s+` +
		`(\*|([01]?\d|2\d|3[01])(-([01]?\d|2\d|3[01]))?(,([01]?\d|2\d|3[01])(-([01]?\d|2\d|3[01]))?)*)(\/\d+)?` +
		`\s+` +
		`(\*|([01]?\d|1[012])(-([01]?\d|1[012]))?(,([01]?\d|1[012])(-([01]?\d|1[012]))?)*)(\/\d+)?` +
		`\s+` +
		`(\*|[0-6](,[0-6])*(-[0-6])?(,[0-6](-[0-6])?)*)(\/\d+)?` +
		`$`,
)

// createScheduleReq is the expected payload for POST /schedules.
type createScheduleReq struct {
	InstitutionID string   `json:"institution_id" binding:"required"`
	Name          string   `json:"name" binding:"required"`
	CronExpr      string   `json:"cron_expr" binding:"required"`
	Timezone      string   `json:"timezone"`
	ServiceNames  []string `json:"service_names" binding:"required"`
	MissedPolicy  string   `json:"missed_policy"`
	Enabled       *bool    `json:"enabled"`
}

// updateScheduleReq is the expected payload for PATCH /schedules/:id.
// All fields are pointers to distinguish between "not provided" and "set to zero value".
type updateScheduleReq struct {
	Name         *string   `json:"name"`
	CronExpr     *string   `json:"cron_expr"`
	Timezone     *string   `json:"timezone"`
	ServiceNames *[]string `json:"service_names"`
	MissedPolicy *string   `json:"missed_policy"`
	Enabled      *bool     `json:"enabled"`
}

// CreateSchedule handles POST /schedules.
// Creates a new schedule after validating the institution, cron expression, and timezone.
func (h *ScheduleHandler) CreateSchedule(c *gin.Context) {
	var req createScheduleReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}

	// Validate institution exists.
	_, err := h.Repo.GetInstitutionByID(req.InstitutionID)
	if err != nil {
		if err == repository.ErrNotFound {
			c.JSON(http.StatusBadRequest, gin.H{"error": "institution not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	// Validate cron expression.
	if !cronRegex.MatchString(req.CronExpr) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid cron expression"})
		return
	}

	// Validate timezone.
	tz := req.Timezone
	if tz == "" {
		tz = "Asia/Jakarta"
	}
	_, err = time.LoadLocation(tz)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid timezone: " + tz})
		return
	}

	// Validate missed_policy.
	missedPolicy := req.MissedPolicy
	if missedPolicy == "" {
		missedPolicy = "skip"
	}
	if missedPolicy != "skip" && missedPolicy != "catch_up_once" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missed_policy must be one of: skip, catch_up_once"})
		return
	}

	// Determine enabled.
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	// Validate service_names not empty.
	if len(req.ServiceNames) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "service_names must not be empty"})
		return
	}

	now := time.Now()
	schedule := models.Schedule{
		ID:            uuid.New().String(),
		InstitutionID: req.InstitutionID,
		Name:          req.Name,
		CronExpr:      req.CronExpr,
		Timezone:      tz,
		ServiceNames:  req.ServiceNames,
		MissedPolicy:  missedPolicy,
		Enabled:       enabled,
		LastRunAt:     nil,
		NextRunAt:     nil, // set by the actual scheduler on next iteration
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := h.Repo.ScheduleRepo().CreateSchedule(c.Request.Context(), &schedule); err != nil {
		log.Printf("CreateSchedule error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create schedule"})
		return
	}

	c.JSON(http.StatusCreated, schedule)
}

// GetSchedule handles GET /schedules/:id.
// Returns a single schedule by ID, or 404 if not found.
func (h *ScheduleHandler) GetSchedule(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "schedule id is required"})
		return
	}

	schedule, err := h.Repo.ScheduleRepo().GetSchedule(c.Request.Context(), id)
	if err != nil {
		if err == repository.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "schedule not found"})
			return
		}
		log.Printf("GetSchedule error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	c.JSON(http.StatusOK, schedule)
}

// ListSchedules handles GET /schedules.
// Returns schedules filtered by optional institution_id and enabled query params.
func (h *ScheduleHandler) ListSchedules(c *gin.Context) {
	institutionID := c.Query("institution_id")

	enabledOnly := false
	enabledStr := c.Query("enabled")
	if enabledStr != "" {
		// Accept "true", "1", "false", "0".
		if enabledStr == "true" || enabledStr == "1" {
			enabledOnly = true
		}
	}

	schedules, err := h.Repo.ScheduleRepo().ListSchedules(c.Request.Context(), institutionID, enabledOnly)
	if err != nil {
		log.Printf("ListSchedules error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch schedules"})
		return
	}

	if schedules == nil {
		schedules = []models.Schedule{}
	}

	c.JSON(http.StatusOK, schedules)
}

// UpdateSchedule handles PATCH /schedules/:id.
// Performs a partial update — only fields present in JSON are changed.
func (h *ScheduleHandler) UpdateSchedule(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "schedule id is required"})
		return
	}

	existing, err := h.Repo.ScheduleRepo().GetSchedule(c.Request.Context(), id)
	if err != nil {
		if err == repository.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "schedule not found"})
			return
		}
		log.Printf("UpdateSchedule fetch error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	var req updateScheduleReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}

	changed := false

	if req.Name != nil {
		existing.Name = *req.Name
		changed = true
	}

	if req.CronExpr != nil {
		if !cronRegex.MatchString(*req.CronExpr) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid cron expression"})
			return
		}
		existing.CronExpr = *req.CronExpr
		changed = true
	}

	if req.Timezone != nil {
		_, err := time.LoadLocation(*req.Timezone)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid timezone: " + *req.Timezone})
			return
		}
		existing.Timezone = *req.Timezone
		changed = true
	}

	if req.ServiceNames != nil {
		if len(*req.ServiceNames) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "service_names must not be empty"})
			return
		}
		existing.ServiceNames = *req.ServiceNames
		changed = true
	}

	if req.MissedPolicy != nil {
		mp := *req.MissedPolicy
		if mp != "skip" && mp != "catch_up_once" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "missed_policy must be one of: skip, catch_up_once"})
			return
		}
		existing.MissedPolicy = mp
		changed = true
	}

	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
		changed = true
	}

	if !changed {
		c.JSON(http.StatusOK, existing)
		return
	}

	existing.UpdatedAt = time.Now()

	if err := h.Repo.ScheduleRepo().UpdateSchedule(c.Request.Context(), existing); err != nil {
		log.Printf("UpdateSchedule save error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update schedule"})
		return
	}

	c.JSON(http.StatusOK, existing)
}

// DeleteSchedule handles DELETE /schedules/:id.
// Deletes the schedule and returns 204 No Content on success.
func (h *ScheduleHandler) DeleteSchedule(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "schedule id is required"})
		return
	}

	err := h.Repo.ScheduleRepo().DeleteSchedule(c.Request.Context(), id)
	if err != nil {
		if err == repository.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "schedule not found"})
			return
		}
		log.Printf("DeleteSchedule error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete schedule"})
		return
	}

	c.Status(http.StatusNoContent)
}
