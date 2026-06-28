package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/provider"
)

// getSystemSettings returns the current platform-wide settings.
func (s *Server) getSystemSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.systemSettings.Get(r.Context())
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusOK, settings)
}

// updateSystemSettings persists platform-wide settings.
func (s *Server) updateSystemSettings(w http.ResponseWriter, r *http.Request) {
	var req model.SystemSettings
	if !decode(w, r, &req) {
		return
	}
	if err := s.systemSettings.Save(r.Context(), &req); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusOK, req)
}

// generateGlobalGuardrails uses an LLM to produce well-written guardrail text
// from a plain-English description of what the user wants to prevent or enforce.
func (s *Server) generateGlobalGuardrails(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Description string `json:"description"`
		ProviderID  string `json:"provider_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Description) == "" {
		respondErr(w, http.StatusBadRequest, "description is required")
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

	prompt := fmt.Sprintf(`You are an AI safety and governance expert helping to write platform-wide guardrail rules for an AI agent orchestration system.

These guardrails will be injected into the system prompt of EVERY agent across ALL projects, overriding any agent-specific instructions. They are the final word on what agents may and may not do.

Based on the following intent, write clear, firm, and actionable guardrail rules:

Intent: %s

Requirements:
- Write 3-7 concise bullet points or numbered rules
- Each rule should be direct and unambiguous (e.g. "Never modify, create, or delete Jira issues unless explicitly instructed by the task description")
- Use imperative language ("Never", "Always", "Do not", "Only")
- Focus on preventing unintended side effects and protecting external systems
- Do not include explanation or preamble — output the rules only, in plain text

Output only the guardrail rules, one per line, starting each with a bullet (•) or number.`, req.Description)

	resp, err := prov.Execute(r.Context(), provider.TaskRequest{
		SystemPrompt: "You are a precise technical writer. Output only the requested content with no preamble or explanation.",
		Prompt:       prompt,
	})
	if err != nil {
		respondErr(w, http.StatusInternalServerError, fmt.Sprintf("generation failed: %v", err))
		return
	}

	output := strings.TrimSpace(resp.Output)
	respond(w, http.StatusOK, map[string]string{"guardrails": output})
}

// resetAll permanently deletes all user-configured data (providers, agents,
// projects, tasks, teams, memos, plugins, system settings) and returns the
// instance to a clean factory state. Irreversible — no backup is taken here.
func (s *Server) resetAll(w http.ResponseWriter, r *http.Request) {
	// Require explicit confirmation header to prevent accidental calls.
	if r.Header.Get("X-Confirm-Reset") != "RESET" {
		respondErr(w, http.StatusBadRequest, "missing or incorrect confirmation header")
		return
	}

	slog.Warn("admin: factory reset initiated", "remote_addr", r.RemoteAddr)

	if err := s.admin.Reset(r.Context()); err != nil {
		slog.Error("admin: factory reset failed", "err", err)
		respondInternalErr(w, err)
		return
	}

	slog.Warn("admin: factory reset complete — all user data deleted")
	respond(w, http.StatusOK, map[string]string{"status": "reset"})
}
