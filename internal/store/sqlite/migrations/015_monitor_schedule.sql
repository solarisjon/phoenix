-- Move the schedule from Agent.heartbeat_interval to Project (monitors only).
-- schedule_interval is in seconds; NULL means no automatic schedule.
ALTER TABLE projects ADD COLUMN schedule_interval INTEGER;
