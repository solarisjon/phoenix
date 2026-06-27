package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/solarisjon/phoenix/internal/model"
)

// ObsidianVaultRepo persists Obsidian vault configurations.
type ObsidianVaultRepo struct{ db *DB }

func NewObsidianVaultRepo(db *DB) *ObsidianVaultRepo {
	return &ObsidianVaultRepo{db: db}
}

const obsidianVaultCols = `id, name, path, context, enabled, sort_order, created_at`

func scanVault(row interface {
	Scan(...any) error
}) (*model.ObsidianVault, error) {
	var v model.ObsidianVault
	var enabled int
	var createdAt string
	if err := row.Scan(&v.ID, &v.Name, &v.Path, &v.Context, &enabled, &v.SortOrder, &createdAt); err != nil {
		return nil, err
	}
	v.Enabled = enabled == 1
	if t, err := time.Parse("2006-01-02 15:04:05", createdAt); err == nil {
		v.CreatedAt = t
	}
	return &v, nil
}

func (r *ObsidianVaultRepo) List(ctx context.Context) ([]*model.ObsidianVault, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+obsidianVaultCols+` FROM obsidian_vaults ORDER BY sort_order ASC, name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.ObsidianVault
	for rows.Next() {
		v, err := scanVault(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *ObsidianVaultRepo) ListEnabled(ctx context.Context) ([]*model.ObsidianVault, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+obsidianVaultCols+` FROM obsidian_vaults WHERE enabled = 1 ORDER BY sort_order ASC, name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.ObsidianVault
	for rows.Next() {
		v, err := scanVault(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *ObsidianVaultRepo) Get(ctx context.Context, id string) (*model.ObsidianVault, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+obsidianVaultCols+` FROM obsidian_vaults WHERE id = ?`, id)
	v, err := scanVault(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func (r *ObsidianVaultRepo) Create(ctx context.Context, v *model.ObsidianVault) error {
	enabled := 0
	if v.Enabled {
		enabled = 1
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO obsidian_vaults (id, name, path, context, enabled, sort_order, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		v.ID, v.Name, v.Path, v.Context, enabled, v.SortOrder,
		v.CreatedAt.UTC().Format("2006-01-02 15:04:05"))
	return err
}

func (r *ObsidianVaultRepo) Update(ctx context.Context, v *model.ObsidianVault) error {
	enabled := 0
	if v.Enabled {
		enabled = 1
	}
	_, err := r.db.ExecContext(ctx,
		`UPDATE obsidian_vaults SET name=?, path=?, context=?, enabled=?, sort_order=? WHERE id=?`,
		v.Name, v.Path, v.Context, enabled, v.SortOrder, v.ID)
	return err
}

func (r *ObsidianVaultRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM obsidian_vaults WHERE id = ?`, id)
	return err
}
