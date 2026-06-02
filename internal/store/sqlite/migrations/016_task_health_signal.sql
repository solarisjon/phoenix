-- Migration 016: add health_signal to tasks
-- Values: 'all_clear' | 'needs_attention' | 'failed' | NULL (non-monitor tasks)
ALTER TABLE tasks ADD COLUMN health_signal TEXT;
