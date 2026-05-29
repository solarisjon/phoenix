ALTER TABLE agents ADD COLUMN can_hire_agents INTEGER NOT NULL DEFAULT 0;

CREATE TABLE agent_drafts (
  id                   TEXT PRIMARY KEY,
  created_by_agent_id  TEXT NOT NULL REFERENCES agents(id),
  created_by_task_id   TEXT REFERENCES tasks(id),
  name                 TEXT NOT NULL,
  persona              TEXT NOT NULL DEFAULT '',
  instructions         TEXT NOT NULL DEFAULT '',
  guardrails           TEXT NOT NULL DEFAULT '',
  provider_id          TEXT NOT NULL REFERENCES providers(id),
  status               TEXT NOT NULL DEFAULT 'pending_approval',
  dismissed            INTEGER NOT NULL DEFAULT 0,
  created_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
