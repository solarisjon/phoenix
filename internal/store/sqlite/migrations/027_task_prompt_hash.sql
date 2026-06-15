-- Add prompt_hash to tasks to enable monitor output diffing.
-- When a monitor task's fully-assembled prompt is identical to a recent
-- completed run (same hash), the runner can skip the LLM call entirely
-- and reuse the cached output, saving 100% of that run's cost.
ALTER TABLE tasks ADD COLUMN prompt_hash TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_tasks_prompt_hash ON tasks (project_id, prompt_hash, status);
