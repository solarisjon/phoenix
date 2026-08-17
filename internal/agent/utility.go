package agent

import (
	"context"
	"fmt"

	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/provider"
)

// UtilityChoice is the resolved helper model: which provider/model to use for
// small, frequent LLM jobs (summaries, classification, description generation)
// and where the choice came from.
type UtilityChoice struct {
	ProviderID string
	Model      string // "" = provider's configured default
	Source     string // "setting" | "pool:fast" | "first-llm" | "requested"
}

// ProviderResolver is the subset of *registry.Registry the resolver needs.
type ProviderResolver interface {
	Get(ctx context.Context, providerID string) (provider.Provider, error)
	GetWithOverride(ctx context.Context, providerID, modelOverride string) (provider.Provider, error)
}

// ProviderLister is the subset of store.ProviderRepo the resolver needs.
type ProviderLister interface {
	List(ctx context.Context, userID string) ([]*model.Provider, error)
}

// SettingsGetter is the subset of store.SystemSettingsRepo the resolver needs.
type SettingsGetter interface {
	Get(ctx context.Context) (*model.SystemSettings, error)
}

// ChooseUtilityProvider decides which provider/model should run a helper job.
//
// Precedence:
//  1. requestedProviderID (an explicit choice by the caller/UI) — as-is.
//  2. Settings → utility_provider_id (+ utility_model).
//  3. The cheapest model tagged tier=fast in any LLM provider's model pool
//     (all-zero costs, e.g. local models, tie → first listed).
//  4. The first LLM-type provider, else the first provider of any type.
//
// Returns ok=false when there are no providers at all. It never touches the
// network. providers/settings may be nil (tests).
func ChooseUtilityProvider(ctx context.Context, providers ProviderLister, settings SettingsGetter, requestedProviderID string) (UtilityChoice, bool) {
	if requestedProviderID != "" {
		return UtilityChoice{ProviderID: requestedProviderID, Source: "requested"}, true
	}
	if settings != nil {
		if s, err := settings.Get(ctx); err == nil && s != nil && s.UtilityProviderID != "" {
			return UtilityChoice{ProviderID: s.UtilityProviderID, Model: s.UtilityModel, Source: "setting"}, true
		}
	}
	if providers == nil {
		return UtilityChoice{}, false
	}
	all, err := providers.List(ctx, "")
	if err != nil || len(all) == 0 {
		return UtilityChoice{}, false
	}

	// Cheapest fast-tier model across pools.
	best := UtilityChoice{}
	bestCost := -1.0
	for _, p := range all {
		if p == nil || p.Type != model.ProviderTypeLLM {
			continue
		}
		for _, m := range p.AllowedModels {
			if m.CapabilityTier != model.ModelTierFast {
				continue
			}
			cost := m.InputCostPer1K + m.OutputCostPer1K
			if bestCost < 0 || cost < bestCost {
				bestCost = cost
				best = UtilityChoice{ProviderID: p.ID, Model: m.ModelID, Source: "pool:fast"}
			}
		}
	}
	if best.ProviderID != "" {
		return best, true
	}

	for _, p := range all {
		if p != nil && p.Type == model.ProviderTypeLLM {
			return UtilityChoice{ProviderID: p.ID, Source: "first-llm"}, true
		}
	}
	return UtilityChoice{ProviderID: all[0].ID, Source: "first-any"}, true
}

// ResolveUtilityProvider is ChooseUtilityProvider plus building the live
// provider through the registry (with the model override applied when set).
func ResolveUtilityProvider(ctx context.Context, reg ProviderResolver, providers ProviderLister, settings SettingsGetter, requestedProviderID string) (provider.Provider, UtilityChoice, error) {
	choice, ok := ChooseUtilityProvider(ctx, providers, settings, requestedProviderID)
	if !ok {
		return nil, choice, fmt.Errorf("no providers available")
	}
	var (
		prov provider.Provider
		err  error
	)
	if choice.Model != "" {
		prov, err = reg.GetWithOverride(ctx, choice.ProviderID, choice.Model)
	} else {
		prov, err = reg.Get(ctx, choice.ProviderID)
	}
	if err != nil {
		return nil, choice, fmt.Errorf("utility provider %s: %w", choice.ProviderID, err)
	}
	return prov, choice, nil
}
