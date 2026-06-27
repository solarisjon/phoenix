-- Allow monitors to be paused (scheduling halted) without archiving them.
-- SQLite does not support ALTER COLUMN, so we recreate the projects table
-- with the updated CHECK constraint.

PRAGMA foreign_keys = OFF;

CREATE TABLE projects_new (
    id                    TEXT PRIMARY KEY,
    name                  TEXT NOT NULL,
    description           TEXT NOT NULL DEFAULT '',
    owner                 TEXT NOT NULL REFERENCES users(id),
    status                TEXT NOT NULL DEFAULT 'active'
                              CHECK (status IN ('active', 'archived', 'paused')),
    created_at            DATETIME NOT NULL DEFAULT (datetime('now')),
    working_dir           TEXT NOT NULL DEFAULT '',
    kind                  TEXT NOT NULL DEFAULT 'project'
                              CHECK (kind IN ('project', 'monitor')),
    schedule_interval     INTEGER,
    critic_agent_id       TEXT,
    tags                  TEXT NOT NULL DEFAULT '[]',
    critic_mode           TEXT NOT NULL DEFAULT 'none',
    monitor_model         TEXT NOT NULL DEFAULT '',
    budget_usd            REAL NOT NULL DEFAULT 0,
    budget_period         TEXT NOT NULL DEFAULT 'total',
    schedule_kind         TEXT NOT NULL DEFAULT 'interval',
    schedule_times        TEXT NOT NULL DEFAULT '[]',
    schedule_catch_up     INTEGER NOT NULL DEFAULT 0,
    objective             TEXT NOT NULL DEFAULT '',
    context_summarisation INTEGER NOT NULL DEFAULT 0
);

INSERT INTO projects_new SELECT
    id, name, description, owner, status, created_at, working_dir, kind,
    schedule_interval, critic_agent_id, tags, critic_mode, monitor_model,
    budget_usd, budget_period, schedule_kind, schedule_times, schedule_catch_up,
    objective, context_summarisation
FROM projects;

DROP TABLE projects;
ALTER TABLE projects_new RENAME TO projects;

PRAGMA foreign_keys = ON;
