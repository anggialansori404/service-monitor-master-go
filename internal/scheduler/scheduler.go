package scheduler

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"monitoring-master/internal/models"
	"monitoring-master/internal/repository"
	"monitoring-master/internal/ws"

	"github.com/go-co-op/gocron/v2"
	"github.com/google/uuid"
)

// ParseLocation loads a time.Location supporting standard IANA names and Indonesian timezone aliases (WIB, WITA, WIT).
func ParseLocation(tz string) (*time.Location, error) {
	cleanTz := strings.TrimSpace(tz)
	switch strings.ToUpper(cleanTz) {
	case "WIB", "ASIA/JAKARTA":
		return time.LoadLocation("Asia/Jakarta")
	case "WITA", "ASIA/MAKASSAR":
		return time.LoadLocation("Asia/Makassar")
	case "WIT", "ASIA/JAYAPURA":
		return time.LoadLocation("Asia/Jayapura")
	case "UTC", "GMT":
		return time.UTC, nil
	default:
		if cleanTz == "" {
			return time.LoadLocation("Asia/Jakarta")
		}
		return time.LoadLocation(cleanTz)
	}
}

// ServiceScheduler manages scheduled restarts via gocron v2.
type ServiceScheduler struct {
	gocronScheduler gocron.Scheduler
	repo            repository.Repository
	registry        *ws.Registry
	jobs            map[string]gocron.Job
	institutionMu   map[string]*sync.Mutex
	mu              sync.RWMutex
}

// New creates a ServiceScheduler, starts the gocron engine, loads existing enabled schedules from DB,
// and registers them as gocron jobs.
func New(repo repository.Repository, registry *ws.Registry) (*ServiceScheduler, error) {
	gs, err := gocron.NewScheduler()
	if err != nil {
		return nil, fmt.Errorf("failed to create gocron scheduler: %w", err)
	}

	s := &ServiceScheduler{
		gocronScheduler: gs,
		repo:            repo,
		registry:        registry,
		jobs:            make(map[string]gocron.Job),
		institutionMu:   make(map[string]*sync.Mutex),
	}

	// Start gocron scheduler engine first so that NextRun() can calculate immediately on job registration
	s.gocronScheduler.Start()

	if err := s.LoadSchedules(); err != nil {
		log.Printf("scheduler: warning: failed to load existing schedules: %v", err)
	}

	return s, nil
}

// Start starts or ensures the gocron scheduler is running.
func (s *ServiceScheduler) Start() {
	s.gocronScheduler.Start()
	log.Println("scheduler: started")
}

// Stop performs a graceful shutdown of the gocron scheduler.
func (s *ServiceScheduler) Stop() {
	if err := s.gocronScheduler.Shutdown(); err != nil {
		log.Printf("scheduler: shutdown error: %v", err)
	}
	log.Println("scheduler: stopped")
}

// LoadSchedules queries all enabled schedules from DB and registers each as a gocron job.
func (s *ServiceScheduler) LoadSchedules() error {
	schedules, err := s.repo.ScheduleRepo().GetEnabledSchedules(context.Background())
	if err != nil {
		return fmt.Errorf("GetEnabledSchedules: %w", err)
	}

	for i := range schedules {
		if err := s.AddSchedule(&schedules[i]); err != nil {
			log.Printf("scheduler: failed to add schedule %s (%s): %v", schedules[i].ID, schedules[i].Name, err)
		}
	}

	log.Printf("scheduler: loaded %d enabled schedules", len(schedules))
	return nil
}

// AddSchedule registers a gocron job for the given schedule.
// The job uses the schedule's cron expression and timezone, with singleton mode
// to prevent overlapping runs.
func (s *ServiceScheduler) AddSchedule(schedule *models.Schedule) error {
	loc, err := ParseLocation(schedule.Timezone)
	if err != nil {
		return fmt.Errorf("invalid timezone %q: %w", schedule.Timezone, err)
	}

	// Prepend CRON_TZ for per-schedule timezone support.
	cronExpr := fmt.Sprintf("CRON_TZ=%s %s", loc.String(), schedule.CronExpr)

	job, err := s.gocronScheduler.NewJob(
		gocron.CronJob(cronExpr, false),
		gocron.NewTask(s.executeSchedule, schedule),
		gocron.WithName(schedule.Name),
		gocron.WithTags(schedule.ID),
		gocron.WithSingletonMode(gocron.LimitModeReschedule),
	)
	if err != nil {
		return fmt.Errorf("NewJob: %w", err)
	}

	s.mu.Lock()
	s.jobs[schedule.ID] = job
	s.mu.Unlock()

	// Update NextRunAt in DB only if NextRun() is valid and non-zero
	nextRun, err := job.NextRun()
	if err == nil && !nextRun.IsZero() {
		_ = s.repo.ScheduleRepo().UpdateScheduleRunTimes(context.Background(), schedule.ID, time.Time{}, nextRun)
	}

	if nextRun.IsZero() {
		log.Printf("scheduler: registered job for schedule %s (%s)", schedule.ID, schedule.Name)
	} else {
		nextRunInLoc := nextRun.In(loc)
		log.Printf("scheduler: registered job for schedule %s (%s), next run: %s (%s)",
			schedule.ID, schedule.Name, nextRunInLoc.Format("2006-01-02 15:04:05 MST"), loc.String())
	}
	return nil
}

// RemoveSchedule removes a gocron job by schedule ID and deletes it from the jobs map.
func (s *ServiceScheduler) RemoveSchedule(scheduleID string) error {
	s.mu.Lock()
	job, ok := s.jobs[scheduleID]
	if ok {
		delete(s.jobs, scheduleID)
	}
	s.mu.Unlock()

	if !ok {
		return fmt.Errorf("job not found for schedule %s", scheduleID)
	}

	return s.gocronScheduler.RemoveJob(job.ID())
}

// getInstitutionMutex returns (or creates) a per-institution mutex.
func (s *ServiceScheduler) getInstitutionMutex(institutionID string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()

	mu, ok := s.institutionMu[institutionID]
	if !ok {
		mu = &sync.Mutex{}
		s.institutionMu[institutionID] = mu
	}
	return mu
}

// executeSchedule is called by gocron when a schedule triggers.
// It locks per-institution, checks missed policy, sends restart commands
// to each service via WS, records the run, and updates timestamps.
func (s *ServiceScheduler) executeSchedule(schedule *models.Schedule) {
	mu := s.getInstitutionMutex(schedule.InstitutionID)
	mu.Lock()
	defer mu.Unlock()

	ctx := context.Background()
	catchUp := false

	// Check missed policy.
	if schedule.MissedPolicy == "catch_up_once" && schedule.LastRunAt != nil {
		loc, err := ParseLocation(schedule.Timezone)
		if err != nil {
			loc = time.UTC
		}
		now := time.Now().In(loc)
		// Simple heuristic: if more than 2x the shortest reasonable interval
		// (1 minute for "* * * * *", cron parser handles actual intervals),
		// consider it missed. gocron handles the actual cron scheduling;
		// this check catches cases where the scheduler was down.
		elapsed := now.Sub(*schedule.LastRunAt)
		if elapsed > 2*time.Minute {
			catchUp = true
			log.Printf("scheduler: catch_up_once for schedule %s (elapsed: %v)", schedule.ID, elapsed)
		}
	}

	// Get institution to find AgentID.
	institution, err := s.repo.GetInstitutionByID(schedule.InstitutionID)
	if err != nil {
		log.Printf("scheduler: institution %s not found for schedule %s: %v", schedule.InstitutionID, schedule.ID, err)
		return
	}

	// Execute restart for each service.
	successCount := 0
	failCount := 0
	resultMessages := make([]string, 0, len(schedule.ServiceNames))

	for _, serviceName := range schedule.ServiceNames {
		reqID := fmt.Sprintf("sched-%s-%d", schedule.ID, time.Now().UnixNano())

		respChan := make(chan ws.WsResponse)
		s.registry.PendingRequests.Store(reqID, respChan)

		payload := map[string]interface{}{
			"request_id": reqID,
			"action":     "restart",
			"service":    serviceName,
		}

		err := s.registry.SendToAgent(institution.AgentID, payload)
		if err != nil {
			s.registry.PendingRequests.Delete(reqID)
			failCount++
			resultMessages = append(resultMessages, fmt.Sprintf("%s: agent offline", serviceName))
			continue
		}

		select {
		case resp := <-respChan:
			success, ok := resp["success"].(bool)
			if ok && success {
				successCount++
				resultMessages = append(resultMessages, fmt.Sprintf("%s: success", serviceName))
			} else {
				failCount++
				msg := "restart failed"
				if m, ok := resp["message"].(string); ok {
					msg = m
				}
				resultMessages = append(resultMessages, fmt.Sprintf("%s: %s", serviceName, msg))
			}
		case <-time.After(10 * time.Second):
			s.registry.PendingRequests.Delete(reqID)
			failCount++
			resultMessages = append(resultMessages, fmt.Sprintf("%s: timeout", serviceName))
		}
	}

	// Determine overall status.
	status := "SUCCESS"
	if failCount > 0 && successCount == 0 {
		status = "FAILED"
	} else if failCount > 0 {
		status = "PARTIAL"
	}

	resultMsg := fmt.Sprintf("restarted %d/%d services (%d failed)", successCount, len(schedule.ServiceNames), failCount)
	now := time.Now()

	run := models.ScheduleRun{
		ID:            uuid.New().String(),
		ScheduleID:    schedule.ID,
		ScheduleName:  schedule.Name,
		InstitutionID: schedule.InstitutionID,
		TriggeredBy:   "SCHEDULER",
		ServiceNames:  schedule.ServiceNames,
		Status:        status,
		ResultMessage: resultMsg,
		RunAt:         now,
		CatchUp:       catchUp,
		CreatedAt:     now,
	}

	if err := s.repo.ScheduleRepo().CreateScheduleRun(ctx, &run); err != nil {
		log.Printf("scheduler: CreateScheduleRun error for schedule %s: %v", schedule.ID, err)
	}

	// Update last_run_at and next_run_at.
	s.mu.RLock()
	job, hasJob := s.jobs[schedule.ID]
	s.mu.RUnlock()

	var nextRun time.Time
	if hasJob {
		nextRun, _ = job.NextRun()
	}

	if err := s.repo.ScheduleRepo().UpdateScheduleRunTimes(ctx, schedule.ID, now, nextRun); err != nil {
		log.Printf("scheduler: UpdateScheduleRunTimes error for schedule %s: %v", schedule.ID, err)
	}

	// Update in-memory schedule for next missed-policy check.
	schedule.LastRunAt = &now

	log.Printf("scheduler: schedule %s (%s) completed: %s — %s", schedule.ID, schedule.Name, status, resultMsg)
}
