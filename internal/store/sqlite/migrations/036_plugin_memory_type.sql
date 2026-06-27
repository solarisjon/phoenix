-- Extend the plugins.type CHECK constraint to allow the 'memory' type.
-- SQLite cannot ALTER a CHECK constraint, so we rebuild the table.

PRAGMA foreign_keys=OFF;

CREATE TABLE plugins_new (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    type        TEXT NOT NULL CHECK(type IN ('notifier', 'theme', 'memory')),
    kind        TEXT NOT NULL,
    is_core     BOOLEAN NOT NULL DEFAULT 0,
    enabled     BOOLEAN NOT NULL DEFAULT 1,
    config      TEXT NOT NULL DEFAULT '{}',
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO plugins_new SELECT * FROM plugins;

DROP TABLE plugins;
ALTER TABLE plugins_new RENAME TO plugins;

CREATE INDEX idx_plugins_type ON plugins(type);
CREATE INDEX idx_plugins_enabled ON plugins(enabled);

PRAGMA foreign_keys=ON;
