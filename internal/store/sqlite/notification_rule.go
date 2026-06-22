package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/solarisjon/phoenix/internal/model"
)

// NotificationRuleRepo persists notification rules in SQLite.
type NotificationRuleRepo struct{ db *DB }

func NewNotificationRuleRepo(db *DB) *NotificationRuleRepo {
	return &NotificationRuleRepo{db: db}
}

const ruleSelectCols = `id, plugin_id, event_type, project_id, enabled, template, created_at`

func scanRule(row interface{ Scan(...any) error }) (*model.NotificationRule, error) {
	var r model.NotificationRule
	err := row.Scan(
		&r.ID, &r.PluginID, &r.EventType,
		&r.ProjectID, &r.Enabled, &r.Template,
		&r.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (repo *NotificationRuleRepo) ListByPlugin(ctx context.Context, pluginID string) ([]*model.NotificationRule, error) {
	rows, err := repo.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT %s FROM notification_rules WHERE plugin_id = ? ORDER BY created_at`, ruleSelectCols),
		pluginID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*model.NotificationRule
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

func (repo *NotificationRuleRepo) ListByEventType(ctx context.Context, eventType model.NotifyEventType) ([]*model.NotificationRule, error) {
	rows, err := repo.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT %s FROM notification_rules WHERE event_type = ? AND enabled = 1 ORDER BY created_at`, ruleSelectCols),
		string(eventType))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*model.NotificationRule
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

func (repo *NotificationRuleRepo) ListEnabled(ctx context.Context) ([]*model.NotificationRule, error) {
	rows, err := repo.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT %s FROM notification_rules WHERE enabled = 1 ORDER BY created_at`, ruleSelectCols))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*model.NotificationRule
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

func (repo *NotificationRuleRepo) Get(ctx context.Context, id string) (*model.NotificationRule, error) {
	row := repo.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT %s FROM notification_rules WHERE id = ?`, ruleSelectCols), id)
	r, err := scanRule(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return r, err
}

func (repo *NotificationRuleRepo) Create(ctx context.Context, r *model.NotificationRule) error {
	now := time.Now().UTC()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = now
	}

	_, err := repo.db.ExecContext(ctx,
		`INSERT INTO notification_rules (id, plugin_id, event_type, project_id, enabled, template, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.PluginID, string(r.EventType),
		r.ProjectID, r.Enabled, r.Template,
		r.CreatedAt,
	)
	return err
}

func (repo *NotificationRuleRepo) Update(ctx context.Context, r *model.NotificationRule) error {
	_, err := repo.db.ExecContext(ctx,
		`UPDATE notification_rules SET event_type=?, project_id=?, enabled=?, template=? WHERE id=?`,
		string(r.EventType), r.ProjectID, r.Enabled, r.Template, r.ID,
	)
	return err
}

func (repo *NotificationRuleRepo) Delete(ctx context.Context, id string) error {
	_, err := repo.db.ExecContext(ctx, `DELETE FROM notification_rules WHERE id = ?`, id)
	return err
}
