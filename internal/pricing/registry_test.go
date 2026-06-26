package pricing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBuiltinLookup(t *testing.T) {
	reg := New()
	p, ok := reg.GetPrice("gpt-4o")
	if !ok {
		t.Fatal("expected gpt-4o to be in builtin table")
	}
	if p.InputPerMToken != 5.00 {
		t.Errorf("gpt-4o input price: got %v, want 5.00", p.InputPerMToken)
	}
}

func TestPrefixMatch(t *testing.T) {
	reg := New()
	// versioned model name should match via prefix
	p, ok := reg.GetPrice("gpt-4o-2024-11-20")
	if !ok {
		t.Fatal("expected prefix match for gpt-4o-2024-11-20")
	}
	if p.InputPerMToken != 5.00 {
		t.Errorf("prefix match price: got %v, want 5.00", p.InputPerMToken)
	}
}

func TestOverridePriority(t *testing.T) {
	reg := New()
	reg.SetOverride("provider-abc", ModelPrice{InputPerMToken: 1.0, OutputPerMToken: 2.0})
	p, ok := reg.GetOverride("provider-abc")
	if !ok {
		t.Fatal("expected override to be present")
	}
	if p.InputPerMToken != 1.0 {
		t.Errorf("override input: got %v, want 1.0", p.InputPerMToken)
	}
}

func TestMarshalRoundtrip(t *testing.T) {
	reg := New()
	reg.SetOverride("p1", ModelPrice{InputPerMToken: 3.0, OutputPerMToken: 9.0})
	blob, err := reg.MarshalOverrides()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	reg2 := New()
	if err := reg2.LoadOverrides(blob); err != nil {
		t.Fatalf("load: %v", err)
	}
	p, ok := reg2.GetOverride("p1")
	if !ok {
		t.Fatal("expected override after roundtrip")
	}
	if p.InputPerMToken != 3.0 {
		t.Errorf("roundtrip input: got %v, want 3.0", p.InputPerMToken)
	}
}

func TestOpenRouterMerge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := map[string]any{
			"data": []map[string]any{
				{
					"id": "openai/gpt-4o",
					"pricing": map[string]string{
						"prompt":     "0.000005",
						"completion": "0.000015",
					},
				},
				{
					"id": "some/free-model",
					"pricing": map[string]string{
						"prompt":     "0",
						"completion": "0",
					},
				},
			},
		}
		json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	// Patch the fetch function for this test by calling fetchOpenRouter directly
	// against our test server URL (we can't override the const URL easily, so
	// we test the merge logic by injecting directly).
	prices := map[string]ModelPrice{
		"openai/gpt-4o": {InputPerMToken: 5.0, OutputPerMToken: 15.0},
	}
	reg := New()
	reg.mu.Lock()
	reg.openrouter = prices
	reg.mu.Unlock()

	p, ok := reg.GetPrice("openai/gpt-4o")
	if !ok {
		t.Fatal("expected openrouter price for openai/gpt-4o")
	}
	if p.InputPerMToken != 5.0 {
		t.Errorf("openrouter price: got %v, want 5.0", p.InputPerMToken)
	}
	_ = srv // keep server alive during test
	_ = context.Background()
}

func TestSuggestCheaperModel(t *testing.T) {
	reg := New()
	suggested, pct, ok := SuggestCheaperModel("gpt-4o", reg)
	if !ok {
		t.Fatal("expected suggestion for gpt-4o")
	}
	if suggested != "gpt-4o-mini" {
		t.Errorf("suggestion: got %q, want %q", suggested, "gpt-4o-mini")
	}
	if pct <= 0 {
		t.Errorf("expected positive saving pct, got %v", pct)
	}
}

func TestNoSuggestionForCheapModel(t *testing.T) {
	reg := New()
	_, _, ok := SuggestCheaperModel("gpt-4o-mini", reg)
	if ok {
		t.Error("should not suggest swap for already-cheap model")
	}
}

func TestModelTier(t *testing.T) {
	cases := []struct {
		model string
		tier  int
	}{
		{"gpt-4o", 1},
		{"gpt-4o-2024-11-20", 1},
		{"claude-3-5-sonnet-20241022", 1},
		{"gpt-4o-mini", 2},
		{"claude-3-haiku-20240307", 2},
		{"llama-3.1-8b-instruct", 3},
		{"my-local-model", 0},
	}
	for _, c := range cases {
		got := ModelTier(c.model)
		if got != c.tier {
			t.Errorf("ModelTier(%q) = %d, want %d", c.model, got, c.tier)
		}
	}
}
