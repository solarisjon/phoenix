package api

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/solarisjon/phoenix/internal/model"
)

// dismissAllInbox bulk-dismisses inbox tasks.
// Accepts optional query param ?filter=failed|awaiting|all (default: all).
// Returns {dismissed: N}.
func (s *Server) dismissAllInbox(w http.ResponseWriter, r *http.Request) {
	filter := r.URL.Query().Get("filter")
	if filter == "" {
		filter = "all"
	}

	var statuses []model.TaskStatus
	switch filter {
	case "failed":
		statuses = []model.TaskStatus{model.TaskStatusFailed}
	case "awaiting":
		statuses = []model.TaskStatus{model.TaskStatusAwaitingApproval}
	case "completed":
		statuses = []model.TaskStatus{model.TaskStatusCompleted}
	default: // "all"
		statuses = []model.TaskStatus{model.TaskStatusFailed, model.TaskStatusAwaitingApproval, model.TaskStatusCompleted}
	}

	tasks, err := s.tasks.ListByStatuses(r.Context(), statuses)
	if err != nil {
		respondInternalErr(w, err)
		return
	}

	dismissed := 0
	for _, t := range tasks {
		if t.Dismissed {
			continue
		}
		t.Dismissed = true
		if err := s.tasks.Update(r.Context(), t); err == nil {
			dismissed++
		}
	}

	respond(w, http.StatusOK, map[string]int{"dismissed": dismissed})
}

type reviseRequest struct {
	Feedback string `json:"feedback"`
}

func (s *Server) listInbox(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.tasks.ListByStatus(r.Context(), model.TaskStatusAwaitingApproval)
	if err != nil {
		respondInternalErr(w, err)
		return
	}

	completed, err := s.tasks.ListCompletedForInbox(r.Context(), 100)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	tasks = append(tasks, completed...)

	// Optional filters.
	projectID := r.URL.Query().Get("project_id")
	agentID := r.URL.Query().Get("agent_id")

	filtered := tasks[:0]
	for _, t := range tasks {
		if projectID != "" && t.ProjectID != projectID {
			continue
		}
		if agentID != "" && t.AgentID != agentID {
			continue
		}
		filtered = append(filtered, t)
	}
	if filtered == nil {
		filtered = []*model.Task{}
	}
	respond(w, http.StatusOK, filtered)
}

func (s *Server) approveTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	task, err := s.tasks.Get(r.Context(), taskID)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if task == nil {
		respondErr(w, http.StatusNotFound, "task not found")
		return
	}
	if task.Status != model.TaskStatusAwaitingApproval {
		respondErr(w, http.StatusConflict, "task is not awaiting approval")
		return
	}

	// Inject approval context so the agent knows it is allowed to proceed.
	// Without this, the agent would see the same prompt on resume and may trigger the same guardrail again.
	if task.GuardrailReason != nil && *task.GuardrailReason != "" {
		task.Description = task.Description + "\n\n## Human Approval Granted\n" +
			"A human has reviewed and approved proceeding with this action: " + *task.GuardrailReason + "\n" +
			"You may now proceed. Do NOT output GUARDRAIL_TRIGGERED for this specific action."
	}
	task.Output = "{}"
	if err := s.tasks.Update(r.Context(), task); err != nil {
		respondInternalErr(w, err)
		return
	}

	if err := s.runner.ResumeTask(r.Context(), taskID); err != nil {
		respondInternalErr(w, err)
		return
	}

	updated, _ := s.tasks.Get(r.Context(), taskID)
	if updated != nil {
		task = updated
	}
	respond(w, http.StatusOK, task)
}

func (s *Server) rejectTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	task, err := s.tasks.Get(r.Context(), taskID)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if task == nil {
		respondErr(w, http.StatusNotFound, "task not found")
		return
	}
	if task.Status != model.TaskStatusAwaitingApproval {
		respondErr(w, http.StatusConflict, "task is not awaiting approval")
		return
	}

	task.Status = model.TaskStatusFailed
	if err := s.tasks.Update(r.Context(), task); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusOK, task)
}

func (s *Server) reviseTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	task, err := s.tasks.Get(r.Context(), taskID)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if task == nil {
		respondErr(w, http.StatusNotFound, "task not found")
		return
	}
	if task.Status != model.TaskStatusAwaitingApproval {
		respondErr(w, http.StatusConflict, "task is not awaiting approval")
		return
	}

	var req reviseRequest
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Feedback) == "" {
		respondErr(w, http.StatusBadRequest, "feedback is required for revision")
		return
	}

	// Append feedback to the task description so the agent sees it on retry.
	task.Description = task.Description + "\n\n## Revision Feedback\n" + req.Feedback
	task.Status = model.TaskStatusPending
	task.Output = "{}"
	if err := s.tasks.Update(r.Context(), task); err != nil {
		respondInternalErr(w, err)
		return
	}

	// Re-run the task.
	if err := s.runner.RunTask(r.Context(), taskID); err != nil {
		respondInternalErr(w, err)
		return
	}

	updated, _ := s.tasks.Get(r.Context(), taskID)
	if updated != nil {
		task = updated
	}
	respond(w, http.StatusOK, task)
}
