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

func (r *ProviderRepo) List(ctx context.Context, userID string) ([]*model.Provider, error) {
	var rows *sql.Rows
	var err error
	if userID == "" {
		rows, err = r.db.QueryContext(ctx,
			`SELECT id, name, type, config, created_by, created_at,
			        health_status, health_latency_ms, health_error, health_checked_at
			 FROM providers ORDER BY created_at ASC`)
	} else {
		rows, err = r.db.QueryContext(ctx,
			`SELECT id, name, type, config, created_by, created_at,
			        health_status, health_latency_ms, health_error, health_checked_at
			 FROM providers WHERE created_by = ? ORDER BY created_at ASC`, userID)
	}
	if err != nil {
		return nil, fmt.Errorf("list providers: %w", err)
	}
	defer rows.Close()
	return scanProviders(rows)
}

func (r *ProviderRepo) Get(ctx context.Context, id string) (*model.Provider, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, name, type, config, created_by, created_at,
		        health_status, health_latency_ms, health_error, health_checked_at
		 FROM providers WHERE id = ?`, id)
	return scanProvider(row)
}

func (r *ProviderRepo) UpdateHealth(ctx context.Context, id, status string, latencyMs *int64, errMsg string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE providers SET health_status = ?, health_latency_ms = ?, health_error = ?, health_checked_at = datetime('now') WHERE id = ?`,
		status, latencyMs, errMsg, id)
	if err != nil {
		return fmt.Errorf("update provider health: %w", err)
	}
	return nil
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
	var latencyMs sql.NullInt64
	var checkedAt sql.NullTime
	err := row.Scan(&p.ID, &p.Name, &typ, &p.Config, &p.CreatedBy, &p.CreatedAt,
		&p.HealthStatus, &latencyMs, &p.HealthError, &checkedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan provider: %w", err)
	}
	p.Type = model.ProviderType(typ)
	if latencyMs.Valid {
		p.HealthLatencyMs = &latencyMs.Int64
	}
	if checkedAt.Valid {
		p.HealthCheckedAt = &checkedAt.Time
	}
	return &p, nil
}

func scanProviders(rows *sql.Rows) ([]*model.Provider, error) {
	var out []*model.Provider
	for rows.Next() {
		var p model.Provider
		var typ string
		var latencyMs sql.NullInt64
		var checkedAt sql.NullTime
		if err := rows.Scan(&p.ID, &p.Name, &typ, &p.Config, &p.CreatedBy, &p.CreatedAt,
			&p.HealthStatus, &latencyMs, &p.HealthError, &checkedAt); err != nil {
			return nil, fmt.Errorf("scan provider row: %w", err)
		}
		p.Type = model.ProviderType(typ)
		if latencyMs.Valid {
			p.HealthLatencyMs = &latencyMs.Int64
		}
		if checkedAt.Valid {
			p.HealthCheckedAt = &checkedAt.Time
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}
