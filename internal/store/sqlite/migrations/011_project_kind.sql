-- Add kind field to projects to distinguish human-driven workbenches (project)
-- from autonomous schedule-driven daemons (monitor).
-- All existing projects default to 'project'.
ALTER TABLE projects ADD COLUMN kind TEXT NOT NULL DEFAULT 'project'
    CHECK (kind IN ('project', 'monitor'));
