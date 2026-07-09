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
	providers      store.ProviderRepo   // nil before SetProviderRepo is called
	obsidianVaults store.ObsidianVaultRepo // nil = disabled
	skills         store.SkillRepo         // nil = disabled
	registry       *registry.Registry
	orchestrator   *Orchestrator        // nil until SetOrchestrator is called
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

// SetSkillRepo wires in the skill store so the runner can inject skill
// instructions into prompts (see InjectSkills).
func (r *Runner) SetSkillRepo(repo store.SkillRepo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.skills = repo
}

// SetMemoryClient sets the memory backend used for recall/retain.
// Pass nil to disable persistent memory.
func (r *Runner) SetMemoryClient(c memory.MemoryClient) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.memoryClient = c
}

// SetProviderRepo wires in the provider store so the runner can pass it to the
// orchestrator for model pool lookups.
func (r *Runner) SetProviderRepo(repo store.ProviderRepo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers = repo
}

// SetOrchestrator wires in the orchestrator. Must be called after New().
func (r *Runner) SetOrchestrator(o *Orchestrator) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.orchestrator = o
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

// serverURL returns the Phoenix API base URL from PHOENIX_BASE_URL env var,
// falling back to http://localhost:8080 when the variable is unset.
func (r *Runner) serverURL() string {
	if u := os.Getenv("PHOENIX_BASE_URL"); u != "" {
		return u
	}
	return "http://localhost:8080"
}

// ---- Internal execution ----

// executionContext holds the resolved agent, project, and provider for a task run.
type executionContext struct {
	agent      *model.Agent
	proj       *model.Project // may be nil
	prov       provider.Provider
	workingDir string
}

// loadExecutionContext resolves the agent, project, and provider for the given task.
func (r *Runner) loadExecutionContext(ctx context.Context, task *model.Task) (*executionContext, error) {
	agent, err := r.agents.Get(ctx, task.AgentID)
	if err != nil || agent == nil {
		return nil, fmt.Errorf("agent %s not found: %w", task.AgentID, err)
	}

	var ec executionContext
	ec.agent = agent

	if p, err := r.projects.Get(ctx, task.ProjectID); err == nil && p != nil {
		ec.proj = p
		ec.workingDir = p.WorkingDir
	}

	modelOverride := agent.ModelOverride
	if task.Source == "monitor" && ec.proj != nil && ec.proj.MonitorModel != "" {
		modelOverride = ec.proj.MonitorModel
	}
	prov, err := r.registry.GetWithOverride(ctx, agent.ProviderID, modelOverride)
	if err != nil {
		return nil, fmt.Errorf("provider load failed: %w", err)
	}
	ec.prov = prov
	return &ec, nil
}

// buildTaskRequest assembles the full provider.TaskRequest for a task, including
// follow-up chain context, Obsidian vault routing, and memory recall injection.
func (r *Runner) buildTaskRequest(ctx context.Context, task *model.Task, ec *executionContext, globalGuardrails string) (provider.TaskRequest, error) {
	var req provider.TaskRequest
	if task.IsCriticReview && task.CriticMode == model.CriticModeBuiltin && task.ReviewedTaskID != nil {
		reviewed, err := r.tasks.Get(ctx, *task.ReviewedTaskID)
		if err != nil || reviewed == nil {
			return req, fmt.Errorf("builtin critic: reviewed task %s not found: %w", *task.ReviewedTaskID, err)
		}
		req = BuildBuiltinCriticRequest(reviewed)
	} else {
		req = AssembleRequest(ec.agent, task, ec.proj, globalGuardrails, r.serverURL())

		// If this agent is the global orchestrator, append decomposition instructions.
		if ec.agent.IsOrchestrator && task.TaskType == model.TaskTypeOrchestration {
			allAgents, _ := r.agents.List(ctx, "")
			r.mu.Lock()
			provRepo := r.providers
			r.mu.Unlock()
			allProviders := []*model.Provider{}
			if provRepo != nil {
				allProviders, _ = provRepo.List(ctx, "")
			}
			sysSettings, _ := r.settings.Get(ctx)
			maxDepth, maxPerLevel := 2, 5
			if sysSettings != nil {
				if sysSettings.MaxSubtaskDepth > 0 {
					maxDepth = sysSettings.MaxSubtaskDepth
				}
				if sysSettings.MaxSubtasksPerLevel > 0 {
					maxPerLevel = sysSettings.MaxSubtasksPerLevel
				}
			}
			req = InjectOrchestratorInstructions(req, allAgents, allProviders, maxDepth, maxPerLevel)
		}

		if task.FollowUpOf != nil {
			rootID := *task.FollowUpOf
			chain, chainErr := r.tasks.ListFollowUpChain(ctx, rootID)
			if chainErr != nil || len(chain) == 0 {
				if parent, err := r.tasks.Get(ctx, rootID); err == nil && parent != nil {
					req = InjectFollowUpContext(req, parent)
				}
			} else if ec.proj != nil && ec.proj.ContextSummarisation && ShouldSummariseChain(chain) {
				root := chain[0]
				summary := root.SummaryCache
				if summary == "" {
					oldTurns := chain
					if len(chain) > contextSummarisationKeepRecent {
						oldTurns = chain[:len(chain)-contextSummarisationKeepRecent]
					}
					summResp, summErr := ec.prov.Execute(ctx, BuildSummaryRequest(oldTurns))
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
				req = InjectFollowUpChainContext(req, chain, "")
			}
		}
	}
	req.WorkingDir = ec.workingDir

	if ec.proj != nil && ec.proj.ReactMode && !task.IsCriticReview {
		maxIter := ec.proj.MaxIterations
		if maxIter <= 0 {
			maxIter = reactMaxIterationsDefault
		}
		req = InjectReactLoopInstructions(req, maxIter, task.LoopIteration)
	}

	if r.obsidianVaults != nil && r.settings != nil && !task.IsCriticReview {
		if sysSettings, err := r.settings.Get(ctx); err == nil && sysSettings.ObsidianEnabled {
			if vaults, err := r.obsidianVaults.ListEnabled(r.bgCtx); err != nil {
				slog.Warn("runner: obsidian vault list failed", "task_id", task.ID, "error", err)
			} else if len(vaults) > 0 {
				req = InjectObsidianVaults(req, vaults)
			}
		}
	}

	if r.skills != nil && !task.IsCriticReview {
		if allSkills, err := r.skills.ListEnabled(r.bgCtx); err != nil {
			slog.Warn("runner: skill list failed", "task_id", task.ID, "error", err)
		} else if len(allSkills) > 0 {
			req = InjectSkills(req, allSkills, task, ec.proj)
		}
	}

	r.mu.Lock()
	memClient := r.memoryClient
	r.mu.Unlock()
	if memClient != nil && !task.IsCriticReview {
		if memories, err := memClient.Recall(ctx, ec.agent.ID, task.Title+" "+task.Description); err != nil {
			slog.Warn("runner: memory recall failed", "task_id", task.ID, "agent_id", ec.agent.ID, "error", err)
		} else if memories != "" {
			req = InjectMemories(req, memories)
		}
	}

	return req, nil
}

// streamResult holds the collected output and usage from a provider stream.
type streamResult struct {
	fullText  string
	tokensIn  int
	tokensOut int
	costUSD   float64
}

// streamAndCollect runs the provider stream for req and collects all output.
// It records the subprocess PID on the first chunk and emits content events.
// Returns an error for stream failures, context cancellations, and empty output.
func (r *Runner) streamAndCollect(ctx context.Context, req provider.TaskRequest, prov provider.Provider, task *model.Task) (*streamResult, error) {
	ch, err := prov.StreamExecute(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("stream execute: %w", err)
	}

	var outputBuilder []string
	var realCostUSD float64
	var realTokensIn, realTokensOut int

	for chunk := range ch {
		if chunk.Error != nil {
			taskErr := chunk.Error
			if cause := context.Cause(ctx); cause == ErrTaskCancelledByUser {
				taskErr = ErrTaskCancelledByUser
			}
			return nil, taskErr
		}
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
		if chunk.TokensIn > 0 {
			realTokensIn = chunk.TokensIn
		}
		if chunk.TokensOut > 0 {
			realTokensOut = chunk.TokensOut
		}
		if chunk.CostUSD > 0 {
			realCostUSD += chunk.CostUSD
		}
	}

	// Fail if the context expired while draining (e.g. orphaned child processes
	// held the stdout pipe open past the deadline).
	if ctxErr := ctx.Err(); ctxErr != nil {
		if cause := context.Cause(ctx); cause == ErrTaskCancelledByUser {
			return nil, ErrTaskCancelledByUser
		} else if errors.Is(ctxErr, context.DeadlineExceeded) {
			return nil, fmt.Errorf("task timed out after %s", r.taskTimeout)
		}
		return nil, ctxErr
	}

	fullOutput := strings.Join(outputBuilder, "")
	if strings.TrimSpace(fullOutput) == "" {
		return nil, fmt.Errorf("provider returned empty output (0 tokens) — the model may have timed out, hit a token limit, or failed silently; retry to try again")
	}

	// Use real token counts; fall back to a character-based estimate (1 token ≈ 4 chars).
	tokensIn := realTokensIn
	if tokensIn == 0 {
		charEstimate := len(req.SystemPrompt) + len(req.Prompt)
		for _, m := range req.Context {
			charEstimate += len(m.Content)
		}
		charEstimate += len(fullOutput)
		tokensIn = charEstimate / 4
	}
	tokensOut := realTokensOut
	if tokensOut == 0 {
		tokensOut = len(fullOutput) / 4
	}

	totalCost := realCostUSD
	if totalCost == 0 {
		if estimate := prov.EstimateCost(req); estimate.EstimatedCostUSD > 0 {
			totalCost = estimate.EstimatedCostUSD
		}
	}

	return &streamResult{
		fullText:  fullOutput,
		tokensIn:  tokensIn,
		tokensOut: tokensOut,
		costUSD:   totalCost,
	}, nil
}

// finaliseTask persists a successful task result, extracts memos/artifacts,
// triggers post-run hooks (critic, memory retain, Obsidian), and emits the
// completion event. Handles the guardrail-triggered awaiting_approval path.
func (r *Runner) finaliseTask(ctx context.Context, task *model.Task, out *streamResult, ec *executionContext) {
	outputJSON, _ := json.Marshal(map[string]interface{}{
		"text":       out.fullText,
		"tokens_in":  out.tokensIn,
		"tokens_out": out.tokensOut,
		"run_id":     uuid.New().String(),
	})
	completedAt := time.Now()
	task.Output = string(outputJSON)
	task.CostUSD = out.costUSD
	task.CompletedAt = &completedAt
	task.RunnerPID = 0
	task.TokensIn = out.tokensIn
	task.TokensOut = out.tokensOut

	if reason := extractGuardrailTrigger(out.fullText); reason != "" {
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

	if task.Source == "monitor" {
		sig := deriveHealthSignal(out.fullText)
		task.HealthSignal = &sig
		if ec.proj != nil {
			consecutiveBad := r.updateHeartbeatSignal(ec.proj, sig)
			if sig != "all_clear" {
				r.maybeReactToHealthSignal(task, ec.proj, sig, consecutiveBad)
			}
		}
	}

	finalStatus := model.TaskStatusCompleted
	if err := r.setStatus(r.bgCtx, task, finalStatus, nil); err != nil {
		slog.Error("runner: set completed status", "error", err)
		return
	}

	// If this is an orchestration task, process the plan asynchronously.
	if task.TaskType == model.TaskTypeOrchestration {
		r.mu.Lock()
		orch := r.orchestrator
		r.mu.Unlock()
		if orch != nil {
			go func() {
				orchCtx, cancel := context.WithTimeout(r.bgCtx, 5*time.Minute)
				defer cancel()
				orch.HandleOrchestrationComplete(orchCtx, task, out.fullText)
			}()
		}
	}

	// Wake any tasks that were blocked on this one.
	if agentIDs, err := r.tasks.UnlockDependents(r.bgCtx, task.ID); err != nil {
		slog.Warn("runner: unlock dependents", "task_id", task.ID, "error", err)
	} else {
		for _, aid := range agentIDs {
			go r.drainQueue(aid)
		}
	}

	if r.memos != nil {
		posted := r.extractAndSaveMemos(task, ec.agent, out.fullText)
		r.extractAndSaveArtifacts(task, ec.agent, out.fullText)
		if !posted && !task.IsCriticReview {
			r.autoMemo(task, ec.agent, out.fullText)
		}
	}

	r.mu.Lock()
	memClient := r.memoryClient
	r.mu.Unlock()
	if memClient != nil && !task.IsCriticReview {
		retainContent := task.Title + "\n\n" + out.fullText
		go func() {
			retainCtx, cancel := context.WithTimeout(r.bgCtx, 30*time.Second)
			defer cancel()
			if err := memClient.Retain(retainCtx, ec.agent.ID, retainContent); err != nil {
				slog.Warn("runner: memory retain failed", "task_id", task.ID, "agent_id", ec.agent.ID, "error", err)
			}
		}()
	}

	if !task.IsCriticReview {
		r.maybeLaunchCritic(task, ec.agent)
		r.maybeAutoWriteObsidian(task, ec.agent, out.fullText)
		r.maybeSpawnReActIteration(task, ec.proj, out.fullText)
	}

	r.emit(StreamEvent{
		TaskID:     task.ID,
		AgentID:    task.AgentID,
		StatusDone: &finalStatus,
	})
}

func (r *Runner) execute(ctx context.Context, task *model.Task) {
	ec, err := r.loadExecutionContext(ctx, task)
	if err != nil {
		r.failTask(ctx, task, err)
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

	req, err := r.buildTaskRequest(ctx, task, ec, globalGuardrails)
	if err != nil {
		r.failTask(ctx, task, err)
		return
	}

	prov := ec.prov

	// Cost budget guardrail: drop oldest context turns until within budget;
	// switch to fallback model if it still exceeds the limit after clearing all context.
	// Providers that return 0 from EstimateCost (CLI adapters, local models) are skipped.
	if ec.agent.MaxCostPerRun > 0 {
		if est := prov.EstimateCost(req); est.EstimatedCostUSD > 0 {
			for prov.EstimateCost(req).EstimatedCostUSD > ec.agent.MaxCostPerRun && len(req.Context) > 0 {
				req.Context = req.Context[1:]
			}
			if prov.EstimateCost(req).EstimatedCostUSD > ec.agent.MaxCostPerRun {
				if ec.agent.FallbackModel != "" {
					slog.Warn("runner: estimated cost exceeds max_cost_per_run — switching to fallback model",
						"task_id", task.ID,
						"cost_usd", prov.EstimateCost(req).EstimatedCostUSD,
						"max_cost_usd", ec.agent.MaxCostPerRun,
						"fallback_model", ec.agent.FallbackModel)
					fallbackProv, err := r.registry.GetWithOverride(ctx, ec.agent.ProviderID, ec.agent.FallbackModel)
					if err != nil {
						r.failTask(ctx, task, fmt.Errorf("cost budget exceeded and fallback model load failed (%s): %w", ec.agent.FallbackModel, err))
						return
					}
					prov = fallbackProv
				} else {
					r.failTask(ctx, task, fmt.Errorf("cost budget exceeded: estimated cost ($%.5f) exceeds max_cost_per_run ($%.5f) even after clearing all context — shorten the task, raise the limit, or set a fallback_model",
						prov.EstimateCost(req).EstimatedCostUSD, ec.agent.MaxCostPerRun))
					return
				}
			}
		}
	}

	// Monitor dedup: skip the LLM call and reuse cached output when the prompt is unchanged.
	// Bypass the cache when MonitorCacheTTL is set and the cached result is older than the TTL.
	if task.Source == "monitor" {
		hash := promptHash(req)
		task.PromptHash = hash
		cacheTTL := 0
		if ec.proj != nil {
			cacheTTL = ec.proj.MonitorCacheTTL
		}
		if cached, err := r.tasks.FindByPromptHash(ctx, task.ProjectID, hash); err == nil && cached != nil &&
			(cacheTTL <= 0 || (cached.CompletedAt != nil && time.Since(*cached.CompletedAt) < time.Duration(cacheTTL)*time.Second)) {
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

	// Project budget gate: runs after cache check so free cached runs never
	// count toward or trigger the limit.
	if ec.proj != nil && ec.proj.BudgetUSD > 0 {
		spent, err := r.tasks.ProjectSpendForPeriod(ctx, ec.proj.ID, ec.proj.BudgetPeriod)
		if err != nil {
			slog.Error("runner: budget check failed", "project_id", ec.proj.ID, "error", err)
		} else if spent >= ec.proj.BudgetUSD {
			slog.Warn("runner: project budget exceeded", "project_id", ec.proj.ID, "spent_usd", spent, "budget_usd", ec.proj.BudgetUSD, "period", ec.proj.BudgetPeriod)
			r.emit(StreamEvent{
				BudgetExceeded: &BudgetInfo{
					ProjectID: ec.proj.ID,
					SpentUSD:  spent,
					BudgetUSD: ec.proj.BudgetUSD,
					Period:    ec.proj.BudgetPeriod,
				},
			})
			r.failTask(ctx, task, fmt.Errorf(
				"budget exceeded: $%.4f spent of $%.4f %s budget — update the budget or wait for the period to reset",
				spent, ec.proj.BudgetUSD, ec.proj.BudgetPeriod))
			return
		}
	}

	out, runErr := r.streamAndCollect(ctx, req, prov, task)
	if runErr != nil {
		r.failTask(r.bgCtx, task, runErr)
		return
	}

	r.finaliseTask(ctx, task, out, ec)
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
