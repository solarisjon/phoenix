-- Migration 053: Skills
-- A skill is a reusable, named instruction set. A user can tell an agent to
-- "execute the morning_coffee skill" in any monitor/project/task description,
-- or bind a skill as a project's default. Skills are injected into the system
-- prompt at prompt-assembly time (internal/agent/prompt.go), so they work
-- identically no matter which provider/CLI actually executes the task.

CREATE TABLE IF NOT EXISTS skills (
    id           TEXT     PRIMARY KEY,
    name         TEXT     NOT NULL,
    slug         TEXT     NOT NULL UNIQUE,
    description  TEXT     NOT NULL DEFAULT '',
    instructions TEXT     NOT NULL DEFAULT '',
    enabled      INTEGER  NOT NULL DEFAULT 1,
    created_at   DATETIME NOT NULL DEFAULT (datetime('now'))
);

ALTER TABLE projects ADD COLUMN default_skill_id TEXT REFERENCES skills(id);
