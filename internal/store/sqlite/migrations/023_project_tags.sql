-- Add tags column to projects. Stored as a JSON array of strings, e.g. '["reporting","escalation"]'.
-- Default is an empty JSON array so existing rows are valid without any data migration.
ALTER TABLE projects ADD COLUMN tags TEXT NOT NULL DEFAULT '[]';
