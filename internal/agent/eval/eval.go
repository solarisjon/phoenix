// Package eval scores how well a model copes with Phoenix's protocols — the
// markers and JSON the platform parses out of agent output — by running it
// through the SAME prompt assembly and the SAME parsers production uses.
//
// The result is a "Phoenix compatibility" report: a 0–100 score, a letter
// grade, per-case detail, throughput numbers, and a suggested prompt profile /
// capability tier. It is meant to answer, before you trust a local model with
// a monitor: "will this thing actually emit HEALTH_SIGNAL when asked?"
package eval

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/solarisjon/phoenix/internal/agent"
	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/provider"
)

// Options tunes a run.
type Options struct {
	// Profile forces a prompt profile; empty = the model's resolved profile.
	Profile model.PromptProfile
	// ContextWindow is the model's usable window (0 = unknown); used to size
	// the long-prompt case and to suggest a profile.
	ContextWindow int
	// Model / ProviderID are recorded on the report.
	Model, ProviderID string
	// SkipPerf skips the throughput case (hosted models: costs tokens).
	SkipPerf bool
	// Timeout per case. Default 5 minutes.
	Timeout time.Duration
	// Progress, if set, is called before each case runs.
	Progress func(caseName string, index, total int)
}

// CaseResult is one scored check.
type CaseResult struct {
	Name       string `json:"name"`
	Passed     bool   `json:"passed"`
	Detail     string `json:"detail"`
	DurationMs int64  `json:"duration_ms"`
	TokensIn   int    `json:"tokens_in"`
	TokensOut  int    `json:"tokens_out"`
	Skipped    bool   `json:"skipped,omitempty"`
}

// Perf holds throughput measurements from the perf case.
type Perf struct {
	TTFTMs       int64   `json:"ttft_ms"`        // time to first streamed chunk (warm run)
	TokensPerSec float64 `json:"tokens_per_sec"` // output tokens / generation time (warm run)
	ColdMs       int64   `json:"cold_ms"`        // first run wall time
	WarmMs       int64   `json:"warm_ms"`        // second run wall time (cache_prompt / KV reuse)
	OutputTokens int     `json:"output_tokens"`
	Streamed     bool    `json:"streamed"`
	ErrorMessage string  `json:"error,omitempty"`
}

// Report is the outcome of a run — stored on the model pool entry as
// model.ModelCompatibility.
type Report struct {
	ProviderID       string              `json:"provider_id"`
	Model            string              `json:"model"`
	Profile          model.PromptProfile `json:"profile"`
	ContextWindow    int                 `json:"context_window"`
	Score            int                 `json:"score"` // 0–100
	Grade            string              `json:"grade"` // A | B | C | D
	Cases            []CaseResult        `json:"cases"`
	Perf             *Perf               `json:"perf,omitempty"`
	SuggestedProfile model.PromptProfile `json:"suggested_profile"`
	SuggestedTier    string              `json:"suggested_tier"`
	Summary          string              `json:"summary"`
	StartedAt        time.Time           `json:"started_at"`
	FinishedAt       time.Time           `json:"finished_at"`
}

// Case is one scored check: it builds a request with the real prompt code,
// runs it, and inspects the output with the real parsers.
type Case struct {
	Name  string
	Build func(opts Options) provider.TaskRequest
	Check func(output string) (passed bool, detail string)
	// Weight in the score (default 1). Marker and guardrail cases weigh 2 —
	// they are what monitors, loops and safety depend on.
	Weight int
}

// Run executes the suite against prov and returns the report. It never
// panics on a misbehaving model; a provider error marks the case failed.
func Run(ctx context.Context, prov provider.Provider, opts Options) Report {
	if opts.Timeout <= 0 {
		opts.Timeout = 5 * time.Minute
	}
	if opts.Profile == "" {
		opts.Profile = model.PromptProfileStandard
	}
	rep := Report{ProviderID: opts.ProviderID, Model: opts.Model, Profile: opts.Profile, ContextWindow: opts.ContextWindow, StartedAt: time.Now()}

	cases := Cases()
	total := len(cases)
	if !opts.SkipPerf {
		total++
	}
	weightSum, weightPassed := 0, 0
	for i, c := range cases {
		if opts.Progress != nil {
			opts.Progress(c.Name, i, total)
		}
		w := c.Weight
		if w <= 0 {
			w = 1
		}
		weightSum += w
		res := runCase(ctx, prov, c, opts)
		if res.Passed {
			weightPassed += w
		}
		rep.Cases = append(rep.Cases, res)
		if ctx.Err() != nil {
			break
		}
	}
	if !opts.SkipPerf && ctx.Err() == nil {
		if opts.Progress != nil {
			opts.Progress("perf", total-1, total)
		}
		p := runPerf(ctx, prov, opts)
		rep.Perf = &p
	}

	if weightSum > 0 {
		rep.Score = int(float64(weightPassed) / float64(weightSum) * 100)
	}
	rep.Grade = grade(rep.Score)
	rep.SuggestedProfile, rep.SuggestedTier = suggest(rep, opts)
	rep.Summary = summarise(rep)
	rep.FinishedAt = time.Now()
	return rep
}

func runCase(ctx context.Context, prov provider.Provider, c Case, opts Options) CaseResult {
	res := CaseResult{Name: c.Name}
	start := time.Now()
	req := c.Build(opts)
	if req.MaxOutputTokens == 0 {
		req.MaxOutputTokens = 1024
	}
	cctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	resp, err := prov.Execute(cctx, req)
	res.DurationMs = time.Since(start).Milliseconds()
	if err != nil {
		res.Detail = "provider error: " + truncate(err.Error(), 200)
		return res
	}
	res.TokensIn, res.TokensOut = resp.TokensIn, resp.TokensOut
	if strings.TrimSpace(resp.Output) == "" {
		res.Detail = "empty output"
		return res
	}
	res.Passed, res.Detail = c.Check(resp.Output)
	return res
}

// runPerf streams a fixed prompt twice and measures TTFT / throughput on the
// warm run (the second one benefits from KV-cache reuse on llama.cpp).
func runPerf(ctx context.Context, prov provider.Provider, opts Options) Perf {
	p := Perf{}
	req := provider.TaskRequest{
		SystemPrompt:    "You are a helpful assistant. Answer exactly as asked.",
		Prompt:          "Write the numbers 1 to 40 separated by commas, then the word DONE.",
		MaxOutputTokens: 256,
	}
	one := func() (wall, ttft time.Duration, outTokens int, err error) {
		cctx, cancel := context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
		start := time.Now()
		ch, err := prov.StreamExecute(cctx, req)
		if err != nil {
			return 0, 0, 0, err
		}
		first := true
		var sb strings.Builder
		for c := range ch {
			if c.Error != nil {
				return time.Since(start), ttft, outTokens, c.Error
			}
			if c.Content != "" && first {
				ttft = time.Since(start)
				first = false
			}
			sb.WriteString(c.Content)
			if c.Done && c.TokensOut > 0 {
				outTokens = c.TokensOut
			}
		}
		if outTokens == 0 {
			outTokens = provider.HeuristicTokenCount(sb.String())
		}
		return time.Since(start), ttft, outTokens, nil
	}
	cold, _, _, err := one()
	if err != nil {
		p.ErrorMessage = truncate(err.Error(), 200)
		return p
	}
	warm, ttft, out, err := one()
	if err != nil {
		p.ErrorMessage = truncate(err.Error(), 200)
		return p
	}
	p.ColdMs, p.WarmMs, p.TTFTMs, p.OutputTokens, p.Streamed = cold.Milliseconds(), warm.Milliseconds(), ttft.Milliseconds(), out, true
	if gen := warm - ttft; gen > 0 && out > 0 {
		p.TokensPerSec = float64(out) / gen.Seconds()
	}
	return p
}

func grade(score int) string {
	switch {
	case score >= 90:
		return "A"
	case score >= 75:
		return "B"
	case score >= 50:
		return "C"
	default:
		return "D"
	}
}

// suggest derives a prompt profile and capability tier from the results.
// Heuristic by design — the user can override on the model pool row.
func suggest(rep Report, opts Options) (model.PromptProfile, string) {
	failed := map[string]bool{}
	for _, c := range rep.Cases {
		if !c.Passed && !c.Skipped {
			failed[c.Name] = true
		}
	}
	markerFails := 0
	for _, n := range []string{"marker_memo", "marker_health", "marker_react", "format_under_pressure"} {
		if failed[n] {
			markerFails++
		}
	}
	profile := model.PromptProfileStandard
	if (opts.ContextWindow > 0 && opts.ContextWindow <= 16384) || markerFails >= 1 {
		profile = model.PromptProfileCompact
	}
	// Tier: the suite measures protocol compliance, not reasoning depth, so
	// be conservative — only a clean A (incl. the guardrail stop and the
	// buried-instruction case) earns "standard"; everything else is "fast".
	// "powerful"/"planning" are never suggested; the user decides those.
	tier := "fast"
	if rep.Score >= 90 && !failed["guardrail_stop"] && !failed["long_prompt_follow"] && !failed["json_plan_schema"] {
		tier = "standard"
	}
	return profile, tier
}

func summarise(rep Report) string {
	var fails []string
	for _, c := range rep.Cases {
		if !c.Passed && !c.Skipped {
			fails = append(fails, c.Name)
		}
	}
	s := fmt.Sprintf("Score %d (%s).", rep.Score, rep.Grade)
	if len(fails) == 0 {
		s += " Emitted every Phoenix protocol correctly."
	} else {
		s += " Failed: " + strings.Join(fails, ", ") + "."
	}
	if rep.Perf != nil && rep.Perf.TokensPerSec > 0 {
		s += fmt.Sprintf(" ~%.0f tok/s, first token in %d ms.", rep.Perf.TokensPerSec, rep.Perf.TTFTMs)
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ToCompatibility converts a Report into the compact form stored on a
// model pool entry.
func ToCompatibility(rep Report) model.ModelCompatibility {
	c := model.ModelCompatibility{
		Score:            rep.Score,
		Grade:            rep.Grade,
		Profile:          string(rep.Profile),
		SuggestedProfile: string(rep.SuggestedProfile),
		SuggestedTier:    rep.SuggestedTier,
		Summary:          rep.Summary,
		ProbedAt:         rep.FinishedAt,
		Cases:            map[string]bool{},
	}
	for _, cs := range rep.Cases {
		c.Cases[cs.Name] = cs.Passed
	}
	if rep.Perf != nil {
		c.TokensPerSec = rep.Perf.TokensPerSec
		c.TTFTMs = rep.Perf.TTFTMs
	}
	return c
}

// ensure the agent import is used even if a future refactor moves parsers.
var _ = agent.ExtractJSONObject
