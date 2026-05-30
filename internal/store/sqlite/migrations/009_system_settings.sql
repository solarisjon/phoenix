-- System-wide key/value settings table.
-- Used for global guardrails and other platform-level configuration.
CREATE TABLE IF NOT EXISTS system_settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL DEFAULT '',
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

-- Seed the global guardrails row so GET always returns a record.
INSERT OR IGNORE INTO system_settings (key, value) VALUES ('global_guardrails_enabled', '0');
INSERT OR IGNORE INTO system_settings (key, value) VALUES ('global_guardrails', '');
