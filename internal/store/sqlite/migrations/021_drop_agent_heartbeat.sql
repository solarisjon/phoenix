-- Remove heartbeat_interval from agents. Scheduling is done via monitor
-- schedule_interval (project-level), not per-agent.
ALTER TABLE agents DROP COLUMN heartbeat_interval;
