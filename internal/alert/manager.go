package alert

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

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

// Manager handles alert deduplication and batched sending
type Manager struct {
	repo         repository.Repository
	EventChan    chan AlertEvent
	debounceMap  map[string]*time.Timer
	pendingDowns map[string]AlertEvent // key: instID_serviceName
	mu           sync.Mutex
	debounceTime time.Duration
}

// NewManager creates a new AlertManager
func NewManager(repo repository.Repository, debounceTime time.Duration) *Manager {
	if debounceTime == 0 {
		debounceTime = 2 * time.Minute
	}
	return &Manager{
		repo:         repo,
		EventChan:    make(chan AlertEvent, 1000),
		debounceMap:  make(map[string]*time.Timer),
		pendingDowns: make(map[string]AlertEvent),
		debounceTime: debounceTime,
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
	m.mu.Lock()
	defer m.mu.Unlock()

	key := fmt.Sprintf("%s_%s", event.InstitutionID, event.ServiceName)

	switch event.Status {
	case "DOWN":
		// Store it in pending and start a timer
		m.pendingDowns[key] = event

		// If a timer already exists, we cancel it and reset (flapping case where it goes DOWN immediately again)
		if t, ok := m.debounceMap[key]; ok {
			t.Stop()
		}

		// Create a new timer
		// Note: The timer runs in a separate goroutine. It must re-acquire lock when processTimeout fires.
		timer := time.AfterFunc(m.debounceTime, func() {
			m.processTimeout(context.Background(), key)
		})
		m.debounceMap[key] = timer

	case "UP":
		// If it was pending DOWN, clear the timer (Recovery during debounce window!)
		if t, ok := m.debounceMap[key]; ok {
			t.Stop()
			delete(m.debounceMap, key)
			delete(m.pendingDowns, key)
			log.Printf("Flap detected and mitigated for %s: it recovered within %v", key, m.debounceTime)
		} else {
			// It's not in pendingDowns, meaning it already triggered the real alert long ago.
			// Send recovery alert instantly!
			m.sendDirectAlert(ctx, event)
		}
	}
}

func (m *Manager) processTimeout(ctx context.Context, key string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Double check if it's still pending
	event, exists := m.pendingDowns[key]
	if !exists {
		return
	}

	// Remove from tracking maps
	delete(m.debounceMap, key)
	delete(m.pendingDowns, key)

	log.Printf("Alert Timer fired for %s. Triggering notification protocol.", key)

	// Since we want to GROUP alerts from the same institution...
	// Gather ALL CURRENT pendingDowns for this institution, fire them all together, and remove their timers!
	instID := event.InstitutionID
	var servicesDown []AlertEvent

	// Gather all DOWNs for this instID
	for k, ev := range m.pendingDowns {
		if ev.InstitutionID == instID {
			servicesDown = append(servicesDown, ev)
			// Clean them up too
			if t, ok := m.debounceMap[k]; ok {
				t.Stop()
				delete(m.debounceMap, k)
			}
			delete(m.pendingDowns, k)
		}
	}

	servicesDown = append(servicesDown, event) // add the triggering one

	if len(servicesDown) > 0 {
		m.sendBatchedAlert(ctx, instID, servicesDown)
	}
}

func (m *Manager) sendBatchedAlert(ctx context.Context, instID string, events []AlertEvent) {
	// Group them into one telegram message
	inst, err := m.repo.GetInstitutionByID(instID)
	if err != nil {
		log.Printf("AlertEngine error: failed to fetch institution %s: %v", instID, err)
		return
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🔴 *CRITICAL ALERT BATCH*\n\n🏢 *Lembaga:* %s\n⏰ *Waktu:* %s\n\n", inst.Name, time.Now().Local().Format("02 Jan 2006 15:04:05")))
	sb.WriteString("_Daftar Service Terkena Dampak (DOWN):_\n")
	
	for i, ev := range events {
		sb.WriteString(fmt.Sprintf("%d. ⚙️ *%s* - _Agent sedang auto-restart_\n", i+1, ev.ServiceName))
	}

	setting, err := m.repo.GetSettings(ctx)
	if err == nil && setting.EnableTelegramAlerts {
		log.Printf("AlertEngine sending BATCH telegram message to %s", inst.Name)
		services.SendTelegramAlert(setting.TelegramBotToken, setting.TelegramChatID, setting.TelegramTopicID, sb.String())
	}
}

func (m *Manager) sendDirectAlert(ctx context.Context, event AlertEvent) {
	inst, err := m.repo.GetInstitutionByID(event.InstitutionID)
	if err != nil {
		return
	}

	msg := fmt.Sprintf("✅ *SERVICE RECOVERY*\n\n🏢 *Lembaga:* %s\n⚙️ *Service:* %s\n⏰ *Waktu:* %s",
		inst.Name, event.ServiceName, time.Now().Local().Format("02 Jan 2006 15:04:05"))

	if event.Status == "DOWN" {
        // Fallback for direct down (unused in current batched logic, but here just in case)
		msg = fmt.Sprintf("⚠️ *SERVICE DOWN*\n\n🏢 *Lembaga:* %s\n⚙️ *Service:* %s\n⏰ *Waktu:* %s\n\n_Agent sedang mencoba auto-restart._",
			inst.Name, event.ServiceName, time.Now().Local().Format("02 Jan 2006 15:04:05"))
	}

	setting, err := m.repo.GetSettings(ctx)
	if err == nil && setting.EnableTelegramAlerts {
		log.Printf("AlertEngine sending DIRECT telegram message: %s - %s", inst.Name, event.ServiceName)
		services.SendTelegramAlert(setting.TelegramBotToken, setting.TelegramChatID, setting.TelegramTopicID, msg)
	}
}
