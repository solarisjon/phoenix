-- Replace token-based per-run budget with a cost-based one (USD).
-- max_tokens_per_run (migration 030) is retained as a dead column for
-- backwards compatibility but is no longer used by the runner.
ALTER TABLE agents ADD COLUMN max_cost_per_run REAL NOT NULL DEFAULT 0;
