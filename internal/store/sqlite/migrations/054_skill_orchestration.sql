-- Migration 054: skill orchestration modes and workflow run metadata

ALTER TABLE skills ADD COLUMN execution_mode TEXT NOT NULL DEFAULT 'direct'
    CHECK (execution_mode IN ('direct', 'orchestrate'));
ALTER TABLE skills ADD COLUMN steps_json TEXT NOT NULL DEFAULT '[]';

ALTER TABLE tasks ADD COLUMN step_slug TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN deliverables_json TEXT NOT NULL DEFAULT '[]';
ALTER TABLE tasks ADD COLUMN derived_health TEXT NOT NULL DEFAULT '';
