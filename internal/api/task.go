package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/solarisjon/phoenix/internal/model"
)

// search returns a unified search result across tasks, agents, and projects.
func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		respond(w, http.StatusOK, map[string]interface{}{
			"tasks":    []*model.Task{},
			"agents":   []*model.Agent{},
			"projects": []*model.Project{},
		})
		return
	}
	safe := fts5Quote(q)

	tasks, err := s.tasks.Search(r.Context(), safe)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	userID := userFromCtx(r.Context()).ID
	agents, err := s.agents.Search(r.Context(), safe, userID)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	projects, err := s.projects.Search(r.Context(), safe, userID)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if tasks == nil {
		tasks = []*model.Task{}
	}
	if agents == nil {
		agents = []*model.Agent{}
	}
	if projects == nil {
		projects = []*model.Project{}
	}
	respond(w, http.StatusOK, map[string]interface{}{
		"tasks":    tasks,
		"agents":   agents,
		"projects": projects,
	})
}

func (s *Server) searchTasks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		respond(w, http.StatusOK, []*model.Task{})
		return
	}
	// Escape FTS5 special chars by wrapping each token in double-quotes.
	// This lets users type plain words without FTS5 syntax errors.
	safe := fts5Quote(q)
	list, err := s.tasks.Search(r.Context(), safe)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if list == nil {
		list = []*model.Task{}
	}
	respond(w, http.StatusOK, list)
}

// fts5Quote wraps each whitespace-delimited token in double-quotes so that
// plain user input is treated as literal phrase search in FTS5.
func fts5Quote(q string) string {
	tokens := strings.Fields(q)
	for i, t := range tokens {
		tokens[i] = `"` + strings.ReplaceAll(t, `"`, `""`) + `"`
	}
	return strings.Join(tokens, " ")
}

func (s *Server) listRunningTasks(w http.ResponseWriter, r *http.Request) {
	statuses := []model.TaskStatus{
		model.TaskStatusRunning,
		model.TaskStatusQueued,
	}
	list, err := s.tasks.ListByStatuses(r.Context(), statuses)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if list == nil {
		list = []*model.Task{}
	}
	respond(w, http.StatusOK, list)
}

// listAttentionTasks returns all tasks needing human attention:
// failed, awaiting_approval, and recently completed, across all projects, newest first.
func (s *Server) listAttentionTasks(w http.ResponseWriter, r *http.Request) {
	statuses := []model.TaskStatus{
		model.TaskStatusFailed,
		model.TaskStatusAwaitingApproval,
	}
	list, err := s.tasks.ListByStatuses(r.Context(), statuses)
	if err != nil {
		respondInternalErr(w, err)
		return
	}

	completed, err := s.tasks.ListCompletedForInbox(r.Context(), 100)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	list = append(list, completed...)

	if list == nil {
		list = []*model.Task{}
	}
	respond(w, http.StatusOK, list)
}

func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("project_id")
	if projectID == "" {
		// No project_id → return all tasks across all projects
		list, err := s.tasks.ListAll(r.Context())
		if err != nil {
			respondInternalErr(w, err)
			return
		}
		if list == nil {
			list = []*model.Task{}
		}
		respond(w, http.StatusOK, list)
		return
	}

	// Optional ?status= and ?limit= filters.
	status := model.TaskStatus(r.URL.Query().Get("status"))
	limitStr := r.URL.Query().Get("limit")
	limit := 0
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			limit = n
		}
	}

	list, err := s.tasks.ListByProject(r.Context(), projectID, status, limit)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if list == nil {
		list = []*model.Task{}
	}
	respond(w, http.StatusOK, list)
}

func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	t, err := s.tasks.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if t == nil {
		respondErr(w, http.StatusNotFound, "task not found")
		return
	}
	respond(w, http.StatusOK, t)
}

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	var req createTaskRequest
	if !decode(w, r, &req) {
		return
	}
	if msg := req.validate(); msg != "" {
		respondErr(w, http.StatusBadRequest, msg)
		return
	}

	// Verify project exists.
	proj, err := s.projects.Get(r.Context(), req.ProjectID)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if proj == nil {
		respondErr(w, http.StatusBadRequest, "project not found")
		return
	}

	agentID := req.AgentID
	taskType := model.TaskTypeStandard

	if agentID == "" {
		// No explicit agent — try dynamic orchestration.
		sysSettings, settErr := s.systemSettings.Get(r.Context())
		if settErr != nil {
			respondInternalErr(w, settErr)
			return
		}
		if !sysSettings.DynamicOrchestrationEnabled || sysSettings.OrchestratorAgentID == "" {
			respondErr(w, http.StatusBadRequest, "agent_id is required (dynamic orchestration is not enabled)")
			return
		}
		agentID = sysSettings.OrchestratorAgentID
		taskType = model.TaskTypeOrchestration
	} else {
		// Verify agent exists.
		a, err := s.agents.Get(r.Context(), agentID)
		if err != nil {
			respondInternalErr(w, err)
			return
		}
		if a == nil {
			respondErr(w, http.StatusBadRequest, "agent not found")
			return
		}

		// Verify agent is assigned to this project.
		assigned, err := s.projects.IsAgentAssigned(r.Context(), req.ProjectID, agentID)
		if err != nil {
			respondInternalErr(w, err)
			return
		}
		if !assigned {
			// Check if dynamic orchestration can handle this case.
			sysSettings, _ := s.systemSettings.Get(r.Context())
			if sysSettings != nil && sysSettings.DynamicOrchestrationEnabled && sysSettings.OrchestratorAgentID != "" {
				agentID = sysSettings.OrchestratorAgentID
				taskType = model.TaskTypeOrchestration
			} else {
				respondErr(w, http.StatusBadRequest,
					"agent is not assigned to this project — call POST /projects/{id}/agents first")
				return
			}
		}
	}

	input := req.Input
	if input == "" {
		input = "{}"
	}

	status := model.TaskStatusPending
	if len(req.DependsOn) > 0 {
		// Tasks with dependencies start as queued (not pending) so the scheduler
		// doesn't try to dispatch them — they stay blocked until UnlockDependents
		// clears their depends_on after all prereqs complete.
		status = model.TaskStatusQueued
	}
	t := &model.Task{
		ID:          uuid.New().String(),
		ProjectID:   req.ProjectID,
		AgentID:     agentID,
		Title:       strings.TrimSpace(req.Title),
		Description: req.Description,
		Source:      req.Source,
		CriticMode:  req.CriticMode,
		Status:      status,
		TaskType:    taskType,
		Input:       input,
		Output:      "{}",
		DependsOn:   req.DependsOn,
		CreatedAt:   time.Now(),
	}
	if err := s.tasks.Create(r.Context(), t); err != nil {
		respondInternalErr(w, err)
		return
	}

	// Tasks with unmet dependencies sit as queued until UnlockDependents promotes them.
	if len(req.DependsOn) > 0 {
		respond(w, http.StatusCreated, t)
		return
	}

	// Kick off execution asynchronously.
	if err := s.runner.RunTask(r.Context(), t.ID); err != nil {
		// Task is created but failed to queue — return it with a warning so the
		// caller knows it needs to be retried or monitored.
		respond(w, http.StatusCreated, map[string]interface{}{
			"task":    t,
			"warning": "task created but failed to queue for execution: " + err.Error(),
		})
		return
	}

	// Re-fetch to get the updated queued status.
	updated, _ := s.tasks.Get(r.Context(), t.ID)
	if updated != nil {
		t = updated
	}

	respond(w, http.StatusCreated, t)
}

func (s *Server) updateTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := s.tasks.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if existing == nil {
		respondErr(w, http.StatusNotFound, "task not found")
		return
	}

	// Allow updating title, description, and input on non-running tasks.
	var req struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Input       string `json:"input"`
	}
	if !decode(w, r, &req) {
		return
	}

	if existing.Status == model.TaskStatusRunning || existing.Status == model.TaskStatusQueued {
		respondErr(w, http.StatusConflict, "cannot edit a running or queued task")
		return
	}

	if strings.TrimSpace(req.Title) != "" {
		existing.Title = strings.TrimSpace(req.Title)
	}
	existing.Description = req.Description
	if req.Input != "" {
		existing.Input = req.Input
	}

	if err := s.tasks.Update(r.Context(), existing); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusOK, existing)
}

func (s *Server) dismissTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	task, err := s.tasks.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if task == nil {
		respondErr(w, http.StatusNotFound, "task not found")
		return
	}
	task.Dismissed = true
	if err := s.tasks.Update(r.Context(), task); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusOK, task)
}

func (s *Server) undismissTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	task, err := s.tasks.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if task == nil {
		respondErr(w, http.StatusNotFound, "task not found")
		return
	}
	task.Dismissed = false
	if err := s.tasks.Update(r.Context(), task); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusOK, task)
}

func (s *Server) retryTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	task, err := s.tasks.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if task == nil {
		respondErr(w, http.StatusNotFound, "task not found")
		return
	}
	if task.Status != model.TaskStatusFailed && task.Status != model.TaskStatusPending {
		respondErr(w, http.StatusConflict, "only failed or pending tasks can be retried")
		return
	}

	// Before clearing output: preserve the failure reason so it survives the retry.
	// This gives the human context when diagnosing tasks that fail repeatedly.
	if task.Status == model.TaskStatusFailed {
		var prev struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal([]byte(task.Output), &prev); err == nil && prev.Error != "" {
			task.LastError = prev.Error
		}
	}

	// Reset state for a fresh run.
	task.Status = model.TaskStatusPending
	task.Output = "{}"
	task.StartedAt = nil
	task.CompletedAt = nil
	task.CostUSD = 0
	if err := s.tasks.Update(r.Context(), task); err != nil {
		respondInternalErr(w, err)
		return
	}

	if err := s.runner.RunTask(r.Context(), task.ID); err != nil {
		respondInternalErr(w, err)
		return
	}

	updated, _ := s.tasks.Get(r.Context(), task.ID)
	if updated != nil {
		task = updated
	}
	respond(w, http.StatusOK, task)
}

// sandboxProjectID is the well-known UUID for the "Quick Tasks" sandbox project.
// Using a fixed ID means ensureSandboxProject is idempotent across restarts.
const sandboxProjectID = "00000000-0000-0000-0000-000000000002"

// ensureSandboxProject creates the Quick Tasks project if it doesn't exist.
func (s *Server) ensureSandboxProject(ctx context.Context) error {
	existing, err := s.projects.Get(ctx, sandboxProjectID)
	if err != nil {
		return fmt.Errorf("check sandbox project: %w", err)
	}
	if existing != nil {
		return nil // already exists
	}
	user, err := s.users.GetDefault(ctx)
	if err != nil {
		return fmt.Errorf("get default user for sandbox project: %w", err)
	}
	p := &model.Project{
		ID:          sandboxProjectID,
		Name:        "Quick Tasks",
		Description: "One-off tasks not tied to a specific project.",
		Owner:       user.ID,
		Status:      model.ProjectStatusActive,
		CreatedAt:   time.Now(),
	}
	return s.projects.Create(ctx, p)
}

// quickTask creates and immediately runs a task without requiring a project.
// It uses a well-known sandbox project as the container.
func (s *Server) quickTask(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AgentID     string `json:"agent_id"`
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.AgentID) == "" {
		respondErr(w, http.StatusBadRequest, "agent_id is required")
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		respondErr(w, http.StatusBadRequest, "title is required")
		return
	}

	// Verify agent exists.
	a, err := s.agents.Get(r.Context(), req.AgentID)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if a == nil {
		respondErr(w, http.StatusBadRequest, "agent not found")
		return
	}

	// Ensure sandbox project exists.
	if err := s.ensureSandboxProject(r.Context()); err != nil {
		respondInternalErr(w, err)
		return
	}

	// Auto-assign agent to sandbox project (idempotent).
	if _, err := s.projects.AssignAgent(r.Context(), sandboxProjectID, req.AgentID); err != nil {
		respondInternalErr(w, err)
		return
	}

	t := &model.Task{
		ID:          uuid.New().String(),
		ProjectID:   sandboxProjectID,
		AgentID:     req.AgentID,
		Title:       strings.TrimSpace(req.Title),
		Description: strings.TrimSpace(req.Description),
		Status:      model.TaskStatusPending,
		Input:       "{}",
		Output:      "{}",
		CreatedAt:   time.Now(),
	}
	if err := s.tasks.Create(r.Context(), t); err != nil {
		respondInternalErr(w, err)
		return
	}
	if err := s.runner.RunTask(r.Context(), t.ID); err != nil {
		respond(w, http.StatusCreated, t)
		return
	}
	updated, _ := s.tasks.Get(r.Context(), t.ID)
	if updated != nil {
		t = updated
	}
	respond(w, http.StatusCreated, t)
}

// followUpTask creates a new task as a human refinement of an existing completed task.
// The parent task's output is automatically injected as context when the follow-up runs.
func (s *Server) followUpTask(w http.ResponseWriter, r *http.Request) {
	parentID := chi.URLParam(r, "id")
	parent, err := s.tasks.Get(r.Context(), parentID)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if parent == nil || parent.Dismissed {
		respondErr(w, http.StatusNotFound, "task not found")
		return
	}
	if parent.Status == model.TaskStatusRunning || parent.Status == model.TaskStatusQueued {
		respondErr(w, http.StatusConflict, "cannot follow up a running or queued task — wait for it to complete")
		return
	}

	var req struct {
		Description string `json:"description"`
		AgentID     string `json:"agent_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Description) == "" {
		respondErr(w, http.StatusBadRequest, "description is required")
		return
	}
	agentID := req.AgentID
	if agentID == "" {
		agentID = parent.AgentID
	}

	t := &model.Task{
		ID:          uuid.New().String(),
		ProjectID:   parent.ProjectID,
		AgentID:     agentID,
		FollowUpOf:  &parentID,
		Title:       parent.Title,
		Description: strings.TrimSpace(req.Description),
		Status:      model.TaskStatusPending,
		Input:       "{}",
		Output:      "{}",
		CreatedAt:   time.Now(),
	}
	if err := s.tasks.Create(r.Context(), t); err != nil {
		respondInternalErr(w, err)
		return
	}
	if err := s.runner.RunTask(r.Context(), t.ID); err != nil {
		respond(w, http.StatusCreated, t)
		return
	}
	updated, _ := s.tasks.Get(r.Context(), t.ID)
	if updated != nil {
		t = updated
	}
	respond(w, http.StatusCreated, t)
}

func (s *Server) deleteTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := s.tasks.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if existing == nil {
		respondErr(w, http.StatusNotFound, "task not found")
		return
	}
	// Cancel if running.
	s.runner.CancelTask(id)

	if err := s.tasks.Delete(r.Context(), id); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusNoContent, nil)
}

func (s *Server) bumpTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	task, err := s.tasks.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if task == nil {
		respondErr(w, http.StatusNotFound, "task not found")
		return
	}
	if task.Status != model.TaskStatusQueued {
		respondErr(w, http.StatusConflict, "only queued tasks can be bumped")
		return
	}
	if err := s.tasks.BumpPriority(r.Context(), id); err != nil {
		respondInternalErr(w, err)
		return
	}
	task.Priority += 10
	respond(w, http.StatusOK, task)
}

func (s *Server) cancelTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	task, err := s.tasks.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if task == nil {
		respondErr(w, http.StatusNotFound, "task not found")
		return
	}
	if task.Status != model.TaskStatusRunning && task.Status != model.TaskStatusQueued {
		respondErr(w, http.StatusConflict, "task is not running or queued")
		return
	}
	s.runner.CancelTask(id)
	respond(w, http.StatusNoContent, nil)
}

// forceResetTask immediately marks a task as failed regardless of its current state.
// It kills the subprocess PID if recorded and cancels any running goroutine.
// Use this when the regular cancel button has no effect on a stuck task.
func (s *Server) forceResetTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	task, err := s.tasks.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if task == nil {
		respondErr(w, http.StatusNotFound, "task not found")
		return
	}
	if task.Status == model.TaskStatusCompleted || task.Status == model.TaskStatusFailed {
		respondErr(w, http.StatusConflict, "task is already in a terminal state")
		return
	}
	if err := s.runner.ForceCancel(id); err != nil {
		respondInternalErr(w, err)
		return
	}
	updated, _ := s.tasks.Get(r.Context(), id)
	if updated == nil {
		updated = task
	}
	respond(w, http.StatusOK, updated)
}
