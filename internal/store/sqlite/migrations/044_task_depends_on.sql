ALTER TABLE tasks ADD COLUMN depends_on TEXT; -- JSON array of task IDs, NULL means no dependencies
