package handlers

import (
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"monitoring-master/internal/models"
	"monitoring-master/internal/repository"
	"monitoring-master/internal/ws"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ScheduleHandler handles CRUD operations for schedules.
type ScheduleHandler struct {
	Repo      repository.Repository
	Registry  *ws.Registry
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

// EnableSchedule handles POST /schedules/:id/enable.
// Sets Enabled=true and returns the updated schedule.
func (h *ScheduleHandler) EnableSchedule(c *gin.Context) {
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
		log.Printf("EnableSchedule fetch error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	schedule.Enabled = true
	schedule.UpdatedAt = time.Now()

	if err := h.Repo.ScheduleRepo().UpdateSchedule(c.Request.Context(), schedule); err != nil {
		log.Printf("EnableSchedule save error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update schedule"})
		return
	}

	c.JSON(http.StatusOK, schedule)
}

// DisableSchedule handles POST /schedules/:id/disable.
// Sets Enabled=false and returns the updated schedule.
func (h *ScheduleHandler) DisableSchedule(c *gin.Context) {
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
		log.Printf("DisableSchedule fetch error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	schedule.Enabled = false
	schedule.UpdatedAt = time.Now()

	if err := h.Repo.ScheduleRepo().UpdateSchedule(c.Request.Context(), schedule); err != nil {
		log.Printf("DisableSchedule save error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update schedule"})
		return
	}

	c.JSON(http.StatusOK, schedule)
}

// TriggerSchedule handles POST /schedules/:id/trigger.
// Sends restart commands to the agent for all services in the schedule
// and records a ScheduleRun with manual trigger.
func (h *ScheduleHandler) TriggerSchedule(c *gin.Context) {
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
		log.Printf("TriggerSchedule fetch error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	institution, err := h.Repo.GetInstitutionByID(schedule.InstitutionID)
	if err != nil {
		if err == repository.ErrNotFound {
			c.JSON(http.StatusBadRequest, gin.H{"error": "institution not found"})
			return
		}
		log.Printf("TriggerSchedule institution fetch error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	results := make([]gin.H, 0, len(schedule.ServiceNames))
	successCount := 0
	failCount := 0

	for _, serviceName := range schedule.ServiceNames {
		result := gin.H{"service": serviceName, "status": "FAILED", "message": ""}

		reqID := fmt.Sprintf("sched-manual-%d", time.Now().UnixNano())

		respChan := make(chan ws.WsResponse)
		h.Registry.PendingRequests.Store(reqID, respChan)
		defer h.Registry.PendingRequests.Delete(reqID)

		payload := map[string]interface{}{
			"request_id": reqID,
			"action":     "restart",
			"service":    serviceName,
		}

		err := h.Registry.SendToAgent(institution.AgentID, payload)
		if err != nil {
			result["message"] = "agent offline: " + err.Error()
			failCount++
			results = append(results, result)
			continue
		}

		select {
		case resp := <-respChan:
			success, ok := resp["success"].(bool)
			if ok && success {
				result["status"] = "SUCCESS"
				successCount++
			} else {
				failCount++
			}
			if msg, ok := resp["message"].(string); ok {
				result["message"] = msg
			}
		case <-time.After(10 * time.Second):
			result["message"] = "timeout waiting for agent response"
			failCount++
		}

		results = append(results, result)
	}

	// Determine overall status.
	status := "SUCCESS"
	if failCount > 0 && successCount == 0 {
		status = "FAILED"
	} else if failCount > 0 {
		status = "PARTIAL"
	}

	// Build result message.
	resultMsg := fmt.Sprintf("restarted %d/%d services (%d failed)", successCount, len(schedule.ServiceNames), failCount)

	now := time.Now()
	run := models.ScheduleRun{
		ID:            uuid.New().String(),
		ScheduleID:    schedule.ID,
		ScheduleName:  schedule.Name,
		InstitutionID: schedule.InstitutionID,
		TriggeredBy:   "MANUAL",
		ServiceNames:  schedule.ServiceNames,
		Status:        status,
		ResultMessage: resultMsg,
		RunAt:         now,
		CatchUp:       false,
		CreatedAt:     now,
	}

	if err := h.Repo.ScheduleRepo().CreateScheduleRun(c.Request.Context(), &run); err != nil {
		log.Printf("TriggerSchedule CreateScheduleRun error: %v", err)
		// Don't fail the response — the restart already happened.
	}

	c.JSON(http.StatusOK, gin.H{
		"schedule_id":   schedule.ID,
		"schedule_name": schedule.Name,
		"triggered_by":  "MANUAL",
		"status":        status,
		"message":       resultMsg,
		"results":       results,
	})
}

// ListScheduleRuns handles GET /schedules/:id/runs.
// Returns paginated run history for a schedule.
func (h *ScheduleHandler) ListScheduleRuns(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "schedule id is required"})
		return
	}

	page := 1
	if p := c.Query("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			page = v
		}
	}

	limit := 20
	if l := c.Query("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}

	runs, total, err := h.Repo.ScheduleRepo().ListScheduleRuns(c.Request.Context(), id, page, limit)
	if err != nil {
		log.Printf("ListScheduleRuns error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch schedule runs"})
		return
	}

	if runs == nil {
		runs = []models.ScheduleRun{}
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  runs,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}
