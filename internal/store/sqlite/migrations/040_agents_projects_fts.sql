-- FTS5 full-text search for agents (name, behaviour, instructions, persona)
-- and projects (name, description, objective). Mirrors the pattern from 013_tasks_fts.sql.

CREATE VIRTUAL TABLE IF NOT EXISTS agents_fts USING fts5(
    name,
    behaviour,
    instructions,
    persona,
    content=agents,
    content_rowid=rowid
);

INSERT INTO agents_fts(rowid, name, behaviour, instructions, persona)
    SELECT rowid, name, behaviour, instructions, persona FROM agents;

CREATE TRIGGER agents_fts_ai AFTER INSERT ON agents BEGIN
    INSERT INTO agents_fts(rowid, name, behaviour, instructions, persona)
        VALUES (new.rowid, new.name, new.behaviour, new.instructions, new.persona);
END;

CREATE TRIGGER agents_fts_ad AFTER DELETE ON agents BEGIN
    INSERT INTO agents_fts(agents_fts, rowid, name, behaviour, instructions, persona)
        VALUES ('delete', old.rowid, old.name, old.behaviour, old.instructions, old.persona);
END;

CREATE TRIGGER agents_fts_au AFTER UPDATE ON agents BEGIN
    INSERT INTO agents_fts(agents_fts, rowid, name, behaviour, instructions, persona)
        VALUES ('delete', old.rowid, old.name, old.behaviour, old.instructions, old.persona);
    INSERT INTO agents_fts(rowid, name, behaviour, instructions, persona)
        VALUES (new.rowid, new.name, new.behaviour, new.instructions, new.persona);
END;

-- FTS5 full-text search for projects (name, description, objective).

CREATE VIRTUAL TABLE IF NOT EXISTS projects_fts USING fts5(
    name,
    description,
    objective,
    content=projects,
    content_rowid=rowid
);

INSERT INTO projects_fts(rowid, name, description, objective)
    SELECT rowid, name, description, objective FROM projects;

CREATE TRIGGER projects_fts_ai AFTER INSERT ON projects BEGIN
    INSERT INTO projects_fts(rowid, name, description, objective)
        VALUES (new.rowid, new.name, new.description, new.objective);
END;

CREATE TRIGGER projects_fts_ad AFTER DELETE ON projects BEGIN
    INSERT INTO projects_fts(projects_fts, rowid, name, description, objective)
        VALUES ('delete', old.rowid, old.name, old.description, old.objective);
END;

CREATE TRIGGER projects_fts_au AFTER UPDATE ON projects BEGIN
    INSERT INTO projects_fts(projects_fts, rowid, name, description, objective)
        VALUES ('delete', old.rowid, old.name, old.description, old.objective);
    INSERT INTO projects_fts(rowid, name, description, objective)
        VALUES (new.rowid, new.name, new.description, new.objective);
END;
