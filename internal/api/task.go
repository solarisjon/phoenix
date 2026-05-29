package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/solarisjon/phoenix/internal/model"
)

type createTaskRequest struct {
	ProjectID   string `json:"project_id"`
	AgentID     string `json:"agent_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Input       string `json:"input"`
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
		respondErr(w, http.StatusBadRequest, "project_id query param is required")
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

	// Allow updating description and input on pending tasks only.
	var req struct {
		Description string `json:"description"`
		Input       string `json:"input"`
	}
	if !decode(w, r, &req) {
		return
	}

	if existing.Status != model.TaskStatusPending {
		respondErr(w, http.StatusConflict, "only pending tasks can be updated")
		return
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
