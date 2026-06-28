-- Add health-check state to providers so the background checker can persist results.
ALTER TABLE providers ADD COLUMN health_status     TEXT    NOT NULL DEFAULT 'unknown';
ALTER TABLE providers ADD COLUMN health_latency_ms INTEGER;
ALTER TABLE providers ADD COLUMN health_error      TEXT    NOT NULL DEFAULT '';
ALTER TABLE providers ADD COLUMN health_checked_at DATETIME;
