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
	HeartbeatInterval *int   `json:"heartbeat_interval,omitempty"` // kept for bundle compat; ignored on import
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
			ID:             uuid.New().String(),
			Name:           ba.Name,
			Behaviour:      ba.Behaviour,
			Persona:        ba.Persona,
			Instructions:   ba.Instructions,
			Guardrails:     ba.Guardrails,
			HardGuardrails: ba.HardGuardrails,
			ProviderID:     provID,
			ModelOverride:  ba.ModelOverride,
			CanSpawnAgents: ba.CanSpawnAgents,
			Status:         model.AgentStatusActive,
			CreatedBy:      user.ID,
			CreatedAt:      time.Now(),
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
