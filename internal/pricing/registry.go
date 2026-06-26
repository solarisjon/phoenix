// Package pricing provides a model pricing registry for LLM cost projections.
// It merges a built-in seed table, an OpenRouter API cache, and per-provider
// user overrides. Lookup priority: user override > OpenRouter cache > built-in.
package pricing

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ModelPrice holds the cost per 1 million tokens for a given model.
type ModelPrice struct {
	InputPerMToken  float64 `json:"input_per_mtoken"`
	OutputPerMToken float64 `json:"output_per_mtoken"`
}

// Registry is a thread-safe store of model prices.
type Registry struct {
	mu        sync.RWMutex
	builtin   map[string]ModelPrice // seed table, never modified after init
	openrouter map[string]ModelPrice // populated by Refresh()
	overrides  map[string]ModelPrice // keyed by provider ID
}

// New creates a Registry pre-loaded with the built-in seed table.
func New() *Registry {
	r := &Registry{
		openrouter: map[string]ModelPrice{},
		overrides:  map[string]ModelPrice{},
	}
	r.builtin = builtinPrices()
	return r
}

// GetPrice returns the best available price for modelName.
// Lookup checks the OpenRouter cache first (exact match), then falls back to
// the built-in table using longest-prefix matching so that "gpt-4o-mini" does
// not accidentally match the shorter "gpt-4" entry.
// Returns (zero, false) if the model is unknown.
func (r *Registry) GetPrice(modelName string) (ModelPrice, bool) {
	normalized := normalizeName(modelName)
	r.mu.RLock()
	defer r.mu.RUnlock()

	if p, ok := r.openrouter[normalized]; ok {
		return p, true
	}
	// Longest-prefix match against built-in table.
	bestLen := -1
	var bestPrice ModelPrice
	for k, p := range r.builtin {
		if strings.HasPrefix(normalized, k) && len(k) > bestLen {
			bestLen = len(k)
			bestPrice = p
		}
	}
	if bestLen >= 0 {
		return bestPrice, true
	}
	return ModelPrice{}, false
}

// GetOverride returns the user-configured price for the given provider ID.
func (r *Registry) GetOverride(providerID string) (ModelPrice, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.overrides[providerID]
	return p, ok
}

// SetOverride sets a user-configured price for the given provider ID.
func (r *Registry) SetOverride(providerID string, p ModelPrice) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.overrides[providerID] = p
}

// DeleteOverride removes a provider-level price override.
func (r *Registry) DeleteOverride(providerID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.overrides, providerID)
}

// AllOverrides returns a copy of all provider overrides.
func (r *Registry) AllOverrides() map[string]ModelPrice {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]ModelPrice, len(r.overrides))
	for k, v := range r.overrides {
		out[k] = v
	}
	return out
}

// LoadOverrides replaces the in-memory overrides map from a JSON blob.
func (r *Registry) LoadOverrides(jsonBlob string) error {
	if jsonBlob == "" {
		return nil
	}
	m := map[string]ModelPrice{}
	if err := json.Unmarshal([]byte(jsonBlob), &m); err != nil {
		return fmt.Errorf("pricing: load overrides: %w", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.overrides = m
	return nil
}

// MarshalOverrides serialises the current overrides to JSON for persistence.
func (r *Registry) MarshalOverrides() (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	b, err := json.Marshal(r.overrides)
	if err != nil {
		return "", fmt.Errorf("pricing: marshal overrides: %w", err)
	}
	return string(b), nil
}

// Refresh fetches model pricing from the OpenRouter public API and merges the
// results into the registry. Safe to call concurrently; uses a write lock only
// during the merge phase so reads are not blocked during the HTTP call.
func (r *Registry) Refresh(ctx context.Context) error {
	prices, err := fetchOpenRouter(ctx)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.openrouter = prices
	log.Printf("pricing: refreshed %d model prices from OpenRouter", len(prices))
	return nil
}

// StartRefreshLoop runs Refresh every interval in a background goroutine.
// The goroutine stops when ctx is cancelled.
func (r *Registry) StartRefreshLoop(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := r.Refresh(ctx); err != nil {
					log.Printf("pricing: refresh error: %v", err)
				}
			}
		}
	}()
}

// openRouterModel mirrors the fields we care about in the OpenRouter models response.
type openRouterModel struct {
	ID      string `json:"id"`
	Pricing struct {
		Prompt     string `json:"prompt"`     // USD per token as string
		Completion string `json:"completion"` // USD per token as string
	} `json:"pricing"`
}

func fetchOpenRouter(ctx context.Context) (map[string]ModelPrice, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://openrouter.ai/api/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("pricing: openrouter request: %w", err)
	}
	req.Header.Set("User-Agent", "phoenix/1.0")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pricing: openrouter fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pricing: openrouter returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20)) // 4 MB cap
	if err != nil {
		return nil, fmt.Errorf("pricing: openrouter read: %w", err)
	}

	var payload struct {
		Data []openRouterModel `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("pricing: openrouter parse: %w", err)
	}

	out := make(map[string]ModelPrice, len(payload.Data))
	for _, m := range payload.Data {
		inputPerToken := parseFloat(m.Pricing.Prompt)
		outputPerToken := parseFloat(m.Pricing.Completion)
		if inputPerToken <= 0 && outputPerToken <= 0 {
			continue // skip free/unknown models
		}
		key := normalizeName(m.ID)
		out[key] = ModelPrice{
			InputPerMToken:  inputPerToken * 1_000_000,
			OutputPerMToken: outputPerToken * 1_000_000,
		}
	}
	return out, nil
}

func normalizeName(model string) string {
	return strings.ToLower(strings.TrimSpace(model))
}

func parseFloat(s string) float64 {
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
}

// builtinPrices returns the seed table of known model prices (USD per 1M tokens).
// These are approximate list prices as of mid-2026 and serve as a fallback
// when OpenRouter does not have the model.
func builtinPrices() map[string]ModelPrice {
	return map[string]ModelPrice{
		// OpenAI
		"gpt-4o":              {InputPerMToken: 5.00, OutputPerMToken: 15.00},
		"gpt-4o-mini":         {InputPerMToken: 0.15, OutputPerMToken: 0.60},
		"gpt-4-turbo":         {InputPerMToken: 10.00, OutputPerMToken: 30.00},
		"gpt-4":               {InputPerMToken: 30.00, OutputPerMToken: 60.00},
		"gpt-3.5-turbo":       {InputPerMToken: 0.50, OutputPerMToken: 1.50},
		"o1":                  {InputPerMToken: 15.00, OutputPerMToken: 60.00},
		"o1-mini":             {InputPerMToken: 3.00, OutputPerMToken: 12.00},
		"o3-mini":             {InputPerMToken: 1.10, OutputPerMToken: 4.40},
		// Anthropic
		"claude-3-5-sonnet":   {InputPerMToken: 3.00, OutputPerMToken: 15.00},
		"claude-3-5-haiku":    {InputPerMToken: 0.80, OutputPerMToken: 4.00},
		"claude-3-haiku":      {InputPerMToken: 0.25, OutputPerMToken: 1.25},
		"claude-3-opus":       {InputPerMToken: 15.00, OutputPerMToken: 75.00},
		"claude-3-sonnet":     {InputPerMToken: 3.00, OutputPerMToken: 15.00},
		"claude-opus-4":       {InputPerMToken: 15.00, OutputPerMToken: 75.00},
		"claude-sonnet-4":     {InputPerMToken: 3.00, OutputPerMToken: 15.00},
		// Meta Llama
		"llama-3.1-8b":        {InputPerMToken: 0.05, OutputPerMToken: 0.08},
		"llama-3.1-70b":       {InputPerMToken: 0.35, OutputPerMToken: 0.40},
		"llama-3.1-405b":      {InputPerMToken: 2.70, OutputPerMToken: 2.70},
		"llama-3.3-70b":       {InputPerMToken: 0.23, OutputPerMToken: 0.40},
		// Mistral
		"mistral-7b":          {InputPerMToken: 0.07, OutputPerMToken: 0.07},
		"mixtral-8x7b":        {InputPerMToken: 0.45, OutputPerMToken: 0.45},
		"mistral-small":       {InputPerMToken: 0.20, OutputPerMToken: 0.60},
		"mistral-large":       {InputPerMToken: 2.00, OutputPerMToken: 6.00},
		// Google
		"gemini-1.5-pro":      {InputPerMToken: 3.50, OutputPerMToken: 10.50},
		"gemini-1.5-flash":    {InputPerMToken: 0.35, OutputPerMToken: 1.05},
		"gemini-2.0-flash":    {InputPerMToken: 0.10, OutputPerMToken: 0.40},
		"gemini-2.5-pro":      {InputPerMToken: 2.50, OutputPerMToken: 10.00},
		// DeepSeek
		"deepseek-chat":       {InputPerMToken: 0.27, OutputPerMToken: 1.10},
		"deepseek-r1":         {InputPerMToken: 0.55, OutputPerMToken: 2.19},
	}
}
