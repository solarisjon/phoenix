package api

import (
	"encoding/json"
	"fmt"
	"log"
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

// ---- Bundle export/import types ----

type bundleProviderConfig struct {
	BaseURL string `json:"base_url,omitempty"`
	Model   string `json:"model,omitempty"`
	APIKey  string `json:"api_key"` // always empty on export
}

type bundleProvider struct {
	Ref    string               `json:"ref"`
	Name   string               `json:"name"`
	Type   string               `json:"type"`
	Kind   string               `json:"kind,omitempty"`
	Config bundleProviderConfig `json:"config"`
}

type bundleAgent struct {
	Name              string `json:"name"`
	Behaviour         string `json:"behaviour,omitempty"`
	Persona           string `json:"persona,omitempty"`
	Instructions      string `json:"instructions,omitempty"`
	Guardrails        string `json:"guardrails"`
	HardGuardrails    string `json:"hard_guardrails,omitempty"`
	HeartbeatInterval *int   `json:"heartbeat_interval,omitempty"`
	CanSpawnAgents    bool   `json:"can_spawn_agents"`
	ModelOverride     string `json:"model_override,omitempty"`
	ProviderRef       string `json:"provider_ref"`
}

type teamBundle struct {
	PhoenixBundleVersion string           `json:"phoenix_bundle_version"`
	ExportedAt           time.Time        `json:"exported_at"`
	Team                 bundleTeamMeta   `json:"team"`
	Agents               []bundleAgent    `json:"agents"`
	Providers            []bundleProvider `json:"providers"`
}

type bundleTeamMeta struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// importBundleRequest is the body for POST /api/import/team
type importBundleRequest struct {
	Bundle  teamBundle        `json:"bundle"`
	APIKeys map[string]string `json:"api_keys"` // ref -> key, may be empty
}

// exportTeam handles GET /api/teams/:id/export
func (s *Server) exportTeam(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	team, err := s.teams.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if team == nil {
		respondErr(w, http.StatusNotFound, "team not found")
		return
	}

	// Build provider ref map: provider UUID -> short ref
	providerRef := make(map[string]string)
	providerMap := make(map[string]*model.Provider)
	var bundleProviders []bundleProvider

	for _, agent := range team.Agents {
		if _, seen := providerRef[agent.ProviderID]; seen {
			continue
		}
		prov, err := s.providers.Get(r.Context(), agent.ProviderID)
		if err != nil || prov == nil {
			continue
		}
		ref := fmt.Sprintf("provider_%d", len(bundleProviders)+1)
		providerRef[prov.ID] = ref
		providerMap[prov.ID] = prov

		// Extract fields from config JSON, strip api_key
		var cfg map[string]interface{}
		_ = json.Unmarshal([]byte(prov.Config), &cfg)
		bpCfg := bundleProviderConfig{APIKey: ""}
		if v, ok := cfg["base_url"].(string); ok {
			bpCfg.BaseURL = v
		}
		if v, ok := cfg["model"].(string); ok {
			bpCfg.Model = v
		}
		kind, _ := cfg["kind"].(string)
		bundleProviders = append(bundleProviders, bundleProvider{
			Ref:    ref,
			Name:   prov.Name,
			Type:   string(prov.Type),
			Kind:   kind,
			Config: bpCfg,
		})
	}

	var bundleAgents []bundleAgent
	for _, agent := range team.Agents {
		ba := bundleAgent{
			Name:           agent.Name,
			Behaviour:      agent.Behaviour,
			Guardrails:     agent.Guardrails,
			HardGuardrails: agent.HardGuardrails,
			CanSpawnAgents: agent.CanSpawnAgents,
			ModelOverride:  agent.ModelOverride,
			ProviderRef:    providerRef[agent.ProviderID],
		}
		if agent.HeartbeatInterval != nil {
			v := *agent.HeartbeatInterval
			ba.HeartbeatInterval = &v
		}
		bundleAgents = append(bundleAgents, ba)
	}

	bundle := teamBundle{
		PhoenixBundleVersion: "1",
		ExportedAt:           time.Now().UTC(),
		Team: bundleTeamMeta{
			Name:        team.Name,
			Description: team.Description,
		},
		Agents:    bundleAgents,
		Providers: bundleProviders,
	}

	// Slugify team name for filename
	slug := strings.ToLower(strings.ReplaceAll(team.Name, " ", "-"))
	slug = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return -1
	}, slug)
	if slug == "" {
		slug = "team"
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-bundle.json"`, slug))
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(bundle)
}

// importTeam handles POST /api/import/team
func (s *Server) importTeam(w http.ResponseWriter, r *http.Request) {
	var req importBundleRequest
	if !decode(w, r, &req) {
		return
	}

	bundle := req.Bundle
	if bundle.PhoenixBundleVersion != "1" {
		respondErr(w, http.StatusBadRequest, "unsupported bundle version")
		return
	}

	user, err := s.users.GetDefault(r.Context())
	if err != nil || user == nil {
		respondInternalErr(w, err)
		return
	}

	// Create/reuse providers
	refToProviderID := make(map[string]string)
	existingProviders, _ := s.providers.List(r.Context())

	var createdProviderIDs []string
	var skipped []string

	for _, bp := range bundle.Providers {
		apiKey := req.APIKeys[bp.Ref]

		// Check for existing provider with same endpoint+model
		var matched *model.Provider
		for _, ep := range existingProviders {
			var cfg map[string]interface{}
			_ = json.Unmarshal([]byte(ep.Config), &cfg)
			if cfg["base_url"] == bp.Config.BaseURL && cfg["model"] == bp.Config.Model {
				matched = ep
				break
			}
		}

		if matched != nil {
			refToProviderID[bp.Ref] = matched.ID
			skipped = append(skipped, fmt.Sprintf("provider '%s' (reused existing)", bp.Name))
			continue
		}

		// Build config JSON — start with an empty map and layer in all fields
		cfgMap := map[string]interface{}{
			"base_url": bp.Config.BaseURL,
			"model":    bp.Config.Model,
			"api_key":  apiKey,
		}
		if bp.Kind != "" {
			cfgMap["kind"] = bp.Kind
		}
		cfgBytes, _ := json.Marshal(cfgMap)

		prov := &model.Provider{
			ID:        uuid.New().String(),
			Name:      bp.Name,
			Type:      model.ProviderType(bp.Type),
			Config:    string(cfgBytes),
			CreatedBy: user.ID,
			CreatedAt: time.Now(),
		}
		if err := s.providers.Create(r.Context(), prov); err != nil {
			respondInternalErr(w, err)
			return
		}
		refToProviderID[bp.Ref] = prov.ID
		createdProviderIDs = append(createdProviderIDs, prov.ID)
	}

	// Create agents
	var agentIDs []string
	for _, ba := range bundle.Agents {
		provID := refToProviderID[ba.ProviderRef]
		if provID == "" {
			// Fall back to first available provider
			if len(existingProviders) > 0 {
				provID = existingProviders[0].ID
			}
		}
		agent := &model.Agent{
			ID:                uuid.New().String(),
			Name:              ba.Name,
			Behaviour:         ba.Behaviour,
			Persona:           ba.Persona,
			Instructions:      ba.Instructions,
			Guardrails:        ba.Guardrails,
			HardGuardrails:    ba.HardGuardrails,
			ProviderID:        provID,
			ModelOverride:     ba.ModelOverride,
			CanSpawnAgents:    ba.CanSpawnAgents,
			HeartbeatInterval: ba.HeartbeatInterval,
			Status:            model.AgentStatusActive,
			CreatedBy:         user.ID,
			CreatedAt:         time.Now(),
		}
		if err := s.agents.Create(r.Context(), agent); err != nil {
			respondInternalErr(w, err)
			return
		}
		agentIDs = append(agentIDs, agent.ID)
	}

	// Create team
	team := &model.Team{
		ID:          uuid.New().String(),
		Name:        bundle.Team.Name,
		Description: bundle.Team.Description,
		CreatedBy:   user.ID,
		CreatedAt:   time.Now(),
	}
	if err := s.teams.Create(r.Context(), team); err != nil {
		respondInternalErr(w, err)
		return
	}
	for _, agentID := range agentIDs {
		_ = s.teams.AddAgent(r.Context(), team.ID, agentID)
	}

	respond(w, http.StatusCreated, map[string]interface{}{
		"team_id":      team.ID,
		"team_name":    team.Name,
		"agent_ids":    agentIDs,
		"provider_ids": createdProviderIDs,
		"skipped":      skipped,
	})
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

	proj, err := s.projects.Get(r.Context(), req.ProjectID)
	if err != nil || proj == nil {
		respondErr(w, http.StatusBadRequest, "project not found")
		return
	}

	var taskIDs []string
	for _, agent := range team.Agents {
		_ = s.projects.AssignAgent(r.Context(), req.ProjectID, agent.ID)

		task := &model.Task{
			ID:          uuid.New().String(),
			ProjectID:   req.ProjectID,
			AgentID:     agent.ID,
			Title:       req.Title,
			Description: req.Description,
			Status:      model.TaskStatusPending,
			Source:      "team_broadcast:" + teamID,
			CreatedAt:   time.Now(),
		}
		if err := s.tasks.Create(r.Context(), task); err != nil {
			respondInternalErr(w, err)
			return
		}
		if err := s.runner.RunTask(r.Context(), task.ID); err != nil {
			log.Printf("broadcast: run task %s: %v", task.ID, err)
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
