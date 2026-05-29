package api

import (
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

type createAgentRequest struct {
	Name              string `json:"name"`
	Persona           string `json:"persona"`
	Instructions      string `json:"instructions"`
	Guardrails        string `json:"guardrails"`
	ProviderID        string `json:"provider_id"`
	ModelOverride     string `json:"model_override"`
	CanSpawnAgents    bool   `json:"can_spawn_agents"`
	CanHireAgents     bool   `json:"can_hire_agents"`
	HeartbeatInterval *int   `json:"heartbeat_interval"`
	Status            string `json:"status"`
}

func (r createAgentRequest) validate() string {
	if strings.TrimSpace(r.Name) == "" {
		return "name is required"
	}
	if strings.TrimSpace(r.ProviderID) == "" {
		return "provider_id is required"
	}
	if r.Status != "" &&
		r.Status != string(model.AgentStatusActive) &&
		r.Status != string(model.AgentStatusPaused) &&
		r.Status != string(model.AgentStatusDisabled) {
		return "status must be 'active', 'paused', or 'disabled'"
	}
	return ""
}

func (s *Server) listAgents(w http.ResponseWriter, r *http.Request) {
	list, err := s.agents.List(r.Context())
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if list == nil {
		list = []*model.Agent{}
	}
	respond(w, http.StatusOK, list)
}

func (s *Server) getAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	a, err := s.agents.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if a == nil {
		respondErr(w, http.StatusNotFound, "agent not found")
		return
	}
	respond(w, http.StatusOK, a)
}

func (s *Server) createAgent(w http.ResponseWriter, r *http.Request) {
	var req createAgentRequest
	if !decode(w, r, &req) {
		return
	}
	if msg := req.validate(); msg != "" {
		respondErr(w, http.StatusBadRequest, msg)
		return
	}

	// Verify provider exists.
	p, err := s.providers.Get(r.Context(), req.ProviderID)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if p == nil {
		respondErr(w, http.StatusBadRequest, "provider not found")
		return
	}

	user, err := s.users.GetDefault(r.Context())
	if err != nil || user == nil {
		respondInternalErr(w, err)
		return
	}

	status := model.AgentStatusActive
	if req.Status != "" {
		status = model.AgentStatus(req.Status)
	}

	a := &model.Agent{
		ID:                uuid.New().String(),
		Name:              strings.TrimSpace(req.Name),
		Persona:           req.Persona,
		Instructions:      req.Instructions,
		Guardrails:        req.Guardrails,
		ProviderID:        req.ProviderID,
		ModelOverride:     req.ModelOverride,
		CanSpawnAgents:    req.CanSpawnAgents,
		CanHireAgents:     req.CanHireAgents,
		HeartbeatInterval: req.HeartbeatInterval,
		CreatedBy:         user.ID,
		Status:            status,
		CreatedAt:         time.Now(),
	}
	if err := s.agents.Create(r.Context(), a); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusCreated, a)
}

func (s *Server) updateAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := s.agents.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if existing == nil {
		respondErr(w, http.StatusNotFound, "agent not found")
		return
	}

	var req createAgentRequest
	if !decode(w, r, &req) {
		return
	}
	if msg := req.validate(); msg != "" {
		respondErr(w, http.StatusBadRequest, msg)
		return
	}

	// Verify new provider exists if changed.
	if req.ProviderID != existing.ProviderID {
		p, err := s.providers.Get(r.Context(), req.ProviderID)
		if err != nil {
			respondInternalErr(w, err)
			return
		}
		if p == nil {
			respondErr(w, http.StatusBadRequest, "provider not found")
			return
		}
	}

	existing.Name = strings.TrimSpace(req.Name)
	existing.Persona = req.Persona
	existing.Instructions = req.Instructions
	existing.Guardrails = req.Guardrails
	existing.ProviderID = req.ProviderID
	existing.ModelOverride = req.ModelOverride
	existing.CanSpawnAgents = req.CanSpawnAgents
	existing.CanHireAgents = req.CanHireAgents
	existing.HeartbeatInterval = req.HeartbeatInterval
	if req.Status != "" {
		existing.Status = model.AgentStatus(req.Status)
	}

	if err := s.agents.Update(r.Context(), existing); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusOK, existing)
}

// generateAgent uses an LLM provider to generate persona, instructions, and
// guardrails from a plain-text description of the agent's role.
func (s *Server) generateAgent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Description string `json:"description"`
		ProviderID  string `json:"provider_id"` // which provider to use for generation
	}
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Description) == "" {
		respondErr(w, http.StatusBadRequest, "description is required")
		return
	}

	// Fall back to first available LLM provider if none specified.
	providerID := req.ProviderID
	if providerID == "" {
		providers, err := s.providers.List(r.Context())
		if err != nil || len(providers) == 0 {
			respondErr(w, http.StatusBadRequest, "no providers available for generation")
			return
		}
		// Prefer LLM providers for generation.
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

	prompt := fmt.Sprintf(`You are an AI agent configuration assistant. Given a description of an AI agent's role, generate a structured JSON configuration with three fields:

- "persona": 2-3 sentences describing who the agent is, their personality, and communication style
- "instructions": detailed operational instructions for what the agent does and how (4-8 bullet points or paragraphs)
- "guardrails": constraints and boundaries the agent must respect (3-5 items)

Return ONLY valid JSON with exactly these three string fields. No markdown, no explanation.

Agent description: %s`, req.Description)

	resp, err := prov.Execute(r.Context(), provider.TaskRequest{
		SystemPrompt: "You are a precise JSON generator. Always return valid JSON only, with no markdown formatting or extra text.",
		Prompt:       prompt,
	})
	if err != nil {
		respondErr(w, http.StatusInternalServerError, fmt.Sprintf("generation failed: %v", err))
		return
	}

	// Extract JSON from the response (strip any markdown fences if present).
	output := strings.TrimSpace(resp.Output)
	output = strings.TrimPrefix(output, "```json")
	output = strings.TrimPrefix(output, "```")
	output = strings.TrimSuffix(output, "```")
	output = strings.TrimSpace(output)

	var result struct {
		Persona      string `json:"persona"`
		Instructions string `json:"instructions"`
		Guardrails   string `json:"guardrails"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		// Return the raw text so the UI can show it rather than failing silently.
		respond(w, http.StatusOK, map[string]string{
			"persona":      output,
			"instructions": "",
			"guardrails":   "",
			"raw":          output,
		})
		return
	}
	respond(w, http.StatusOK, result)
}

// spawnTask allows an agent (identified by source_agent_id) to create a task
// for another agent. The source agent must have can_spawn_agents=true.
// This is the programmatic hook; agents call this via their system prompt instructions.
func (s *Server) spawnTask(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SourceAgentID string `json:"source_agent_id"`
		TargetAgentID string `json:"target_agent_id"`
		ProjectID     string `json:"project_id"`
		Title         string `json:"title"`
		Description   string `json:"description"`
	}
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.SourceAgentID) == "" {
		respondErr(w, http.StatusBadRequest, "source_agent_id is required")
		return
	}
	if strings.TrimSpace(req.TargetAgentID) == "" {
		respondErr(w, http.StatusBadRequest, "target_agent_id is required")
		return
	}
	if strings.TrimSpace(req.ProjectID) == "" {
		respondErr(w, http.StatusBadRequest, "project_id is required")
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		respondErr(w, http.StatusBadRequest, "title is required")
		return
	}

	// Verify source agent exists and has spawn permission.
	src, err := s.agents.Get(r.Context(), req.SourceAgentID)
	if err != nil || src == nil {
		respondErr(w, http.StatusBadRequest, "source agent not found")
		return
	}
	if !src.CanSpawnAgents {
		respondErr(w, http.StatusForbidden, "source agent is not permitted to spawn tasks")
		return
	}

	// Verify target agent exists.
	tgt, err := s.agents.Get(r.Context(), req.TargetAgentID)
	if err != nil || tgt == nil {
		respondErr(w, http.StatusBadRequest, "target agent not found")
		return
	}

	t := &model.Task{
		ID:          uuid.New().String(),
		ProjectID:   req.ProjectID,
		AgentID:     req.TargetAgentID,
		Title:       strings.TrimSpace(req.Title),
		Description: fmt.Sprintf("[Spawned by agent: %s]\n\n%s", src.Name, req.Description),
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

func (s *Server) deleteAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := s.agents.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if existing == nil {
		respondErr(w, http.StatusNotFound, "agent not found")
		return
	}
	if err := s.agents.Delete(r.Context(), id); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusNoContent, nil)
}
