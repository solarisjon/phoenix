-- Plugin system: core + community plugins with notification rules.

CREATE TABLE plugins (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    type        TEXT NOT NULL CHECK(type IN ('notifier', 'theme')),
    kind        TEXT NOT NULL,
    is_core     BOOLEAN NOT NULL DEFAULT 0,
    enabled     BOOLEAN NOT NULL DEFAULT 1,
    config      TEXT NOT NULL DEFAULT '{}',
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE notification_rules (
    id          TEXT PRIMARY KEY,
    plugin_id   TEXT NOT NULL REFERENCES plugins(id) ON DELETE CASCADE,
    event_type  TEXT NOT NULL CHECK(event_type IN (
        'task.completed', 'task.failed',
        'task.needs_approval', 'task.guardrail_triggered'
    )),
    project_id  TEXT,
    enabled     BOOLEAN NOT NULL DEFAULT 1,
    template    TEXT,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_plugins_type ON plugins(type);
CREATE INDEX idx_plugins_enabled ON plugins(enabled);
CREATE INDEX idx_notification_rules_plugin_id ON notification_rules(plugin_id);
CREATE INDEX idx_notification_rules_event_type ON notification_rules(event_type);

-- Master switches for plugin categories (both off by default — opt-in).
INSERT OR IGNORE INTO system_settings (key, value, updated_at)
VALUES ('core_plugins_enabled', '0', CURRENT_TIMESTAMP);
INSERT OR IGNORE INTO system_settings (key, value, updated_at)
VALUES ('community_plugins_enabled', '0', CURRENT_TIMESTAMP);
