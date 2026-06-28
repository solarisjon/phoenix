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
)

type agentBundleData struct {
	Name           string `json:"name"`
	Behaviour      string `json:"behaviour,omitempty"`
	Guardrails     string `json:"guardrails,omitempty"`
	HardGuardrails string `json:"hard_guardrails,omitempty"`
	CanSpawnAgents bool   `json:"can_spawn_agents"`
	ModelOverride  string `json:"model_override,omitempty"`
	ProviderRef    string `json:"provider_ref"`
}

type agentProviderHint struct {
	Type  string `json:"type"`  // "llm" | "coding_agent"
	Kind  string `json:"kind"`  // "anthropic" | "openai" | "claude_code" | etc.
	Model string `json:"model"` // model name if applicable
	Note  string `json:"note"`  // human-readable setup note
}

type agentBundle struct {
	PhoenixBundleVersion string             `json:"phoenix_bundle_version"`
	ExportedAt           time.Time          `json:"exported_at"`
	Agent                agentBundleData    `json:"agent"`
	Provider             *bundleProvider    `json:"provider,omitempty"`
	ProviderHint         *agentProviderHint `json:"provider_hint,omitempty"`
}

type importAgentRequest struct {
	Bundle agentBundle `json:"bundle"`
	APIKey string      `json:"api_key,omitempty"`
}

func (s *Server) exportAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, err := s.agents.Get(r.Context(), id)
	if err != nil || agent == nil {
		respondErr(w, http.StatusNotFound, "agent not found")
		return
	}

	var bp *bundleProvider
	if prov, perr := s.providers.Get(r.Context(), agent.ProviderID); perr == nil && prov != nil {
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
		bp = &bundleProvider{
			Ref:    "provider_1",
			Name:   prov.Name,
			Type:   string(prov.Type),
			Kind:   kind,
			Config: bpCfg,
		}
	}

	var hint *agentProviderHint
	if bp != nil {
		note := "Create a provider of type '" + bp.Type + "'"
		if bp.Kind != "" {
			note += ", kind '" + bp.Kind + "'"
		}
		if bp.Config.Model != "" {
			note += ", model '" + bp.Config.Model + "'"
		}
		note += ", then link it to this agent."
		hint = &agentProviderHint{
			Type:  bp.Type,
			Kind:  bp.Kind,
			Model: bp.Config.Model,
			Note:  note,
		}
	}

	bundle := agentBundle{
		PhoenixBundleVersion: "1",
		ExportedAt:           time.Now().UTC(),
		Agent: agentBundleData{
			Name:           agent.Name,
			Behaviour:      agent.Behaviour,
			Guardrails:     agent.Guardrails,
			HardGuardrails: agent.HardGuardrails,
			CanSpawnAgents: agent.CanSpawnAgents,
			ModelOverride:  agent.ModelOverride,
			ProviderRef:    "provider_1",
		},
		Provider:     bp,
		ProviderHint: hint,
	}

	slug := strings.ToLower(strings.ReplaceAll(agent.Name, " ", "-"))
	slug = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return -1
	}, slug)
	if slug == "" {
		slug = "agent"
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-agent.json"`, slug))
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(bundle)
}

func (s *Server) importAgent(w http.ResponseWriter, r *http.Request) {
	var req importAgentRequest
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

	var provID string
	existingProviders, _ := s.providers.List(r.Context())
	if bundle.Provider != nil {
		for _, ep := range existingProviders {
			var cfg map[string]interface{}
			_ = json.Unmarshal([]byte(ep.Config), &cfg)
			sameURL := cfg["base_url"] == bundle.Provider.Config.BaseURL
			sameModel := cfg["model"] == bundle.Provider.Config.Model
			sameKind := bundle.Provider.Kind == "" || cfg["kind"] == bundle.Provider.Kind
			sameType := string(ep.Type) == bundle.Provider.Type
			// Match on base_url+model for LLMs, or type+kind for coding agents (no base_url).
			if (sameURL && sameModel) || (bundle.Provider.Config.BaseURL == "" && sameType && sameKind) {
				provID = ep.ID
				break
			}
		}
		if provID == "" {
			cfgMap := map[string]interface{}{
				"base_url": bundle.Provider.Config.BaseURL,
				"model":    bundle.Provider.Config.Model,
				"api_key":  req.APIKey,
			}
			if bundle.Provider.Kind != "" {
				cfgMap["kind"] = bundle.Provider.Kind
			}
			cfgBytes, _ := json.Marshal(cfgMap)
			prov := &model.Provider{
				ID:        uuid.New().String(),
				Name:      bundle.Provider.Name,
				Type:      model.ProviderType(bundle.Provider.Type),
				Config:    string(cfgBytes),
				CreatedBy: user.ID,
				CreatedAt: time.Now(),
			}
			if err := s.providers.Create(r.Context(), prov); err != nil {
				respondInternalErr(w, err)
				return
			}
			provID = prov.ID
		}
	}
	if provID == "" && len(existingProviders) > 0 {
		provID = existingProviders[0].ID
	}

	agent := &model.Agent{
		ID:             uuid.New().String(),
		Name:           bundle.Agent.Name,
		Behaviour:      bundle.Agent.Behaviour,
		Guardrails:     bundle.Agent.Guardrails,
		HardGuardrails: bundle.Agent.HardGuardrails,
		CanSpawnAgents: bundle.Agent.CanSpawnAgents,
		ModelOverride:  bundle.Agent.ModelOverride,
		ProviderID:     provID,
		Status:         model.AgentStatusActive,
		CreatedBy:      user.ID,
		CreatedAt:      time.Now(),
	}
	if err := s.agents.Create(r.Context(), agent); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusCreated, agent)
}
