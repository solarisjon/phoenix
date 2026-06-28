-- Add priority to tasks so queued tasks can be manually bumped to run sooner.
-- Higher value = runs first; default 0 preserves existing FIFO behaviour.
ALTER TABLE tasks ADD COLUMN priority INTEGER NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_tasks_priority ON tasks(priority DESC, created_at ASC);
