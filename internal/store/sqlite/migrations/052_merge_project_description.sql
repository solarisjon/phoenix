-- Merge project description into objective.
-- Where a project has no objective but has a description, promote the description.
UPDATE projects
    SET objective = description
    WHERE TRIM(objective) = '' AND TRIM(description) != '';

-- Rebuild FTS triggers to no longer index the description column so that
-- future INSERT/UPDATE operations only reflect name + objective in search.
DROP TRIGGER IF EXISTS projects_fts_ai;
DROP TRIGGER IF EXISTS projects_fts_au;
DROP TRIGGER IF EXISTS projects_fts_ad;

CREATE TRIGGER projects_fts_ai AFTER INSERT ON projects BEGIN
    INSERT INTO projects_fts(rowid, name, description, objective)
        VALUES (new.rowid, new.name, '', new.objective);
END;

CREATE TRIGGER projects_fts_ad AFTER DELETE ON projects BEGIN
    INSERT INTO projects_fts(projects_fts, rowid, name, description, objective)
        VALUES ('delete', old.rowid, old.name, old.description, old.objective);
END;

CREATE TRIGGER projects_fts_au AFTER UPDATE ON projects BEGIN
    INSERT INTO projects_fts(projects_fts, rowid, name, description, objective)
        VALUES ('delete', old.rowid, old.name, old.description, old.objective);
    INSERT INTO projects_fts(rowid, name, description, objective)
        VALUES (new.rowid, new.name, '', new.objective);
END;
