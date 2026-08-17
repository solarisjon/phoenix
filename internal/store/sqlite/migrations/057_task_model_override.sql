-- Migration 057: per-task model override (local-models phase 3).
-- Set by the orchestrator when SelectModelForDomain picks a model on the
-- assigned agent's provider, and usable by any caller that wants one task to
-- run on a specific model. Empty = agent/monitor/provider default.
ALTER TABLE tasks ADD COLUMN model_override TEXT NOT NULL DEFAULT '';
