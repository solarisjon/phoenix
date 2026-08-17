package agent

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/provider"
)

// ModelProfile is everything prompt assembly needs to know about the model a
// task will run on: how much room there is, how much to reserve for the
// answer, which prompt wording to use, and how to count tokens.
type ModelProfile struct {
	ModelID         string
	ContextWindow   int                 // 0 = unknown → no budgeting
	MaxOutputTokens int                 // reserved for the reply; always > 0 when ContextWindow > 0
	Profile         model.PromptProfile // resolved: standard | compact (never auto)
	Local           bool
	Reasoning       bool
	ExactTokens     bool // Count is exact (server tokenizer) rather than heuristic
	Count           func(string) int
	Source          string // where ContextWindow came from: "entry" | "probe" | "" — for logs/UI
}

// Budget-related defaults.
const (
	// compactContextThreshold: models with a usable context at or below this
	// get the compact profile when prompt_profile is auto.
	compactContextThreshold = 16384
	// defaultReserveOutput is reserved for the reply when nothing else says.
	defaultReserveOutput = 4096
	// profileProbeTimeout bounds the Capabilities/tokenizer probes so a dead
	// local server can't stall task start.
	profileProbeTimeout = 5 * time.Second
)

// ResolveModelProfile combines the curated ModelEntry (if any) for modelID,
// the adapter's probed Capabilities, and defaults — in that precedence order.
//
//   - ContextWindow: entry → probe → 0 (unknown; budgeting off)
//   - MaxOutputTokens: entry → probe cap → defaultReserveOutput (only used when
//     ContextWindow > 0)
//   - Profile: entry (if pinned) → auto: compact when ContextWindow ≤ 16k or
//     the entry's tier is "fast", else standard
//   - Count: provider.TokenCounter (exact for llama.cpp) → HeuristicTokenCount
//
// providerRec may be nil (tests, or repo unavailable); prov may be nil.
func ResolveModelProfile(ctx context.Context, prov provider.Provider, providerRec *model.Provider, modelID string) ModelProfile {
	mp := ModelProfile{ModelID: modelID, Count: provider.HeuristicTokenCount}

	// If the caller didn't know the model, fall back to the provider config's.
	if modelID == "" && providerRec != nil {
		modelID = configModel(providerRec.Config)
		mp.ModelID = modelID
	}

	var entry *model.ModelEntry
	if providerRec != nil {
		for i := range providerRec.AllowedModels {
			if providerRec.AllowedModels[i].ModelID == modelID {
				entry = &providerRec.AllowedModels[i]
				break
			}
		}
	}

	var caps provider.Capabilities
	if capable, ok := prov.(provider.Capable); ok {
		pctx, cancel := context.WithTimeout(ctx, profileProbeTimeout)
		caps = capable.Capabilities(pctx)
		cancel()
	}
	mp.Local = caps.Local
	mp.Reasoning = caps.Reasoning
	mp.ExactTokens = caps.ExactTokenCount

	if entry != nil {
		if entry.ContextWindow > 0 {
			mp.ContextWindow, mp.Source = entry.ContextWindow, "entry"
		}
		if entry.MaxOutputTokens > 0 {
			mp.MaxOutputTokens = entry.MaxOutputTokens
		}
		if entry.Reasoning {
			mp.Reasoning = true
		}
		mp.Profile = entry.PromptProfile
	}
	if mp.ContextWindow == 0 && caps.ContextWindow > 0 {
		mp.ContextWindow, mp.Source = caps.ContextWindow, "probe"
	}
	if mp.MaxOutputTokens == 0 {
		mp.MaxOutputTokens = caps.MaxOutputTokens
	}
	if mp.MaxOutputTokens <= 0 {
		// Nobody said: reserve the default, but never more than a quarter of a
		// small window — an 8k model shouldn't lose half its context to a
		// reply cap that is rarely reached.
		mp.MaxOutputTokens = defaultReserveOutput
		if mp.ContextWindow > 0 && mp.MaxOutputTokens > mp.ContextWindow/4 {
			mp.MaxOutputTokens = mp.ContextWindow / 4
			if mp.MaxOutputTokens < 512 {
				mp.MaxOutputTokens = 512
			}
		}
	}
	if mp.ContextWindow > 0 && mp.MaxOutputTokens >= mp.ContextWindow {
		// Never let the reserve eat the whole window; keep at least half for the prompt.
		mp.MaxOutputTokens = mp.ContextWindow / 2
	}

	if mp.Profile == model.PromptProfileAuto {
		tier := model.ModelCapabilityTier("")
		if entry != nil {
			tier = entry.CapabilityTier
		}
		mp.Profile = autoProfile(mp.ContextWindow, tier)
	}

	if tc, ok := prov.(provider.TokenCounter); ok {
		mp.Count = func(s string) int {
			cctx, cancel := context.WithTimeout(ctx, profileProbeTimeout)
			defer cancel()
			n, err := tc.CountTokens(cctx, s)
			if err != nil || n <= 0 {
				return provider.HeuristicTokenCount(s)
			}
			return n
		}
	}
	return mp
}

// autoProfile implements the "auto" prompt-profile rule.
func autoProfile(contextWindow int, tier model.ModelCapabilityTier) model.PromptProfile {
	if (contextWindow > 0 && contextWindow <= compactContextThreshold) || tier == model.ModelTierFast {
		return model.PromptProfileCompact
	}
	return model.PromptProfileStandard
}

// configModel extracts the "model" key from a provider config JSON blob.
func configModel(config string) string {
	var m struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal([]byte(config), &m); err != nil {
		return ""
	}
	return strings.TrimSpace(m.Model)
}

// PromptBudget is the token budget derived from a profile: how many tokens the
// assembled prompt may occupy. Zero when the context window is unknown.
func (mp ModelProfile) PromptBudget() int {
	if mp.ContextWindow <= 0 {
		return 0
	}
	// Reserve the reply plus a safety margin for chat-template framing
	// (role tags, BOS/EOS) and tokenizer drift: 256 tokens + 5 %.
	b := mp.ContextWindow - mp.MaxOutputTokens - 256 - mp.ContextWindow/20
	if b < 512 {
		b = 512 // degenerate windows: still leave room for the task itself
	}
	return b
}
