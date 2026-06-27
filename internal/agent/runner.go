package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/plugin/memory"
	"github.com/solarisjon/phoenix/internal/provider"
	"github.com/solarisjon/phoenix/internal/provider/registry"
	"github.com/solarisjon/phoenix/internal/store"
)

// DefaultTaskTimeout is the maximum time a single task may run before
// being forcibly cancelled and marked failed.
const DefaultTaskTimeout = 30 * time.Minute

// MemoHandler is called when the runner extracts a memo from task output.
// Implementations must be non-blocking.
type MemoHandler func(memo *model.Memo)

// Runner manages agent task execution. Each task runs in its own goroutine.
// All active goroutines are tracked so they can be cancelled on shutdown.
type Runner struct {
	agents         store.AgentRepo
	tasks          store.TaskRepo
	projects       store.ProjectRepo
	settings       store.SystemSettingsRepo
	memos          store.MemoRepo
	obsidianVaults store.ObsidianVaultRepo // nil = disabled
	registry       *registry.Registry
	onEvent        EventHandler
	onMemo         MemoHandler
	memoryClient   memory.MemoryClient // nil = disabled
	bgCtx          context.Context     // long-lived background context for task goroutines
	bgCancel       context.CancelFunc  // cancelled on Shutdown

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

// SetObsidianVaultRepo wires in the Obsidian vault store so the runner can
// inject vault context into prompts and write notes on task completion.
func (r *Runner) SetObsidianVaultRepo(repo store.ObsidianVaultRepo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.obsidianVaults = repo
}

// SetMemoryClient sets the memory backend used for recall/retain.
// Pass nil to disable persistent memory.
func (r *Runner) SetMemoryClient(c memory.MemoryClient) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.memoryClient = c
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
		slog.Error("runner: cancel queued task", "task_id", taskID, "error", err)
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
				slog.Error("runner: force cancel: kill pid", "pid", task.RunnerPID, "error", killErr)
			} else {
				slog.Info("runner: force cancel: killed pid", "pid", task.RunnerPID, "task_id", taskID)
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
		slog.Info("runner: force cancelled task", "task_id", taskID)
	}
	return nil
}

// StartTimeoutWatchdog starts a background goroutine that periodically finds
// tasks whose timeout_at has passed but whose goroutine exited without updating
// the DB (e.g. because the context was already dead when the DB write ran).
// It force-cancels any such tasks every minute until the runner shuts down.
func (r *Runner) StartTimeoutWatchdog() {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-r.bgCtx.Done():
				return
			case <-ticker.C:
				r.reapTimedOutTasks()
			}
		}
	}()
}

// reapTimedOutTasks finds tasks that are past timeout_at but still show
// running in the DB without an active goroutine, and force-cancels them.
func (r *Runner) reapTimedOutTasks() {
	tasks, err := r.tasks.ListTimedOut(r.bgCtx)
	if err != nil {
		slog.Error("runner: watchdog: list timed-out tasks", "error", err)
		return
	}
	r.mu.Lock()
	orphans := make([]string, 0, len(tasks))
	for _, t := range tasks {
		if _, active := r.cancels[t.ID]; !active {
			orphans = append(orphans, t.ID)
		}
	}
	r.mu.Unlock()

	for _, id := range orphans {
		slog.Info("runner: watchdog: force-cancelling timed-out task", "task_id", id)
		if err := r.ForceCancel(id); err != nil {
			slog.Error("runner: watchdog: force-cancel", "task_id", id, "error", err)
		}
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
		slog.Error("runner: set running status", "error", err)
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
			// Walk the full follow-up chain from root so all prior context is available.
			rootID := *task.FollowUpOf
			chain, chainErr := r.tasks.ListFollowUpChain(ctx, rootID)
			if chainErr != nil || len(chain) == 0 {
				// Fallback: single-parent injection (original behaviour).
				if parent, err := r.tasks.Get(ctx, rootID); err == nil && parent != nil {
					req = InjectFollowUpContext(req, parent)
				}
			} else if proj != nil && proj.ContextSummarisation && ShouldSummariseChain(chain) {
				// Summarisation path: use cached summary or fire a summarise call.
				root := chain[0]
				summary := root.SummaryCache
				if summary == "" {
					// Turns to summarise = everything except the most recent contextSummarisationKeepRecent.
					oldTurns := chain
					if len(chain) > contextSummarisationKeepRecent {
						oldTurns = chain[:len(chain)-contextSummarisationKeepRecent]
					}
					summReq := BuildSummaryRequest(oldTurns)
					summResp, summErr := prov.Execute(ctx, summReq)
					if summErr != nil {
						slog.Warn("runner: context summarisation failed (falling back to verbatim)", "task_id", task.ID, "error", summErr)
					} else {
						summary = summResp.Output
						if saveErr := r.tasks.SaveSummaryCache(ctx, root.ID, summary); saveErr != nil {
							slog.Error("runner: save summary cache", "task_id", task.ID, "error", saveErr)
						}
					}
				}
				req = InjectFollowUpChainContext(req, chain, summary)
			} else {
				// Verbatim path (no summarisation): include all prior turns.
				req = InjectFollowUpChainContext(req, chain, "")
			}
		}
	}
	req.WorkingDir = workingDir

	// Inject Obsidian vault routing context when vaults are configured.
	if r.obsidianVaults != nil && !task.IsCriticReview {
		if vaults, err := r.obsidianVaults.ListEnabled(r.bgCtx); err != nil {
			slog.Warn("runner: obsidian vault list failed", "task_id", task.ID, "error", err)
		} else if len(vaults) > 0 {
			req = InjectObsidianVaults(req, vaults)
		}
	}

	// Recall relevant memories for this task and inject them into the prompt.
	// Errors are logged but never propagate — memory failure must not block execution.
	r.mu.Lock()
	memClient := r.memoryClient
	r.mu.Unlock()
	if memClient != nil && !task.IsCriticReview {
		if memories, err := memClient.Recall(ctx, agent.ID, task.Title+" "+task.Description); err != nil {
			slog.Warn("runner: memory recall failed", "task_id", task.ID, "agent_id", agent.ID, "error", err)
		} else if memories != "" {
			req = InjectMemories(req, memories)
		}
	}

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
					slog.Warn("runner: estimated cost exceeds max_cost_per_run — switching to fallback model",
						"task_id", task.ID,
						"cost_usd", prov.EstimateCost(req).EstimatedCostUSD,
						"max_cost_usd", agent.MaxCostPerRun,
						"fallback_model", agent.FallbackModel)
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
			slog.Info("runner: monitor task prompt unchanged, reusing cached output", "task_id", task.ID, "hash", hash[:8])
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
				slog.Error("runner: set cached status", "error", err)
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
			slog.Error("runner: budget check failed", "project_id", proj.ID, "error", err)
		} else if spent >= proj.BudgetUSD {
			slog.Warn("runner: project budget exceeded", "project_id", proj.ID, "spent_usd", spent, "budget_usd", proj.BudgetUSD, "period", proj.BudgetPeriod)
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
			r.failTask(r.bgCtx, task, taskErr)
			return
		}
		// Capture subprocess PID on the first chunk and persist it so
		// we can kill the process if Phoenix restarts mid-task.
		if chunk.PID != 0 && task.RunnerPID == 0 {
			task.RunnerPID = chunk.PID
			if updateErr := r.tasks.Update(ctx, task); updateErr != nil {
				slog.Error("runner: persist pid", "error", updateErr)
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

	// If the context was cancelled (timeout or user cancel) while the chunk loop
	// was draining, fail the task now rather than attempting completion. This
	// handles the case where the provider's goroutine kept running after the
	// deadline fired (e.g. because orphaned child processes held the stdout pipe
	// open) and the loop only exited when they eventually died — long after the
	// context had already expired.
	if ctxErr := ctx.Err(); ctxErr != nil {
		taskErr := ctxErr
		if cause := context.Cause(ctx); cause == ErrTaskCancelledByUser {
			taskErr = ErrTaskCancelledByUser
		} else if errors.Is(ctxErr, context.DeadlineExceeded) {
			taskErr = fmt.Errorf("task timed out after %s", r.taskTimeout)
		}
		r.failTask(r.bgCtx, task, taskErr)
		return
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
			slog.Error("runner: set awaiting_approval status", "error", err)
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
	if err := r.setStatus(r.bgCtx, task, finalStatus, nil); err != nil {
		slog.Error("runner: set completed status", "error", err)
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

	// Retain the task output as a memory for this agent.
	// Fire-and-forget: errors are logged but never affect the task result.
	if memClient != nil && !task.IsCriticReview {
		retainContent := task.Title + "\n\n" + fullOutput
		go func() {
			retainCtx, cancel := context.WithTimeout(r.bgCtx, 30*time.Second)
			defer cancel()
			if err := memClient.Retain(retainCtx, agent.ID, retainContent); err != nil {
				slog.Warn("runner: memory retain failed", "task_id", task.ID, "agent_id", agent.ID, "error", err)
			}
		}()
	}

	// After successful completion, run a critic/devil's-advocate review if configured.
	// Critic reviews are never themselves reviewed (avoids infinite loops).
	if !task.IsCriticReview {
		r.maybeLaunchCritic(task, agent)
		r.maybeAutoWriteObsidian(task, agent, fullOutput)
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
	slog.Error("runner: task failed", "task_id", task.ID, "error", err)

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
		slog.Error("runner: set failed status", "error", setErr)
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
