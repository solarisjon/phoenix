package agent

import (
	"context"
	"testing"

	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/provider"
)

// capableProv is a mockProvider that also reports Capabilities and counts
// tokens exactly (one token per word), like the llama.cpp adapter.
type capableProv struct {
	mockProvider
	caps provider.Capabilities
}

func (c *capableProv) Capabilities(_ context.Context) provider.Capabilities { return c.caps }
func (c *capableProv) CountTokens(_ context.Context, s string) (int, error) {
	n := 0
	inWord := false
	for _, r := range s {
		if r == ' ' || r == '\n' || r == '\t' {
			inWord = false
			continue
		}
		if !inWord {
			n++
			inWord = true
		}
	}
	return n, nil
}

func TestResolveModelProfile_Defaults(t *testing.T) {
	mp := ResolveModelProfile(context.Background(), &mockProvider{}, nil, "")
	if mp.ContextWindow != 0 || mp.PromptBudget() != 0 || mp.Profile != model.PromptProfileStandard || mp.ExactTokens {
		t.Errorf("defaults = %+v", mp)
	}
	if mp.Count("abcdefgh") != 2 {
		t.Errorf("heuristic count = %d", mp.Count("abcdefgh"))
	}
}

func TestResolveModelProfile_FromProbe(t *testing.T) {
	prov := &capableProv{caps: provider.Capabilities{ContextWindow: 8192, Local: true, ExactTokenCount: true, Reasoning: true}}
	rec := &model.Provider{Config: `{"model":"qwen3-8b"}`}
	mp := ResolveModelProfile(context.Background(), prov, rec, "")
	if mp.ModelID != "qwen3-8b" || mp.ContextWindow != 8192 || mp.Source != "probe" || !mp.Local || !mp.Reasoning || !mp.ExactTokens {
		t.Errorf("probe profile = %+v", mp)
	}
	if mp.Profile != model.PromptProfileCompact {
		t.Errorf("8k context should auto-select compact, got %q", mp.Profile)
	}
	if mp.MaxOutputTokens != 2048 { // default reserve capped at 1/4 of an 8k window
		t.Errorf("reserve = %d", mp.MaxOutputTokens)
	}
	// 8192 - 2048 - 256 - 409 = 5479
	if b := mp.PromptBudget(); b != 5479 {
		t.Errorf("budget = %d", b)
	}
	if mp.Count("one two three") != 3 {
		t.Errorf("exact count not used: %d", mp.Count("one two three"))
	}
}

func TestResolveModelProfile_EntryWinsAndPins(t *testing.T) {
	prov := &capableProv{caps: provider.Capabilities{ContextWindow: 8192, Local: true}}
	rec := &model.Provider{Config: `{"model":"x"}`, AllowedModels: []model.ModelEntry{
		{ModelID: "big", ContextWindow: 131072, MaxOutputTokens: 8192, PromptProfile: model.PromptProfileStandard, CapabilityTier: model.ModelTierPowerful},
		{ModelID: "small", CapabilityTier: model.ModelTierFast}, // no window → probe; fast tier → compact
		{ModelID: "pinned", ContextWindow: 4096, PromptProfile: model.PromptProfileStandard},
	}}
	mp := ResolveModelProfile(context.Background(), prov, rec, "big")
	if mp.ContextWindow != 131072 || mp.Source != "entry" || mp.MaxOutputTokens != 8192 || mp.Profile != model.PromptProfileStandard {
		t.Errorf("entry profile = %+v", mp)
	}
	mp = ResolveModelProfile(context.Background(), prov, rec, "small")
	if mp.ContextWindow != 8192 || mp.Source != "probe" || mp.Profile != model.PromptProfileCompact {
		t.Errorf("small profile = %+v", mp)
	}
	mp = ResolveModelProfile(context.Background(), prov, rec, "pinned")
	if mp.Profile != model.PromptProfileStandard {
		t.Errorf("pinned standard must win over auto-compact: %+v", mp)
	}
	// Reserve can't exceed the window.
	rec.AllowedModels = append(rec.AllowedModels, model.ModelEntry{ModelID: "tiny", ContextWindow: 2048, MaxOutputTokens: 4096})
	mp = ResolveModelProfile(context.Background(), prov, rec, "tiny")
	if mp.MaxOutputTokens != 1024 || mp.PromptBudget() < 512 {
		t.Errorf("tiny profile = %+v budget=%d", mp, mp.PromptBudget())
	}
}

func TestAutoProfile(t *testing.T) {
	cases := []struct {
		ctx  int
		tier model.ModelCapabilityTier
		want model.PromptProfile
	}{
		{0, "", model.PromptProfileStandard},
		{16384, "", model.PromptProfileCompact},
		{16385, "", model.PromptProfileStandard},
		{131072, model.ModelTierFast, model.PromptProfileCompact},
		{131072, model.ModelTierPowerful, model.PromptProfileStandard},
	}
	for _, c := range cases {
		if got := autoProfile(c.ctx, c.tier); got != c.want {
			t.Errorf("autoProfile(%d,%q) = %q, want %q", c.ctx, c.tier, got, c.want)
		}
	}
}
