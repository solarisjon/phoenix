-- Migration 055: store the HEALTH_REASON line emitted alongside HEALTH_SIGNAL
-- on monitor runs (or the reason the signal was inferred when no marker was
-- emitted). Free text, shown on monitor run cards. Part of local-models phase 0.2.
ALTER TABLE tasks ADD COLUMN health_reason TEXT NOT NULL DEFAULT '';
