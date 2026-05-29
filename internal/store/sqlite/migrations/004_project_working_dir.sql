-- Migration 004: add working_dir to projects
ALTER TABLE projects ADD COLUMN working_dir TEXT NOT NULL DEFAULT '';
