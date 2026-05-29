-- Add model_override to agents so a specific agent can override its provider's default model.
ALTER TABLE agents ADD COLUMN model_override TEXT NOT NULL DEFAULT '';

-- Add dismissed status support: allow tasks to be dismissed from inbox without deletion.
-- (dismissed tasks are hidden from inbox but preserved for audit)
ALTER TABLE tasks ADD COLUMN dismissed INTEGER NOT NULL DEFAULT 0;
