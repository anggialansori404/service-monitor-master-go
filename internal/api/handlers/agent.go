package handlers

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"monitoring-master/internal/models"
	"monitoring-master/internal/repository"
	"monitoring-master/internal/services"

	"github.com/gin-gonic/gin"
)

// AgentHandler holds dependencies for agent-facing endpoints.
type AgentHandler struct {
	Repo repository.Repository
}

// NewAgentHandler creates a new AgentHandler.
func NewAgentHandler(repo repository.Repository) *AgentHandler {
	return &AgentHandler{Repo: repo}
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
	institutionID, _ := c.Get("InstitutionID")
	instID := institutionID.(string)

	var req syncRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	now := time.Now().Local()

	for _, svc := range req.Services {
		statusToSave := svc.Status
		if statusToSave == "" || statusToSave == "UNKNOWN" {
			statusToSave = "UP" // Default fallback
		}

		service := models.Service{
			InstitutionID: instID,
			Name:          svc.Name,
			Type:          svc.Type,
			Status:        statusToSave,
			AutoRestart:   svc.AutoRestart,
			LastChecked:   &now,
		}

		// Fetch old status to compare
		oldService, err := h.Repo.GetService(instID, svc.Name)
		oldStatus := ""
		if err == nil {
			oldStatus = oldService.Status
		}

		if err := h.Repo.UpsertService(c.Request.Context(), &service); err != nil {
			log.Printf("MONGODB UPSERT ERROR in Sync: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to sync services"})
			return
		}

		// Create audit log entry if status changed.
		if oldStatus != statusToSave && oldStatus != "" && oldStatus != "UNKNOWN" {
			// Trigger Telegram Alert on status change
			inst, err := h.Repo.GetInstitutionByID(instID)
			if err == nil {
				msg := fmt.Sprintf("⚠️ *SERVICE DOWN*\n\n🏢 *Lembaga:* %s\n⚙️ *Service:* %s\n⏰ *Waktu:* %s\n\n_Agent sedang mencoba auto-restart (jika diaktifkan)._",
					inst.Name, svc.Name, time.Now().Format("02 Jan 2006 15:04:05"))
				if statusToSave == "UP" {
					msg = fmt.Sprintf("✅ *SERVICE RECOVERY*\n\n🏢 *Lembaga:* %s\n⚙️ *Service:* %s\n⏰ *Waktu:* %s",
						inst.Name, svc.Name, time.Now().Format("02 Jan 2006 15:04:05"))
				}
				if setting, err := h.Repo.GetSettings(c.Request.Context()); err == nil && setting.EnableTelegramAlerts {
					services.SendTelegramAlert(setting.TelegramBotToken, setting.TelegramChatID, msg)
				}
			}

			auditLog := models.AuditLog{
				InstitutionID: instID,
				ServiceName:   svc.Name,
				Action:        "STATE_CHANGE",
				Status:        statusToSave,
				TriggeredBy:   "SYSTEM",
				Details:       fmt.Sprintf("Service reported as %s by Agent", statusToSave),
				Timestamp:     now,
				CreatedAt:     now,
			}
			if err := h.Repo.CreateAuditLog(c.Request.Context(), &auditLog); err != nil {
				log.Printf("MONGODB INSERT ERROR (CreateAuditLog) in Sync: %v", err)
			}
		}
	}

	// Update institution's last_ping and hardware info.
	if err := h.Repo.UpdateInstitutionPing(c.Request.Context(), instID, req.HardwareInfo, now); err != nil {
		log.Printf("UPDATE ERROR (UpdateInstitutionPing) in Sync: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to sync services"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "synced", "count": len(req.Services)})
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
		log.Printf("MONGODB UPDATE ERROR (UpdateServiceStatus) in Alert: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to process alert"})
		return
	}

	// Create audit log entry if status changed.
	if oldStatus != req.Status && oldStatus != "" {
		// Trigger Telegram Alert on status change
		inst, err := h.Repo.GetInstitutionByID(instID)
		if err == nil {
			msg := fmt.Sprintf("⚠️ *SERVICE DOWN*\n\n🏢 *Lembaga:* %s\n⚙️ *Service:* %s\n⏰ *Waktu:* %s\n\n_Agent sedang mencoba auto-restart (jika diaktifkan)._",
				inst.Name, req.Service, time.Now().Format("02 Jan 2006 15:04:05"))
			if req.Status == "UP" {
				msg = fmt.Sprintf("✅ *SERVICE RECOVERY*\n\n🏢 *Lembaga:* %s\n⚙️ *Service:* %s\n⏰ *Waktu:* %s",
					inst.Name, req.Service, time.Now().Format("02 Jan 2006 15:04:05"))
			}
			if setting, err := h.Repo.GetSettings(c.Request.Context()); err == nil && setting.EnableTelegramAlerts {
				services.SendTelegramAlert(setting.TelegramBotToken, setting.TelegramChatID, msg)
			}
		}

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
			log.Printf("MONGODB INSERT ERROR (CreateAuditLog) in Alert: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to process alert"})
			return
		}
	}
	// Update institution's last_ping here too to keep it fresh (no hardware info for alert route).
	if err := h.Repo.UpdateInstitutionPing(c.Request.Context(), instID, nil, now); err != nil {
		// Don't fail the request just because ping update failed.
		log.Printf("UPDATE ERROR (UpdateInstitutionPing) in Alert for institution %s: %v", instID, err)
	}

	c.JSON(http.StatusOK, gin.H{"status": "alert processed"})
}
