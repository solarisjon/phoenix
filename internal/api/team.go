package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/provider"
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
	list, err := s.teams.List(r.Context(), userFromCtx(r.Context()).ID)
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

	user := userFromCtx(r.Context())

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

	// Assign each team member to the project; count only new assignments.
	var assigned int
	for _, agent := range team.Agents {
		added, err := s.projects.AssignAgent(r.Context(), projectID, agent.ID)
		if err != nil {
			continue
		}
		if added {
			assigned++
		}
	}

	respond(w, http.StatusOK, map[string]interface{}{
		"team_id":  team.ID,
		"team":     team.Name,
		"assigned": assigned,         // newly added this call
		"total":    len(team.Agents), // total agents in team
	})
}

type broadcastRequest struct {
	ProjectID   string `json:"project_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

func (s *Server) broadcastTeam(w http.ResponseWriter, r *http.Request) {
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

	var req broadcastRequest
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		respondErr(w, http.StatusBadRequest, "title is required")
		return
	}
	if strings.TrimSpace(req.ProjectID) == "" {
		respondErr(w, http.StatusBadRequest, "project_id is required")
		return
	}
	if len(team.Agents) == 0 {
		respondErr(w, http.StatusBadRequest, "team has no agents — add agents to the team before broadcasting")
		return
	}

	proj, err := s.projects.Get(r.Context(), req.ProjectID)
	if err != nil || proj == nil {
		respondErr(w, http.StatusBadRequest, "project not found")
		return
	}

	// Phase 1: create all tasks. On any failure, delete the ones already
	// created so we never leave orphaned tasks with no corresponding response.
	var created []*model.Task
	for _, agent := range team.Agents {
		// Enroll the agent in the project if not already assigned.
		if _, err := s.projects.AssignAgent(r.Context(), req.ProjectID, agent.ID); err != nil {
			slog.Error("broadcast: assign agent to project", "agent_id", agent.ID, "project_id", req.ProjectID, "error", err)
		}

		task := &model.Task{
			ID:          uuid.New().String(),
			ProjectID:   req.ProjectID,
			AgentID:     agent.ID,
			Title:       req.Title,
			Description: req.Description,
			Status:      model.TaskStatusPending,
			Input:       "{}",
			Output:      "{}",
			Source:      "team_broadcast:" + teamID,
			CreatedAt:   time.Now(),
		}
		if err := s.tasks.Create(r.Context(), task); err != nil {
			// Rollback: delete everything created so far.
			for _, t := range created {
				if delErr := s.tasks.Delete(r.Context(), t.ID); delErr != nil {
					slog.Error("broadcast: rollback delete task", "task_id", t.ID, "error", delErr)
				}
			}
			respondInternalErr(w, fmt.Errorf("create task for agent %s: %w", agent.ID, err))
			return
		}
		created = append(created, task)
	}

	// Phase 2: queue all created tasks. Runner failures are non-fatal —
	// the task exists and the human can retry from the inbox.
	taskIDs := make([]string, 0, len(created))
	for _, task := range created {
		if err := s.runner.RunTask(r.Context(), task.ID); err != nil {
			slog.Error("broadcast: queue task", "task_id", task.ID, "agent_id", task.AgentID, "error", err)
		}
		taskIDs = append(taskIDs, task.ID)
	}

	respond(w, http.StatusCreated, map[string]interface{}{
		"team_id":  teamID,
		"task_ids": taskIDs,
		"count":    len(taskIDs),
	})
}

// generateTeamDescription uses an LLM to draft a description for a team.
func (s *Server) generateTeamDescription(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name       string `json:"name"`
		Hint       string `json:"hint"`
		ProviderID string `json:"provider_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		respondErr(w, http.StatusBadRequest, "name is required")
		return
	}

	providerID := req.ProviderID
	if providerID == "" {
		providers, err := s.providers.List(r.Context(), userFromCtx(r.Context()).ID)
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

	prompt := fmt.Sprintf(`Write a concise description for an AI agent team named "%s".%s

Explain:
- What this team's purpose and mission is
- What kinds of tasks these agents handle together
- The team's area of responsibility

Return ONLY the description text — no JSON, no markdown headings.`,
		req.Name, hintSection)

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
