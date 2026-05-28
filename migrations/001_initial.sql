-- Phoenix Phase 1 schema

CREATE TABLE IF NOT EXISTS users (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    email       TEXT NOT NULL DEFAULT '',
    settings    TEXT NOT NULL DEFAULT '{}',
    created_at  DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS providers (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    type        TEXT NOT NULL CHECK (type IN ('llm', 'coding_agent')),
    config      TEXT NOT NULL DEFAULT '{}',
    created_by  TEXT NOT NULL REFERENCES users(id),
    created_at  DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS agents (
    id                  TEXT PRIMARY KEY,
    name                TEXT NOT NULL,
    persona             TEXT NOT NULL DEFAULT '',
    instructions        TEXT NOT NULL DEFAULT '',
    guardrails          TEXT NOT NULL DEFAULT '',
    provider_id         TEXT NOT NULL REFERENCES providers(id),
    heartbeat_interval  INTEGER,          -- seconds; NULL = manual only
    created_by          TEXT NOT NULL REFERENCES users(id),
    status              TEXT NOT NULL DEFAULT 'active'
                            CHECK (status IN ('active', 'paused', 'disabled')),
    created_at          DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS projects (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    owner       TEXT NOT NULL REFERENCES users(id),
    status      TEXT NOT NULL DEFAULT 'active'
                    CHECK (status IN ('active', 'archived')),
    created_at  DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS project_agents (
    project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    agent_id    TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    PRIMARY KEY (project_id, agent_id)
);

CREATE TABLE IF NOT EXISTS tasks (
    id              TEXT PRIMARY KEY,
    project_id      TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    agent_id        TEXT NOT NULL REFERENCES agents(id),
    parent_task_id  TEXT REFERENCES tasks(id),
    title           TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'pending'
                        CHECK (status IN (
                            'pending', 'queued', 'running',
                            'completed', 'failed', 'awaiting_approval'
                        )),
    input           TEXT NOT NULL DEFAULT '{}',
    output          TEXT NOT NULL DEFAULT '{}',
    cost_usd        REAL NOT NULL DEFAULT 0.0,
    created_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    started_at      DATETIME,
    completed_at    DATETIME
);

CREATE TABLE IF NOT EXISTS todo_items (
    id              TEXT PRIMARY KEY,
    target_agent_id TEXT NOT NULL REFERENCES agents(id),
    source_agent_id TEXT REFERENCES agents(id),
    project_id      TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    title           TEXT NOT NULL,
    payload         TEXT NOT NULL DEFAULT '{}',
    status          TEXT NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending', 'picked_up', 'done')),
    created_at      DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS broadcasts (
    id              TEXT PRIMARY KEY,
    project_id      TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    source_agent_id TEXT NOT NULL REFERENCES agents(id),
    message         TEXT NOT NULL,
    created_at      DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS broadcast_subscriptions (
    project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    agent_id    TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    PRIMARY KEY (project_id, agent_id)
);

-- Indexes for common query patterns
CREATE INDEX IF NOT EXISTS idx_tasks_project    ON tasks(project_id);
CREATE INDEX IF NOT EXISTS idx_tasks_agent      ON tasks(agent_id);
CREATE INDEX IF NOT EXISTS idx_tasks_status     ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_todo_target      ON todo_items(target_agent_id, status);
CREATE INDEX IF NOT EXISTS idx_agents_provider  ON agents(provider_id);
