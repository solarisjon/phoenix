-- Add monitor_model to projects so a cheap model can be selected for
-- monitor runs independently of the agent's model_override. When set,
-- this takes priority over the agent's model_override for monitor tasks.
-- Empty string means "use agent default".
ALTER TABLE projects ADD COLUMN monitor_model TEXT NOT NULL DEFAULT '';
