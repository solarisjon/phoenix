-- Add free-text provenance field to tasks.
-- Populated by dispatching agents (e.g. Monitors) when they create tasks in
-- other projects. Empty for human-created tasks.
ALTER TABLE tasks ADD COLUMN source TEXT NOT NULL DEFAULT '';
