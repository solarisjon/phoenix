-- Gap #6: Distinguish soft (advisory) vs hard (mandatory approval-triggering) guardrails
ALTER TABLE agents ADD COLUMN hard_guardrails TEXT NOT NULL DEFAULT '';
-- Store the reason a task was paused by a hard guardrail
ALTER TABLE tasks ADD COLUMN guardrail_reason TEXT;
