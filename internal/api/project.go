package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/solarisjon/phoenix/internal/model"
)

type createProjectRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	WorkingDir  string `json:"working_dir"`
	Status      string `json:"status"`
}

func (r createProjectRequest) validate() string {
	if strings.TrimSpace(r.Name) == "" {
		return "name is required"
	}
	if r.Status != "" &&
		r.Status != string(model.ProjectStatusActive) &&
		r.Status != string(model.ProjectStatusArchived) {
		return "status must be 'active' or 'archived'"
	}
	return ""
}

type assignAgentRequest struct {
	AgentID string `json:"agent_id"`
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	list, err := s.projects.List(r.Context())
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if list == nil {
		list = []*model.Project{}
	}
	respond(w, http.StatusOK, list)
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, err := s.projects.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if p == nil {
		respondErr(w, http.StatusNotFound, "project not found")
		return
	}
	respond(w, http.StatusOK, p)
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var req createProjectRequest
	if !decode(w, r, &req) {
		return
	}
	if msg := req.validate(); msg != "" {
		respondErr(w, http.StatusBadRequest, msg)
		return
	}

	user, err := s.users.GetDefault(r.Context())
	if err != nil || user == nil {
		respondInternalErr(w, err)
		return
	}

	status := model.ProjectStatusActive
	if req.Status != "" {
		status = model.ProjectStatus(req.Status)
	}

	p := &model.Project{
		ID:          uuid.New().String(),
		Name:        strings.TrimSpace(req.Name),
		Description: req.Description,
		WorkingDir:  strings.TrimSpace(req.WorkingDir),
		Owner:       user.ID,
		Status:      status,
		CreatedAt:   time.Now(),
	}
	if err := s.projects.Create(r.Context(), p); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusCreated, p)
}

func (s *Server) updateProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := s.projects.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if existing == nil {
		respondErr(w, http.StatusNotFound, "project not found")
		return
	}

	var req createProjectRequest
	if !decode(w, r, &req) {
		return
	}
	if msg := req.validate(); msg != "" {
		respondErr(w, http.StatusBadRequest, msg)
		return
	}

	existing.Name = strings.TrimSpace(req.Name)
	existing.Description = req.Description
	existing.WorkingDir = strings.TrimSpace(req.WorkingDir)
	if req.Status != "" {
		existing.Status = model.ProjectStatus(req.Status)
	}

	if err := s.projects.Update(r.Context(), existing); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusOK, existing)
}

func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := s.projects.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if existing == nil {
		respondErr(w, http.StatusNotFound, "project not found")
		return
	}
	if err := s.projects.Delete(r.Context(), id); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusNoContent, nil)
}

func (s *Server) assignAgent(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")

	proj, err := s.projects.Get(r.Context(), projectID)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if proj == nil {
		respondErr(w, http.StatusNotFound, "project not found")
		return
	}

	var req assignAgentRequest
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.AgentID) == "" {
		respondErr(w, http.StatusBadRequest, "agent_id is required")
		return
	}

	agent, err := s.agents.Get(r.Context(), req.AgentID)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if agent == nil {
		respondErr(w, http.StatusBadRequest, "agent not found")
		return
	}

	if err := s.projects.AssignAgent(r.Context(), projectID, req.AgentID); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusNoContent, nil)
}

func (s *Server) removeAgent(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	agentID := chi.URLParam(r, "agentId")

	if err := s.projects.RemoveAgent(r.Context(), projectID, agentID); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusNoContent, nil)
}

func (s *Server) listProjectAgents(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")

	proj, err := s.projects.Get(r.Context(), projectID)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if proj == nil {
		respondErr(w, http.StatusNotFound, "project not found")
		return
	}

	agents, err := s.projects.ListAgents(r.Context(), projectID)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if agents == nil {
		agents = []*model.Agent{}
	}
	respond(w, http.StatusOK, agents)
}
