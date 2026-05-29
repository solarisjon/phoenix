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
