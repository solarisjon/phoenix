package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/provider/registry"
	"github.com/solarisjon/phoenix/internal/store"
)

// StreamEvent is emitted during task execution and consumed by the WebSocket hub.
type StreamEvent struct {
	TaskID  string
	AgentID string
	// One of the following is set per event:
	Chunk      *string // partial output text
	StatusDone *model.TaskStatus
	Err        error
}

// EventHandler is called with each StreamEvent during task execution.
// Implementations must be non-blocking (e.g. send to a buffered channel).
type EventHandler func(StreamEvent)

// Runner manages agent task execution. Each task runs in its own goroutine.
// All active goroutines are tracked so they can be cancelled on shutdown.
type Runner struct {
	agents    store.AgentRepo
	tasks     store.TaskRepo
	registry  *registry.Registry
	onEvent   EventHandler

	mu      sync.Mutex
	cancels map[string]context.CancelFunc // task ID → cancel
}

// New creates a Runner. onEvent may be nil (events are dropped).
func New(
	agents store.AgentRepo,
	tasks store.TaskRepo,
	reg *registry.Registry,
	onEvent EventHandler,
) *Runner {
	return &Runner{
		agents:   agents,
		tasks:    tasks,
		registry: reg,
		onEvent:  onEvent,
		cancels:  make(map[string]context.CancelFunc),
	}
}

// SetEventHandler replaces the event handler after construction.
// Safe to call before any tasks are started.
func (r *Runner) SetEventHandler(h EventHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onEvent = h
}

// RunTask starts execution of the given task in a background goroutine.
// It transitions the task to Queued immediately, then Running when the
// goroutine starts.
func (r *Runner) RunTask(ctx context.Context, taskID string) error {
	task, err := r.tasks.Get(ctx, taskID)
	if err != nil {
		return fmt.Errorf("runner: get task: %w", err)
	}
	if task == nil {
		return fmt.Errorf("runner: task %s not found", taskID)
	}

	// Only start tasks that are pending.
	if task.Status != model.TaskStatusPending {
		return fmt.Errorf("runner: task %s is not pending (status: %s)", taskID, task.Status)
	}

	if err := r.setStatus(ctx, task, model.TaskStatusQueued, nil); err != nil {
		return err
	}

	taskCtx, cancel := context.WithCancel(ctx)
	r.mu.Lock()
	r.cancels[taskID] = cancel
	r.mu.Unlock()

	go func() {
		defer func() {
			r.mu.Lock()
			delete(r.cancels, taskID)
			r.mu.Unlock()
			cancel()
		}()
		r.execute(taskCtx, task)
	}()

	return nil
}

// ResumeTask resumes a task that is awaiting approval.
// It re-fetches the task (which may have updated output/instructions from the
// approval action) and continues execution.
func (r *Runner) ResumeTask(ctx context.Context, taskID string) error {
	task, err := r.tasks.Get(ctx, taskID)
	if err != nil {
		return fmt.Errorf("runner: get task for resume: %w", err)
	}
	if task == nil {
		return fmt.Errorf("runner: task %s not found", taskID)
	}
	if task.Status != model.TaskStatusAwaitingApproval {
		return fmt.Errorf("runner: task %s is not awaiting approval", taskID)
	}

	if err := r.setStatus(ctx, task, model.TaskStatusQueued, nil); err != nil {
		return err
	}

	taskCtx, cancel := context.WithCancel(ctx)
	r.mu.Lock()
	r.cancels[taskID] = cancel
	r.mu.Unlock()

	go func() {
		defer func() {
			r.mu.Lock()
			delete(r.cancels, taskID)
			r.mu.Unlock()
			cancel()
		}()
		r.execute(taskCtx, task)
	}()

	return nil
}

// CancelTask cancels a running task.
func (r *Runner) CancelTask(taskID string) {
	r.mu.Lock()
	cancel, ok := r.cancels[taskID]
	r.mu.Unlock()
	if ok {
		cancel()
	}
}

// Shutdown cancels all running tasks and clears the cancel map.
func (r *Runner) Shutdown() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, cancel := range r.cancels {
		cancel()
		delete(r.cancels, id)
	}
}

// ActiveTasks returns the IDs of all currently running tasks.
func (r *Runner) ActiveTasks() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := make([]string, 0, len(r.cancels))
	for id := range r.cancels {
		ids = append(ids, id)
	}
	return ids
}

// ---- Internal execution ----

func (r *Runner) execute(ctx context.Context, task *model.Task) {
	// Load agent.
	agent, err := r.agents.Get(ctx, task.AgentID)
	if err != nil || agent == nil {
		r.failTask(ctx, task, fmt.Errorf("agent %s not found: %w", task.AgentID, err))
		return
	}

	// Load provider.
	prov, err := r.registry.Get(ctx, agent.ProviderID)
	if err != nil {
		r.failTask(ctx, task, fmt.Errorf("provider load failed: %w", err))
		return
	}

	// Transition to Running.
	now := time.Now()
	task.StartedAt = &now
	if err := r.setStatus(ctx, task, model.TaskStatusRunning, nil); err != nil {
		log.Printf("runner: set running status: %v", err)
		return
	}

	// Assemble prompt.
	req := AssembleRequest(agent, task)

	// Stream execution.
	ch, err := prov.StreamExecute(ctx, req)
	if err != nil {
		r.failTask(ctx, task, fmt.Errorf("stream execute: %w", err))
		return
	}

	var outputBuilder []string
	var totalCost float64

	for chunk := range ch {
		if chunk.Error != nil {
			r.failTask(ctx, task, chunk.Error)
			return
		}
		if chunk.Content != "" {
			outputBuilder = append(outputBuilder, chunk.Content)
			r.emit(StreamEvent{
				TaskID:  task.ID,
				AgentID: task.AgentID,
				Chunk:   &chunk.Content,
			})
		}
	}

	// Collect full output.
	fullOutput := ""
	for _, s := range outputBuilder {
		fullOutput += s
	}

	// Calculate cost from token usage (we re-execute non-streaming for token counts
	// only when cost pricing is configured — otherwise use streaming result).
	// For Phase 1 simplicity: run Execute() to get accurate token counts.
	// TODO Phase 2: extract token counts from streaming response headers/trailer.
	nonStreamResp, costErr := prov.Execute(ctx, req)
	if costErr == nil {
		totalCost = nonStreamResp.CostUSD
	}

	// Build output JSON.
	outputJSON, _ := json.Marshal(map[string]interface{}{
		"text":       fullOutput,
		"tokens_in":  nonStreamResp.TokensIn,
		"tokens_out": nonStreamResp.TokensOut,
		"run_id":     uuid.New().String(),
	})

	// Persist result.
	completedAt := time.Now()
	task.Output = string(outputJSON)
	task.CostUSD = totalCost
	task.CompletedAt = &completedAt

	finalStatus := model.TaskStatusCompleted
	if err := r.setStatus(ctx, task, finalStatus, nil); err != nil {
		log.Printf("runner: set completed status: %v", err)
		return
	}

	r.emit(StreamEvent{
		TaskID:     task.ID,
		AgentID:    task.AgentID,
		StatusDone: &finalStatus,
	})
}

// failTask marks a task as failed and emits an error event.
func (r *Runner) failTask(ctx context.Context, task *model.Task, err error) {
	log.Printf("runner: task %s failed: %v", task.ID, err)

	errJSON, _ := json.Marshal(map[string]string{"error": err.Error()})
	task.Output = string(errJSON)

	completedAt := time.Now()
	task.CompletedAt = &completedAt

	status := model.TaskStatusFailed
	if setErr := r.setStatus(ctx, task, status, nil); setErr != nil {
		log.Printf("runner: set failed status: %v", setErr)
	}

	r.emit(StreamEvent{
		TaskID:     task.ID,
		AgentID:    task.AgentID,
		StatusDone: &status,
		Err:        err,
	})
}

// setStatus updates a task's status in the DB and in-memory.
func (r *Runner) setStatus(ctx context.Context, task *model.Task, status model.TaskStatus, completedAt *time.Time) error {
	task.Status = status
	if completedAt != nil {
		task.CompletedAt = completedAt
	}
	return r.tasks.Update(ctx, task)
}

// emit calls the event handler if one is registered.
func (r *Runner) emit(ev StreamEvent) {
	if r.onEvent != nil {
		r.onEvent(ev)
	}
}
