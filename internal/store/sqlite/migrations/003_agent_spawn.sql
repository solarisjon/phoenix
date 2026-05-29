-- Allow agents to be configured to spawn tasks for other agents.
ALTER TABLE agents ADD COLUMN can_spawn_agents INTEGER NOT NULL DEFAULT 0;
