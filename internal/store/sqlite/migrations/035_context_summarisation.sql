-- Add context summarisation toggle to projects.
-- Add summary_cache to tasks for caching follow-up chain summaries.
ALTER TABLE projects ADD COLUMN context_summarisation INTEGER NOT NULL DEFAULT 0;
ALTER TABLE tasks ADD COLUMN summary_cache TEXT NOT NULL DEFAULT '';
