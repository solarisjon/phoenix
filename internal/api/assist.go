package api

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/solarisjon/phoenix/internal/agent"
	"github.com/solarisjon/phoenix/internal/provider"
)

// assistProvider resolves the provider used by the API's "assist" features
// (agent/task/project/team description generation, guardrail generation,
// Obsidian vault context and note writing, next-action suggestions).
//
// requestedID (from the request body) wins; otherwise the configured helper
// model (Settings → System → Helper Model) is used, then the cheapest
// fast-tier pool model, then the first LLM provider — see
// agent.ChooseUtilityProvider. Before local-models phase 3 every endpoint
// picked "the first LLM provider" with its default model.
func (s *Server) assistProvider(ctx context.Context, requestedID string) (provider.Provider, error) {
	prov, choice, err := agent.ResolveUtilityProvider(ctx, s.registry, s.providers, s.systemSettings, requestedID)
	if err != nil {
		return nil, fmt.Errorf("provider load failed: %w", err)
	}
	slog.Debug("assist: provider resolved", "provider_id", choice.ProviderID, "model", choice.Model, "source", choice.Source)
	return prov, nil
}
