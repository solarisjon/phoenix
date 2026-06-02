package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/provider"
)

type createTaskRequest struct {
	ProjectID   string `json:"project_id"`
	AgentID     string `json:"agent_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Input       string `json:"input"`
	Source      string `json:"source"` // free-text provenance, e.g. "Jira triage 2026-05-30"
}

func (r createTaskRequest) validate() string {
	if strings.TrimSpace(r.ProjectID) == "" {
		return "project_id is required"
	}
	if strings.TrimSpace(r.AgentID) == "" {
		return "agent_id is required"
	}
	if strings.TrimSpace(r.Title) == "" {
		return "title is required"
	}
	return ""
}

// listRunningTasks returns all tasks currently running or queued, across all projects.
func (s *Server) estimateTask(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AgentID     string `json:"agent_id"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	agent, err := s.agents.Get(r.Context(), req.AgentID)
	if err != nil || agent == nil {
		respondErr(w, http.StatusBadRequest, "agent not found")
		return
	}
	prov, err := s.registry.GetWithOverride(r.Context(), agent.ProviderID, agent.ModelOverride)
	if err != nil {
		// Provider not available — return unsupported
		respond(w, http.StatusOK, map[string]interface{}{"supported": false, "estimated_cost_usd": 0})
		return
	}
	est := prov.EstimateCost(provider.TaskRequest{
		Prompt:       req.Description,
		SystemPrompt: agent.Instructions,
	})
	respond(w, http.StatusOK, map[string]interface{}{
		"supported":          est.EstimatedCostUSD > 0,
		"estimated_cost_usd": est.EstimatedCostUSD,
	})
}

// generateTaskDescription uses an LLM to draft a detailed description for a task.
func (s *Server) generateTaskDescription(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title      string `json:"title"`
		Hint       string `json:"hint"`
		ProviderID string `json:"provider_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		respondErr(w, http.StatusBadRequest, "title is required")
		return
	}

	providerID := req.ProviderID
	if providerID == "" {
		providers, err := s.providers.List(r.Context())
		if err != nil || len(providers) == 0 {
			respondErr(w, http.StatusBadRequest, "no providers available for generation")
			return
		}
		for _, p := range providers {
			if p.Type == model.ProviderTypeLLM {
				providerID = p.ID
				break
			}
		}
		if providerID == "" {
			providerID = providers[0].ID
		}
	}

	prov, err := s.registry.Get(r.Context(), providerID)
	if err != nil {
		respondErr(w, http.StatusBadRequest, fmt.Sprintf("provider load failed: %v", err))
		return
	}

	hintSection := ""
	if strings.TrimSpace(req.Hint) != "" {
		hintSection = "\nAdditional context: " + strings.TrimSpace(req.Hint)
	}

	prompt := fmt.Sprintf(`Write a detailed description for an AI agent task titled "%s".%s

Explain clearly:
- What the agent needs to accomplish
- Any specific requirements or constraints
- What a good result looks like

Be specific and actionable. Return ONLY the description text — no JSON, no markdown headings.`,
		req.Title, hintSection)

	resp, err := prov.Execute(r.Context(), provider.TaskRequest{
		SystemPrompt: "You are a concise technical writer. Return only plain text, no markdown, no JSON.",
		Prompt:       prompt,
	})
	if err != nil {
		respondErr(w, http.StatusInternalServerError, fmt.Sprintf("generation failed: %v", err))
		return
	}

	respond(w, http.StatusOK, map[string]string{"description": strings.TrimSpace(resp.Output)})
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
// failed and awaiting_approval, across all projects, newest first.
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

	list, err := s.tasks.List(r.Context(), projectID)
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

	input := req.Input
	if input == "" {
		input = "{}"
	}

	t := &model.Task{
		ID:          uuid.New().String(),
		ProjectID:   req.ProjectID,
		AgentID:     req.AgentID,
		Title:       strings.TrimSpace(req.Title),
		Description: req.Description,
		Source:      req.Source,
		Status:      model.TaskStatusPending,
		Input:       input,
		Output:      "{}",
		CreatedAt:   time.Now(),
	}
	if err := s.tasks.Create(r.Context(), t); err != nil {
		respondInternalErr(w, err)
		return
	}

	// Kick off execution asynchronously.
	if err := s.runner.RunTask(r.Context(), t.ID); err != nil {
		// Task is created but failed to start — return it with pending status.
		// The user can see it in the UI and retry.
		respond(w, http.StatusCreated, t)
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
	if err := s.projects.AssignAgent(r.Context(), sandboxProjectID, req.AgentID); err != nil {
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
