-- Task templates: reusable prompt scaffolds for quick task creation.
-- project_id NULL = global; agent_id NULL = inherits from project context.
CREATE TABLE IF NOT EXISTS task_templates (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    title       TEXT NOT NULL,
    body        TEXT NOT NULL DEFAULT '',
    project_id  TEXT REFERENCES projects(id) ON DELETE CASCADE,
    agent_id    TEXT REFERENCES agents(id)   ON DELETE SET NULL,
    created_at  DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_task_templates_project ON task_templates(project_id);
