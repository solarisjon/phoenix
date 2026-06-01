-- Per-agent concurrency limit.
-- 0 = unlimited (preserves existing behaviour for all current agents).
ALTER TABLE agents ADD COLUMN max_concurrent INTEGER NOT NULL DEFAULT 0;
