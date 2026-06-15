package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/provider"
	"github.com/solarisjon/phoenix/internal/provider/registry"
	"github.com/solarisjon/phoenix/internal/store"
)

// ErrTaskCancelledByUser is the cancel cause set when a task is stopped via the API.
var ErrTaskCancelledByUser = errors.New("task cancelled by user")

// BudgetInfo carries the budget state when a project's cost limit is exceeded.
// It is set on a StreamEvent to trigger a budget.exceeded WebSocket broadcast.
type BudgetInfo struct {
	ProjectID string
	SpentUSD  float64
	BudgetUSD float64
	Period    string
}

// StreamEvent is emitted during task execution and consumed by the WebSocket hub.
type StreamEvent struct {
	TaskID  string
	AgentID string
	// One of the following is set per event:
	Chunk          *string    // partial output text
	StatusDone     *model.TaskStatus
	Err            error
	BudgetExceeded *BudgetInfo // non-nil when the project budget was exceeded
}

// EventHandler is called with each StreamEvent during task execution.
// Implementations must be non-blocking (e.g. send to a buffered channel).
type EventHandler func(StreamEvent)

// DefaultTaskTimeout is the maximum time a single task may run before
// being forcibly cancelled and marked failed.
const DefaultTaskTimeout = 30 * time.Minute

// MemoHandler is called when the runner extracts a memo from task output.
// Implementations must be non-blocking.
type MemoHandler func(memo *model.Memo)

// Runner manages agent task execution. Each task runs in its own goroutine.
// All active goroutines are tracked so they can be cancelled on shutdown.
type Runner struct {
	agents   store.AgentRepo
	tasks    store.TaskRepo
	projects store.ProjectRepo
	settings store.SystemSettingsRepo
	memos    store.MemoRepo
	registry *registry.Registry
	onEvent  EventHandler
	onMemo   MemoHandler
	bgCtx    context.Context    // long-lived background context for task goroutines
	bgCancel context.CancelFunc // cancelled on Shutdown

	taskTimeout  time.Duration
	mu           sync.Mutex
	cancels      map[string]context.CancelCauseFunc // task ID → cancel-with-cause
	agentRunning map[string]int                     // agent ID → count of running goroutines
}

// New creates a Runner. onEvent and onMemo may be nil (events/memos are dropped).
func New(
	agents store.AgentRepo,
	tasks store.TaskRepo,
	projects store.ProjectRepo,
	settings store.SystemSettingsRepo,
	memos store.MemoRepo,
	reg *registry.Registry,
	onEvent EventHandler,
) *Runner {
	bgCtx, bgCancel := context.WithCancel(context.Background())
	return &Runner{
		agents:       agents,
		tasks:        tasks,
		projects:     projects,
		settings:     settings,
		memos:        memos,
		registry:     reg,
		onEvent:      onEvent,
		bgCtx:        bgCtx,
		bgCancel:     bgCancel,
		taskTimeout:  DefaultTaskTimeout,
		cancels:      make(map[string]context.CancelCauseFunc),
		agentRunning: make(map[string]int),
	}
}

// SetTaskTimeout overrides the per-task execution deadline.
// Must be called before any tasks are started.
func (r *Runner) SetTaskTimeout(d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.taskTimeout = d
}

// SetMemoHandler sets the callback invoked when a memo is extracted from output.
func (r *Runner) SetMemoHandler(h MemoHandler) { r.onMemo = h }

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

// ForceCancel immediately marks a task as failed regardless of its current state.
// It sends a cancel signal to any running goroutine, kills the subprocess PID if
// one is recorded, and directly updates the DB — bypassing the normal goroutine
// cleanup path. Use this to unstick a task that won't respond to regular cancel.
func (r *Runner) ForceCancel(taskID string) error {
	// 1. Cancel any running goroutine (best-effort).
	r.mu.Lock()
	if cancel, ok := r.cancels[taskID]; ok {
		cancel(ErrTaskCancelledByUser)
	}
	r.mu.Unlock()

	// 2. Kill the subprocess PID if it was recorded.
	task, err := r.tasks.Get(r.bgCtx, taskID)
	if err != nil {
		return fmt.Errorf("force cancel: get task: %w", err)
	}
	if task == nil {
		return fmt.Errorf("force cancel: task %s not found", taskID)
	}
	if task.RunnerPID > 0 {
		if proc, procErr := os.FindProcess(task.RunnerPID); procErr == nil {
			if killErr := proc.Kill(); killErr != nil {
				log.Printf("runner: force cancel: kill pid %d: %v", task.RunnerPID, killErr)
			} else {
				log.Printf("runner: force cancel: killed pid %d for task %s", task.RunnerPID, taskID)
			}
		}
	}

	// 3. Write failed status directly to DB so the UI sees it immediately.
	updated, err := r.tasks.ForceFailTask(r.bgCtx, taskID)
	if err != nil {
		return fmt.Errorf("force cancel: db update: %w", err)
	}
	if updated {
		status := model.TaskStatusFailed
		r.emit(StreamEvent{
			TaskID:     taskID,
			StatusDone: &status,
			Err:        ErrTaskCancelledByUser,
		})
		log.Printf("runner: force cancelled task %s", taskID)
	}
	return nil
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
	timeoutCtx, timeoutCancel := context.WithTimeout(r.bgCtx, r.taskTimeout)
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
			if started := r.tryStartLocked(next); !started {
				// Another concurrent drainQueue call already claimed this task.
				// That goroutine will drain the rest when it completes.
				r.mu.Unlock()
				return
			}
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

	// Load project to get working directory and monitor model override.
	var workingDir string
	var monitorModel string
	var proj *model.Project
	if p, err := r.projects.Get(ctx, task.ProjectID); err == nil && p != nil {
		proj = p
		workingDir = proj.WorkingDir
		monitorModel = proj.MonitorModel
	}

	// Load provider. For monitor tasks, the project-level monitor_model takes
	// priority over the agent's model_override so cheap models can be used for
	// signal-detection without changing the agent's default for project tasks.
	modelOverride := agent.ModelOverride
	if task.Source == "monitor" && monitorModel != "" {
		modelOverride = monitorModel
	}
	prov, err := r.registry.GetWithOverride(ctx, agent.ProviderID, modelOverride)
	if err != nil {
		r.failTask(ctx, task, fmt.Errorf("provider load failed: %w", err))
		return
	}

	// Transition to Running.
	now := time.Now()
	task.StartedAt = &now
	timeoutAt := now.Add(r.taskTimeout)
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

	// Assemble prompt. Builtin-critic tasks use a specialist prompt built from
	// the reviewed task's output; all other tasks go through the normal path.
	var req provider.TaskRequest
	if task.IsCriticReview && task.CriticMode == model.CriticModeBuiltin && task.ReviewedTaskID != nil {
		reviewed, err := r.tasks.Get(ctx, *task.ReviewedTaskID)
		if err != nil || reviewed == nil {
			r.failTask(ctx, task, fmt.Errorf("builtin critic: reviewed task %s not found: %w", *task.ReviewedTaskID, err))
			return
		}
		req = BuildBuiltinCriticRequest(reviewed)
	} else {
		req = AssembleRequest(agent, task, globalGuardrails)
		if task.FollowUpOf != nil {
			parent, err := r.tasks.Get(ctx, *task.FollowUpOf)
			if err == nil && parent != nil {
				req = InjectFollowUpContext(req, parent)
			}
		}
	}
	req.WorkingDir = workingDir

	// Cost budget guardrail: if the agent has a max_cost_per_run ceiling and
	// the provider can estimate cost, check the assembled prompt against the
	// budget. Context turns are dropped from the oldest end until it fits.
	// If it still exceeds the budget after clearing all context:
	//   - If a fallback_model is configured, switch to it and continue.
	//   - Otherwise fail with a clear error.
	// Providers that return 0 from EstimateCost (CLI adapters, local models)
	// are skipped — there's no cost to guard against.
	if agent.MaxCostPerRun > 0 {
		if est := prov.EstimateCost(req); est.EstimatedCostUSD > 0 {
			for prov.EstimateCost(req).EstimatedCostUSD > agent.MaxCostPerRun && len(req.Context) > 0 {
				req.Context = req.Context[1:] // drop oldest context turn
			}
			if prov.EstimateCost(req).EstimatedCostUSD > agent.MaxCostPerRun {
				if agent.FallbackModel != "" {
					log.Printf("runner: task %s: estimated cost ($%.5f) exceeds max_cost_per_run ($%.5f) after context truncation — switching to fallback model %q",
						task.ID, prov.EstimateCost(req).EstimatedCostUSD, agent.MaxCostPerRun, agent.FallbackModel)
					fallbackProv, err := r.registry.GetWithOverride(ctx, agent.ProviderID, agent.FallbackModel)
					if err != nil {
						r.failTask(ctx, task, fmt.Errorf("cost budget exceeded and fallback model load failed (%s): %w", agent.FallbackModel, err))
						return
					}
					prov = fallbackProv
				} else {
					r.failTask(ctx, task, fmt.Errorf("cost budget exceeded: estimated cost ($%.5f) exceeds max_cost_per_run ($%.5f) even after clearing all context — shorten the task, raise the limit, or set a fallback_model",
						prov.EstimateCost(req).EstimatedCostUSD, agent.MaxCostPerRun))
					return
				}
			}
		}
	}
	// whether a previous run produced identical output. If so, skip the LLM
	// call entirely and reuse the cached result — cost $0.
	if task.Source == "monitor" {
		hash := promptHash(req)
		task.PromptHash = hash
		if cached, err := r.tasks.FindByPromptHash(ctx, task.ProjectID, hash); err == nil && cached != nil {
			log.Printf("runner: monitor task %s: prompt unchanged (hash=%s), reusing cached output", task.ID, hash[:8])
			completedAt := time.Now()
			task.Output = cached.Output
			task.HealthSignal = cached.HealthSignal
			task.TokensIn = 0
			task.TokensOut = 0
			task.CostUSD = 0
			task.CompletedAt = &completedAt
			task.RunnerPID = 0
			task.Source = "monitor:cached"
			if err := r.setStatus(ctx, task, model.TaskStatusCompleted, nil); err != nil {
				log.Printf("runner: set cached status: %v", err)
			}
			r.emit(StreamEvent{
				TaskID:     task.ID,
				AgentID:    task.AgentID,
				StatusDone: func() *model.TaskStatus { s := model.TaskStatusCompleted; return &s }(),
			})
			return
		}
	}

	// Budget check: if the project has a cost limit, verify we haven't exceeded it
	// before firing the LLM. This runs after the cache check so free cached runs
	// never count toward or trigger the budget gate.
	if proj != nil && proj.BudgetUSD > 0 {
		spent, err := r.tasks.ProjectSpendForPeriod(ctx, proj.ID, proj.BudgetPeriod)
		if err != nil {
			log.Printf("runner: budget check failed for project %s: %v", proj.ID, err)
		} else if spent >= proj.BudgetUSD {
			log.Printf("runner: project %s budget exceeded — spent=$%.4f budget=$%.4f period=%s",
				proj.ID, spent, proj.BudgetUSD, proj.BudgetPeriod)
			r.emit(StreamEvent{
				BudgetExceeded: &BudgetInfo{
					ProjectID: proj.ID,
					SpentUSD:  spent,
					BudgetUSD: proj.BudgetUSD,
					Period:    proj.BudgetPeriod,
				},
			})
			r.failTask(ctx, task, fmt.Errorf(
				"budget exceeded: $%.4f spent of $%.4f %s budget — update the budget or wait for the period to reset",
				spent, proj.BudgetUSD, proj.BudgetPeriod))
			return
		}
	}

	// Stream execution.
	ch, err := prov.StreamExecute(ctx, req)
	if err != nil {
		r.failTask(ctx, task, fmt.Errorf("stream execute: %w", err))
		return
	}

	var outputBuilder []string
	var realCostUSD float64
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
		// Capture real token counts when the provider reports them (e.g. Ollama, LLM SSE,
		// opencode all send these on the final Done chunk). Zero means not available.
		if chunk.TokensIn > 0 {
			realTokensIn = chunk.TokensIn
		}
		if chunk.TokensOut > 0 {
			realTokensOut = chunk.TokensOut
		}
		// Accumulate actual cost when the provider reports it directly (e.g. opencode
		// sums across multiple step_finish events; LLM calculates from rate config).
		if chunk.CostUSD > 0 {
			realCostUSD += chunk.CostUSD
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

	// Prefer actual cost reported by the provider over the pre-run estimate.
	// Fall back to the estimate only when the provider couldn't report cost.
	totalCost := realCostUSD
	if totalCost == 0 {
		if estimate := prov.EstimateCost(req); estimate.EstimatedCostUSD > 0 {
			totalCost = estimate.EstimatedCostUSD
		}
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
	// PromptHash is already set for monitor tasks (set before the cache check above).
	// For non-monitor tasks it stays empty — no diffing needed.

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

	// Extract any MEMO blocks the agent embedded in its output and persist them.
	// For any completed task (not critic reviews), if the agent didn't post a memo,
	// auto-create one from the output so the Briefing always reflects task completions.
	if r.memos != nil {
		posted := r.extractAndSaveMemos(task, agent, fullOutput)
		// Extract artifact declarations and create briefing entries for each.
		r.extractAndSaveArtifacts(task, agent, fullOutput)
		if !posted && !task.IsCriticReview {
			r.autoMemo(task, agent, fullOutput)
		}
	}

	// After successful completion, run a critic/devil's-advocate review if configured.
	// Critic reviews are never themselves reviewed (avoids infinite loops).
	if !task.IsCriticReview {
		r.maybeLaunchCritic(task, agent)
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

// ---- Memo extraction ----

// extractAndSaveMemos scans agent output for MEMO blocks and persists each one.
// A MEMO block looks like:
//
//	MEMO_START
//	Title: <single line title>
//	Priority: high          (optional; defaults to normal)
//	<body markdown — everything until MEMO_END>
//	MEMO_END
//
// Multiple blocks are supported in a single output.
// extractAndSaveMemos scans agent output for MEMO blocks and persists each one.
// Returns true if at least one memo was saved.
func (r *Runner) extractAndSaveMemos(task *model.Task, a *model.Agent, output string) bool {
	memoBlocks := parseMemoBlocks(output)
	if len(memoBlocks) == 0 {
		return false
	}

	// Look up project name for display (best-effort; empty string is fine).
	var projectName string
	if proj, err := r.projects.Get(r.bgCtx, task.ProjectID); err == nil && proj != nil {
		projectName = proj.Name
	}

	saved := false
	for _, block := range memoBlocks {
		memo := &model.Memo{
			ID:          uuid.New().String(),
			ProjectID:   task.ProjectID,
			ProjectName: projectName,
			TaskID:      task.ID,
			AgentID:     a.ID,
			AgentName:   a.Name,
			Title:       block.title,
			Body:        block.body,
			Priority:    block.priority,
			Status:      model.MemoStatusUnread,
			CreatedAt:   time.Now(),
		}
		if err := r.memos.Create(r.bgCtx, memo); err != nil {
			log.Printf("runner: save memo from task %s: %v", task.ID, err)
			continue
		}
		log.Printf("runner: memo saved from task %s: %q", task.ID, memo.Title)
		if r.onMemo != nil {
			r.onMemo(memo)
		}
		saved = true
	}
	return saved
}

// autoMemo creates a fallback memo for any task that completed successfully but
// whose agent didn't emit a MEMO_START block. This ensures the Briefing always
// reflects what every completed task did, even when the agent skips the memo.
func (r *Runner) autoMemo(task *model.Task, a *model.Agent, output string) {
	var projectName string
	if proj, err := r.projects.Get(r.bgCtx, task.ProjectID); err == nil && proj != nil {
		projectName = proj.Name
	}

	// Truncate very long outputs so the memo body is readable.
	body := output
	const maxBody = 4000
	if len(body) > maxBody {
		body = body[:maxBody] + "\n\n_[output truncated — open the task for the full run log]_"
	}

	memo := &model.Memo{
		ID:          uuid.New().String(),
		ProjectID:   task.ProjectID,
		ProjectName: projectName,
		TaskID:      task.ID,
		AgentID:     a.ID,
		AgentName:   a.Name,
		Title:       task.Title,
		Body:        body,
		Priority:    model.MemoPriorityNormal,
		Status:      model.MemoStatusUnread,
		CreatedAt:   time.Now(),
	}
	if err := r.memos.Create(r.bgCtx, memo); err != nil {
		log.Printf("runner: auto-memo for task %s: %v", task.ID, err)
		return
	}
	log.Printf("runner: auto-memo created for task %s: %q", task.ID, memo.Title)
	if r.onMemo != nil {
		r.onMemo(memo)
	}
}

// ---- Critic / Devil's Advocate ----

// maybeLaunchCritic resolves the effective critic mode for the completed task
// (task-level overrides project-level) and launches a critic task if needed.
func (r *Runner) maybeLaunchCritic(task *model.Task, originalAgent *model.Agent) {
	// Resolve effective critic mode: task setting, unless it says "inherit".
	mode := task.CriticMode
	if mode == "" || mode == model.CriticModeInherit {
		// Fall back to project setting.
		if proj, err := r.projects.Get(r.bgCtx, task.ProjectID); err == nil && proj != nil {
			mode = proj.CriticMode
		}
	}
	if mode == "" || mode == model.CriticModeNone {
		return
	}

	switch {
	case mode == model.CriticModeBuiltin:
		r.launchBuiltinCritic(task, originalAgent)
	case len(mode) > 6 && mode[:6] == "agent:":
		agentID := mode[6:]
		r.launchAgentCritic(task, agentID)
	}
}

// launchBuiltinCritic spawns an ephemeral devil's advocate review using the
// same provider as the original agent — no registered agent record needed.
// Routes through RunTask so MaxConcurrent is respected like any other task.
func (r *Runner) launchBuiltinCritic(task *model.Task, originalAgent *model.Agent) {
	criticTask := &model.Task{
		ID:             uuid.New().String(),
		ProjectID:      task.ProjectID,
		AgentID:        originalAgent.ID, // same agent; execute() swaps in the critic system prompt
		Title:          "Devil's Advocate: " + task.Title,
		Status:         model.TaskStatusPending,
		Source:         "critic",
		IsCriticReview: true,
		CriticMode:     model.CriticModeBuiltin,
		ReviewedTaskID: &task.ID,
		CreatedAt:      time.Now(),
	}
	if err := r.tasks.Create(r.bgCtx, criticTask); err != nil {
		log.Printf("runner: create builtin critic task: %v", err)
		return
	}
	if err := r.RunTask(r.bgCtx, criticTask.ID); err != nil {
		log.Printf("runner: run builtin critic task: %v", err)
	}
}

// launchAgentCritic spawns a critic task using a specific registered agent.
func (r *Runner) launchAgentCritic(task *model.Task, criticAgentID string) {
	criticAgent, err := r.agents.Get(r.bgCtx, criticAgentID)
	if err != nil || criticAgent == nil {
		log.Printf("runner: critic agent %s not found: %v", criticAgentID, err)
		return
	}
	criticTask := &model.Task{
		ID:             uuid.New().String(),
		ProjectID:      task.ProjectID,
		AgentID:        criticAgentID,
		Title:          "Critic Review: " + task.Title,
		Description:    "You are reviewing the output of a completed task. Provide an objective critique: what was done well, what could be improved, any risks or concerns.\n\nOriginal Task: " + task.Title + "\n\nTask Output:\n" + task.Output,
		Status:         model.TaskStatusPending,
		Source:         "critic",
		IsCriticReview: true,
		CriticMode:     "agent:" + criticAgentID,
		ReviewedTaskID: &task.ID,
		CreatedAt:      time.Now(),
	}
	if err := r.tasks.Create(r.bgCtx, criticTask); err != nil {
		log.Printf("runner: create agent critic task: %v", err)
		return
	}
	if err := r.RunTask(r.bgCtx, criticTask.ID); err != nil {
		log.Printf("runner: run agent critic task: %v", err)
	}
}

type parsedMemo struct {
	title    string
	body     string
	priority model.MemoPriority
}

// parseMemoBlocks extracts all MEMO_START … MEMO_END sections from text.
func parseMemoBlocks(output string) []parsedMemo {
	var results []parsedMemo
	lines := strings.Split(output, "\n")

	i := 0
	for i < len(lines) {
		if strings.TrimSpace(lines[i]) != "MEMO_START" {
			i++
			continue
		}
		// Found a block start — collect until MEMO_END.
		i++
		var title string
		priority := model.MemoPriorityNormal
		var bodyLines []string
		headerDone := false

		for i < len(lines) {
			if strings.TrimSpace(lines[i]) == "MEMO_END" {
				i++
				break
			}
			line := lines[i]
			if !headerDone {
				if strings.HasPrefix(line, "Title:") {
					title = strings.TrimSpace(strings.TrimPrefix(line, "Title:"))
					i++
					continue
				}
				if strings.HasPrefix(line, "Priority:") {
					pval := strings.TrimSpace(strings.ToLower(strings.TrimPrefix(line, "Priority:")))
					if pval == "high" {
						priority = model.MemoPriorityHigh
					}
					i++
					continue
				}
				// First non-header line starts the body.
				headerDone = true
			}
			bodyLines = append(bodyLines, line)
			i++
		}

		if title == "" || len(bodyLines) == 0 {
			continue // skip malformed blocks
		}
		results = append(results, parsedMemo{
			title:    title,
			body:     strings.TrimSpace(strings.Join(bodyLines, "\n")),
			priority: priority,
		})
	}
	return results
}

// ---- Artifact extraction ----

// parsedArtifact holds one ARTIFACT_START … ARTIFACT_END block.
type parsedArtifact struct {
	artType string // "file" | "url" | "jira" | "confluence" | "html"
	path    string // file path or URL
	title   string
}

// parseArtifactBlocks extracts all ARTIFACT_START … ARTIFACT_END sections from text.
//
// Expected format in agent output:
//
//	ARTIFACT_START
//	Type: file          (or "url", "jira", "confluence", "html")
//	Path: /abs/path     (use URL: for non-file types)
//	Title: My Document
//	ARTIFACT_END
func parseArtifactBlocks(output string) []parsedArtifact {
	var results []parsedArtifact
	lines := strings.Split(output, "\n")
	i := 0
	for i < len(lines) {
		if strings.TrimSpace(lines[i]) != "ARTIFACT_START" {
			i++
			continue
		}
		i++
		var a parsedArtifact
		for i < len(lines) {
			if strings.TrimSpace(lines[i]) == "ARTIFACT_END" {
				i++
				break
			}
			line := lines[i]
			switch {
			case strings.HasPrefix(line, "Type:"):
				a.artType = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "Type:")))
			case strings.HasPrefix(line, "Path:"):
				a.path = strings.TrimSpace(strings.TrimPrefix(line, "Path:"))
			case strings.HasPrefix(line, "URL:"):
				a.path = strings.TrimSpace(strings.TrimPrefix(line, "URL:"))
			case strings.HasPrefix(line, "Title:"):
				a.title = strings.TrimSpace(strings.TrimPrefix(line, "Title:"))
			}
			i++
		}
		if a.artType != "" && a.path != "" {
			results = append(results, a)
		}
	}
	return results
}

// extractAndSaveArtifacts scans agent output for ARTIFACT_START blocks and creates
// a briefing memo entry for each one so they appear in the Briefing panel.
func (r *Runner) extractAndSaveArtifacts(task *model.Task, a *model.Agent, output string) {
	artifacts := parseArtifactBlocks(output)
	if len(artifacts) == 0 {
		return
	}
	var projectName string
	if proj, err := r.projects.Get(r.bgCtx, task.ProjectID); err == nil && proj != nil {
		projectName = proj.Name
	}
	for _, art := range artifacts {
		title := art.title
		if title == "" {
			title = art.path
		}
		body := fmt.Sprintf("**Type:** %s\n\n**Location:** %s", art.artType, art.path)
		memo := &model.Memo{
			ID:          uuid.New().String(),
			ProjectID:   task.ProjectID,
			ProjectName: projectName,
			TaskID:      task.ID,
			AgentID:     a.ID,
			AgentName:   a.Name,
			Title:       "Artifact: " + title,
			Body:        body,
			Priority:    model.MemoPriorityNormal,
			Status:      model.MemoStatusUnread,
			CreatedAt:   time.Now(),
		}
		if err := r.memos.Create(r.bgCtx, memo); err != nil {
			log.Printf("runner: save artifact memo for task %s: %v", task.ID, err)
			continue
		}
		log.Printf("runner: artifact memo saved for task %s: %q", task.ID, memo.Title)
		if r.onMemo != nil {
			r.onMemo(memo)
		}
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

// promptHash returns a hex SHA-256 of the assembled prompt components.
// Used by monitor diffing to detect unchanged prompts and skip the LLM call.
func promptHash(req provider.TaskRequest) string {
	h := sha256.New()
	h.Write([]byte(req.SystemPrompt))
	h.Write([]byte{0})
	h.Write([]byte(req.Prompt))
	for _, m := range req.Context {
		h.Write([]byte{0})
		h.Write([]byte(m.Role))
		h.Write([]byte{0})
		h.Write([]byte(m.Content))
	}
	return hex.EncodeToString(h.Sum(nil))
}
