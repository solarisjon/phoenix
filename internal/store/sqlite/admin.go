package sqlite

import (
	"context"
	"fmt"
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
