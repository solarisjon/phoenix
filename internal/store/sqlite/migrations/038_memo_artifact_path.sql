-- Add artifact_path to memos so the Briefing UI can link directly to .md files.
ALTER TABLE memos ADD COLUMN artifact_path TEXT NOT NULL DEFAULT '';
