package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/provider"
)

// DependsOn is an optional ordered list of task IDs that must complete
// before this task is dispatched. Stored as a JSON array in the DB.
// Populated on createTaskRequest; zero value means no dependencies.

type createTaskRequest struct {
	ProjectID   string   `json:"project_id"`
	AgentID     string   `json:"agent_id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Input       string   `json:"input"`
	Source      string   `json:"source"`      // free-text provenance, e.g. "Jira triage 2026-05-30"
	CriticMode  string   `json:"critic_mode"` // "" | "inherit" | "none" | "builtin" | "agent:<id>"
	DependsOn   []string `json:"depends_on"`  // task IDs that must complete before this task runs
}

func (r createTaskRequest) validate() string {
	if strings.TrimSpace(r.ProjectID) == "" {
		return "project_id is required"
	}
	if strings.TrimSpace(r.AgentID) == "" {
		return "agent_id is required"
	}
	if strings.TrimSpace(r.Title) == "" {
		return "title is required"
	}
	return ""
}

func (s *Server) estimateTask(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AgentID     string `json:"agent_id"`
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	agent, err := s.agents.Get(r.Context(), req.AgentID)
	if err != nil || agent == nil {
		respondErr(w, http.StatusBadRequest, "agent not found")
		return
	}

	systemPrompt := agent.Behaviour
	if systemPrompt == "" {
		systemPrompt = agent.Instructions
	}
	userPrompt := req.Title
	if req.Description != "" {
		userPrompt += "\n\n" + req.Description
	}
	promptTokens := (len(systemPrompt) + len(userPrompt)) / 4
	if promptTokens < 1 {
		promptTokens = 1
	}

	var modelName, providerType string
	if rec, perr := s.providers.Get(r.Context(), agent.ProviderID); perr == nil && rec != nil {
		providerType = string(rec.Type)
		var cfg map[string]interface{}
		_ = json.Unmarshal([]byte(rec.Config), &cfg)
		if m, ok := cfg["model"].(string); ok {
			modelName = m
		}
	}
	if agent.ModelOverride != "" {
		modelName = agent.ModelOverride
	}

	outputLow := promptTokens / 2
	outputHigh := promptTokens * 3

	var costLow, costHigh float64
	if override, ok := s.pricingReg.GetOverride(agent.ProviderID); ok {
		inP := override.InputPerMToken / 1_000_000
		outP := override.OutputPerMToken / 1_000_000
		costLow = float64(promptTokens)*inP + float64(outputLow)*outP
		costHigh = float64(promptTokens)*inP + float64(outputHigh)*outP
	} else if modelName != "" {
		if mp, ok := s.pricingReg.GetPrice(modelName); ok {
			inP := mp.InputPerMToken / 1_000_000
			outP := mp.OutputPerMToken / 1_000_000
			costLow = float64(promptTokens)*inP + float64(outputLow)*outP
			costHigh = float64(promptTokens)*inP + float64(outputHigh)*outP
		}
	}

	respond(w, http.StatusOK, map[string]interface{}{
		"supported":               costLow > 0 || costHigh > 0,
		"prompt_tokens":           promptTokens,
		"estimated_output_tokens": map[string]int{"low": outputLow, "high": outputHigh},
		"estimated_cost_usd":      map[string]float64{"low": costLow, "high": costHigh},
		"provider":                map[string]string{"type": providerType, "model": modelName},
	})
}

// generateTaskDescription uses an LLM to draft a detailed description for a task.
func (s *Server) generateTaskDescription(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title      string `json:"title"`
		Hint       string `json:"hint"`
		ProviderID string `json:"provider_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		respondErr(w, http.StatusBadRequest, "title is required")
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

	prompt := fmt.Sprintf(`Write a detailed description for an AI agent task titled "%s".%s

Explain clearly:
- What the agent needs to accomplish
- Any specific requirements or constraints
- What a good result looks like

Be specific and actionable. Return ONLY the description text — no JSON, no markdown headings.`,
		req.Title, hintSection)

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
