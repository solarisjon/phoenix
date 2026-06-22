package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/solarisjon/phoenix/internal/model"
)

// SystemSettingsRepo persists platform-wide key/value settings.
type SystemSettingsRepo struct{ db *DB }

func NewSystemSettingsRepo(db *DB) *SystemSettingsRepo {
	return &SystemSettingsRepo{db: db}
}

// Get returns the current system settings, always succeeding (returns defaults
// if the rows have not been seeded yet).
func (r *SystemSettingsRepo) Get(ctx context.Context) (*model.SystemSettings, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT key, value FROM system_settings`)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &model.SystemSettings{}, nil
		}
		return nil, err
	}
	defer rows.Close()

	kv := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		kv[k] = v
	}

	s := &model.SystemSettings{
		GlobalGuardrailsEnabled: kv["global_guardrails_enabled"] == "1",
		GlobalGuardrails:        kv["global_guardrails"],
		CorePluginsEnabled:      kv["core_plugins_enabled"] == "1",
		CommunityPluginsEnabled: kv["community_plugins_enabled"] == "1",
	}
	return s, nil
}

// Save upserts all settings fields.
func (r *SystemSettingsRepo) Save(ctx context.Context, s *model.SystemSettings) error {
	enabled := "0"
	if s.GlobalGuardrailsEnabled {
		enabled = "1"
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05")

	upsert := `INSERT INTO system_settings (key, value, updated_at) VALUES (?, ?, ?)
               ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`

	if _, err := r.db.ExecContext(ctx, upsert, "global_guardrails_enabled", enabled, now); err != nil {
		return err
	}
	if _, err := r.db.ExecContext(ctx, upsert, "global_guardrails", s.GlobalGuardrails, now); err != nil {
		return err
	}

	coreEnabled := "0"
	if s.CorePluginsEnabled {
		coreEnabled = "1"
	}
	if _, err := r.db.ExecContext(ctx, upsert, "core_plugins_enabled", coreEnabled, now); err != nil {
		return err
	}

	communityEnabled := "0"
	if s.CommunityPluginsEnabled {
		communityEnabled = "1"
	}
	if _, err := r.db.ExecContext(ctx, upsert, "community_plugins_enabled", communityEnabled, now); err != nil {
		return err
	}

	return nil
}
