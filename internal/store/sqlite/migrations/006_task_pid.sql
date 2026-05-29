-- Migration 006: track subprocess PID for running tasks so we can kill
-- orphaned processes on restart, and add a timeout_at column so the
-- startup health check can report how long tasks were running.
ALTER TABLE tasks ADD COLUMN runner_pid INTEGER DEFAULT 0;
ALTER TABLE tasks ADD COLUMN timeout_at DATETIME;
