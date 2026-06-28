package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/pricing"
	"github.com/solarisjon/phoenix/internal/provider"
)

type createProviderRequest struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Config string `json:"config"`
}

func (r createProviderRequest) validate() string {
	if strings.TrimSpace(r.Name) == "" {
		return "name is required"
	}
	if r.Type != string(model.ProviderTypeLLM) && r.Type != string(model.ProviderTypeCodingAgent) {
		return "type must be 'llm' or 'coding_agent'"
	}
	return ""
}

func (s *Server) listProviders(w http.ResponseWriter, r *http.Request) {
	list, err := s.providers.List(r.Context())
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if list == nil {
		list = []*model.Provider{}
	}
	respond(w, http.StatusOK, list)
}

func (s *Server) getProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, err := s.providers.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if p == nil {
		respondErr(w, http.StatusNotFound, "provider not found")
		return
	}
	respond(w, http.StatusOK, p)
}

func (s *Server) createProvider(w http.ResponseWriter, r *http.Request) {
	var req createProviderRequest
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

	config := req.Config
	if config == "" {
		config = "{}"
	}

	p := &model.Provider{
		ID:        uuid.New().String(),
		Name:      strings.TrimSpace(req.Name),
		Type:      model.ProviderType(req.Type),
		Config:    config,
		CreatedBy: user.ID,
		CreatedAt: time.Now(),
	}
	if err := s.providers.Create(r.Context(), p); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusCreated, p)
}

func (s *Server) updateProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := s.providers.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if existing == nil {
		respondErr(w, http.StatusNotFound, "provider not found")
		return
	}

	var req createProviderRequest
	if !decode(w, r, &req) {
		return
	}
	if msg := req.validate(); msg != "" {
		respondErr(w, http.StatusBadRequest, msg)
		return
	}

	existing.Name = strings.TrimSpace(req.Name)
	existing.Type = model.ProviderType(req.Type)
	if req.Config != "" {
		existing.Config = req.Config
	}

	if err := s.providers.Update(r.Context(), existing); err != nil {
		respondInternalErr(w, err)
		return
	}

	// Invalidate the registry cache so next execution picks up new config.
	s.registry.Invalidate(id)

	respond(w, http.StatusOK, existing)
}

// listProviderModels calls the provider's ListModels() if it supports it,
// and returns {"models":[...]} or {"supported":false}.
func (s *Server) listProviderModels(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, err := s.providers.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if p == nil {
		respondErr(w, http.StatusNotFound, "provider not found")
		return
	}

	prov, err := s.registry.Get(r.Context(), id)
	if err != nil {
		respondErr(w, http.StatusBadRequest, "could not build provider: "+err.Error())
		return
	}

	lister, ok := prov.(provider.ModelLister)
	if !ok {
		respond(w, http.StatusOK, map[string]any{"supported": false, "models": []string{}})
		return
	}

	models, err := lister.ListModels(r.Context())
	if err != nil {
		// Return partial failure as a soft error — don't 500, let UI show free-text fallback
		respond(w, http.StatusOK, map[string]any{"supported": true, "error": err.Error(), "models": []string{}})
		return
	}

	respond(w, http.StatusOK, map[string]any{"supported": true, "models": models})
}

func (s *Server) resyncProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := s.providers.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if existing == nil {
		respondErr(w, http.StatusNotFound, "provider not found")
		return
	}

	// Re-source the user's login shell so freshly-rotated API keys or updated
	// ~/.config values are visible to subprocesses spawned after this point.
	// This is best-effort — a failure here is logged but doesn't block the resync.
	envMsg := "environment refreshed"
	if err := refreshEnvFromLoginShell(); err != nil {
		slog.Warn("resync: refresh env from login shell", "error", err)
		envMsg = "environment refresh skipped (" + err.Error() + ")"
	}

	s.registry.Invalidate(id)
	respond(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"message": envMsg + " · provider cache cleared",
	})
}

func (s *Server) deleteProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := s.providers.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if existing == nil {
		respondErr(w, http.StatusNotFound, "provider not found")
		return
	}
	if err := s.providers.Delete(context.Background(), id); err != nil {
		respondInternalErr(w, err)
		return
	}
	s.registry.Invalidate(id)
	respond(w, http.StatusNoContent, nil)
}

// testProvider validates that a provider is reachable and correctly configured.
// For all provider types it sends a minimal prompt ("Say: ok") with a 15-second
// deadline for LLM/Ollama and a 60-second deadline for coding agents (which must
// spawn a subprocess). A quick binary-existence check is run first for coding
// agents to give a clearer error than a raw exec failure.
// Results are also persisted as the provider's health state.
func (s *Server) testProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rec, err := s.providers.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if rec == nil {
		respondErr(w, http.StatusNotFound, "provider not found")
		return
	}

	start := time.Now()
	type result struct {
		OK        bool   `json:"ok"`
		Message   string `json:"message"`
		LatencyMs int64  `json:"latency_ms"`
	}

	persist := func(ok bool, latencyMs int64, errMsg string) {
		status := "error"
		if ok {
			status = "ok"
		}
		ms := latencyMs
		if err := s.providers.UpdateHealth(context.Background(), id, status, &ms, errMsg); err != nil {
			slog.Warn("testProvider: persist health", "provider_id", id, "error", err)
		}
	}

	// For coding agents do a fast binary preflight before spawning.
	if rec.Type == model.ProviderTypeCodingAgent {
		if err := provider.CheckCodingAgentBinary(rec.Config); err != nil {
			latencyMs := time.Since(start).Milliseconds()
			persist(false, latencyMs, err.Error())
			respond(w, http.StatusOK, result{false, err.Error(), latencyMs})
			return
		}
	}

	// Build provider from registry.
	prov, err := s.registry.Get(r.Context(), id)
	if err != nil {
		latencyMs := time.Since(start).Milliseconds()
		msg := "failed to build provider: " + err.Error()
		persist(false, latencyMs, msg)
		respond(w, http.StatusOK, result{false, msg, latencyMs})
		return
	}

	// Choose timeout: coding agents spawn subprocesses so need more headroom.
	// Use 55s to stay safely under the server's 60s middleware timeout.
	timeout := 15 * time.Second
	if rec.Type == model.ProviderTypeCodingAgent {
		timeout = 55 * time.Second
	}

	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	_, testErr := prov.Execute(ctx, provider.TaskRequest{
		Prompt: "Reply with exactly one word: ok",
	})
	latencyMs := time.Since(start).Milliseconds()
	if testErr != nil {
		persist(false, latencyMs, testErr.Error())
		respond(w, http.StatusOK, result{false, testErr.Error(), latencyMs})
		return
	}
	persist(true, latencyMs, "")
	respond(w, http.StatusOK, result{true, fmt.Sprintf("Connected · %dms", latencyMs), latencyMs})
}

// healthProvider returns the cached health state for a provider as last written
// by the background healthcheck.Checker. It does not fire a live probe — use
// POST /providers/{id}/test for that.
func (s *Server) healthProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rec, err := s.providers.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if rec == nil {
		respondErr(w, http.StatusNotFound, "provider not found")
		return
	}

	type healthResult struct {
		Status    string `json:"status"`
		LatencyMs *int64 `json:"latency_ms,omitempty"`
		Error     string `json:"error,omitempty"`
		CheckedAt string `json:"checked_at,omitempty"`
	}
	var checkedAt string
	if rec.HealthCheckedAt != nil {
		checkedAt = rec.HealthCheckedAt.UTC().Format(time.RFC3339)
	}
	respond(w, http.StatusOK, healthResult{
		Status:    rec.HealthStatus,
		LatencyMs: rec.HealthLatencyMs,
		Error:     rec.HealthError,
		CheckedAt: checkedAt,
	})
}

// refreshEnvFromLoginShell spawns the user's login shell, captures its
// environment with `env`, and calls os.Setenv for every key=value pair.
// This lets Phoenix pick up freshly-rotated API keys, updated ~/.config
// values, or new PATH entries without a full process restart.
func refreshEnvFromLoginShell() error {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Build the shell command based on the shell type.
	// Fish doesn't support -l/-i reliably without a TTY; source config.fish directly.
	// Zsh/bash need -i to source .zshrc/.bashrc (login-only skips those files).
	shellBase := shell
	if idx := strings.LastIndex(shell, "/"); idx >= 0 {
		shellBase = shell[idx+1:]
	}

	var shellArgs []string
	switch shellBase {
	case "fish":
		// Fish sources config.fish explicitly. Also print universal variables
		// that are exported (set -Ux) since they won't appear otherwise.
		shellArgs = []string{"-c", "source ~/.config/fish/config.fish 2>/dev/null; env"}
	case "zsh", "bash", "sh":
		// -i = interactive, sources .zshrc/.bashrc; -l = login, sources .zprofile
		shellArgs = []string{"-i", "-l", "-c", "env 2>/dev/null"}
	default:
		shellArgs = []string{"-l", "-c", "env 2>/dev/null"}
	}

	out, err := exec.CommandContext(ctx, shell, shellArgs...).Output()
	if err != nil {
		// Best-effort fallback: plain -c env
		out, err = exec.CommandContext(ctx, shell, "-c", "env").Output()
		if err != nil {
			return fmt.Errorf("shell %q env capture: %w", shell, err)
		}
	}

	updated := 0
	for _, line := range strings.Split(string(out), "\n") {
		idx := strings.IndexByte(line, '=')
		if idx <= 0 {
			continue
		}
		key := line[:idx]
		val := line[idx+1:]
		// Skip shell internals that could destabilise the running process.
		switch key {
		case "_", "SHLVL", "OLDPWD", "PWD", "PS1", "PS2":
			continue
		}
		os.Setenv(key, val) //nolint:errcheck // os.Setenv only fails on empty key
		updated++
	}
	slog.Info("resync: refreshed environment variables", "count", updated, "shell", shellBase)
	return nil
}

// updateProviderPricing saves per-provider token price overrides used by
// the Cost Insights page to compute projected monthly costs.
func (s *Server) updateProviderPricing(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	var body struct {
		InputPerMToken  float64 `json:"input_per_mtoken"`
		OutputPerMToken float64 `json:"output_per_mtoken"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}

	// Validate provider exists.
	p, err := s.providers.Get(ctx, id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if p == nil {
		respondErr(w, http.StatusNotFound, "provider not found")
		return
	}

	if body.InputPerMToken == 0 && body.OutputPerMToken == 0 {
		s.pricingReg.DeleteOverride(id)
	} else {
		s.pricingReg.SetOverride(id, pricing.ModelPrice{
			InputPerMToken:  body.InputPerMToken,
			OutputPerMToken: body.OutputPerMToken,
		})
	}

	// Persist to system_settings so overrides survive restarts.
	blob, err := s.pricingReg.MarshalOverrides()
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if err := s.systemSettings.SetRaw(ctx, "pricing_overrides", blob); err != nil {
		respondInternalErr(w, err)
		return
	}

	respond(w, http.StatusOK, map[string]string{"status": "ok"})
}
