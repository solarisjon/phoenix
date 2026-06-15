-- Add per-agent input-token budget. 0 = unlimited.
ALTER TABLE agents ADD COLUMN max_tokens_per_run INTEGER NOT NULL DEFAULT 0;
