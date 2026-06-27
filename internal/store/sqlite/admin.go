package sqlite

import (
	"context"
	"fmt"
	"io"
	"os"
)

// AdminRepo exposes low-level DB operations needed for admin endpoints.
type AdminRepo struct{ db *DB }

func NewAdminRepo(db *DB) *AdminRepo { return &AdminRepo{db} }

// DBPath returns the filesystem path to the SQLite file.
func (r *AdminRepo) DBPath() string { return r.db.Path }

// VacuumInto creates a consistent, WAL-consolidated snapshot of the database
// at destPath. Safe to call while the server is running.
func (r *AdminRepo) VacuumInto(ctx context.Context, destPath string) error {
	_, err := r.db.ExecContext(ctx, fmt.Sprintf("VACUUM INTO '%s'", destPath))
	if err != nil {
		return fmt.Errorf("vacuum into %s: %w", destPath, err)
	}
	return nil
}

// Reset deletes all user-configured data — providers, agents, projects, tasks,
// teams, memos, plugins, and system settings — in FK-safe order inside a
// single transaction. The users table and schema migrations are preserved.
// This is irreversible; callers should take a backup first.
func (r *AdminRepo) Reset(ctx context.Context) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin reset transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Delete every user-data table explicitly in FK-safe order — leaf tables
	// first, parents last. We do NOT rely on CASCADE because some FKs in this
	// schema omit ON DELETE CASCADE (e.g. agent_drafts → tasks), and relying on
	// it silently skips child rows when the cascade doesn't fire.
	tables := []string{
		"notification_rules",          // child of plugins
		"agent_drafts",                // child of agents + tasks (no CASCADE)
		"todo_items",                  // child of projects + agents
		"broadcasts",                  // child of projects + agents
		"broadcast_subscriptions",     // child of projects + agents
		"tasks",                       // child of projects + agents
		"project_agents",              // child of projects + agents
		"team_agents",                 // child of teams + agents
		"memos",
		"plugins",
		"obsidian_vaults",
		"projects",
		"teams",
		"agents",
		"providers",
		"system_settings",
	}
	for _, t := range tables {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+t); err != nil {
			return fmt.Errorf("reset: clear %s: %w", t, err)
		}
	}

	return tx.Commit()
}

// StageRestore copies srcPath to {dbPath}.restore-pending. On the next server
// start, Open() will atomically apply it before opening the database.
func (r *AdminRepo) StageRestore(srcPath string) error {
	dest := r.db.Path + ".restore-pending"
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("open restore source: %w", err)
	}
	defer src.Close()

	// Validate: SQLite files begin with "SQLite format 3\000"
	header := make([]byte, 16)
	if _, err := src.Read(header); err != nil || string(header) != "SQLite format 3\x00" {
		return fmt.Errorf("uploaded file is not a valid SQLite database")
	}
	if _, err := src.Seek(0, 0); err != nil {
		return fmt.Errorf("seek restore file: %w", err)
	}

	tmp := dest + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("create staged restore file: %w", err)
	}
	defer os.Remove(tmp)

	if _, err := io.Copy(out, src); err != nil {
		out.Close()
		return fmt.Errorf("copy restore file: %w", err)
	}
	out.Close()

	return os.Rename(tmp, dest)
}
