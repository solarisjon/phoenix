package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/solarisjon/phoenix/internal/model"
)

func (s *Server) listTaskTemplates(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("project_id")
	list, err := s.taskTemplates.List(r.Context(), projectID)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if list == nil {
		list = []*model.TaskTemplate{}
	}
	respond(w, http.StatusOK, list)
}

func (s *Server) createTaskTemplate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string  `json:"name"`
		Description string  `json:"description"`
		Title       string  `json:"title"`
		Body        string  `json:"body"`
		ProjectID   *string `json:"project_id"`
		AgentID     *string `json:"agent_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		respondErr(w, http.StatusBadRequest, "name is required")
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		respondErr(w, http.StatusBadRequest, "title is required")
		return
	}

	// Treat empty string pointer as nil for optional FK fields.
	if req.ProjectID != nil && *req.ProjectID == "" {
		req.ProjectID = nil
	}
	if req.AgentID != nil && *req.AgentID == "" {
		req.AgentID = nil
	}

	t := &model.TaskTemplate{
		ID:          uuid.New().String(),
		Name:        strings.TrimSpace(req.Name),
		Description: req.Description,
		Title:       strings.TrimSpace(req.Title),
		Body:        req.Body,
		ProjectID:   req.ProjectID,
		AgentID:     req.AgentID,
		CreatedAt:   time.Now().UTC(),
	}
	if err := s.taskTemplates.Create(r.Context(), t); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusCreated, t)
}

func (s *Server) deleteTaskTemplate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := s.taskTemplates.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if existing == nil {
		respondErr(w, http.StatusNotFound, "template not found")
		return
	}
	if err := s.taskTemplates.Delete(r.Context(), id); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusNoContent, nil)
}
