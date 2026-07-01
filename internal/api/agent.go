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
	Name              string  `json:"name"`
	Behaviour         string  `json:"behaviour"`
	Persona           string  `json:"persona"`
	Instructions      string  `json:"instructions"`
	Guardrails        string  `json:"guardrails"`
	HardGuardrails    string  `json:"hard_guardrails"`
	ProviderID        string  `json:"provider_id"`
	ModelOverride     string  `json:"model_override"`
	CanSpawnAgents    bool    `json:"can_spawn_agents"`
	CanHireAgents     bool    `json:"can_hire_agents"`
	IsOrchestrator    bool    `json:"is_orchestrator"`
	HeartbeatInterval *int    `json:"heartbeat_interval,omitempty"` // kept for bundle import compat; ignored
	Status            string  `json:"status"`
	TemplateID        *string `json:"template_id"`
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
	u := userFromCtx(r.Context())
	list, err := s.agents.List(r.Context(), u.ID)
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

	user := userFromCtx(r.Context())

	status := model.AgentStatusActive
	if req.Status != "" {
		status = model.AgentStatus(req.Status)
	}

	a := &model.Agent{
		ID:                uuid.New().String(),
		Name:              strings.TrimSpace(req.Name),
		Behaviour:         req.Behaviour,
		Persona:           req.Persona,
		Instructions:      req.Instructions,
		Guardrails:        req.Guardrails,
		HardGuardrails:    req.HardGuardrails,
		ProviderID:        req.ProviderID,
		ModelOverride:     req.ModelOverride,
		CanSpawnAgents:    req.CanSpawnAgents,
		CanHireAgents:     req.CanHireAgents,
		IsOrchestrator:    req.IsOrchestrator,
		CreatedBy:         user.ID,
		Status:            status,
		CreatedAt:         time.Now(),
		TemplateID:        req.TemplateID,
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
	existing.Behaviour = req.Behaviour
	existing.Persona = req.Persona
	existing.Instructions = req.Instructions
	existing.Guardrails = req.Guardrails
	existing.HardGuardrails = req.HardGuardrails
	existing.ProviderID = req.ProviderID
	existing.ModelOverride = req.ModelOverride
	existing.CanSpawnAgents = req.CanSpawnAgents
	existing.CanHireAgents = req.CanHireAgents
	existing.IsOrchestrator = req.IsOrchestrator
	existing.TemplateID = req.TemplateID
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
		providers, err := s.providers.List(r.Context(), userFromCtx(r.Context()).ID)
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

	prompt := fmt.Sprintf(`You are an AI agent configuration assistant. Given a description of an AI agent's role, generate a structured JSON configuration with four fields:

- "behaviour": a unified description of who the agent is, their personality, communication style, and detailed operational instructions (2-3 paragraphs)
- "guardrails": advisory constraints and soft boundaries the agent should try to follow (3-5 items)
- "hard_guardrails": mandatory rules that require human approval before the agent can act (1-3 items; use sparingly for truly sensitive operations like deleting data, sending external communications, or making production changes)
- "persona": brief personality summary (1-2 sentences, legacy field)
- "instructions": operational detail (legacy field)

Return ONLY valid JSON with exactly these five string fields. No markdown, no explanation.

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
		Persona        string `json:"persona"`
		Instructions   string `json:"instructions"`
		Guardrails     string `json:"guardrails"`
		HardGuardrails string `json:"hard_guardrails"`
		Behaviour      string `json:"behaviour"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		// Return the raw text so the UI can show it rather than failing silently.
		respond(w, http.StatusOK, map[string]string{
			"behaviour":       output,
			"persona":         output,
			"instructions":    "",
			"guardrails":      "",
			"hard_guardrails": "",
			"raw":             output,
		})
		return
	}
	// Synthesise behaviour from persona + instructions if not directly provided.
	if result.Behaviour == "" {
		parts := []string{}
		if result.Persona != "" {
			parts = append(parts, result.Persona)
		}
		if result.Instructions != "" {
			parts = append(parts, result.Instructions)
		}
		result.Behaviour = strings.Join(parts, "\n\n")
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
		Source        string `json:"source"` // free-text provenance
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
		Source:      req.Source,
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

func (s *Server) clearAgentMemory(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	mc := s.pluginManager.MemoryClient()
	if mc == nil {
		respondErr(w, http.StatusServiceUnavailable, "memory plugin is not enabled")
		return
	}

	if err := mc.ClearBank(r.Context(), id); err != nil {
		respondErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
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

func (s *Server) listAgentTasks(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, err := s.agents.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if agent == nil {
		respondErr(w, http.StatusNotFound, "agent not found")
		return
	}
	tasks, err := s.tasks.ListByAgent(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if tasks == nil {
		tasks = []*model.Task{}
	}
	respond(w, http.StatusOK, tasks)
}
