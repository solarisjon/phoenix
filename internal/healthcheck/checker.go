// Package healthcheck runs periodic connectivity probes for each configured
// provider and persists the results so the UI can show a live health indicator.
package healthcheck

import (
	"context"
	"log/slog"
	"time"

	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/provider"
	"github.com/solarisjon/phoenix/internal/provider/registry"
	"github.com/solarisjon/phoenix/internal/store"
)

// Checker probes all configured providers on a fixed interval and persists
// results via UpdateHealth. On LLM failure it also invalidates the registry
// cache so the next task attempt rebuilds the provider from fresh config.
type Checker struct {
	providers store.ProviderRepo
	registry  *registry.Registry
	interval  time.Duration
	cancel    context.CancelFunc
}

// New creates a Checker. Call Start to begin the background loop.
func New(providers store.ProviderRepo, reg *registry.Registry, interval time.Duration) *Checker {
	return &Checker{
		providers: providers,
		registry:  reg,
		interval:  interval,
	}
}

// Start begins the background probe loop, sharing the given context's lifetime.
func (c *Checker) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	go c.loop(ctx)
}

// Stop cancels the background loop.
func (c *Checker) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
}

func (c *Checker) loop(ctx context.Context) {
	// Run an initial pass immediately so health state is populated at startup.
	c.probeAll(ctx)
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.probeAll(ctx)
		}
	}
}

func (c *Checker) probeAll(ctx context.Context) {
	providers, err := c.providers.List(ctx)
	if err != nil {
		slog.Error("healthcheck: list providers", "error", err)
		return
	}
	for _, rec := range providers {
		if ctx.Err() != nil {
			return
		}
		c.probe(ctx, rec)
	}
}

func (c *Checker) probe(ctx context.Context, rec *model.Provider) {
	var (
		status string
		ms     int64
		errMsg string
	)

	if rec.Type == model.ProviderTypeCodingAgent {
		start := time.Now()
		if err := provider.CheckCodingAgentBinary(rec.Config); err != nil {
			status = "error"
			errMsg = err.Error()
		} else {
			status = "ok"
		}
		ms = time.Since(start).Milliseconds()
	} else {
		prov, err := c.registry.Get(ctx, rec.ID)
		if err != nil {
			status = "error"
			errMsg = "could not build provider: " + err.Error()
		} else {
			start := time.Now()
			tctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			_, testErr := prov.Execute(tctx, provider.TaskRequest{
				Prompt: "Reply with exactly one word: ok",
			})
			cancel()
			ms = time.Since(start).Milliseconds()
			if testErr != nil {
				status = "error"
				errMsg = testErr.Error()
				c.registry.Invalidate(rec.ID)
			} else {
				status = "ok"
			}
		}
	}

	if err := c.providers.UpdateHealth(ctx, rec.ID, status, &ms, errMsg); err != nil {
		slog.Error("healthcheck: persist", "provider_id", rec.ID, "error", err)
	} else {
		slog.Debug("healthcheck: probed", "provider", rec.Name, "status", status, "latency_ms", ms)
	}
}
