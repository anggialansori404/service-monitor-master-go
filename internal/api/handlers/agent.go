package handlers

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"monitoring-master/internal/alert"
	"monitoring-master/internal/models"
	"monitoring-master/internal/repository"

	"github.com/gin-gonic/gin"
)

type AgentHandler struct {
	Repo        repository.Repository
	AlertMgr    *alert.Manager
}

// NewAgentHandler creates a new AgentHandler.
func NewAgentHandler(repo repository.Repository, alertMgr *alert.Manager) *AgentHandler {
	return &AgentHandler{Repo: repo, AlertMgr: alertMgr}
}

// syncRequest is the expected payload for POST /agent/sync.
type syncRequest struct {
	HardwareInfo *models.HardwareInfo `json:"hardware_info" binding:"omitempty"`
	Services     []syncService        `json:"services" binding:"required,dive"`
}

type syncService struct {
	Name        string `json:"name" binding:"required"`
	Type        string `json:"type" binding:"required"`
	Status      string `json:"status" binding:"required,oneof=UP DOWN"`
	AutoRestart bool   `json:"auto_restart"`
}

// Sync handles POST /agent/sync.
// It creates or updates service records for the authenticated institution
// and bumps the institution's LastPing timestamp.
func (h *AgentHandler) Sync(c *gin.Context) {
	// Direct DB Integration (Redis Removed) - Created by Kang Anggi
	institutionID, _ := c.Get("InstitutionID")
	instID := institutionID.(string)

	var req syncRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	agentID := c.GetHeader("X-Agent-ID")
	if agentID == "" {
		agentID = "unknown_agent"
	}
	log.Printf("[SYNC] Received payload from AgentID: %s | Total Services: %d", agentID, len(req.Services))

	now := time.Now().Local()

	// Optimized Bulk Upsert - Created by Kang Anggi
	oldServicesList, _, _ := h.Repo.GetServicesByInstitution(instID, "", "", 1, 5000)
	oldStatusMap := make(map[string]string)
	for _, old := range oldServicesList {
		oldStatusMap[old.Name] = old.Status
	}

	var servicesToUpsert []models.Service
	var statusChanges []models.Service

	for _, svc := range req.Services {
		statusToSave := strings.ToUpper(strings.TrimSpace(svc.Status))
		if statusToSave == "" || statusToSave == "UNKNOWN" {
			statusToSave = "UP"
		}

		service := models.Service{
			InstitutionID: instID,
			Name:          svc.Name,
			Type:          svc.Type,
			Status:        statusToSave,
			AutoRestart:   svc.AutoRestart,
			LastChecked:   &now,
		}
		servicesToUpsert = append(servicesToUpsert, service)

		oldStatus := oldStatusMap[svc.Name]
		if oldStatus != statusToSave && oldStatus != "" && oldStatus != "UNKNOWN" {
			statusChanges = append(statusChanges, service)
		}
	}

	if err := h.Repo.BulkUpsertServices(c.Request.Context(), servicesToUpsert); err != nil {
		log.Printf("[SYNC] Bulk upsert services error (Agent HTTP): %v", err)
	}

	// Cleanup: delete services that are no longer reported by the agent
	newServiceNames := make(map[string]bool, len(req.Services))
	for _, svc := range req.Services {
		newServiceNames[svc.Name] = true
	}
	for _, old := range oldServicesList {
		if !newServiceNames[old.Name] {
			if err := h.Repo.DeleteService(c.Request.Context(), instID, old.Name); err != nil {
				log.Printf("Cleanup: failed to delete stale service %s for institution %s: %v", old.Name, instID, err)
			} else {
				log.Printf("Cleanup: removed stale service %s from institution %s", old.Name, instID)
			}
		}
	}

	for _, svc := range statusChanges {
		msg := fmt.Sprintf("Reported via Sync at %v", now.Format(time.RFC3339))
		if svc.Status == "DOWN" {
			msg = "Service stopped / unreachable"
		}
		h.AlertMgr.AddEvent(instID, svc.Name, svc.Status, msg)

		auditLog := models.AuditLog{
			InstitutionID: instID,
			ServiceName:   svc.Name,
			Action:        "STATE_CHANGE",
			Status:        svc.Status,
			TriggeredBy:   "SYSTEM",
			Details:       fmt.Sprintf("Service reported as %s by Agent", svc.Status),
			Timestamp:     now,
			CreatedAt:     now,
		}
		_ = h.Repo.CreateAuditLog(c.Request.Context(), &auditLog)
	}

	if err := h.Repo.UpdateInstitutionPing(c.Request.Context(), instID, req.HardwareInfo, now); err != nil {
		if errors.Is(err, repository.ErrNotFound) || err.Error() == "record not found" {
			log.Printf("[SYNC] Institution %s was deleted from DB. Aborting sync.", instID)
			c.JSON(http.StatusNotFound, gin.H{"error": "institution not found"})
			return
		}
		log.Printf("[SYNC] Failed to update institution ping for %s: %v", instID, err)
	}

	c.JSON(http.StatusOK, gin.H{"status": "sync processed", "count": len(req.Services)})
}

// alertRequest is the expected payload for POST /agent/alert.
type alertRequest struct {
	Service string `json:"service" binding:"required"`
	Status  string `json:"status" binding:"required,oneof=UP DOWN"`
}

// Alert handles POST /agent/alert.
// It updates the service status and creates an audit log entry.
func (h *AgentHandler) Alert(c *gin.Context) {
	institutionID, _ := c.Get("InstitutionID")
	instID := institutionID.(string)

	var req alertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	now := time.Now().Local()

	// Fetch old status to compare
	oldService, err := h.Repo.GetService(instID, req.Service)
	oldStatus := ""
	if err == nil {
		oldStatus = oldService.Status
	}

	// Update the service status.
	if err := h.Repo.UpdateServiceStatus(c.Request.Context(), instID, req.Service, req.Status, now); err != nil {
		if err == repository.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "service not found"})
			return
		}
		log.Printf("[ALERT] Update service status error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to process alert"})
		return
	}

	// Create audit log entry if status changed.
	if oldStatus != req.Status && oldStatus != "" {
		// Redirect via AlertManager (Phase 4)
		h.AlertMgr.AddEvent(instID, req.Service, req.Status, "Fallback trigger via HTTP Alert")

		auditLog := models.AuditLog{
			InstitutionID: instID,
			ServiceName:   req.Service,
			Status:        req.Status, // UP or DOWN
			TriggeredBy:   "SYSTEM",
			Details:       fmt.Sprintf("Service reported as %s by Agent", req.Status),
			Timestamp:     now,
			CreatedAt:     now,
		}
		if err := h.Repo.CreateAuditLog(c.Request.Context(), &auditLog); err != nil {
			log.Printf("[ALERT] Create audit log error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to process alert"})
			return
		}
	}
	// Update institution's last_ping here too to keep it fresh (no hardware info for alert route).
	if err := h.Repo.UpdateInstitutionPing(c.Request.Context(), instID, nil, now); err != nil {
		if errors.Is(err, repository.ErrNotFound) || err.Error() == "record not found" {
			log.Printf("[ALERT] Institution %s was deleted from DB. Aborting alert.", instID)
			c.JSON(http.StatusNotFound, gin.H{"error": "institution not found"})
			return
		}
		log.Printf("[ALERT] Failed to update institution ping for %s: %v", instID, err)
	}

	c.JSON(http.StatusOK, gin.H{"status": "alert processed"})
}
