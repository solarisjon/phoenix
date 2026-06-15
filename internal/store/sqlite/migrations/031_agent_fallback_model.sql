-- Fallback model used when token budget is exceeded after context truncation.
-- Empty string = no fallback (task fails on budget overflow).
ALTER TABLE agents ADD COLUMN fallback_model TEXT NOT NULL DEFAULT '';
