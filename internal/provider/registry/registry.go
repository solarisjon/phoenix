// Package registry resolves provider DB records to live Provider instances.
package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/provider"
	"github.com/solarisjon/phoenix/internal/provider/llm"
	"github.com/solarisjon/phoenix/internal/provider/opencode"
	"github.com/solarisjon/phoenix/internal/store"
)


// Registry resolves a provider ID to a live Provider instance.
// It caches instances so adapters are not re-created on every task.
type Registry struct {
	repo  store.ProviderRepo
	mu    sync.RWMutex
	cache map[string]provider.Provider // keyed by provider ID
}

// NewRegistry creates a Registry backed by the given ProviderRepo.
func NewRegistry(repo store.ProviderRepo) *Registry {
	return &Registry{
		repo:  repo,
		cache: make(map[string]provider.Provider),
	}
}

// Get returns the Provider for the given provider ID, building it if needed.
func (r *Registry) Get(ctx context.Context, providerID string) (provider.Provider, error) {
	r.mu.RLock()
	if p, ok := r.cache[providerID]; ok { //nolint
		r.mu.RUnlock()
		return p, nil
	}
	r.mu.RUnlock()

	record, err := r.repo.Get(ctx, providerID)
	if err != nil {
		return nil, fmt.Errorf("registry: load provider %s: %w", providerID, err)
	}
	if record == nil {
		return nil, fmt.Errorf("registry: provider %s not found", providerID)
	}

	p, err := buildProvider(record)
	if err != nil {
		return nil, fmt.Errorf("registry: build provider %s: %w", providerID, err)
	}

	r.mu.Lock()
	r.cache[providerID] = p
	r.mu.Unlock()

	return p, nil
}

// Invalidate removes a provider from the cache (call after update/delete).
func (r *Registry) Invalidate(providerID string) {
	r.mu.Lock()
	delete(r.cache, providerID)
	r.mu.Unlock()
}

// InjectForTest pre-loads a provider into the cache. Only for use in tests.
func (r *Registry) InjectForTest(providerID string, p provider.Provider) {
	r.mu.Lock()
	r.cache[providerID] = p
	r.mu.Unlock()
}

// buildProvider constructs a Provider from a model.Provider record.
// Environment variable placeholders (${VAR}) in the config are expanded
// at build time so secrets never need to be stored in the database.
func buildProvider(rec *model.Provider) (provider.Provider, error) {
	expandedConfig := provider.ExpandEnv(rec.Config)
	switch rec.Type {
	case model.ProviderTypeLLM:
		return llm.New(expandedConfig)
	case model.ProviderTypeCodingAgent:
		// Dispatch on the "kind" field in config to support multiple coding agents.
		var meta struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal([]byte(expandedConfig), &meta); err != nil {
			return nil, fmt.Errorf("coding_agent config: parse kind: %w", err)
		}
		switch meta.Kind {
		case "opencode", "":
			return opencode.New(expandedConfig)
		default:
			return nil, fmt.Errorf("coding_agent: unknown kind %q", meta.Kind)
		}
	default:
		return nil, fmt.Errorf("unknown provider type: %s", rec.Type)
	}
}
