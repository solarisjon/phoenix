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
	"log/slog"
	"strings"
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

// Scheduler periodically creates tasks for monitors that have a schedule
// configured. Two schedule kinds are supported:
//
//   - interval: a per-monitor ticker fires every schedule_interval seconds.
//   - daily:    each refresh tick evaluates the monitor's HH:MM times against
//     the wall clock, with optional same-day catch-up for missed runs.
type Scheduler struct {
	agents   store.AgentRepo
	projects store.ProjectRepo
	tasks    store.TaskRepo
	settings store.SystemSettingsRepo
	runner   TaskRunner

	refreshInterval time.Duration

	// dailyPunctualWindow is how long after a scheduled daily time a non-catch-up
	// monitor may still fire. Sized to comfortably exceed refreshInterval so the
	// minute is never skipped between ticks.
	dailyPunctualWindow time.Duration

	mu         sync.Mutex
	stops      map[string]scheduleEntry // key: monitorID (interval schedules only)
	agentIndex map[string]int           // key: monitorID → next round-robin index
	ctx        context.Context
	cancel     context.CancelFunc
}

// scheduleEntry tracks the cancel function and the interval that was active when
// the goroutine was started, so sync() can detect interval changes.
type scheduleEntry struct {
	cancel   context.CancelFunc
	interval time.Duration
}

// New creates a Scheduler. Call Start to begin scheduling.
func New(
	agents store.AgentRepo,
	projects store.ProjectRepo,
	tasks store.TaskRepo,
	settings store.SystemSettingsRepo,
	runner TaskRunner,
	refreshInterval time.Duration,
) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &Scheduler{
		agents:              agents,
		projects:            projects,
		tasks:               tasks,
		settings:            settings,
		runner:              runner,
		refreshInterval:     refreshInterval,
		dailyPunctualWindow: refreshInterval + time.Minute,
		stops:               make(map[string]scheduleEntry),
		agentIndex:          make(map[string]int),
		ctx:                 ctx,
		cancel:              cancel,
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
	monitor         *model.Project
	agent           *model.Agent
	useOrchestrator bool          // true when agent is the fallback orchestrator
	interval        time.Duration // interval kind only

	kind    string      // model.ScheduleKindInterval or ScheduleKindDaily
	times   []timeOfDay // daily kind only
	catchUp bool        // daily kind only
}

// timeOfDay is a wall-clock HH:MM with no date.
type timeOfDay struct {
	h, m int
}

// sync re-reads monitors, starts/stops interval schedules, and evaluates daily
// schedules against the wall clock.
func (s *Scheduler) sync() {
	ctx := s.ctx

	projects, err := s.projects.List(ctx, "")
	if err != nil {
		slog.Error("scheduler: list projects", "error", err)
		return
	}

	// Build the desired set of interval schedules and collect daily schedules.
	desired := make(map[string]scheduleSpec) // key: monitorID (interval only)
	var dailySpecs []scheduleSpec

	for _, proj := range projects {
		if proj.Kind != model.ProjectKindMonitor {
			continue
		}
		if proj.Status != model.ProjectStatusActive {
			continue
		}

		// Collect active assigned agents and select via round-robin.
		assigned, err := s.projects.ListAgents(ctx, proj.ID)
		if err != nil {
			continue
		}
		var activeAgents []*model.Agent
		for _, a := range assigned {
			if a.Status == model.AgentStatusActive {
				activeAgents = append(activeAgents, a)
			}
		}

		var execAgent *model.Agent
		useOrchestrator := false

		if len(activeAgents) == 0 {
			// No assigned agent — fall back to orchestrator if enabled.
			orch := s.findOrchestratorAgent(ctx)
			if orch == nil {
				slog.Debug("scheduler: skipping monitor — no agent assigned and no orchestrator available", "monitor", proj.Name)
				continue
			}
			execAgent = orch
			useOrchestrator = true
		} else {
			s.mu.Lock()
			idx := s.agentIndex[proj.ID] % len(activeAgents)
			s.agentIndex[proj.ID] = idx + 1
			s.mu.Unlock()
			execAgent = activeAgents[idx]
		}

		kind := proj.ScheduleKind
		if kind == "" {
			kind = model.ScheduleKindInterval
		}

		switch kind {
		case model.ScheduleKindDaily:
			times := parseTimes(proj.ScheduleTimes)
			if len(times) == 0 {
				continue
			}
			dailySpecs = append(dailySpecs, scheduleSpec{
				monitor:         proj,
				agent:           execAgent,
				useOrchestrator: useOrchestrator,
				kind:            model.ScheduleKindDaily,
				times:           times,
				catchUp:         proj.ScheduleCatchUp,
			})
		default: // interval
			if proj.ScheduleInterval == nil || *proj.ScheduleInterval <= 0 {
				continue
			}
			desired[proj.ID] = scheduleSpec{
				monitor:         proj,
				agent:           execAgent,
				useOrchestrator: useOrchestrator,
				interval:        time.Duration(*proj.ScheduleInterval) * time.Second,
				kind:            model.ScheduleKindInterval,
			}
		}
	}

	s.mu.Lock()

	// Stop schedules no longer desired or whose interval changed.
	for key, entry := range s.stops {
		spec, ok := desired[key]
		if !ok || entry.interval != spec.interval {
			entry.cancel()
			delete(s.stops, key)
			if !ok {
				slog.Info("scheduler: stopped schedule", "monitor", key)
			} else {
				slog.Info("scheduler: restarting schedule (interval changed)", "monitor", key, "interval", spec.interval)
			}
		}
	}

	// Start new schedules (includes restarted ones whose entries were just deleted).
	for key, spec := range desired {
		if _, running := s.stops[key]; running {
			continue
		}
		hbCtx, hbCancel := context.WithCancel(s.ctx)
		s.stops[key] = scheduleEntry{cancel: hbCancel, interval: spec.interval}
		go s.scheduleLoop(hbCtx, spec)
		slog.Info("scheduler: started schedule", "monitor", spec.monitor.Name, "interval", spec.interval)
	}

	s.mu.Unlock()

	// Evaluate daily schedules synchronously against the wall clock. sync() is
	// only ever called from the single-threaded refresh loop, so there is no
	// risk of a daily monitor being evaluated concurrently with itself.
	now := time.Now()
	for _, spec := range dailySpecs {
		s.evaluateDaily(ctx, spec, now)
	}
}

func (s *Scheduler) stopAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, entry := range s.stops {
		entry.cancel()
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
				slog.Error("scheduler: fire", "monitor_id", spec.monitor.ID, "error", err)
			}
		}
	}
}

// fire creates and enqueues a scheduled task if the monitor has no active task.
func (s *Scheduler) fire(ctx context.Context, spec scheduleSpec) error {
	active, err := s.tasks.HasActiveTaskForProject(ctx, spec.monitor.ID)
	if err != nil {
		return fmt.Errorf("check active task: %w", err)
	}
	if active {
		slog.Debug("scheduler: skipping monitor — task already active", "monitor", spec.monitor.Name)
		return nil
	}

	now := time.Now()
	taskType := model.TaskTypeStandard
	if spec.useOrchestrator {
		taskType = model.TaskTypeOrchestration
	}
	task := &model.Task{
		ID:          uuid.New().String(),
		ProjectID:   spec.monitor.ID,
		AgentID:     spec.agent.ID,
		Title:       fmt.Sprintf("Scheduled run — %s", now.Format("2006-01-02 15:04")),
		Description: spec.monitor.Objective,
		Status:      model.TaskStatusPending,
		Source:      "monitor",
		TaskType:    taskType,
		Input:       "{}",
		Output:      "{}",
		CreatedAt:   now,
	}
	if err := s.tasks.Create(ctx, task); err != nil {
		return fmt.Errorf("create task: %w", err)
	}
	slog.Info("scheduler: fired task", "task_id", task.ID, "monitor", spec.monitor.Name, "agent", spec.agent.Name)
	return s.runner.RunTask(ctx, task.ID)
}

// evaluateDaily fires a daily monitor if a scheduled time has passed today and
// the monitor has not yet run for that occurrence. With catch-up enabled the
// most recent missed occurrence still fires (once); with catch-up disabled it
// only fires within dailyPunctualWindow of the scheduled time.
func (s *Scheduler) evaluateDaily(ctx context.Context, spec scheduleSpec, now time.Time) {
	occ, ok := mostRecentOccurrence(spec.times, now)
	if !ok {
		return // no scheduled time has passed yet today
	}

	last, err := s.lastMonitorRun(ctx, spec.monitor.ID)
	if err != nil {
		slog.Error("scheduler: daily last-run lookup", "monitor_id", spec.monitor.ID, "error", err)
		return
	}
	if last != nil && !last.Before(occ) {
		return // already ran at or after the due occurrence
	}

	if !spec.catchUp && now.Sub(occ) > s.dailyPunctualWindow {
		return // missed the punctual window and catch-up is disabled
	}

	if err := s.fire(ctx, spec); err != nil {
		slog.Error("scheduler: daily fire", "monitor_id", spec.monitor.ID, "error", err)
	}
}

// lastMonitorRun returns the creation time of the most recent monitor-sourced
// task for the project, regardless of dismissed status. Dismissed tasks must
// count — a user clearing the inbox should not cause the monitor to re-fire.
func (s *Scheduler) lastMonitorRun(ctx context.Context, projectID string) (*time.Time, error) {
	return s.tasks.LastMonitorRunAt(ctx, projectID)
}

// findOrchestratorAgent returns the first active agent with IsOrchestrator=true,
// or nil if orchestration is disabled in settings or no such agent exists.
func (s *Scheduler) findOrchestratorAgent(ctx context.Context) *model.Agent {
	if s.settings == nil {
		return nil
	}
	cfg, err := s.settings.Get(ctx)
	if err != nil || cfg == nil || !cfg.DynamicOrchestrationEnabled {
		return nil
	}
	all, err := s.agents.List(ctx, "")
	if err != nil {
		return nil
	}
	for _, a := range all {
		if a.IsOrchestrator && a.Status == model.AgentStatusActive {
			return a
		}
	}
	return nil
}

// parseTimes converts HH:MM strings into timeOfDay values, skipping invalid entries.
func parseTimes(strs []string) []timeOfDay {
	out := make([]timeOfDay, 0, len(strs))
	for _, raw := range strs {
		t, err := time.Parse("15:04", strings.TrimSpace(raw))
		if err != nil {
			continue
		}
		out = append(out, timeOfDay{h: t.Hour(), m: t.Minute()})
	}
	return out
}

// mostRecentOccurrence returns the latest occurrence today (in now's location)
// at or before now, and whether any time qualified.
func mostRecentOccurrence(times []timeOfDay, now time.Time) (time.Time, bool) {
	y, mo, d := now.Date()
	loc := now.Location()
	var best time.Time
	found := false
	for _, t := range times {
		occ := time.Date(y, mo, d, t.h, t.m, 0, 0, loc)
		if occ.After(now) {
			continue
		}
		if !found || occ.After(best) {
			best = occ
			found = true
		}
	}
	return best, found
}
