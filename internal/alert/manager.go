package alert

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"monitoring-master/internal/models"
	"monitoring-master/internal/repository"
	"monitoring-master/internal/services"
)

// WebSocket Streaming & Alert Deduplication (Prop 3 & 4) - Created by Kang Anggi

// AlertEvent represents a single status change from an agent
type AlertEvent struct {
	InstitutionID string
	ServiceName   string
	Status        string // "UP" or "DOWN"
	Message       string
	Timestamp     time.Time
}

// BatchState tracks an active Telegram batch alert for one institution.
// Used to edit the batch message when services recover.
type BatchState struct {
	SentMessages []TargetMessageID
	DownService  map[string]AlertEvent // key: serviceName
}

// Manager handles alert deduplication and batched sending
type Manager struct {
	repo         repository.Repository
	batchStore   repository.BatchStateStore // nil disables persistence (in-memory only)
	EventChan    chan AlertEvent
	debounceMap  map[string]*time.Timer
	pendingDowns map[string]AlertEvent // key: instID_serviceName
	batchStates  map[string]*BatchState // instID -> active batch message
	mu           sync.Mutex
	debounceTime time.Duration
}

// NewManager creates a new AlertManager
func NewManager(repo repository.Repository, debounceTime time.Duration) *Manager {
	if debounceTime == 0 {
		debounceTime = 2 * time.Minute
	}
	store, ok := repo.(repository.BatchStateStore)
	if !ok {
		log.Println("AlertEngine: repository has no batch state store, batch alerts will not survive a restart")
	}
	return &Manager{
		repo:         repo,
		batchStore:   store,
		EventChan:    make(chan AlertEvent, 1000),
		debounceMap:  make(map[string]*time.Timer),
		pendingDowns: make(map[string]AlertEvent),
		batchStates:  make(map[string]*BatchState),
		debounceTime: debounceTime,
	}
}

// displayMap fetches all services for one institution once and maps
// service name -> display label (DisplayName fallback Name).
// Avoids N+1 GetService calls when rendering batch lists.
func (m *Manager) displayMap(ctx context.Context, instID string) map[string]string {
	names := map[string]string{}
	list, _, err := m.repo.GetServicesByInstitution(instID, "", "", 1, 5000)
	if err != nil {
		return names
	}
	for _, s := range list {
		if s.DisplayName != "" {
			names[s.Name] = s.DisplayName
		} else {
			names[s.Name] = s.Name
		}
	}
	return names
}

// label resolves display label from pre-fetched map, fallback to raw name.
func label(dm map[string]string, name string) string {
	if v, ok := dm[name]; ok && v != "" {
		return v
	}
	return name
}
// GetBatchState returns a copy of active batch state for an institution.
func (m *Manager) GetBatchState(instID string) *BatchState {
	m.mu.Lock()
	defer m.mu.Unlock()
	bs, ok := m.batchStates[instID]
	if !ok || bs == nil {
		return nil
	}
	res := &BatchState{
		SentMessages: append([]TargetMessageID(nil), bs.SentMessages...),
		DownService:  make(map[string]AlertEvent),
	}
	for k, v := range bs.DownService {
		res.DownService[k] = v
	}
	return res
}

// hydrateBatch loads the persisted batch state of an institution into memory
// when this process has none yet (e.g. right after a master restart).
// Telegram targets are re-resolved from current settings so a rotated bot token
// still edits the old message; targets that no longer exist are dropped.
func (m *Manager) hydrateBatch(ctx context.Context, instID string) {
	m.mu.Lock()
	_, cached := m.batchStates[instID]
	m.mu.Unlock()
	if cached {
		return
	}

	if m.batchStore == nil {
		return
	}
	stored, err := m.batchStore.GetAlertBatchState(ctx, instID)
	if err != nil {
		log.Printf("AlertEngine: failed to load persisted batch state for %s: %v", instID, err)
		return
	}
	if stored == nil || len(stored.Messages) == 0 {
		return
	}

	inst, err := m.repo.GetInstitutionByID(instID)
	if err != nil || inst == nil {
		return
	}
	setting, _ := m.repo.GetSettings(ctx)
	byKey := make(map[string]TelegramTarget)
	for _, t := range ResolveTelegramTargets(inst, setting) {
		byKey[targetKey(t.ChatID, t.TopicID, t.IsGlobal)] = t
	}

	var msgs []TargetMessageID
	for _, msg := range stored.Messages {
		target, ok := byKey[targetKey(msg.ChatID, msg.TopicID, msg.IsGlobal)]
		if !ok {
			continue
		}
		msgs = append(msgs, TargetMessageID{Target: target, MessageID: msg.MessageID})
	}
	if len(msgs) == 0 {
		_ = m.batchStore.DeleteAlertBatchState(ctx, instID)
		return
	}

	down := make(map[string]AlertEvent, len(stored.DownServices))
	for _, name := range stored.DownServices {
		down[name] = AlertEvent{InstitutionID: instID, ServiceName: name, Status: "DOWN", Timestamp: stored.UpdatedAt}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.batchStates[instID]; exists {
		return
	}
	m.batchStates[instID] = &BatchState{SentMessages: msgs, DownService: down}
	log.Printf("AlertEngine: restored batch state for %s (%d services, %d messages) from DB", inst.Name, len(down), len(msgs))
}

// targetKey identifies a Telegram destination without its (rotatable) token.
func targetKey(chatID, topicID string, isGlobal bool) string {
	return fmt.Sprintf("%s|%s|%t", chatID, topicID, isGlobal)
}

// persistBatch writes the current in-memory batch state of an institution to the DB.
func (m *Manager) persistBatch(ctx context.Context, instID string) {
	m.mu.Lock()
	bs := m.batchStates[instID]
	if bs == nil {
		m.mu.Unlock()
		if m.batchStore != nil {
			_ = m.batchStore.DeleteAlertBatchState(ctx, instID)
		}
		return
	}
	state := &models.AlertBatchState{InstitutionID: instID}
	for _, sent := range bs.SentMessages {
		state.Messages = append(state.Messages, models.AlertBatchMessage{
			ChatID:    sent.Target.ChatID,
			TopicID:   sent.Target.TopicID,
			IsGlobal:  sent.Target.IsGlobal,
			MessageID: sent.MessageID,
		})
	}
	for name := range bs.DownService {
		state.DownServices = append(state.DownServices, name)
	}
	m.mu.Unlock()

	sort.Strings(state.DownServices)
	if m.batchStore == nil {
		return
	}
	if err := m.batchStore.SaveAlertBatchState(ctx, state); err != nil {
		log.Printf("AlertEngine: failed to persist batch state for %s: %v", instID, err)
	}
}

// Start begins processing the alert channel
func (m *Manager) Start(ctx context.Context) {
	log.Println("Starting Alert Deduplication Engine...")
	for {
		select {
		case <-ctx.Done():
			log.Println("AlertEngine stopped.")
			return
		case event := <-m.EventChan:
			m.handleEvent(ctx, event)
		}
	}
}

// AddEvent safely adds an event to the processing queue
func (m *Manager) AddEvent(instID, serviceName, status, message string) {
	m.EventChan <- AlertEvent{
		InstitutionID: instID,
		ServiceName:   serviceName,
		Status:        status,
		Message:       message,
		Timestamp:     time.Now().Local(),
	}
}

func (m *Manager) handleEvent(ctx context.Context, event AlertEvent) {
	key := fmt.Sprintf("%s_%s", event.InstitutionID, event.ServiceName)

	switch event.Status {
	case "DOWN":
		m.handleDown(ctx, key, event)

	case "UP":
		m.handleUp(ctx, key, event)
	}
}

func (m *Manager) handleDown(ctx context.Context, key string, event AlertEvent) {
	// 1. Immediately record incident if not in maintenance and not already active
	inst, err := m.repo.GetInstitutionByID(event.InstitutionID)
	inMaintenance := false
	if err == nil && inst != nil {
		if inst.InMaintenance && (inst.MaintenanceUntil == nil || time.Now().Before(*inst.MaintenanceUntil)) {
			inMaintenance = true
		}
	}

	if !inMaintenance {
		active, _ := m.repo.GetActiveIncident(ctx, event.InstitutionID, event.ServiceName, "service_down")
		if active == nil {
			causeMsg := sanitizeCauseMessage(event.Message)
			_ = m.repo.CreateIncident(ctx, &models.IncidentLog{
				InstitutionID: event.InstitutionID,
				ServiceName:   event.ServiceName,
				Type:          "service_down",
				StartedAt:     event.Timestamp,
				Cause:         causeMsg,
				CreatedAt:     time.Now(),
			})
			log.Printf("Recorded service_down incident for %s (institution: %s, cause: %s)", event.ServiceName, event.InstitutionID, causeMsg)
		}
	}

	// 2. Telegram Alert Deduplication / Debounce
	// Restore any batch that was active before a master restart.
	m.hydrateBatch(ctx, event.InstitutionID)

	m.mu.Lock()
	defer m.mu.Unlock()

	// Service already listed in the active batch message: refresh the tracked
	// event and stay silent. The agent re-reports DOWN on every failed
	// auto-restart cycle, which must not fan out into new Telegram messages.
	if bs, ok := m.batchStates[event.InstitutionID]; ok && bs != nil {
		if _, tracked := bs.DownService[event.ServiceName]; tracked {
			bs.DownService[event.ServiceName] = event
			if t, pending := m.debounceMap[key]; pending {
				t.Stop()
				delete(m.debounceMap, key)
				delete(m.pendingDowns, key)
			}
			return
		}
	}

	// If a timer already exists, do NOT reset it. Just update the pending message.
	if _, ok := m.debounceMap[key]; ok {
		m.pendingDowns[key] = event
		return
	}

	// Store it in pending and start a timer
	m.pendingDowns[key] = event

	timer := time.AfterFunc(m.debounceTime, func() {
		m.processTimeout(context.Background(), key)
	})
	m.debounceMap[key] = timer
}

func (m *Manager) handleUp(ctx context.Context, key string, event AlertEvent) {
	instID := event.InstitutionID

	m.hydrateBatch(ctx, instID)

	m.mu.Lock()

	// Case 1: Still in debounce window — flap mitigation for Telegram
	timerActive := false
	if t, ok := m.debounceMap[key]; ok {
		timerActive = true
		t.Stop()
		delete(m.debounceMap, key)
		delete(m.pendingDowns, key)
	}

	// Case 2: Check active batch
	bs, hasBatch := m.batchStates[instID]
	inBatch := false
	if hasBatch && bs != nil {
		if _, ok := bs.DownService[event.ServiceName]; ok {
			inBatch = true
		}
	}

	// Clean up pendingDowns if still there (edge case)
	delete(m.pendingDowns, key)

	m.mu.Unlock()

	// Determine if resolved by auto (e.g. recovered within debounce window or agent reported auto-restart)
	resolvedByAuto := timerActive || strings.Contains(strings.ToLower(event.Message), "auto-restart")

	// ALWAYS resolve open incident for this service
	resolvedInc, _ := m.repo.ResolveActiveIncident(ctx, event.InstitutionID, event.ServiceName, "service_down", event.Timestamp, resolvedByAuto)
	if resolvedInc != nil {
		log.Printf("Resolved service_down incident for %s (inst: %s, duration: %ds, auto: %v)",
			event.ServiceName, event.InstitutionID, resolvedInc.DurationSec, resolvedByAuto)
	}

	if inBatch {
		// Edit the existing batch Telegram message even if timerActive was true
		m.editBatchOnRecovery(ctx, instID, bs, event)
		return
	}

	if timerActive {
		log.Printf("Flap detected and mitigated for %s: it recovered within %v (Telegram notification suppressed)", key, m.debounceTime)
		return
	}

	if hasBatch {
		// Edit the existing batch Telegram message
		m.editBatchOnRecovery(ctx, instID, bs, event)
	} else {
		// No active batch — send standalone recovery
		m.sendDirectAlert(ctx, event)
	}
}
// editBatchOnRecovery removes a recovered service from the active batch message
// and edits the Telegram message across all sent targets.
func (m *Manager) editBatchOnRecovery(ctx context.Context, instID string, bs *BatchState, recoveryEvent AlertEvent) {
	m.mu.Lock()

	// Remove this service from the batch
	delete(bs.DownService, recoveryEvent.ServiceName)

	remaining := len(bs.DownService)
	sentMessages := make([]TargetMessageID, len(bs.SentMessages))
	copy(sentMessages, bs.SentMessages)

	if remaining == 0 {
		// All services recovered — delete batch state
		delete(m.batchStates, instID)
	}
	m.mu.Unlock()
	m.persistBatch(ctx, instID)

	// Build the new message text
	inst, err := m.repo.GetInstitutionByID(instID)
	if err != nil {
		log.Printf("AlertEngine error: failed to fetch institution %s for recovery edit: %v", instID, err)
		return
	}

	var newText string
	if remaining == 0 {
		// All clear
		newText = fmt.Sprintf("✅ *ALL SERVICES RECOVERED*\n\n🏢 *Lembaga:* %s\n⏰ *Waktu:* %s\n\n_Semua service telah kembali normal._",
			inst.Name, time.Now().Local().Format("02 Jan 2006 15:04:05"))
	} else {
		// Still some down — rebuild the list (display names pre-fetched once).
		dm := m.displayMap(ctx, instID)
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("🟡 *CRITICAL ALERT BATCH (UPDATED)*\n\n🏢 *Lembaga:* %s\n⏰ *Waktu:* %s\n\n",
			inst.Name, time.Now().Local().Format("02 Jan 2006 15:04:05")))
		sb.WriteString(fmt.Sprintf("_✅ Baru pulih: %s_\n\n", label(dm, recoveryEvent.ServiceName)))
		sb.WriteString(fmt.Sprintf("_Daftar Service Masih DOWN (%d):_\n", remaining))

		// Sort service names for consistent output
		names := make([]string, 0, len(bs.DownService))
		for name := range bs.DownService {
			names = append(names, name)
		}
		sort.Strings(names)

		i := 1
		for _, name := range names {
			sb.WriteString(fmt.Sprintf("%d. ⚙️ *%s* - _Agent sedang auto-restart_\n", i, label(dm, name)))
			i++
		}
		newText = sb.String()
	}

	// Edit the Telegram message across all sent targets
	for _, sent := range sentMessages {
		log.Printf("AlertEngine editing BATCH message %d for %s (recovery: %s, remaining: %d, isGlobal: %v)",
			sent.MessageID, inst.Name, recoveryEvent.ServiceName, remaining, sent.Target.IsGlobal)
		if err := services.EditTelegramMessage(sent.Target.Token, sent.Target.ChatID, sent.Target.TopicID, sent.MessageID, newText); err != nil {
			log.Printf("Failed to edit batch alert for %s (msgID %d, target %+v): %v", inst.Name, sent.MessageID, sent.Target, err)
		}
	}
}

func (m *Manager) processTimeout(ctx context.Context, key string) {
	m.mu.Lock()

	// Double check if it's still pending
	event, exists := m.pendingDowns[key]
	if !exists {
		m.mu.Unlock()
		return
	}

	// Remove from tracking maps
	delete(m.debounceMap, key)
	delete(m.pendingDowns, key)

	// Gather ALL pendingDowns for this institution, fire them all together
	instID := event.InstitutionID
	var servicesDown []AlertEvent

	for k, ev := range m.pendingDowns {
		if ev.InstitutionID == instID {
			servicesDown = append(servicesDown, ev)
			if t, ok := m.debounceMap[k]; ok {
				t.Stop()
				delete(m.debounceMap, k)
			}
			delete(m.pendingDowns, k)
		}
	}

	// Add the triggering event itself
	// Deduplicate: only add if not already in servicesDown
	found := false
	for _, ev := range servicesDown {
		if ev.ServiceName == event.ServiceName {
			found = true
			break
		}
	}
	if !found {
		servicesDown = append(servicesDown, event)
	}

	m.mu.Unlock()

	if len(servicesDown) > 0 {
		log.Printf("Alert Timer fired for %s (%d services down). Triggering notification protocol.", key, len(servicesDown))
		for _, ev := range servicesDown {
			active, _ := m.repo.GetActiveIncident(ctx, ev.InstitutionID, ev.ServiceName, "service_down")
			if active == nil {
				_ = m.repo.CreateIncident(ctx, &models.IncidentLog{
					InstitutionID: ev.InstitutionID,
					ServiceName:   ev.ServiceName,
					Type:          "service_down",
					StartedAt:     ev.Timestamp,
					Cause:         ev.Message,
					CreatedAt:     time.Now(),
				})
			}
		}
		m.sendBatchedAlert(ctx, instID, servicesDown)
	}
}

// buildBatchText renders the critical batch alert body for one institution.
// Used for both the initial send and every in-place edit.
func buildBatchText(instName string, down map[string]AlertEvent, dm map[string]string, t time.Time) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🔴 *CRITICAL ALERT BATCH*\n\n🏢 *Lembaga:* %s\n⏰ *Waktu:* %s\n\n", instName, t.Format("02 Jan 2006 15:04:05")))
	sb.WriteString(fmt.Sprintf("_Daftar Service Terkena Dampak (DOWN) (%d):_\n", len(down)))

	names := make([]string, 0, len(down))
	for name := range down {
		names = append(names, name)
	}
	sort.Strings(names)
	for i, name := range names {
		sb.WriteString(fmt.Sprintf("%d. ⚙️ *%s* - _Agent sedang auto-restart_\n", i+1, label(dm, name)))
	}
	return sb.String()
}

func (m *Manager) sendBatchedAlert(ctx context.Context, instID string, events []AlertEvent) {
	inst, err := m.repo.GetInstitutionByID(instID)
	if err != nil {
		log.Printf("AlertEngine error: failed to fetch institution %s: %v", instID, err)
		return
	}

	setting, _ := m.repo.GetSettings(ctx)
	targets := ResolveTelegramTargets(inst, setting)
	if len(targets) == 0 {
		return
	}

	// Merge this wave into the batch already tracked for the institution.
	m.mu.Lock()
	existingBs := m.batchStates[instID]
	combinedDownMap := make(map[string]AlertEvent)
	var tracked []TargetMessageID
	if existingBs != nil {
		for k, v := range existingBs.DownService {
			combinedDownMap[k] = v
		}
		tracked = append(tracked, existingBs.SentMessages...)
	}
	for _, ev := range events {
		combinedDownMap[ev.ServiceName] = ev
	}
	m.mu.Unlock()

	text := buildBatchText(inst.Name, combinedDownMap, m.displayMap(ctx, instID), time.Now().Local())

	// A batch message already exists: edit it in place so Telegram shows one
	// evolving alert instead of one new message per down wave.
	var kept []TargetMessageID
	if len(tracked) > 0 {
		var staleTargets []TelegramTarget
		for _, sent := range tracked {
			if err := services.EditTelegramMessage(sent.Target.Token, sent.Target.ChatID, sent.Target.TopicID, sent.MessageID, text); err != nil {
				// Usually the message was deleted in Telegram; resend to this target only.
				log.Printf("AlertEngine failed to edit batch message %d for %s: %v", sent.MessageID, inst.Name, err)
				staleTargets = append(staleTargets, sent.Target)
				continue
			}
			kept = append(kept, sent)
		}
		if len(staleTargets) == 0 {
			m.storeBatch(ctx, instID, kept, combinedDownMap)
			log.Printf("AlertEngine: batch for %s edited in place (%d services, %d messages)", inst.Name, len(combinedDownMap), len(kept))
			return
		}
		log.Printf("AlertEngine: batch for %s edited on %d target(s), resending to %d stale target(s)", inst.Name, len(kept), len(staleTargets))
		targets = staleTargets
	}

	log.Printf("AlertEngine sending BATCH telegram message to %s (%d destinations)", inst.Name, len(targets))
	for _, target := range targets {
		msgID, sendErr := services.SendTelegramAlert(target.Token, target.ChatID, target.TopicID, text)
		if sendErr != nil {
			log.Printf("AlertEngine failed to send batch to target %+v: %v", target, sendErr)
		} else if msgID != 0 {
			kept = append(kept, TargetMessageID{Target: target, MessageID: msgID})
		}
	}

	if len(kept) == 0 {
		return
	}
	m.storeBatch(ctx, instID, kept, combinedDownMap)
	log.Printf("AlertEngine: batch tracked for institution %s (%d services, %d destinations)", inst.Name, len(combinedDownMap), len(kept))
}

// storeBatch records the tracked batch messages and down services for one institution.
func (m *Manager) storeBatch(ctx context.Context, instID string, msgs []TargetMessageID, down map[string]AlertEvent) {
	m.mu.Lock()
	if bs := m.batchStates[instID]; bs != nil {
		bs.SentMessages = msgs
		for k, v := range down {
			bs.DownService[k] = v
		}
	} else {
		m.batchStates[instID] = &BatchState{SentMessages: msgs, DownService: down}
	}
	m.mu.Unlock()

	m.persistBatch(ctx, instID)
}

func (m *Manager) sendDirectAlert(ctx context.Context, event AlertEvent) {
	inst, err := m.repo.GetInstitutionByID(event.InstitutionID)
	if err != nil {
		return
	}

	dm := m.displayMap(ctx, event.InstitutionID)
	msg := fmt.Sprintf("✅ *SERVICE RECOVERY*\n\n🏢 *Lembaga:* %s\n⚙️ *Service:* %s\n⏰ *Waktu:* %s",
		inst.Name, label(dm, event.ServiceName), time.Now().Local().Format("02 Jan 2006 15:04:05"))

	setting, _ := m.repo.GetSettings(ctx)
	targets := ResolveTelegramTargets(inst, setting)
	for _, target := range targets {
		log.Printf("AlertEngine sending DIRECT telegram message: %s - %s to %+v", inst.Name, event.ServiceName, target)
		_, _ = services.SendTelegramAlert(target.Token, target.ChatID, target.TopicID, msg)
	}
}

func sanitizeCauseMessage(msg string) string {
	clean := strings.TrimSpace(msg)
	if clean == "" {
		return "Service stopped / unreachable"
	}
	if strings.Contains(clean, "exit status 1") && strings.Contains(clean, "(No output from OS)") {
		return "Process is not running (PID not found)"
	}
	if strings.Contains(clean, "exit status") && strings.Contains(clean, "inactive") {
		return "Service is inactive / stopped"
	}
	return clean
}

