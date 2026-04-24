package bot

import (
	"fmt"
	"strings"
	"time"

	"context"

	"monitoring-master/internal/models"
	"monitoring-master/internal/repository"
	"monitoring-master/internal/ws"

	tele "gopkg.in/telebot.v3"
)

// MonitoringBot wraps the Telegram bot with application dependencies.
type MonitoringBot struct {
	TBot     *tele.Bot
	Repo     repository.Repository
	Registry *ws.Registry
}

// NewBot creates a new MonitoringBot, configures command handlers, and returns it.
// The caller is responsible for starting the bot via bot.TBot.Start().
func NewBot(token string, repo repository.Repository, registry *ws.Registry) (*MonitoringBot, error) {
	settings := tele.Settings{
		Token:  token,
		Poller: &tele.LongPoller{Timeout: 10 * time.Second},
	}

	b, err := tele.NewBot(settings)
	if err != nil {
		return nil, fmt.Errorf("failed to create telegram bot: %w", err)
	}

	mb := &MonitoringBot{
		TBot:     b,
		Repo:     repo,
		Registry: registry,
	}

	mb.setupRoutes()

	return mb, nil
}

// setupRoutes registers all command handlers on the bot.
func (mb *MonitoringBot) setupRoutes() {
	mb.TBot.Handle("/dashboard", mb.handleDashboard)
	mb.TBot.Handle("/status", mb.handleStatus)
	mb.TBot.Handle("/restart", mb.handleRestart)
}

// handleDashboard shows total institutions online/offline.
func (mb *MonitoringBot) handleDashboard(c tele.Context) error {
	institutions, err := mb.Repo.GetAllInstitutions()
	if err != nil {
		return c.Send("❌ Failed to fetch institutions from database.")
	}

	if len(institutions) == 0 {
		return c.Send("📋 No institutions registered.")
	}

	online := 0
	offline := 0
	for _, inst := range institutions {
		if mb.Registry.IsConnected(inst.AgentID) {
			online++
		} else {
			offline++
		}
	}

	msg := fmt.Sprintf(
		"📊 *Dashboard*\n\n"+
			"Total Institutions: %d\n"+
			"🟢 Online: %d\n"+
			"🔴 Offline: %d",
		len(institutions), online, offline,
	)

	return c.Send(msg, &tele.SendOptions{ParseMode: tele.ModeMarkdown})
}

// handleStatus shows service statuses for a specific institution.
// Usage: /status <institution_name>
func (mb *MonitoringBot) handleStatus(c tele.Context) error {
	payload := strings.TrimSpace(c.Message().Payload)
	if payload == "" {
		return c.Send("⚠️ Usage: /status <institution_name>")
	}

	institution, err := mb.Repo.GetInstitutionByName(payload)
	if err != nil {
		if err == repository.ErrNotFound {
			return c.Send(fmt.Sprintf("❌ Institution '%s' not found.", payload))
		}
		return c.Send("❌ Database error while looking up institution.")
	}

	services, _, err := mb.Repo.GetServicesByInstitution(institution.ID, "", "", 1, 100)

	if err != nil {
		return c.Send("❌ Failed to fetch services.")
	}

	if len(services) == 0 {
		return c.Send(fmt.Sprintf("📋 No services found for '%s'.", institution.Name))
	}

	connected := mb.Registry.IsConnected(institution.AgentID)
	connStatus := "🔴 Offline"
	if connected {
		connStatus = "🟢 Online"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🏢 *%s* (%s)\n\n", institution.Name, connStatus))

	for _, svc := range services {
		statusIcon := "🔴"
		if svc.Status == "UP" {
			statusIcon = "🟢"
		}
		sb.WriteString(fmt.Sprintf("%s %s (%s) — %s\n", statusIcon, svc.Name, svc.Type, svc.Status))
	}

	return c.Send(sb.String(), &tele.SendOptions{ParseMode: tele.ModeMarkdown})
}

// handleRestart triggers a service restart via WebSocket and logs it.
// Usage: /restart <institution_name> <service_name>
func (mb *MonitoringBot) handleRestart(c tele.Context) error {
	payload := strings.TrimSpace(c.Message().Payload)
	parts := strings.SplitN(payload, " ", 2)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return c.Send("⚠️ Usage: /restart <institution_name> <service_name>")
	}

	instName := strings.TrimSpace(parts[0])
	serviceName := strings.TrimSpace(parts[1])

	institution, err := mb.Repo.GetInstitutionByName(instName)
	if err != nil {
		if err == repository.ErrNotFound {
			return c.Send(fmt.Sprintf("❌ Institution '%s' not found.", instName))
		}
		return c.Send("❌ Database error while looking up institution.")
	}

	msg := ws.RestartMessage{
		Action:  "restart",
		Service: serviceName,
	}

	if err := mb.Registry.SendToAgent(institution.AgentID, msg); err != nil {
		return c.Send(fmt.Sprintf("❌ Failed to send restart: %s", err.Error()))
	}

	auditLog := models.AuditLog{
		InstitutionID: institution.ID,
		ServiceName:   serviceName,
		Action:        "RESTART_TRIGGERED",
		Timestamp:     time.Now().Local(),
	}
	if err := mb.Repo.CreateAuditLog(context.TODO(), &auditLog); err != nil {
		return c.Send(fmt.Sprintf(
			"⚠️ Restart command sent for '%s' on '%s', but failed to record audit log.",
			serviceName, instName,
		))
	}

	return c.Send(fmt.Sprintf("✅ Restart command sent for service '%s' on '%s'.", serviceName, instName))
}
