package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/solarisjon/phoenix/internal/model"
)

type ProviderRepo struct{ db *DB }

func NewProviderRepo(db *DB) *ProviderRepo { return &ProviderRepo{db} }

func (r *ProviderRepo) List(ctx context.Context) ([]*model.Provider, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, type, config, created_by, created_at FROM providers ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("list providers: %w", err)
	}
	defer rows.Close()
	return scanProviders(rows)
}

func (r *ProviderRepo) Get(ctx context.Context, id string) (*model.Provider, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, name, type, config, created_by, created_at FROM providers WHERE id = ?`, id)
	return scanProvider(row)
}

func (r *ProviderRepo) Create(ctx context.Context, p *model.Provider) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO providers (id, name, type, config, created_by) VALUES (?, ?, ?, ?, ?)`,
		p.ID, p.Name, string(p.Type), p.Config, p.CreatedBy)
	if err != nil {
		return fmt.Errorf("create provider: %w", err)
	}
	return nil
}

func (r *ProviderRepo) Update(ctx context.Context, p *model.Provider) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE providers SET name = ?, type = ?, config = ? WHERE id = ?`,
		p.Name, string(p.Type), p.Config, p.ID)
	if err != nil {
		return fmt.Errorf("update provider: %w", err)
	}
	return nil
}

func (r *ProviderRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM providers WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete provider: %w", err)
	}
	return nil
}

func scanProvider(row *sql.Row) (*model.Provider, error) {
	var p model.Provider
	var typ string
	err := row.Scan(&p.ID, &p.Name, &typ, &p.Config, &p.CreatedBy, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan provider: %w", err)
	}
	p.Type = model.ProviderType(typ)
	return &p, nil
}

func scanProviders(rows *sql.Rows) ([]*model.Provider, error) {
	var out []*model.Provider
	for rows.Next() {
		var p model.Provider
		var typ string
		if err := rows.Scan(&p.ID, &p.Name, &typ, &p.Config, &p.CreatedBy, &p.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan provider row: %w", err)
		}
		p.Type = model.ProviderType(typ)
		out = append(out, &p)
	}
	return out, rows.Err()
}
