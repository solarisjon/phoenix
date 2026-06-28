-- Heartbeat escalation threshold: fire escalate action only after N consecutive non-clear signals
ALTER TABLE projects ADD COLUMN heartbeat_escalate_after INTEGER NOT NULL DEFAULT 0; -- 0 = escalate immediately
