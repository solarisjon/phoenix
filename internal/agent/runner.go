package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/provider/registry"
	"github.com/solarisjon/phoenix/internal/store"
)

// ErrTaskCancelledByUser is the cancel cause set when a task is stopped via the API.
var ErrTaskCancelledByUser = errors.New("task cancelled by user")

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

// DefaultTaskTimeout is the maximum time a single task may run before
// being forcibly cancelled and marked failed.
const DefaultTaskTimeout = 30 * time.Minute

// Runner manages agent task execution. Each task runs in its own goroutine.
// All active goroutines are tracked so they can be cancelled on shutdown.
type Runner struct {
	agents   store.AgentRepo
	tasks    store.TaskRepo
	projects store.ProjectRepo
	settings store.SystemSettingsRepo
	registry *registry.Registry
	onEvent  EventHandler
	bgCtx    context.Context    // long-lived background context for task goroutines
	bgCancel context.CancelFunc // cancelled on Shutdown

	mu           sync.Mutex
	cancels      map[string]context.CancelCauseFunc // task ID → cancel-with-cause
	agentRunning map[string]int                     // agent ID → count of running goroutines
}

// New creates a Runner. onEvent may be nil (events are dropped).
func New(
	agents store.AgentRepo,
	tasks store.TaskRepo,
	projects store.ProjectRepo,
	settings store.SystemSettingsRepo,
	reg *registry.Registry,
	onEvent EventHandler,
) *Runner {
	bgCtx, bgCancel := context.WithCancel(context.Background())
	return &Runner{
		agents:       agents,
		tasks:        tasks,
		projects:     projects,
		settings:     settings,
		registry:     reg,
		onEvent:      onEvent,
		bgCtx:        bgCtx,
		bgCancel:     bgCancel,
		cancels:      make(map[string]context.CancelCauseFunc),
		agentRunning: make(map[string]int),
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
// It transitions the task to Queued immediately. If the agent has capacity,
// the task starts running right away; otherwise it stays queued until a
// running task completes and drainQueue picks it up.
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

	if err := r.setStatus(r.bgCtx, task, model.TaskStatusQueued, nil); err != nil {
		return err
	}

	agent, err := r.agents.Get(ctx, task.AgentID)
	if err != nil || agent == nil {
		return fmt.Errorf("runner: agent %s not found", task.AgentID)
	}

	r.mu.Lock()
	running := r.agentRunning[task.AgentID]
	maxC := agent.MaxConcurrent
	if maxC == 0 || running < maxC {
		r.tryStartLocked(task)
	}
	r.mu.Unlock()

	return nil
}

// ResumeTask resumes a task that is awaiting approval.
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

	if err := r.setStatus(r.bgCtx, task, model.TaskStatusQueued, nil); err != nil {
		return err
	}

	agent, err := r.agents.Get(ctx, task.AgentID)
	if err != nil || agent == nil {
		return fmt.Errorf("runner: agent %s not found for resume", task.AgentID)
	}

	r.mu.Lock()
	running := r.agentRunning[task.AgentID]
	maxC := agent.MaxConcurrent
	if maxC == 0 || running < maxC {
		r.tryStartLocked(task)
	}
	r.mu.Unlock()

	return nil
}

// CancelTask stops a running or queued task. Safe to call if the task is
// not running — it will attempt to cancel a queued-but-not-started task via DB.
func (r *Runner) CancelTask(taskID string) {
	r.mu.Lock()
	cancel, ok := r.cancels[taskID]
	r.mu.Unlock()
	if ok {
		cancel(ErrTaskCancelledByUser)
		return
	}

	// Task may be queued but not yet running (no goroutine). Cancel it in DB.
	cancelled, err := r.tasks.CancelQueuedTask(r.bgCtx, taskID)
	if err != nil {
		log.Printf("runner: cancel queued task %s: %v", taskID, err)
		return
	}
	if cancelled {
		status := model.TaskStatusFailed
		r.emit(StreamEvent{
			TaskID:     taskID,
			StatusDone: &status,
			Err:        ErrTaskCancelledByUser,
		})
	}
}

// Shutdown cancels all running tasks and clears the cancel map.
func (r *Runner) Shutdown() {
	// Cancelling bgCtx cascades to all task contexts.
	r.bgCancel()
	r.mu.Lock()
	defer r.mu.Unlock()
	for id := range r.cancels {
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

// ---- Concurrency helpers ----

// tryStartLocked starts a goroutine for task. Must be called with r.mu held.
// Returns false without starting if the task is already tracked (concurrent call).
func (r *Runner) tryStartLocked(task *model.Task) bool {
	if _, already := r.cancels[task.ID]; already {
		return false
	}
	timeoutCtx, timeoutCancel := context.WithTimeout(r.bgCtx, DefaultTaskTimeout)
	taskCtx, cancel := context.WithCancelCause(timeoutCtx)
	r.cancels[task.ID] = cancel
	r.agentRunning[task.AgentID]++
	go func() {
		defer func() {
			r.mu.Lock()
			delete(r.cancels, task.ID)
			r.agentRunning[task.AgentID]--
			r.mu.Unlock()
			cancel(nil)
			timeoutCancel()
			r.drainQueue(task.AgentID)
		}()
		r.execute(taskCtx, task)
	}()
	return true
}

// drainQueue starts queued tasks for the agent while it has capacity.
// Called outside the mutex from goroutine defers.
func (r *Runner) drainQueue(agentID string) {
	for {
		next, err := r.tasks.NextQueuedTask(r.bgCtx, agentID)
		if err != nil || next == nil {
			return
		}
		agent, err := r.agents.Get(r.bgCtx, agentID)
		if err != nil || agent == nil {
			return
		}
		r.mu.Lock()
		running := r.agentRunning[agentID]
		maxC := agent.MaxConcurrent
		canStart := maxC == 0 || running < maxC
		if canStart {
			r.tryStartLocked(next) // no-op if another caller already claimed it
		}
		r.mu.Unlock()
		if !canStart {
			return
		}
	}
}

// ---- Internal execution ----

func (r *Runner) execute(ctx context.Context, task *model.Task) {
	// Load agent.
	agent, err := r.agents.Get(ctx, task.AgentID)
	if err != nil || agent == nil {
		r.failTask(ctx, task, fmt.Errorf("agent %s not found: %w", task.AgentID, err))
		return
	}

	// Load provider, applying any agent-level model override.
	prov, err := r.registry.GetWithOverride(ctx, agent.ProviderID, agent.ModelOverride)
	if err != nil {
		r.failTask(ctx, task, fmt.Errorf("provider load failed: %w", err))
		return
	}

	// Load project to get working directory.
	var workingDir string
	if proj, err := r.projects.Get(ctx, task.ProjectID); err == nil && proj != nil {
		workingDir = proj.WorkingDir
	}

	// Transition to Running.
	now := time.Now()
	task.StartedAt = &now
	timeoutAt := now.Add(DefaultTaskTimeout)
	task.TimeoutAt = &timeoutAt
	if err := r.setStatus(ctx, task, model.TaskStatusRunning, nil); err != nil {
		log.Printf("runner: set running status: %v", err)
		return
	}

	// Load global guardrails (non-fatal if unavailable).
	var globalGuardrails string
	if r.settings != nil {
		if sysSettings, err := r.settings.Get(ctx); err == nil && sysSettings.GlobalGuardrailsEnabled {
			globalGuardrails = sysSettings.GlobalGuardrails
		}
	}

	// Assemble prompt. For follow-up tasks, inject the parent output as context.
	req := AssembleRequest(agent, task, globalGuardrails)
	req.WorkingDir = workingDir
	if task.FollowUpOf != nil {
		parent, err := r.tasks.Get(ctx, *task.FollowUpOf)
		if err == nil && parent != nil {
			req = InjectFollowUpContext(req, parent)
		}
	}

	// Stream execution.
	ch, err := prov.StreamExecute(ctx, req)
	if err != nil {
		r.failTask(ctx, task, fmt.Errorf("stream execute: %w", err))
		return
	}

	var outputBuilder []string
	var totalCost float64
	var realTokensIn, realTokensOut int

	for chunk := range ch {
		if chunk.Error != nil {
			// Translate context cancellation into a user-friendly message.
			taskErr := chunk.Error
			if cause := context.Cause(ctx); cause == ErrTaskCancelledByUser {
				taskErr = ErrTaskCancelledByUser
			}
			r.failTask(ctx, task, taskErr)
			return
		}
		// Capture subprocess PID on the first chunk and persist it so
		// we can kill the process if Phoenix restarts mid-task.
		if chunk.PID != 0 && task.RunnerPID == 0 {
			task.RunnerPID = chunk.PID
			if updateErr := r.tasks.Update(ctx, task); updateErr != nil {
				log.Printf("runner: persist pid: %v", updateErr)
			}
		}
		if chunk.Content != "" {
			outputBuilder = append(outputBuilder, chunk.Content)
			r.emit(StreamEvent{
				TaskID:  task.ID,
				AgentID: task.AgentID,
				Chunk:   &chunk.Content,
			})
		}
		// Capture real token counts when the provider reports them (e.g. Ollama
		// sends these on the final Done chunk). Zero means not available.
		if chunk.TokensIn > 0 {
			realTokensIn = chunk.TokensIn
		}
		if chunk.TokensOut > 0 {
			realTokensOut = chunk.TokensOut
		}
	}

	// Collect full output.
	fullOutput := ""
	for _, s := range outputBuilder {
		fullOutput += s
	}

	// Treat empty output as a failure — the provider ran but produced nothing.
	// This catches hung/stalled subprocess runs that exit cleanly but silently.
	if strings.TrimSpace(fullOutput) == "" {
		r.failTask(ctx, task, fmt.Errorf("provider returned empty output (0 tokens) — the model may have timed out, hit a token limit, or failed silently; retry to try again"))
		return
	}

	// Use real token counts when the provider reported them; fall back to a
	// character-based estimate (1 token ≈ 4 chars) for CLI adapters and LLM
	// SSE streams that don't include a usage payload.
	tokensIn := realTokensIn
	tokensOut := realTokensOut
	if tokensIn == 0 {
		charEstimate := len(req.SystemPrompt) + len(req.Prompt)
		for _, m := range req.Context {
			charEstimate += len(m.Content)
		}
		charEstimate += len(fullOutput)
		tokensIn = charEstimate / 4
	}
	if tokensOut == 0 {
		tokensOut = len(fullOutput) / 4
	}
	estimate := prov.EstimateCost(req)
	if estimate.EstimatedCostUSD > 0 {
		totalCost = estimate.EstimatedCostUSD
	}

	// Build output JSON.
	outputJSON, _ := json.Marshal(map[string]interface{}{
		"text":       fullOutput,
		"tokens_in":  tokensIn,
		"tokens_out": tokensOut,
		"run_id":     uuid.New().String(),
	})

	// Persist result.
	completedAt := time.Now()
	task.Output = string(outputJSON)
	task.CostUSD = totalCost
	task.CompletedAt = &completedAt
	task.RunnerPID = 0 // subprocess is done
	task.TokensIn = tokensIn
	task.TokensOut = tokensOut

	// Check if the agent triggered a hard guardrail.
	// A hard guardrail trigger is signalled by the agent outputting a line that starts
	// with "GUARDRAIL_TRIGGERED:" (case-sensitive, matched at line boundary).
	if reason := extractGuardrailTrigger(fullOutput); reason != "" {
		task.GuardrailReason = &reason
		if err := r.setStatus(ctx, task, model.TaskStatusAwaitingApproval, nil); err != nil {
			log.Printf("runner: set awaiting_approval status: %v", err)
		}
		r.emit(StreamEvent{
			TaskID:     task.ID,
			AgentID:    task.AgentID,
			StatusDone: func() *model.TaskStatus { s := model.TaskStatusAwaitingApproval; return &s }(),
		})
		return
	}

	// Derive health signal for monitor tasks.
	if task.Source == "monitor" {
		sig := deriveHealthSignal(fullOutput)
		task.HealthSignal = &sig
	}

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
// It always uses the runner's long-lived background context for DB operations
// so that a cancelled task context (e.g. user cancellation) doesn't prevent
// the failure record from being persisted.
func (r *Runner) failTask(ctx context.Context, task *model.Task, err error) {
	log.Printf("runner: task %s failed: %v", task.ID, err)

	errJSON, _ := json.Marshal(map[string]string{"error": err.Error()})
	task.Output = string(errJSON)
	task.RunnerPID = 0 // subprocess is done or killed

	completedAt := time.Now()
	task.CompletedAt = &completedAt

	// Use bgCtx so DB writes succeed even when the task context was cancelled.
	// Fall back to a plain background context if the server is shutting down.
	dbCtx := r.bgCtx
	if dbCtx.Err() != nil {
		var dbCancel context.CancelFunc
		dbCtx, dbCancel = context.WithTimeout(context.Background(), 5*time.Second)
		defer dbCancel()
	}

	status := model.TaskStatusFailed
	// Monitor tasks always get a health signal, even on failure.
	if task.Source == "monitor" {
		sig := "failed"
		task.HealthSignal = &sig
	}
	if setErr := r.setStatus(dbCtx, task, status, nil); setErr != nil {
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

// deriveHealthSignal inspects the output text of a completed monitor task and
// returns one of three health signals:
//   - "all_clear"       — completed successfully with no alert keywords
//   - "needs_attention" — completed but output contains warning/issue keywords
//   - "failed"          — task itself failed (set separately in failTask)
func deriveHealthSignal(output string) string {
	lower := strings.ToLower(output)
	alertKeywords := []string{
		"error", "warning", "alert", "critical", "failure", "fail", "issue",
		"problem", "exception", "danger", "anomaly", "breach", "exceeded",
		"unavailable", "down", "offline", "unreachable", "timeout", "timed out",
	}
	for _, kw := range alertKeywords {
		if strings.Contains(lower, kw) {
			return "needs_attention"
		}
	}
	return "all_clear"
}

// extractGuardrailTrigger scans the agent output for a hard guardrail trigger.
// It looks for a line that starts exactly with "GUARDRAIL_TRIGGERED:" and returns
// the reason text. Returns "" if no trigger is found.
// The match is anchored to the start of a line to avoid false positives in explanatory text.
func extractGuardrailTrigger(output string) string {
	const marker = "GUARDRAIL_TRIGGERED:"
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, marker) {
			reason := strings.TrimSpace(strings.TrimPrefix(trimmed, marker))
			if reason == "" {
				reason = "Hard guardrail triggered (no reason provided)"
			}
			return reason
		}
	}
	return ""
}
