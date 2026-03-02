package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"monitoring-master/internal/models"
	"monitoring-master/internal/repository"
	"monitoring-master/internal/services"
	"monitoring-master/internal/ws"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// WSHandler holds dependencies for WebSocket endpoints.
type WSHandler struct {
	Registry *ws.Registry
	Repo     repository.Repository
}

// NewWSHandler creates a new WSHandler.
func NewWSHandler(registry *ws.Registry, repo repository.Repository) *WSHandler {
	return &WSHandler{Registry: registry, Repo: repo}
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

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("websocket upgrade failed for agent %s: %v", agentID, err)
		return
	}

	client := ws.NewClient(agentID, conn)
	h.Registry.Register(client)
	log.Printf("agent connected: %s (institution: %s)", agentID, institution.Name)

	defer func() {
		h.Registry.Unregister(agentID)
		log.Printf("agent disconnected: %s (institution: %s)", agentID, institution.Name)

		// TRIGGER TELEGRAM ALERT
		alertMsg := fmt.Sprintf("🔴 *CRITICAL ALERT*\n\nNode Agent terputus (Offline)!\n🏢 *Lembaga:* %s\n⏰ *Waktu:* %s\n\n_Mohon segera periksa server/VPS lembaga terkait._",
			institution.Name, time.Now().Format("02 Jan 2006 15:04:05"))

		if setting, err := h.Repo.GetSettings(context.Background()); err == nil && setting.EnableTelegramAlerts {
			services.SendTelegramAlert(setting.TelegramBotToken, setting.TelegramChatID, setting.TelegramTopicID, alertMsg)
		}
	}()

	for {
		_, rawMsg, err := conn.ReadMessage()
		if err != nil {
			log.Printf("WS read error for agent %s: %v", agentID, err)
			break
		}

		var msgData map[string]interface{}
		if err := json.Unmarshal(rawMsg, &msgData); err != nil {
			log.Printf("WS Unmarshal error for agent %s: %v", agentID, err)
			continue
		}

		log.Printf("DEBUG WS MSG from agent %s: %+v", agentID, msgData)

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
