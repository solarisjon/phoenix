-- FTS5 virtual table for full-text search over task titles, descriptions and output.
-- Uses content= to avoid duplicating data; triggers keep the index in sync.

CREATE VIRTUAL TABLE IF NOT EXISTS tasks_fts USING fts5(
    title,
    description,
    output,
    content=tasks,
    content_rowid=rowid
);

-- Populate index from existing rows
INSERT INTO tasks_fts(rowid, title, description, output)
    SELECT rowid, title, description, output FROM tasks;

CREATE TRIGGER tasks_fts_ai AFTER INSERT ON tasks BEGIN
    INSERT INTO tasks_fts(rowid, title, description, output)
        VALUES (new.rowid, new.title, new.description, new.output);
END;

CREATE TRIGGER tasks_fts_ad AFTER DELETE ON tasks BEGIN
    INSERT INTO tasks_fts(tasks_fts, rowid, title, description, output)
        VALUES ('delete', old.rowid, old.title, old.description, old.output);
END;

CREATE TRIGGER tasks_fts_au AFTER UPDATE ON tasks BEGIN
    INSERT INTO tasks_fts(tasks_fts, rowid, title, description, output)
        VALUES ('delete', old.rowid, old.title, old.description, old.output);
    INSERT INTO tasks_fts(rowid, title, description, output)
        VALUES (new.rowid, new.title, new.description, new.output);
END;
