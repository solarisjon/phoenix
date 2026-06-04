-- Memos: agent-posted or human-pinned briefing notes that surface important
-- findings from completed tasks. Separate from the inbox (which tracks task
-- lifecycle state) — memos are about content worth reading.

CREATE TABLE IF NOT EXISTS memos (
    id           TEXT PRIMARY KEY,
    project_id   TEXT NOT NULL DEFAULT '',
    project_name TEXT NOT NULL DEFAULT '',  -- denormalised for display without join
    task_id      TEXT NOT NULL DEFAULT '',
    agent_id     TEXT NOT NULL DEFAULT '',
    agent_name   TEXT NOT NULL DEFAULT '',  -- denormalised for display without join
    title        TEXT NOT NULL DEFAULT '',
    body         TEXT NOT NULL DEFAULT '',  -- markdown content
    priority     TEXT NOT NULL DEFAULT 'normal' CHECK(priority IN ('normal', 'high')),
    status       TEXT NOT NULL DEFAULT 'unread' CHECK(status IN ('unread', 'read', 'flagged', 'archived')),
    created_at   DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_memos_status     ON memos(status);
CREATE INDEX IF NOT EXISTS idx_memos_project_id ON memos(project_id);
CREATE INDEX IF NOT EXISTS idx_memos_created_at ON memos(created_at DESC);
