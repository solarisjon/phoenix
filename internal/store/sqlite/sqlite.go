// Package sqlite provides SQLite-backed implementations of all store interfaces.
package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log"

	_ "modernc.org/sqlite"
)

//go:embed migrations
var migrationsFS embed.FS

// DB wraps a *sql.DB with Phoenix-specific helpers.
type DB struct {
	*sql.DB
}

// Open opens (or creates) the SQLite database at the given path and
// runs any pending migrations.
func Open(path string) (*DB, error) {
	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL&_foreign_keys=on&_busy_timeout=5000", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// SQLite performs best with a single writer connection.
	db.SetMaxOpenConns(1)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	wrapped := &DB{db}
	if err := wrapped.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return wrapped, nil
}

// migrate creates the migrations tracking table and runs any SQL files
// in the embedded migrations directory that have not yet been applied.
func (db *DB) migrate() error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS _migrations (
			name       TEXT PRIMARY KEY,
			applied_at DATETIME NOT NULL DEFAULT (datetime('now'))
		)
	`)
	if err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()

		var exists bool
		err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM _migrations WHERE name = ?)`, name).Scan(&exists)
		if err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if exists {
			continue
		}

		data, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}

		log.Printf("applying migration: %s", name)
		if _, err := db.Exec(string(data)); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}

		if _, err := db.Exec(`INSERT INTO _migrations (name) VALUES (?)`, name); err != nil {
			return fmt.Errorf("record migration %s: %w", name, err)
		}
	}

	return nil
}

// ResetOrphanedTasks marks any queued or running tasks as failed.
// Called on startup because those tasks lost their runner goroutines when the
// process exited. Without this, they block project deletion and confuse the UI.
func (db *DB) ResetOrphanedTasks(ctx context.Context) error {
	res, err := db.ExecContext(ctx, `
		UPDATE tasks
		SET status = 'failed',
		    output = json_object('error', 'Task abandoned: server restarted while task was ' || status)
		WHERE status IN ('queued', 'running')
	`)
	if err != nil {
		return fmt.Errorf("reset orphaned tasks: %w", err)
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		log.Printf("startup: reset %d orphaned task(s) to failed", n)
	}
	return nil
}

// Seed ensures a default user exists. Called once after migration.
func (db *DB) Seed(ctx context.Context) error {
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	_, err := db.ExecContext(ctx, `
		INSERT INTO users (id, name, email, settings)
		VALUES ('00000000-0000-0000-0000-000000000001', 'Admin', '', '{}')
	`)
	return err
}

// nullString converts a *string to sql.NullString.
func nullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

// nullInt converts a *int to sql.NullInt64.
func nullInt(i *int) sql.NullInt64 {
	if i == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*i), Valid: true}
}
