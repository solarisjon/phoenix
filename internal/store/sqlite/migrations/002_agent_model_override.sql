-- Migration 002: agent model override and task dismissal
ALTER TABLE agents ADD COLUMN model_override TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN dismissed INTEGER NOT NULL DEFAULT 0;
