package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/solarisjon/phoenix/internal/agent"
	"github.com/solarisjon/phoenix/internal/agent/eval"
	"github.com/solarisjon/phoenix/internal/config"
	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/paths"
	"github.com/solarisjon/phoenix/internal/provider/registry"
	"github.com/solarisjon/phoenix/internal/store/sqlite"
)

// runEvalModel implements `phoenix eval-model --provider <id|name> [--model M]
// [--profile compact|standard] [--skip-perf] [--json] [--save]`.
//
// It scores how well a model handles Phoenix's protocol markers and JSON,
// using the same prompts and parsers as production, and prints a report.
// --save writes the result onto the provider's model-pool entry (creating one
// if needed) so the Providers page shows the compatibility badge.
func runEvalModel(args []string) int {
	fs := flag.NewFlagSet("eval-model", flag.ContinueOnError)
	providerFlag := fs.String("provider", "", "provider ID or name (required)")
	modelFlag := fs.String("model", "", "model to evaluate (default: provider's configured model)")
	profileFlag := fs.String("profile", "", "prompt profile to test: standard | compact (default: resolved for the model)")
	skipPerf := fs.Bool("skip-perf", false, "skip the throughput case")
	asJSON := fs.Bool("json", false, "print the full report as JSON")
	save := fs.Bool("save", false, "store the result on the provider's model pool entry")
	timeout := fs.Duration("timeout", 5*time.Minute, "per-case timeout")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *providerFlag == "" {
		fmt.Fprintln(os.Stderr, "eval-model: --provider is required")
		fs.Usage()
		return 2
	}

	if err := paths.Init(); err != nil {
		fmt.Fprintln(os.Stderr, "paths:", err)
		return 1
	}
	cfg := config.Load(paths.DataFile("phoenix.db"))
	db, err := sqlite.Open(cfg.DBPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open database:", err)
		return 1
	}
	defer db.Close()

	ctx := context.Background()
	providerRepo := sqlite.NewProviderRepo(db)
	all, err := providerRepo.List(ctx, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "list providers:", err)
		return 1
	}
	var rec *model.Provider
	for _, p := range all {
		if p.ID == *providerFlag || strings.EqualFold(p.Name, *providerFlag) {
			rec = p
			break
		}
	}
	if rec == nil {
		fmt.Fprintf(os.Stderr, "eval-model: provider %q not found. Available:\n", *providerFlag)
		for _, p := range all {
			fmt.Fprintf(os.Stderr, "  %s  %s (%s)\n", p.ID, p.Name, p.Type)
		}
		return 1
	}

	reg := registry.NewRegistry(providerRepo)
	prov, err := reg.GetWithOverride(ctx, rec.ID, *modelFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, "build provider:", err)
		return 1
	}
	profile := agent.ResolveModelProfile(ctx, prov, rec, *modelFlag)
	opts := eval.Options{
		Profile:       profile.Profile,
		ContextWindow: profile.ContextWindow,
		Model:         profile.ModelID,
		ProviderID:    rec.ID,
		SkipPerf:      *skipPerf,
		Timeout:       *timeout,
	}
	if *profileFlag != "" {
		opts.Profile = model.PromptProfile(*profileFlag)
	}
	if !*asJSON {
		fmt.Fprintf(os.Stderr, "Evaluating %s / %s (profile %s, context %d)…\n", rec.Name, opts.Model, opts.Profile, opts.ContextWindow)
		opts.Progress = func(name string, i, n int) {
			fmt.Fprintf(os.Stderr, "  [%d/%d] %s\n", i+1, n, name)
		}
	}

	rep := eval.Run(ctx, prov, opts)

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rep)
	} else {
		printReport(rep)
	}

	if *save {
		comp := eval.ToCompatibility(rep)
		found := false
		for i := range rec.AllowedModels {
			if rec.AllowedModels[i].ModelID == opts.Model {
				rec.AllowedModels[i].Compatibility = &comp
				found = true
			}
		}
		if !found {
			rec.AllowedModels = append(rec.AllowedModels, model.ModelEntry{
				ModelID: opts.Model, Label: opts.Model, CapabilityTier: model.ModelCapabilityTier(rep.SuggestedTier),
				PromptProfile: rep.SuggestedProfile, ContextWindow: opts.ContextWindow, Compatibility: &comp,
			})
		}
		if err := providerRepo.UpdateAllowedModels(ctx, rec.ID, rec.AllowedModels); err != nil {
			fmt.Fprintln(os.Stderr, "save:", err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "Saved compatibility on %s / %s.\n", rec.Name, opts.Model)
	}
	if rep.Score < 50 {
		return 3
	}
	return 0
}

func printReport(rep eval.Report) {
	fmt.Printf("\nPhoenix compatibility — %s (%s)\n", rep.Model, rep.Profile)
	fmt.Printf("Score: %d / 100  Grade: %s\n\n", rep.Score, rep.Grade)
	for _, c := range rep.Cases {
		mark := "✔"
		if !c.Passed {
			mark = "✘"
		}
		fmt.Printf("  %s %-22s %6dms  %s\n", mark, c.Name, c.DurationMs, c.Detail)
	}
	if rep.Perf != nil {
		if rep.Perf.ErrorMessage != "" {
			fmt.Printf("\n  perf: %s\n", rep.Perf.ErrorMessage)
		} else {
			fmt.Printf("\n  perf: %.1f tok/s, first token %dms, cold %dms → warm %dms\n", rep.Perf.TokensPerSec, rep.Perf.TTFTMs, rep.Perf.ColdMs, rep.Perf.WarmMs)
		}
	}
	fmt.Printf("\nSuggested prompt profile: %s   suggested tier: %s\n%s\n", rep.SuggestedProfile, rep.SuggestedTier, rep.Summary)
}
