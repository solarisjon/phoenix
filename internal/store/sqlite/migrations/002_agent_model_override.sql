-- Add model_override to agents so a specific agent can override its provider's default model.
ALTER TABLE agents ADD COLUMN model_override TEXT NOT NULL DEFAULT '';

-- Add dismissed flag: allows tasks to be hidden from inbox without deletion.
ALTER TABLE tasks ADD COLUMN dismissed INTEGER NOT NULL DEFAULT 0;
