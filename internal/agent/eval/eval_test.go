package eval

import (
	"context"
	"strings"
	"testing"

	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/provider"
)

// scripted answers each case by inspecting the prompt it receives — a
// "perfect" model when good=true, a chatty model that never emits markers
// when good=false.
type scripted struct {
	good    bool
	prompts []string
}

func (s *scripted) Execute(_ context.Context, req provider.TaskRequest) (provider.TaskResponse, error) {
	s.prompts = append(s.prompts, req.SystemPrompt+"\n"+req.Prompt)
	if !s.good {
		return provider.TaskResponse{Output: "Sure! Here is my answer in plain prose without any special markers."}, nil
	}
	sys, user := req.SystemPrompt, req.Prompt
	switch {
	case strings.Contains(sys, "Orchestration Mode"):
		return provider.TaskResponse{Output: `{"confidence":0.9,"rationale":"three steps","subtasks":[{"title":"Design","description":"d","domain":"design","complexity":"medium","agent_id":null,"provider_id":null,"model_id":null}]}`}, nil
	case strings.Contains(sys, "GUARDRAIL_TRIGGERED"):
		return provider.TaskResponse{Output: "GUARDRAIL_TRIGGERED: deleting /var/cache/build violates the no-delete rule"}, nil
	case strings.Contains(sys, "PHOENIX-OK"):
		return provider.TaskResponse{Output: "42\nPHOENIX-OK"}, nil
	case strings.Contains(sys, "HEALTH_SIGNAL"):
		return provider.TaskResponse{Output: "All fine.\nHEALTH_SIGNAL: all_clear\nHEALTH_REASON: nominal\nMEMO_START\nTitle: ok\nfine\nMEMO_END"}, nil
	case strings.Contains(sys, "Autonomous Loop Mode"):
		return provider.TaskResponse{Output: "Looked at latency graphs.\nNEXT_ACTION: check the slow query log\nEND_NEXT_ACTION"}, nil
	case strings.Contains(user, "briefing memo"):
		return provider.TaskResponse{Output: "MEMO_START\nTitle: Disk at 95%\nPriority: high\nweb-1 root volume 95%\nMEMO_END"}, nil
	}
	return provider.TaskResponse{Output: "ok"}, nil
}

func (s *scripted) StreamExecute(_ context.Context, _ provider.TaskRequest) (<-chan provider.StreamChunk, error) {
	ch := make(chan provider.StreamChunk, 3)
	ch <- provider.StreamChunk{Content: "1, 2, 3"}
	ch <- provider.StreamChunk{Content: " DONE"}
	ch <- provider.StreamChunk{Done: true, TokensOut: 12}
	close(ch)
	return ch, nil
}
func (s *scripted) EstimateCost(_ provider.TaskRequest) provider.CostEstimate {
	return provider.CostEstimate{}
}

func TestRun_PerfectModelScoresA(t *testing.T) {
	prov := &scripted{good: true}
	rep := Run(context.Background(), prov, Options{Model: "perfect", ContextWindow: 32768})
	if rep.Score != 100 || rep.Grade != "A" {
		t.Errorf("score=%d grade=%s cases=%+v", rep.Score, rep.Grade, rep.Cases)
	}
	for _, c := range rep.Cases {
		if !c.Passed {
			t.Errorf("case %s failed: %s", c.Name, c.Detail)
		}
	}
	if rep.SuggestedProfile != model.PromptProfileStandard || rep.SuggestedTier != "standard" {
		t.Errorf("suggestions = %s / %s", rep.SuggestedProfile, rep.SuggestedTier)
	}
	if rep.Perf == nil || !rep.Perf.Streamed || rep.Perf.OutputTokens != 12 {
		t.Errorf("perf = %+v", rep.Perf)
	}
	if len(prov.prompts) != 8 {
		t.Errorf("expected 8 case calls, got %d", len(prov.prompts))
	}
	comp := ToCompatibility(rep)
	if comp.Score != 100 || comp.Cases["marker_health"] != true || comp.Grade != "A" {
		t.Errorf("compat = %+v", comp)
	}
}

func TestRun_ChattyModelScoresLow_SuggestsCompactFast(t *testing.T) {
	prov := &scripted{good: false}
	rep := Run(context.Background(), prov, Options{Model: "chatty", ContextWindow: 8192, SkipPerf: true})
	if rep.Score != 0 || rep.Grade != "D" {
		t.Errorf("score=%d grade=%s", rep.Score, rep.Grade)
	}
	if rep.SuggestedProfile != model.PromptProfileCompact || rep.SuggestedTier != "fast" {
		t.Errorf("suggestions = %s / %s", rep.SuggestedProfile, rep.SuggestedTier)
	}
	if rep.Perf != nil {
		t.Errorf("perf should be skipped")
	}
	if !strings.Contains(rep.Summary, "Failed:") {
		t.Errorf("summary = %q", rep.Summary)
	}
}

func TestRun_CompactProfileUsed(t *testing.T) {
	prov := &scripted{good: true}
	rep := Run(context.Background(), prov, Options{Profile: model.PromptProfileCompact, SkipPerf: true})
	if rep.Profile != model.PromptProfileCompact {
		t.Errorf("profile = %s", rep.Profile)
	}
	// Compact monitor prompt uses the terse contract text.
	found := false
	for _, p := range prov.prompts {
		if strings.Contains(p, "copied verbatim (they are parsed by the platform)") {
			found = true
		}
	}
	if !found {
		t.Errorf("compact monitor prompt not used")
	}
}
