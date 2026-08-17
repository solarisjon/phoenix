-- Migration 056: prompt budgeting metadata (local-models phase 2).
--   prompt_tokens   — token count of the assembled prompt as sent (0 if unknown)
--   prompt_trims    — JSON array of {section, action, from_tokens, to_tokens}
--                     recording what was shrunk/dropped to fit the model's context
--   repair_attempts — number of one-shot structured-output repair calls (phase 4)
ALTER TABLE tasks ADD COLUMN prompt_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE tasks ADD COLUMN prompt_trims TEXT NOT NULL DEFAULT '[]';
ALTER TABLE tasks ADD COLUMN repair_attempts INTEGER NOT NULL DEFAULT 0;
