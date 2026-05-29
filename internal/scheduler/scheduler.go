// Package scheduler manages periodic heartbeat task creation for agents
// that have a heartbeat_interval configured.
//
// Design:
//   - On Start, scheduler polls the agent list and rebuilds a set of tickers
//     whenever agents change (detected via a refresh interval).
//   - For each (agent, project) pair where the agent has heartbeat_interval set,
//     a ticker fires every heartbeat_interval seconds.
//   - Before firing, the scheduler checks whether the agent already has an
//     active (running or queued) task in that project — if so it skips.
//   - Heartbeat tasks have a standard title and description so they're
//     distinguishable in the UI.
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

// Scheduler periodically creates heartbeat tasks for agents that have a
// heartbeat_interval configured.
type Scheduler struct {
	agents   store.AgentRepo
	projects store.ProjectRepo
	tasks    store.TaskRepo
	runner   TaskRunner

	refreshInterval time.Duration // how often to re-check agent/project assignments

	mu     sync.Mutex
	stops  map[string]context.CancelFunc // key: "agentID:projectID"
	ctx    context.Context
	cancel context.CancelFunc
}

// New creates a Scheduler. Call Start to begin scheduling.
// refreshInterval controls how often agent/project assignments are re-scanned
// to pick up changes (new agents, new heartbeat_interval values, new project
// assignments). A value of 60s is reasonable for production.
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
// Call Stop or cancel the parent context to shut down.
func (s *Scheduler) Start() {
	go s.loop()
}

// Stop cancels all scheduled heartbeats and shuts down the loop.
func (s *Scheduler) Stop() {
	s.cancel()
}

// loop runs the refresh cycle.
func (s *Scheduler) loop() {
	// Do an initial sync immediately.
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

// sync re-reads agents and projects, starts new heartbeats, and stops
// removed ones.
func (s *Scheduler) sync() {
	ctx := s.ctx

	agents, err := s.agents.List(ctx)
	if err != nil {
		log.Printf("scheduler: list agents: %v", err)
		return
	}

	projects, err := s.projects.List(ctx)
	if err != nil {
		log.Printf("scheduler: list projects: %v", err)
		return
	}

	// Build the desired set of (agent, project) heartbeat pairs.
	desired := make(map[string]heartbeatSpec) // key: "agentID:projectID"

	for _, agent := range agents {
		if agent.HeartbeatInterval == nil || *agent.HeartbeatInterval <= 0 {
			continue
		}
		if agent.Status != model.AgentStatusActive {
			continue
		}
		interval := time.Duration(*agent.HeartbeatInterval) * time.Second

		for _, proj := range projects {
			if proj.Status != model.ProjectStatusActive {
				continue
			}
			// Check if agent is assigned to this project.
			assigned, err := s.projects.ListAgents(ctx, proj.ID)
			if err != nil {
				continue
			}
			for _, a := range assigned {
				if a.ID == agent.ID {
					key := agent.ID + ":" + proj.ID
					desired[key] = heartbeatSpec{
						agent:    agent,
						project:  proj,
						interval: interval,
					}
					break
				}
			}
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Stop heartbeats no longer desired.
	for key, stop := range s.stops {
		if _, ok := desired[key]; !ok {
			stop()
			delete(s.stops, key)
			log.Printf("scheduler: stopped heartbeat %s", key)
		}
	}

	// Start new heartbeats.
	for key, spec := range desired {
		if _, running := s.stops[key]; running {
			continue // already active
		}
		hbCtx, hbCancel := context.WithCancel(s.ctx)
		s.stops[key] = hbCancel
		go s.heartbeatLoop(hbCtx, spec)
		log.Printf("scheduler: started heartbeat %s every %s", key, spec.interval)
	}
}

// stopAll cancels all running heartbeat goroutines.
func (s *Scheduler) stopAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, stop := range s.stops {
		stop()
		delete(s.stops, key)
	}
}

// heartbeatSpec describes a single (agent, project) heartbeat.
type heartbeatSpec struct {
	agent    *model.Agent
	project  *model.Project
	interval time.Duration
}

// heartbeatLoop fires a task for the given (agent, project) pair every interval.
func (s *Scheduler) heartbeatLoop(ctx context.Context, spec heartbeatSpec) {
	ticker := time.NewTicker(spec.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.fire(ctx, spec); err != nil {
				log.Printf("scheduler: heartbeat fire %s/%s: %v",
					spec.agent.ID, spec.project.ID, err)
			}
		}
	}
}

// fire creates and enqueues a heartbeat task if the agent has no active task
// in this project.
func (s *Scheduler) fire(ctx context.Context, spec heartbeatSpec) error {
	// Check for existing running/queued tasks for this agent in this project.
	existing, err := s.tasks.List(ctx, spec.project.ID)
	if err != nil {
		return fmt.Errorf("list tasks: %w", err)
	}
	for _, t := range existing {
		if t.AgentID == spec.agent.ID &&
			(t.Status == model.TaskStatusRunning || t.Status == model.TaskStatusQueued) {
			log.Printf("scheduler: skipping heartbeat for %s/%s — task already active",
				spec.agent.ID, spec.project.ID)
			return nil
		}
	}

	// Create the heartbeat task.
	now := time.Now()
	task := &model.Task{
		ID:          uuid.New().String(),
		ProjectID:   spec.project.ID,
		AgentID:     spec.agent.ID,
		Title:       fmt.Sprintf("Heartbeat — %s", now.Format("2006-01-02 15:04")),
		Description: "Scheduled heartbeat check-in. Review any pending context, outstanding work items, and provide a status update.",
		Status:      model.TaskStatusPending,
		Input:       "{}",
		Output:      "{}",
		CreatedAt:   now,
	}
	if err := s.tasks.Create(ctx, task); err != nil {
		return fmt.Errorf("create heartbeat task: %w", err)
	}

	log.Printf("scheduler: firing heartbeat task %s for agent %s in project %s",
		task.ID, spec.agent.Name, spec.project.Name)

	if err := s.runner.RunTask(ctx, task.ID); err != nil {
		return fmt.Errorf("run heartbeat task: %w", err)
	}

	return nil
}
