package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/solarisjon/phoenix/internal/model"
)

type createTeamRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	AgentIDs    []string `json:"agent_ids"` // optional: seed agents on creation
}

func (r createTeamRequest) validate() string {
	if strings.TrimSpace(r.Name) == "" {
		return "name is required"
	}
	return ""
}

type teamAgentRequest struct {
	AgentID string `json:"agent_id"`
}

// assignTeamRequest is used by POST /projects/:id/teams
type assignTeamRequest struct {
	TeamID string `json:"team_id"`
}

func (s *Server) listTeams(w http.ResponseWriter, r *http.Request) {
	list, err := s.teams.List(r.Context())
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if list == nil {
		list = []*model.Team{}
	}
	respond(w, http.StatusOK, list)
}

func (s *Server) getTeam(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	t, err := s.teams.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if t == nil {
		respondErr(w, http.StatusNotFound, "team not found")
		return
	}
	respond(w, http.StatusOK, t)
}

func (s *Server) createTeam(w http.ResponseWriter, r *http.Request) {
	var req createTeamRequest
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

	t := &model.Team{
		ID:          uuid.New().String(),
		Name:        strings.TrimSpace(req.Name),
		Description: req.Description,
		CreatedBy:   user.ID,
		CreatedAt:   time.Now(),
	}
	if err := s.teams.Create(r.Context(), t); err != nil {
		respondInternalErr(w, err)
		return
	}

	// Seed agents if provided.
	for _, agentID := range req.AgentIDs {
		if strings.TrimSpace(agentID) == "" {
			continue
		}
		_ = s.teams.AddAgent(r.Context(), t.ID, agentID)
	}

	// Re-fetch with agents populated.
	full, err := s.teams.Get(r.Context(), t.ID)
	if err != nil || full == nil {
		respond(w, http.StatusCreated, t)
		return
	}
	respond(w, http.StatusCreated, full)
}

func (s *Server) updateTeam(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := s.teams.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if existing == nil {
		respondErr(w, http.StatusNotFound, "team not found")
		return
	}

	var req createTeamRequest
	if !decode(w, r, &req) {
		return
	}
	if msg := req.validate(); msg != "" {
		respondErr(w, http.StatusBadRequest, msg)
		return
	}

	existing.Name = strings.TrimSpace(req.Name)
	existing.Description = req.Description
	if err := s.teams.Update(r.Context(), existing); err != nil {
		respondInternalErr(w, err)
		return
	}

	full, _ := s.teams.Get(r.Context(), id)
	if full == nil {
		full = existing
	}
	respond(w, http.StatusOK, full)
}

func (s *Server) deleteTeam(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := s.teams.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if existing == nil {
		respondErr(w, http.StatusNotFound, "team not found")
		return
	}
	if err := s.teams.Delete(r.Context(), id); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusNoContent, nil)
}

func (s *Server) addTeamAgent(w http.ResponseWriter, r *http.Request) {
	teamID := chi.URLParam(r, "id")
	team, err := s.teams.Get(r.Context(), teamID)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if team == nil {
		respondErr(w, http.StatusNotFound, "team not found")
		return
	}

	var req teamAgentRequest
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.AgentID) == "" {
		respondErr(w, http.StatusBadRequest, "agent_id is required")
		return
	}

	agent, err := s.agents.Get(r.Context(), req.AgentID)
	if err != nil || agent == nil {
		respondErr(w, http.StatusBadRequest, "agent not found")
		return
	}

	if err := s.teams.AddAgent(r.Context(), teamID, req.AgentID); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusNoContent, nil)
}

func (s *Server) removeTeamAgent(w http.ResponseWriter, r *http.Request) {
	teamID := chi.URLParam(r, "id")
	agentID := chi.URLParam(r, "agentId")
	if err := s.teams.RemoveAgent(r.Context(), teamID, agentID); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusNoContent, nil)
}

// assignTeamToProject assigns all agents in a team to a project at once.
func (s *Server) assignTeamToProject(w http.ResponseWriter, r *http.Request) {
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

	var req assignTeamRequest
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.TeamID) == "" {
		respondErr(w, http.StatusBadRequest, "team_id is required")
		return
	}

	team, err := s.teams.Get(r.Context(), req.TeamID)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if team == nil {
		respondErr(w, http.StatusBadRequest, "team not found")
		return
	}

	// Assign each team member to the project.
	var assigned int
	for _, agent := range team.Agents {
		if err := s.projects.AssignAgent(r.Context(), projectID, agent.ID); err != nil {
			continue // INSERT OR IGNORE handles duplicates
		}
		assigned++
	}

	respond(w, http.StatusOK, map[string]interface{}{
		"team_id":  team.ID,
		"team":     team.Name,
		"assigned": assigned,
		"total":    len(team.Agents),
	})
}
