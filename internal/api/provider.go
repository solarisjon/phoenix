package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/solarisjon/phoenix/internal/model"
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
	s.registry.Invalidate(id)
	respond(w, http.StatusOK, map[string]string{"status": "ok", "message": "provider cache cleared — next task will reload config from DB"})
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
// For LLM/Ollama providers it sends a minimal prompt with a 15-second deadline.
// For coding-agent providers it verifies the binary exists on PATH.
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

	if rec.Type == model.ProviderTypeCodingAgent {
		if err := testCodingAgentBinary(rec.Config); err != nil {
			respond(w, http.StatusOK, result{false, err.Error(), time.Since(start).Milliseconds()})
		} else {
			respond(w, http.StatusOK, result{true, fmt.Sprintf("Binary found · %dms", time.Since(start).Milliseconds()), time.Since(start).Milliseconds()})
		}
		return
	}

	// LLM / Ollama — build adapter and send a minimal prompt.
	prov, err := s.registry.Get(r.Context(), id)
	if err != nil {
		respond(w, http.StatusOK, result{false, "failed to build provider: " + err.Error(), time.Since(start).Milliseconds()})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	_, testErr := prov.Execute(ctx, provider.TaskRequest{
		Prompt: "Reply with exactly one word: ok",
	})
	latencyMs := time.Since(start).Milliseconds()
	if testErr != nil {
		respond(w, http.StatusOK, result{false, testErr.Error(), latencyMs})
		return
	}
	respond(w, http.StatusOK, result{true, fmt.Sprintf("Connected · %dms", latencyMs), latencyMs})
}

// testCodingAgentBinary checks that the configured binary (or its default) is
// findable via PATH or as an absolute path.
func testCodingAgentBinary(configJSON string) error {
	var cfg struct {
		Kind       string `json:"kind"`
		BinaryPath string `json:"binary_path"`
	}
	expandedConfig := provider.ExpandEnv(configJSON)
	if err := json.Unmarshal([]byte(expandedConfig), &cfg); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	bin := strings.TrimSpace(cfg.BinaryPath)
	if bin == "" {
		switch cfg.Kind {
		case "pi":
			bin = "pi"
		case "claudecode", "claude":
			bin = "claude"
		case "crush":
			bin = "crush"
		default: // opencode or unset
			bin = "opencode"
		}
	}

	if _, err := exec.LookPath(bin); err != nil {
		return fmt.Errorf("binary %q not found on PATH", bin)
	}
	return nil
}
