// Package agenthealthcheck runs periodic self-tests for each configured agent
// and persists the results so the UI can show a live health indicator.
package agenthealthcheck

import (
	"context"
	"log/slog"
	"time"

	"github.com/solarisjon/phoenix/internal/agent"
	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/store"
)

// Checker periodically tests all agents and persists the results.
// Agents are only tested if their status is 'active'.
type Checker struct {
	agents  store.AgentRepo
	runner  *agent.Runner
	interval time.Duration
	cancel  context.CancelFunc
}

// New creates a Checker. Call Start to begin the background loop.
func New(agents store.AgentRepo, runner *agent.Runner, interval time.Duration) *Checker {
	return &Checker{
		agents:   agents,
		runner:   runner,
		interval: interval,
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
	c.testAll(ctx)
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.testAll(ctx)
		}
	}
}

func (c *Checker) testAll(ctx context.Context) {
	agents, err := c.agents.List(ctx, "")
	if err != nil {
		slog.Error("agent healthcheck: list agents", "error", err)
		return
	}
	for _, rec := range agents {
		if ctx.Err() != nil {
			return
		}
		// Only test agents with status 'active'.
		if rec.Status != model.AgentStatusActive {
			continue
		}
		c.testAgent(ctx, rec)
	}
}

func (c *Checker) testAgent(ctx context.Context, rec *model.Agent) {
	elapsed, status, testErr := c.runner.TestAgent(ctx, rec.ID)

	errMsg := ""
	if testErr != nil {
		errMsg = testErr.Error()
	}

	if testErr == nil {
		slog.Debug("agent healthcheck: tested", "agent", rec.Name, "status", status, "elapsed_ms", elapsed)
	} else {
		slog.Warn("agent healthcheck: test failed", "agent", rec.Name, "status", status, "elapsed_ms", elapsed, "error", errMsg)
	}

	if err := c.agents.UpdateHealth(ctx, rec.ID, status, &elapsed, errMsg); err != nil {
		slog.Error("agent healthcheck: persist", "agent_id", rec.ID, "error", err)
	}
}
