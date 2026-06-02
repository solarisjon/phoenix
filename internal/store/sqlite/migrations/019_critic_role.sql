ALTER TABLE projects ADD COLUMN critic_agent_id TEXT;
ALTER TABLE tasks ADD COLUMN is_critic_review INTEGER NOT NULL DEFAULT 0;
ALTER TABLE tasks ADD COLUMN reviewed_task_id TEXT;
