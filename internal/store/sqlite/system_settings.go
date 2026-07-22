package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
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

	maxDepth := 2
	if v := kv["max_subtask_depth"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxDepth = n
		}
	}
	maxPerLevel := 5
	if v := kv["max_subtasks_per_level"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxPerLevel = n
		}
	}
	confidenceThreshold := 0.75
	if v := kv["orchestrator_confidence_threshold"]; v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			confidenceThreshold = f
		}
	}

	skillImportDirs := []string{}
	if raw := kv["skill_import_dirs"]; raw != "" {
		_ = json.Unmarshal([]byte(raw), &skillImportDirs)
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

		DynamicOrchestrationEnabled:     kv["dynamic_orchestration_enabled"] == "1",
		OrchestratorAgentID:             kv["orchestrator_agent_id"],
		MaxSubtaskDepth:                 maxDepth,
		MaxSubtasksPerLevel:             maxPerLevel,
		OrchestratorConfidenceThreshold: confidenceThreshold,
		SkillImportDirs:                 skillImportDirs,
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

	// Orchestration settings.
	orchEnabled := "0"
	if s.DynamicOrchestrationEnabled {
		orchEnabled = "1"
	}
	if _, err := r.db.ExecContext(ctx, upsert, "dynamic_orchestration_enabled", orchEnabled, now); err != nil {
		return err
	}
	if _, err := r.db.ExecContext(ctx, upsert, "orchestrator_agent_id", s.OrchestratorAgentID, now); err != nil {
		return err
	}
	maxDepth := s.MaxSubtaskDepth
	if maxDepth <= 0 {
		maxDepth = 2
	}
	if _, err := r.db.ExecContext(ctx, upsert, "max_subtask_depth", fmt.Sprintf("%d", maxDepth), now); err != nil {
		return err
	}
	maxPerLevel := s.MaxSubtasksPerLevel
	if maxPerLevel <= 0 {
		maxPerLevel = 5
	}
	if _, err := r.db.ExecContext(ctx, upsert, "max_subtasks_per_level", fmt.Sprintf("%d", maxPerLevel), now); err != nil {
		return err
	}
	threshold := s.OrchestratorConfidenceThreshold
	if threshold <= 0 {
		threshold = 0.75
	}
	if _, err := r.db.ExecContext(ctx, upsert, "orchestrator_confidence_threshold", fmt.Sprintf("%g", threshold), now); err != nil {
		return err
	}

	skillImportDirs, err := json.Marshal(s.SkillImportDirs)
	if err != nil {
		return fmt.Errorf("marshal skill_import_dirs: %w", err)
	}
	if _, err := r.db.ExecContext(ctx, upsert, "skill_import_dirs", string(skillImportDirs), now); err != nil {
		return err
	}

	return nil
}
