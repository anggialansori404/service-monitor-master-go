package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"monitoring-master/internal/alert"
	"monitoring-master/internal/logutil"
	"monitoring-master/internal/models"
	"monitoring-master/internal/repository"
	"monitoring-master/internal/services"
	"monitoring-master/internal/ws"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: checkWSOrigin,
}

// allowedWSOrigins is an optional whitelist of origins (scheme://host) allowed
// to open WebSocket connections, configured via ALLOWED_ORIGINS.
// When empty, only same-origin browser requests (Origin host == request host)
// and non-browser clients (no Origin header, e.g. the Go agent) are allowed.
var allowedWSOrigins []string

// SetAllowedWSOrigins configures the WebSocket origin whitelist.
// Call once at startup from main.
func SetAllowedWSOrigins(origins []string) {
	allowedWSOrigins = origins
}

func checkWSOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		// Non-browser clients (Go agent) don't send Origin.
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	// Same-origin (dashboard served by this server).
	if u.Host == r.Host {
		return true
	}
	for _, o := range allowedWSOrigins {
		if strings.EqualFold(strings.TrimSpace(o), origin) {
			return true
		}
	}
	log.Printf("WARNING: rejected WebSocket upgrade from disallowed Origin %q (host %s)", origin, r.Host)
	return false
}

// nodeState tracks disconnect, grace period, and flapping state for one institution.
type nodeState struct {
	disconnectTimer    *time.Timer
	disconnectAt       time.Time
	recentDisconnects  []time.Time
	flappingMutedUntil time.Time
	alertSent          bool
	disconnectMsgIDs   []alert.TargetMessageID
}

// WSHandler holds dependencies for WebSocket endpoints.
type WSHandler struct {
	Registry          *ws.Registry
	Repo              repository.Repository
	AlertMgr          *alert.Manager
	nodeStates        map[string]*nodeState
	mu                sync.Mutex
	gracePeriod       time.Duration
	flappingWindow    time.Duration
	flappingThreshold int
	flappingCooldown  time.Duration
	sendAlertFunc     func(token, chatID, topicID, text string) (int, error)
	editAlertFunc     func(token, chatID, topicID string, messageID int, text string) error
}

// NewWSHandler creates a new WSHandler.
func NewWSHandler(registry *ws.Registry, repo repository.Repository, alertMgr *alert.Manager) *WSHandler {
	return &WSHandler{
		Registry:          registry,
		Repo:              repo,
		AlertMgr:          alertMgr,
		nodeStates:        make(map[string]*nodeState),
		gracePeriod:       2 * time.Minute,
		flappingWindow:    10 * time.Minute,
		flappingThreshold: 3,
		flappingCooldown:  15 * time.Minute,
		sendAlertFunc:     services.SendTelegramAlert,
		editAlertFunc:     services.EditTelegramMessage,
	}
}

// SetTimingConfig overrides timing parameters for testing or configuration.
func (h *WSHandler) SetTimingConfig(gracePeriod, flappingWindow, flappingCooldown time.Duration, flappingThreshold int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.gracePeriod = gracePeriod
	h.flappingWindow = flappingWindow
	h.flappingCooldown = flappingCooldown
	h.flappingThreshold = flappingThreshold
}

// SetAlertHooks overrides telegram send and edit functions for testing.
func (h *WSHandler) SetAlertHooks(
	send func(token, chatID, topicID, text string) (int, error),
	edit func(token, chatID, topicID string, messageID int, text string) error,
) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sendAlertFunc = send
	h.editAlertFunc = edit
}

func formatDowntime(d time.Duration) string {
	d = d.Round(time.Second)
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	if hours > 0 {
		return fmt.Sprintf("%d jam %d menit %d detik", hours, minutes, seconds)
	}
	if minutes > 0 {
		return fmt.Sprintf("%d menit %d detik", minutes, seconds)
	}
	return fmt.Sprintf("%d detik", seconds)
}

// CleanupInstitution cleans up all in-memory states, timers, and active connections for an institution.
func (h *WSHandler) CleanupInstitution(instID, agentID string) {
	if agentID != "" && h.Registry != nil {
		if client := h.Registry.GetClient(agentID); client != nil {
			_ = client.CloseWithReason(websocket.ClosePolicyViolation, "institution deleted")
		}
		h.Registry.Unregister(agentID)
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if state, exists := h.nodeStates[instID]; exists {
		if state.disconnectTimer != nil {
			state.disconnectTimer.Stop()
			state.disconnectTimer = nil
		}
		delete(h.nodeStates, instID)
	}
	log.Printf("[NODE-WS] Cleaned up state and disconnected WS for deleted institution %s (agent %s)", instID, agentID)
}

// OnAgentConnected handles node recovery logic.
func (h *WSHandler) OnAgentConnected(institution *models.Institution) {
	instID := institution.ID
	h.mu.Lock()
	state, exists := h.nodeStates[instID]
	if !exists {
		state = &nodeState{}
		h.nodeStates[instID] = state
	}

	// 1. Cancel pending disconnect timer if agent reconnected within grace period (silent reconnect)
	if state.disconnectTimer != nil {
		state.disconnectTimer.Stop()
		state.disconnectTimer = nil
		log.Printf("[NODE-WS] Agent %s reconnected within grace period. Disconnect alert canceled.", institution.Name)
	}

	// 2. If alert was already sent to Telegram (offline > grace period), edit messages to recovered with downtime duration
	if state.alertSent && len(state.disconnectMsgIDs) > 0 {
		downtime := time.Since(state.disconnectAt)
		durationStr := formatDowntime(downtime)
		recoveryMsg := fmt.Sprintf("✅ *NODE RECOVERED*\n\nNode Agent kembali tersambung!\n🏢 *Lembaga:* %s\n⏱️ *Durasi Offline:* %s\n⏰ *Waktu:* %s\n\n_Sistem kembali normal._",
			institution.Name, durationStr, time.Now().Format("02 Jan 2006 15:04:05"))

		for _, item := range state.disconnectMsgIDs {
			if item.MessageID == 0 {
				continue
			}
			if err := h.editAlertFunc(item.Target.Token, item.Target.ChatID, item.Target.TopicID, item.MessageID, recoveryMsg); err != nil {
				log.Printf("Failed to edit disconnect alert for %s (msgID %d): %v", institution.Name, item.MessageID, err)
			}
		}
		state.disconnectMsgIDs = nil
		state.alertSent = false

		// Resolve active node_offline incident in DB
		_, _ = h.Repo.ResolveActiveIncident(context.Background(), instID, "node-agent", "node_offline", time.Now(), false)
	} else if !state.alertSent {
		// Silent reconnect: resolve any stale incident record
		_, _ = h.Repo.ResolveActiveIncident(context.Background(), instID, "node-agent", "node_offline", time.Now(), false)
	}
	h.mu.Unlock()
}

// OnAgentDisconnected handles node disconnect, grace period, and flapping detection.
func (h *WSHandler) OnAgentDisconnected(institution *models.Institution, client ...*ws.Client) {
	agentID := institution.AgentID
	instID := institution.ID
	var c *ws.Client
	if len(client) > 0 {
		c = client[0]
	}
	h.Registry.UnregisterClient(agentID, c)
	log.Printf("agent disconnected: %s (institution: %s)", agentID, institution.Name)

	// Refresh institution in case settings or maintenance mode changed while agent was connected
	freshInst, err := h.Repo.GetInstitutionByID(instID)
	if err != nil || freshInst == nil {
		log.Printf("[NODE-WS] Institution %s (%s) not found in DB (deleted). Skipping offline alerts and cleaning up.", institution.Name, instID)
		h.mu.Lock()
		if state, exists := h.nodeStates[instID]; exists {
			if state.disconnectTimer != nil {
				state.disconnectTimer.Stop()
				state.disconnectTimer = nil
			}
			delete(h.nodeStates, instID)
		}
		h.mu.Unlock()
		return
	}
	institution = freshInst

	// Check if node offline alerts are disabled for this institution
	if !institution.NotifyNodeOffline {
		log.Printf("[NODE-WS] Node offline notifications disabled for %s. Skipping alerts.", institution.Name)
		return
	}

	// Check if in maintenance mode
	inMaintenance := institution.InMaintenance && (institution.MaintenanceUntil == nil || time.Now().Before(*institution.MaintenanceUntil))
	if inMaintenance {
		log.Printf("[NODE-WS] Institution %s in maintenance. Skipping node offline alert.", institution.Name)
		return
	}

	now := time.Now()

	h.mu.Lock()
	state, exists := h.nodeStates[instID]
	if !exists {
		state = &nodeState{}
		h.nodeStates[instID] = state
	}

	// Clean up recent disconnects outside the flapping window
	windowStart := now.Add(-h.flappingWindow)
	var filtered []time.Time
	for _, t := range state.recentDisconnects {
		if t.After(windowStart) {
			filtered = append(filtered, t)
		}
	}
	filtered = append(filtered, now)
	state.recentDisconnects = filtered
	state.disconnectAt = now
	state.alertSent = false

	// Check if currently muted due to flapping
	if now.Before(state.flappingMutedUntil) {
		log.Printf("[NODE-WS] Institution %s is flapping-muted until %s. Suppressing alert.", institution.Name, state.flappingMutedUntil.Format(time.RFC3339))
		h.mu.Unlock()
		return
	}

	// Check if flapping threshold reached
	if len(state.recentDisconnects) >= h.flappingThreshold {
		state.flappingMutedUntil = now.Add(h.flappingCooldown)
		cooldownMins := int(h.flappingCooldown.Minutes())
		windowMins := int(h.flappingWindow.Minutes())
		count := len(state.recentDisconnects)

		flappingMsg := fmt.Sprintf("⚠️ *KONEKSI NODE TIDAK STABIL (FLAPPING)*\n\n🏢 *Lembaga:* %s\n📊 *Aktivitas:* Terputus %d kali dalam %d menit terakhir.\n⏳ *Tindakan:* Notifikasi offline untuk lembaga ini *diredam selama %d menit* guna mencegah spam.\n\n_Koneksi internet di server lembaga kemungkinan mengalami fluktuasi/jitter._",
			institution.Name, count, windowMins, cooldownMins)

		// Cancel any pending grace period timer
		if state.disconnectTimer != nil {
			state.disconnectTimer.Stop()
			state.disconnectTimer = nil
		}
		h.mu.Unlock()

		// Send 1x flapping warning notification
		setting, _ := h.Repo.GetSettings(context.Background())
		targets := alert.ResolveTelegramTargets(institution, setting)
		for _, target := range targets {
			if _, err := h.sendAlertFunc(target.Token, target.ChatID, target.TopicID, flappingMsg); err != nil {
				log.Printf("Failed to send flapping alert for %s to target %+v: %v", institution.Name, target, err)
			}
		}
		return
	}

	// Cancel any previous timer
	if state.disconnectTimer != nil {
		state.disconnectTimer.Stop()
	}

	// Start Grace Period Timer (e.g. 2 minutes)
	state.disconnectTimer = time.AfterFunc(h.gracePeriod, func() {
		h.handleGracePeriodExpired(instID)
	})
	h.mu.Unlock()

	log.Printf("[NODE-WS] Grace period (%v) started for %s disconnect.", h.gracePeriod, institution.Name)
}

func (h *WSHandler) handleGracePeriodExpired(instID string) {
	h.mu.Lock()
	state, exists := h.nodeStates[instID]
	if !exists {
		h.mu.Unlock()
		return
	}

	// Re-check if muted
	if time.Now().Before(state.flappingMutedUntil) {
		h.mu.Unlock()
		return
	}

	disconnectAt := state.disconnectAt
	state.alertSent = true
	h.mu.Unlock()

	institution, err := h.Repo.GetInstitutionByID(instID)
	if err != nil || institution == nil {
		log.Printf("[NODE-WS] GracePeriodExpired: institution %s not found: %v", instID, err)
		return
	}

	// Check if already reconnected
	if h.Registry.IsConnected(institution.AgentID) {
		log.Printf("[NODE-WS] GracePeriodExpired: agent %s is already connected.", institution.AgentID)
		return
	}

	// Re-check NotifyNodeOffline and Maintenance
	if !institution.NotifyNodeOffline {
		return
	}
	if institution.InMaintenance && (institution.MaintenanceUntil == nil || time.Now().Before(*institution.MaintenanceUntil)) {
		return
	}

	// Record node_offline incident
	_ = h.Repo.CreateIncident(context.Background(), &models.IncidentLog{
		InstitutionID: instID,
		ServiceName:   "node-agent",
		Type:          "node_offline",
		StartedAt:     disconnectAt,
		Cause:         fmt.Sprintf("Agent disconnected (offline > %v)", h.gracePeriod),
		CreatedAt:     time.Now(),
	})

	// Send CRITICAL ALERT to Telegram
	alertMsg := fmt.Sprintf("🔴 *CRITICAL ALERT*\n\nNode Agent terputus (Offline)!\n🏢 *Lembaga:* %s\n⏰ *Waktu Terputus:* %s\n\n_Node tidak merespons lebih dari %v. Mohon segera periksa server/VPS lembaga terkait._",
		institution.Name, disconnectAt.Format("02 Jan 2006 15:04:05"), h.gracePeriod)

	setting, _ := h.Repo.GetSettings(context.Background())
	targets := alert.ResolveTelegramTargets(institution, setting)

	var sentMsgs []alert.TargetMessageID
	for _, target := range targets {
		msgID, sendErr := h.sendAlertFunc(target.Token, target.ChatID, target.TopicID, alertMsg)
		if sendErr != nil {
			log.Printf("Failed to send disconnect alert for %s to target %+v: %v", institution.Name, target, sendErr)
		} else if msgID != 0 {
			sentMsgs = append(sentMsgs, alert.TargetMessageID{
				Target:    target,
				MessageID: msgID,
			})
			log.Printf("Saved disconnect message_id %d for institution %s", msgID, institution.Name)
		}
	}

	h.mu.Lock()
	if s, ok := h.nodeStates[instID]; ok && s.alertSent {
		s.disconnectMsgIDs = append(s.disconnectMsgIDs, sentMsgs...)
	}
	h.mu.Unlock()
}

// wsResponse is used to detect response messages from agents.
type wsResponse struct {
	ReqID  string `json:"req_id"`
	Status string `json:"status"`
	Action string `json:"action"`
}

// AgentWS handles GET /ws/agent.
// Upgrades the connection to WebSocket, registers the agent in the Registry,
// and blocks reading messages until the client disconnects.
func (h *WSHandler) AgentWS(c *gin.Context) {
	institutionVal, exists := c.Get("Institution")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing institution context"})
		return
	}
	institution := institutionVal.(*models.Institution)
	agentID := institution.AgentID
	instID := institution.ID

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("websocket upgrade failed for agent %s: %v", agentID, err)
		return
	}

	// Configure Heartbeat and payload limits
	conn.SetReadLimit(ws.MaxMessageSize)
	_ = conn.SetReadDeadline(time.Now().Add(ws.PongWait))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(ws.PongWait))
		return nil
	})
	conn.SetPingHandler(func(appData string) error {
		_ = conn.SetReadDeadline(time.Now().Add(ws.PongWait))
		return conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(10*time.Second))
	})

	client := ws.NewClient(agentID, conn)
	h.Registry.Register(client)
	go client.StartWritePump()
	log.Printf("agent connected: %s (institution: %s)", agentID, institution.Name)

	h.OnAgentConnected(institution)

	defer func() {
		// Only trigger disconnect if this client was not superseded by a newer connection
		if h.Registry.IsCurrentClient(agentID, client) {
			h.OnAgentDisconnected(institution, client)
		} else {
			log.Printf("[NODE-WS] Agent %s (%s) connection closed, but superseded by newer connection. Skipping disconnect alert.", institution.Name, agentID)
		}
	}()

	for {
		_, rawMsg, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WS unexpected close for agent %s: %v", agentID, err)
			} else {
				log.Printf("WS read error/timeout for agent %s: %v", agentID, err)
			}
			break
		}

		// Reset read deadline on receiving incoming message
		_ = conn.SetReadDeadline(time.Now().Add(ws.PongWait))

		var msgData map[string]interface{}
		if err := json.Unmarshal(rawMsg, &msgData); err != nil {
			log.Printf("WS Unmarshal error for agent %s: %v (raw payload size: %d bytes)", agentID, err, len(rawMsg))
			continue
		}

		// Try to extract request_id (handle both snake_case forms just in case)
		var reqID string
		if id, ok := msgData["request_id"].(string); ok && id != "" {
			reqID = id
		} else if id, ok := msgData["req_id"].(string); ok && id != "" {
			reqID = id
		}

		var action string
		if act, ok := msgData["action"].(string); ok {
			action = act
		}

		if action != "sync" {
			logutil.Debugf("WS MSG from agent %s: %+v", agentID, msgData)
		}

		// Phase 3 & 4: Seamless WS Streaming for Sync and Alert
		if action == "sync" {
			// Direct DB Integration (Redis Removed) - Created by Kang Anggi
			var req struct {
				HardwareInfo *models.HardwareInfo `json:"hardware_info"`
				Services     []struct {
					Name        string `json:"name"`
					Type        string `json:"type"`
					Status      string `json:"status"`
					AutoRestart bool   `json:"auto_restart"`
				} `json:"services"`
			}
			if err := json.Unmarshal(rawMsg, &req); err != nil {
				log.Printf("WS sync unmarshal error for agent %s: %v", agentID, err)
				continue
			}

			log.Printf("[SYNC] Received payload from AgentID: %s | Total Services: %d", agentID, len(req.Services))

			now := time.Now().Local()

			// Check if institution still exists in DB by updating ping first
			if err := h.Repo.UpdateInstitutionPing(context.Background(), instID, req.HardwareInfo, now); err != nil {
				if errors.Is(err, repository.ErrNotFound) || err.Error() == "record not found" {
					log.Printf("[NODE-WS] Institution %s (%s) was deleted from DB. Closing WS connection for agent %s.", institution.Name, instID, agentID)
					_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "institution deleted"))
					break
				}
				log.Printf("[SYNC] Failed to update institution ping for %s: %v", instID, err)
			}
			
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

			if err := h.Repo.BulkUpsertServices(context.Background(), servicesToUpsert); err != nil {
				log.Printf("[SYNC] Bulk upsert services error (Agent WS): %v", err)
			}

			// Cleanup: delete services that are no longer reported by the agent
			newServiceNames := make(map[string]bool, len(req.Services))
			for _, svc := range req.Services {
				newServiceNames[svc.Name] = true
			}
			for _, old := range oldServicesList {
				if !newServiceNames[old.Name] {
					if err := h.Repo.DeleteService(context.Background(), instID, old.Name); err != nil {
						log.Printf("Cleanup: failed to delete stale service %s for institution %s: %v", old.Name, instID, err)
					} else {
						log.Printf("Cleanup: removed stale service %s from institution %s", old.Name, instID)
					}
				}
			}

			for _, svc := range statusChanges {
				msg := fmt.Sprintf("Reported via WS Sync at %v", now.Format(time.RFC3339))
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
				_ = h.Repo.CreateAuditLog(context.Background(), &auditLog)
			}

			continue
		}

		if action == "alert" {
			svcName, _ := msgData["service"].(string)
			status, _ := msgData["status"].(string)
			msgStr, _ := msgData["message"].(string)
			if strings.TrimSpace(msgStr) == "" {
				if strings.ToUpper(strings.TrimSpace(status)) == "DOWN" {
					msgStr = "Service stopped / unreachable"
				} else {
					msgStr = "Service recovered"
				}
			}

			h.AlertMgr.AddEvent(instID, svcName, status, msgStr)

			// Update service status in DB
			now := time.Now().Local()
			statusUpper := strings.ToUpper(strings.TrimSpace(status))
			if statusUpper != "" {
				if err := h.Repo.UpdateServiceStatus(context.Background(), instID, svcName, statusUpper, now); err != nil {
					if errors.Is(err, repository.ErrNotFound) || err.Error() == "record not found" {
						log.Printf("[NODE-WS] Institution %s (%s) was deleted from DB. Closing WS connection for agent %s.", institution.Name, instID, agentID)
						_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "institution deleted"))
						break
					}
					log.Printf("WS Alert: failed to update service status for %s/%s: %v", instID, svcName, err)
				}
			}
			continue
		}

		if reqID != "" {
			// Fallback for old Client.Pending if still used somewhere
			client.ResolvePending(reqID, rawMsg)

			// Check if action is restart_result
			if action == "restart_result" {
				chVal, ok := h.Registry.PendingRequests.Load(reqID)
				if ok {
					if ch, valid := chVal.(chan ws.WsResponse); valid {
						// We can directly send msgData since it's a map[string]interface{}
						// which matches ws.WsResponse
						select {
						case ch <- msgData:
							log.Printf("Forwarded WS response to pending request %s", reqID)
						default:
							log.Printf("Failed to forward WS response to pending request %s: channel blocked", reqID)
						}
					}
				} else {
					log.Printf("No pending request found for ID: %s", reqID)
				}
			}
		}
	}
}
