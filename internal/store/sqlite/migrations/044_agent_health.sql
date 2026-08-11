-- Agent self-test health fields (mirrors provider health pattern)
ALTER TABLE agents ADD COLUMN agent_health_status TEXT NOT NULL DEFAULT 'unknown';
ALTER TABLE agents ADD COLUMN agent_health_latency_ms INTEGER;
ALTER TABLE agents ADD COLUMN agent_health_error TEXT NOT NULL DEFAULT '';
ALTER TABLE agents ADD COLUMN agent_health_checked_at DATETIME;
