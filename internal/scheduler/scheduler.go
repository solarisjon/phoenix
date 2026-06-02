// Package scheduler manages periodic task creation for monitors that have a
// schedule_interval configured.
//
// Design:
//   - On Start, scheduler polls the monitor list and rebuilds a set of tickers
//     whenever monitors change (detected via a refresh interval).
//   - For each monitor with schedule_interval set, a ticker fires every
//     schedule_interval seconds using the first assigned active agent.
//   - Before firing, the scheduler checks whether the monitor already has an
//     active (running or queued) task — if so it skips.
package scheduler

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/store"
)

// TaskRunner is the subset of agent.Runner the scheduler needs.
type TaskRunner interface {
	RunTask(ctx context.Context, taskID string) error
}

// Scheduler periodically creates tasks for monitors that have a
// schedule_interval configured.
type Scheduler struct {
	agents   store.AgentRepo
	projects store.ProjectRepo
	tasks    store.TaskRepo
	runner   TaskRunner

	refreshInterval time.Duration

	mu     sync.Mutex
	stops  map[string]context.CancelFunc // key: monitorID
	ctx    context.Context
	cancel context.CancelFunc
}

// New creates a Scheduler. Call Start to begin scheduling.
func New(
	agents store.AgentRepo,
	projects store.ProjectRepo,
	tasks store.TaskRepo,
	runner TaskRunner,
	refreshInterval time.Duration,
) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &Scheduler{
		agents:          agents,
		projects:        projects,
		tasks:           tasks,
		runner:          runner,
		refreshInterval: refreshInterval,
		stops:           make(map[string]context.CancelFunc),
		ctx:             ctx,
		cancel:          cancel,
	}
}

// Start begins the scheduler loop in a background goroutine.
func (s *Scheduler) Start() {
	go s.loop()
}

// Stop cancels all scheduled tickers and shuts down the loop.
func (s *Scheduler) Stop() {
	s.cancel()
}

func (s *Scheduler) loop() {
	s.sync()
	ticker := time.NewTicker(s.refreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			s.stopAll()
			return
		case <-ticker.C:
			s.sync()
		}
	}
}

// scheduleSpec describes a single monitor's schedule.
type scheduleSpec struct {
	monitor  *model.Project
	agent    *model.Agent
	interval time.Duration
}

// sync re-reads monitors, starts new schedules, and stops removed ones.
func (s *Scheduler) sync() {
	ctx := s.ctx

	projects, err := s.projects.List(ctx)
	if err != nil {
		log.Printf("scheduler: list projects: %v", err)
		return
	}

	// Build the desired set of monitor schedules.
	desired := make(map[string]scheduleSpec) // key: monitorID

	for _, proj := range projects {
		if proj.Kind != model.ProjectKindMonitor {
			continue
		}
		if proj.Status != model.ProjectStatusActive {
			continue
		}
		if proj.ScheduleInterval == nil || *proj.ScheduleInterval <= 0 {
			continue
		}

		// Find the first active assigned agent to execute tasks.
		assigned, err := s.projects.ListAgents(ctx, proj.ID)
		if err != nil || len(assigned) == 0 {
			continue
		}
		var execAgent *model.Agent
		for _, a := range assigned {
			if a.Status == model.AgentStatusActive {
				execAgent = a
				break
			}
		}
		if execAgent == nil {
			continue
		}

		desired[proj.ID] = scheduleSpec{
			monitor:  proj,
			agent:    execAgent,
			interval: time.Duration(*proj.ScheduleInterval) * time.Second,
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Stop schedules no longer desired.
	for key, stop := range s.stops {
		if _, ok := desired[key]; !ok {
			stop()
			delete(s.stops, key)
			log.Printf("scheduler: stopped schedule for monitor %s", key)
		}
	}

	// Start new schedules.
	for key, spec := range desired {
		if _, running := s.stops[key]; running {
			continue
		}
		hbCtx, hbCancel := context.WithCancel(s.ctx)
		s.stops[key] = hbCancel
		go s.scheduleLoop(hbCtx, spec)
		log.Printf("scheduler: started schedule for monitor %s every %s", spec.monitor.Name, spec.interval)
	}
}

func (s *Scheduler) stopAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, stop := range s.stops {
		stop()
		delete(s.stops, key)
	}
}

func (s *Scheduler) scheduleLoop(ctx context.Context, spec scheduleSpec) {
	ticker := time.NewTicker(spec.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.fire(ctx, spec); err != nil {
				log.Printf("scheduler: fire %s: %v", spec.monitor.ID, err)
			}
		}
	}
}

// fire creates and enqueues a scheduled task if the monitor has no active task.
func (s *Scheduler) fire(ctx context.Context, spec scheduleSpec) error {
	existing, err := s.tasks.List(ctx, spec.monitor.ID)
	if err != nil {
		return fmt.Errorf("list tasks: %w", err)
	}
	for _, t := range existing {
		if t.Status == model.TaskStatusRunning || t.Status == model.TaskStatusQueued {
			log.Printf("scheduler: skipping %s — task already active", spec.monitor.Name)
			return nil
		}
	}

	now := time.Now()
	task := &model.Task{
		ID:          uuid.New().String(),
		ProjectID:   spec.monitor.ID,
		AgentID:     spec.agent.ID,
		Title:       fmt.Sprintf("Scheduled run — %s", now.Format("2006-01-02 15:04")),
		Description: spec.monitor.Description,
		Status:      model.TaskStatusPending,
		Source:      "monitor",
		Input:       "{}",
		Output:      "{}",
		CreatedAt:   now,
	}
	if err := s.tasks.Create(ctx, task); err != nil {
		return fmt.Errorf("create task: %w", err)
	}
	log.Printf("scheduler: fired task %s for monitor %s (agent %s)", task.ID, spec.monitor.Name, spec.agent.Name)
	return s.runner.RunTask(ctx, task.ID)
}
