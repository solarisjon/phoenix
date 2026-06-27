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

	// Migration: treat an existing obsidian_root as implicitly enabled if the
	// obsidian_enabled key has never been written (empty = not yet set).
	obsidianEnabled := kv["obsidian_enabled"] == "1"
	if kv["obsidian_enabled"] == "" && kv["obsidian_root"] != "" {
		obsidianEnabled = true
	}

	s := &model.SystemSettings{
		GlobalGuardrailsEnabled: kv["global_guardrails_enabled"] == "1",
		GlobalGuardrails:        kv["global_guardrails"],
		CorePluginsEnabled:      kv["core_plugins_enabled"] == "1",
		CommunityPluginsEnabled: kv["community_plugins_enabled"] == "1",
		ObsidianEnabled:         obsidianEnabled,
		ObsidianRoot:            kv["obsidian_root"],
		ObsidianAutoWrite:       kv["obsidian_auto_write"] == "1",
		Theme:                   kv["theme"],
	}
	return s, nil
}

// GetRaw returns the raw string value for the given key, or "" if not set.
func (r *SystemSettingsRepo) GetRaw(ctx context.Context, key string) (string, error) {
	var v string
	err := r.db.QueryRowContext(ctx, `SELECT value FROM system_settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// SetRaw upserts an arbitrary key/value pair in system_settings.
func (r *SystemSettingsRepo) SetRaw(ctx context.Context, key, value string) error {
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO system_settings (key, value, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		key, value, now)
	return err
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

	obsidianEnabled := "0"
	if s.ObsidianEnabled {
		obsidianEnabled = "1"
	}
	if _, err := r.db.ExecContext(ctx, upsert, "obsidian_enabled", obsidianEnabled, now); err != nil {
		return err
	}

	if _, err := r.db.ExecContext(ctx, upsert, "obsidian_root", s.ObsidianRoot, now); err != nil {
		return err
	}

	obsidianAutoWrite := "0"
	if s.ObsidianAutoWrite {
		obsidianAutoWrite = "1"
	}
	if _, err := r.db.ExecContext(ctx, upsert, "obsidian_auto_write", obsidianAutoWrite, now); err != nil {
		return err
	}

	if _, err := r.db.ExecContext(ctx, upsert, "theme", s.Theme, now); err != nil {
		return err
	}

	return nil
}
