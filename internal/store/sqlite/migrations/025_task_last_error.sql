-- Preserve the most recent failure message when a task is retried.
-- This lets humans see why a task last failed without losing that context on retry.
ALTER TABLE tasks ADD COLUMN last_error TEXT;
