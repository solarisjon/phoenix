-- Migration 037: Obsidian vault integration
-- Adds a table for vault configurations so Phoenix agents can write
-- briefings and task outputs directly into the correct Obsidian vault.

CREATE TABLE IF NOT EXISTS obsidian_vaults (
    id         TEXT     PRIMARY KEY,
    name       TEXT     NOT NULL,
    path       TEXT     NOT NULL,
    context    TEXT     NOT NULL DEFAULT '',
    enabled    INTEGER  NOT NULL DEFAULT 1,
    sort_order INTEGER  NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

-- obsidian_root: filesystem path of the vaults directory, e.g. /Users/jon/vaults
INSERT OR IGNORE INTO system_settings (key, value, updated_at)
VALUES ('obsidian_root', '', datetime('now'));

-- obsidian_auto_write: '1' = auto-write after every task completion
INSERT OR IGNORE INTO system_settings (key, value, updated_at)
VALUES ('obsidian_auto_write', '0', datetime('now'));
