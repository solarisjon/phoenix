package agent

import (
	"context"
	"testing"

	"github.com/solarisjon/phoenix/internal/model"
)

type listProviders []*model.Provider

func (l listProviders) List(_ context.Context, _ string) ([]*model.Provider, error) { return l, nil }

type fixedSettings struct{ s *model.SystemSettings }

func (f fixedSettings) Get(_ context.Context) (*model.SystemSettings, error) { return f.s, nil }

func TestChooseUtilityProvider_Precedence(t *testing.T) {
	provs := listProviders{
		{ID: "cli", Type: model.ProviderTypeCodingAgent},
		{ID: "big", Type: model.ProviderTypeLLM, AllowedModels: []model.ModelEntry{
			{ModelID: "gpt-4o", CapabilityTier: model.ModelTierPowerful, InputCostPer1K: 0.005, OutputCostPer1K: 0.015},
			{ModelID: "gpt-4o-mini", CapabilityTier: model.ModelTierFast, InputCostPer1K: 0.00015, OutputCostPer1K: 0.0006},
		}},
		{ID: "local", Type: model.ProviderTypeLLM, AllowedModels: []model.ModelEntry{
			{ModelID: "qwen3-4b", CapabilityTier: model.ModelTierFast}, // free
		}},
	}
	ctx := context.Background()

	// 1. requested wins
	c, ok := ChooseUtilityProvider(ctx, provs, fixedSettings{&model.SystemSettings{UtilityProviderID: "big"}}, "cli")
	if !ok || c.ProviderID != "cli" || c.Source != "requested" {
		t.Errorf("requested: %+v", c)
	}
	// 2. setting
	c, ok = ChooseUtilityProvider(ctx, provs, fixedSettings{&model.SystemSettings{UtilityProviderID: "big", UtilityModel: "gpt-4o-mini"}}, "")
	if !ok || c.ProviderID != "big" || c.Model != "gpt-4o-mini" || c.Source != "setting" {
		t.Errorf("setting: %+v", c)
	}
	// 3. cheapest fast-tier pool model (free local beats mini)
	c, ok = ChooseUtilityProvider(ctx, provs, fixedSettings{&model.SystemSettings{}}, "")
	if !ok || c.ProviderID != "local" || c.Model != "qwen3-4b" || c.Source != "pool:fast" {
		t.Errorf("pool: %+v", c)
	}
	// 4. first LLM when no fast tier
	noFast := listProviders{provs[0], {ID: "big2", Type: model.ProviderTypeLLM}}
	c, ok = ChooseUtilityProvider(ctx, noFast, nil, "")
	if !ok || c.ProviderID != "big2" || c.Source != "first-llm" {
		t.Errorf("first-llm: %+v", c)
	}
	// 5. any provider at all
	c, ok = ChooseUtilityProvider(ctx, listProviders{provs[0]}, nil, "")
	if !ok || c.ProviderID != "cli" {
		t.Errorf("first-any: %+v", c)
	}
	// none
	if _, ok := ChooseUtilityProvider(ctx, listProviders{}, nil, ""); ok {
		t.Errorf("empty list should not resolve")
	}
	if _, ok := ChooseUtilityProvider(ctx, nil, nil, ""); ok {
		t.Errorf("nil lister should not resolve")
	}
}
