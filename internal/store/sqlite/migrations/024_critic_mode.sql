-- critic_mode replaces the critic_agent_id FK approach with a flexible
-- string field that can be:
--   'none'       — no critic (default)
--   'builtin'    — ephemeral devil's advocate using the same provider as the original agent
--   'agent:<id>' — use a specific registered agent as critic
--
-- projects.critic_mode: project-level default applied to all tasks
-- tasks.critic_mode:    'inherit' = use project setting; otherwise overrides it
--
-- Migrate existing critic_agent_id rows into the new field.

ALTER TABLE projects ADD COLUMN critic_mode TEXT NOT NULL DEFAULT 'none';
ALTER TABLE tasks    ADD COLUMN critic_mode TEXT NOT NULL DEFAULT 'inherit';

-- Carry forward any existing critic_agent_id values.
UPDATE projects SET critic_mode = 'agent:' || critic_agent_id
    WHERE critic_agent_id IS NOT NULL AND critic_agent_id != '';
