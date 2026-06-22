package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/solarisjon/phoenix/internal/model"
)

// PluginRepo persists plugin records in SQLite.
type PluginRepo struct{ db *DB }

func NewPluginRepo(db *DB) *PluginRepo {
	return &PluginRepo{db: db}
}

const pluginSelectCols = `id, name, type, kind, is_core, enabled, config, created_at, updated_at`

func scanPlugin(row interface{ Scan(...any) error }) (*model.Plugin, error) {
	var p model.Plugin
	err := row.Scan(
		&p.ID, &p.Name, &p.Type, &p.Kind,
		&p.IsCore, &p.Enabled, &p.Config,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *PluginRepo) List(ctx context.Context) ([]*model.Plugin, error) {
	rows, err := r.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT %s FROM plugins ORDER BY created_at`, pluginSelectCols))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*model.Plugin
	for rows.Next() {
		p, err := scanPlugin(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func (r *PluginRepo) ListByType(ctx context.Context, pluginType model.PluginType) ([]*model.Plugin, error) {
	rows, err := r.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT %s FROM plugins WHERE type = ? ORDER BY created_at`, pluginSelectCols),
		string(pluginType))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*model.Plugin
	for rows.Next() {
		p, err := scanPlugin(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func (r *PluginRepo) ListEnabled(ctx context.Context) ([]*model.Plugin, error) {
	rows, err := r.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT %s FROM plugins WHERE enabled = 1 ORDER BY created_at`, pluginSelectCols))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*model.Plugin
	for rows.Next() {
		p, err := scanPlugin(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func (r *PluginRepo) Get(ctx context.Context, id string) (*model.Plugin, error) {
	row := r.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT %s FROM plugins WHERE id = ?`, pluginSelectCols), id)
	p, err := scanPlugin(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

func (r *PluginRepo) GetByKind(ctx context.Context, pluginType model.PluginType, kind string) (*model.Plugin, error) {
	row := r.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT %s FROM plugins WHERE type = ? AND kind = ? LIMIT 1`, pluginSelectCols),
		string(pluginType), kind)
	p, err := scanPlugin(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

func (r *PluginRepo) Create(ctx context.Context, p *model.Plugin) error {
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO plugins (id, name, type, kind, is_core, enabled, config, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, string(p.Type), p.Kind,
		p.IsCore, p.Enabled, p.Config,
		p.CreatedAt, p.UpdatedAt,
	)
	return err
}

func (r *PluginRepo) Update(ctx context.Context, p *model.Plugin) error {
	p.UpdatedAt = time.Now().UTC()
	_, err := r.db.ExecContext(ctx,
		`UPDATE plugins SET name=?, enabled=?, config=?, updated_at=? WHERE id=?`,
		p.Name, p.Enabled, p.Config, p.UpdatedAt, p.ID,
	)
	return err
}

func (r *PluginRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM plugins WHERE id = ? AND is_core = 0`, id)
	return err
}
