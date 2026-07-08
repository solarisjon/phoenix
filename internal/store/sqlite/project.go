package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/solarisjon/phoenix/internal/model"
)

type ProjectRepo struct{ db *DB }

func NewProjectRepo(db *DB) *ProjectRepo { return &ProjectRepo{db} }

const projectSelectCols = `id, name, objective, working_dir, kind, schedule_interval,
	schedule_kind, schedule_times, schedule_catch_up,
	owner, status, critic_agent_id, critic_mode, monitor_model, budget_usd, budget_period, context_summarisation, tags,
	heartbeat_on_attention, heartbeat_on_failed, linked_project_id, heartbeat_consecutive_bad, heartbeat_last_signal, heartbeat_escalate_after,
	monitor_cache_ttl,
	react_mode, max_iterations,
	created_at`

// ListByKind returns projects filtered by kind and userID, active only.
func (r *ProjectRepo) ListByKind(ctx context.Context, kind, userID string) ([]*model.Project, error) {
	return r.ListByStatus(ctx, kind, string(model.ProjectStatusActive), userID)
}

// ListByStatus returns projects filtered by kind, status, and userID.
// Empty kind/status matches all values. Empty userID returns all users' projects.
func (r *ProjectRepo) ListByStatus(ctx context.Context, kind, status, userID string) ([]*model.Project, error) {
	// Build WHERE clause dynamically.
	where := ""
	args := []any{}
	addCond := func(cond string, vals ...any) {
		if where == "" {
			where = "WHERE " + cond
		} else {
			where += " AND " + cond
		}
		args = append(args, vals...)
	}
	if kind != "" {
		addCond("kind = ?", kind)
	}
	if status != "" {
		addCond("status = ?", status)
	}
	if userID != "" {
		addCond("owner = ?", userID)
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+projectSelectCols+` FROM projects `+where+` ORDER BY created_at ASC`,
		args...)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()
	return scanProjects(rows)
}

// List returns all projects (scheduler, stats, etc.). Pass userID="" to get all users' projects.
func (r *ProjectRepo) List(ctx context.Context, userID string) ([]*model.Project, error) {
	return r.ListByStatus(ctx, "", "", userID)
}

func (r *ProjectRepo) Get(ctx context.Context, id string) (*model.Project, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+projectSelectCols+` FROM projects WHERE id = ?`, id)
	return scanProject(row)
}

func (r *ProjectRepo) Create(ctx context.Context, p *model.Project) error {
	kind := string(p.Kind)
	if kind == "" {
		kind = string(model.ProjectKindProject)
	}
	scheduleKind := p.ScheduleKind
	if scheduleKind == "" {
		scheduleKind = model.ScheduleKindInterval
	}
	tagsJSON := marshalTags(p.Tags)
	timesJSON := marshalStringSlice(p.ScheduleTimes)
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO projects (id, name, objective, working_dir, kind, schedule_interval,
		 schedule_kind, schedule_times, schedule_catch_up,
		 owner, status, critic_agent_id, critic_mode, monitor_model, budget_usd, budget_period, context_summarisation, tags,
		 heartbeat_on_attention, heartbeat_on_failed, linked_project_id, heartbeat_consecutive_bad, heartbeat_last_signal, heartbeat_escalate_after,
		 monitor_cache_ttl,
		 react_mode, max_iterations)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.Objective, p.WorkingDir, kind, p.ScheduleInterval,
		scheduleKind, timesJSON, boolToInt(p.ScheduleCatchUp),
		p.Owner, string(p.Status), nullString(p.CriticAgentID), p.CriticMode, p.MonitorModel,
		p.BudgetUSD, resolveBudgetPeriod(p.BudgetPeriod), boolToInt(p.ContextSummarisation), tagsJSON,
		p.HeartbeatOnAttention, p.HeartbeatOnFailed, nullString(p.LinkedProjectID),
		p.HeartbeatConsecutiveBad, p.HeartbeatLastSignal, p.HeartbeatEscalateAfter,
		p.MonitorCacheTTL,
		boolToInt(p.ReactMode), p.MaxIterations)
	if err != nil {
		return fmt.Errorf("create project: %w", err)
	}
	return nil
}

func (r *ProjectRepo) Update(ctx context.Context, p *model.Project) error {
	kind := string(p.Kind)
	if kind == "" {
		kind = string(model.ProjectKindProject)
	}
	scheduleKind := p.ScheduleKind
	if scheduleKind == "" {
		scheduleKind = model.ScheduleKindInterval
	}
	tagsJSON := marshalTags(p.Tags)
	timesJSON := marshalStringSlice(p.ScheduleTimes)
	_, err := r.db.ExecContext(ctx,
		`UPDATE projects SET name = ?, objective = ?, working_dir = ?, kind = ?,
		 schedule_interval = ?, schedule_kind = ?, schedule_times = ?, schedule_catch_up = ?,
		 status = ?, critic_agent_id = ?, critic_mode = ?, monitor_model = ?,
		 budget_usd = ?, budget_period = ?, context_summarisation = ?, tags = ?,
		 heartbeat_on_attention = ?, heartbeat_on_failed = ?, linked_project_id = ?, heartbeat_escalate_after = ?,
		 monitor_cache_ttl = ?,
		 react_mode = ?, max_iterations = ?
		 WHERE id = ?`,
		p.Name, p.Objective, p.WorkingDir, kind,
		p.ScheduleInterval, scheduleKind, timesJSON, boolToInt(p.ScheduleCatchUp),
		string(p.Status), nullString(p.CriticAgentID), p.CriticMode, p.MonitorModel,
		p.BudgetUSD, resolveBudgetPeriod(p.BudgetPeriod), boolToInt(p.ContextSummarisation), tagsJSON,
		p.HeartbeatOnAttention, p.HeartbeatOnFailed, nullString(p.LinkedProjectID), p.HeartbeatEscalateAfter,
		p.MonitorCacheTTL,
		boolToInt(p.ReactMode), p.MaxIterations,
		p.ID)
	if err != nil {
		return fmt.Errorf("update project: %w", err)
	}
	return nil
}

// UpdateHeartbeatSignal records the latest health signal and consecutive-bad counter
// without touching any other project fields, avoiding overwrite races during long tasks.
func (r *ProjectRepo) UpdateHeartbeatSignal(ctx context.Context, projectID, signal string, consecutiveBad int) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE projects SET heartbeat_last_signal = ?, heartbeat_consecutive_bad = ? WHERE id = ?`,
		signal, consecutiveBad, projectID)
	if err != nil {
		return fmt.Errorf("update heartbeat signal: %w", err)
	}
	return nil
}

func (r *ProjectRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	return nil
}

// DeleteWithTasks hard-deletes a project and all associated tasks in one transaction.
func (r *ProjectRepo) DeleteWithTasks(ctx context.Context, id string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, `DELETE FROM tasks WHERE project_id = ?`, id); err != nil {
		return fmt.Errorf("delete project tasks: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM project_agents WHERE project_id = ?`, id); err != nil {
		return fmt.Errorf("delete project agents: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	return tx.Commit()
}

func (r *ProjectRepo) IsAgentAssigned(ctx context.Context, projectID, agentID string) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM project_agents WHERE project_id = ? AND agent_id = ?`,
		projectID, agentID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("is agent assigned: %w", err)
	}
	return count > 0, nil
}

func (r *ProjectRepo) AssignAgent(ctx context.Context, projectID, agentID string) (bool, error) {
	result, err := r.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO project_agents (project_id, agent_id) VALUES (?, ?)`,
		projectID, agentID)
	if err != nil {
		return false, fmt.Errorf("assign agent: %w", err)
	}
	n, _ := result.RowsAffected()
	return n > 0, nil
}

func (r *ProjectRepo) RemoveAgent(ctx context.Context, projectID, agentID string) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM project_agents WHERE project_id = ? AND agent_id = ?`,
		projectID, agentID)
	if err != nil {
		return fmt.Errorf("remove agent: %w", err)
	}
	return nil
}

func (r *ProjectRepo) ListAgents(ctx context.Context, projectID string) ([]*model.Agent, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT a.id, a.name, a.persona, a.instructions, a.guardrails, a.behaviour, a.hard_guardrails,
		       a.provider_id, a.model_override, a.can_spawn_agents, a.can_hire_agents,
		       a.max_concurrent, a.max_cost_per_run, a.fallback_model, a.is_orchestrator,
		       a.created_by, a.status, a.created_at, a.template_id
		FROM agents a
		JOIN project_agents pa ON pa.agent_id = a.id
		WHERE pa.project_id = ?
		ORDER BY a.created_at ASC`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list project agents: %w", err)
	}
	defer rows.Close()
	return scanAgents(rows)
}

func (r *ProjectRepo) Search(ctx context.Context, query, userID string) ([]*model.Project, error) {
	var rows *sql.Rows
	var err error
	if userID == "" {
		rows, err = r.db.QueryContext(ctx,
			`SELECT `+projectSelectCols+` FROM projects
             WHERE rowid IN (SELECT rowid FROM projects_fts WHERE projects_fts MATCH ?)
             AND status != 'archived'
             ORDER BY created_at DESC LIMIT 50`, query)
	} else {
		rows, err = r.db.QueryContext(ctx,
			`SELECT `+projectSelectCols+` FROM projects
             WHERE owner = ? AND rowid IN (SELECT rowid FROM projects_fts WHERE projects_fts MATCH ?)
             AND status != 'archived'
             ORDER BY created_at DESC LIMIT 50`, userID, query)
	}
	if err != nil {
		return nil, fmt.Errorf("search projects: %w", err)
	}
	defer rows.Close()
	return scanProjects(rows)
}

// ---- scan helpers ----

func scanProjectRow(dest *model.Project, scanFn func(...any) error) error {
	var status, kind, tagsJSON string
	var scheduleKind, timesJSON string
	var catchUp, ctxSumm, reactMode int
	var criticAgentID, linkedProjectID sql.NullString
	err := scanFn(
		&dest.ID, &dest.Name, &dest.Objective, &dest.WorkingDir, &kind, &dest.ScheduleInterval,
		&scheduleKind, &timesJSON, &catchUp,
		&dest.Owner, &status, &criticAgentID, &dest.CriticMode, &dest.MonitorModel,
		&dest.BudgetUSD, &dest.BudgetPeriod, &ctxSumm, &tagsJSON,
		&dest.HeartbeatOnAttention, &dest.HeartbeatOnFailed, &linkedProjectID,
		&dest.HeartbeatConsecutiveBad, &dest.HeartbeatLastSignal, &dest.HeartbeatEscalateAfter,
		&dest.MonitorCacheTTL,
		&reactMode, &dest.MaxIterations,
		&dest.CreatedAt,
	)
	if err != nil {
		return err
	}
	dest.Status = model.ProjectStatus(status)
	dest.Kind = model.ProjectKind(kind)
	if criticAgentID.Valid {
		dest.CriticAgentID = &criticAgentID.String
	}
	if linkedProjectID.Valid {
		dest.LinkedProjectID = &linkedProjectID.String
	}
	if dest.Kind == "" {
		dest.Kind = model.ProjectKindProject
	}
	if dest.CriticMode == "" {
		dest.CriticMode = model.CriticModeNone
	}
	dest.ScheduleKind = scheduleKind
	if dest.ScheduleKind == "" {
		dest.ScheduleKind = model.ScheduleKindInterval
	}
	dest.ScheduleTimes = unmarshalStringSlice(timesJSON)
	dest.ScheduleCatchUp = catchUp != 0
	dest.ContextSummarisation = ctxSumm != 0
	dest.ReactMode = reactMode != 0
	dest.Tags = unmarshalTags(tagsJSON)
	return nil
}

func scanProject(row *sql.Row) (*model.Project, error) {
	var p model.Project
	err := scanProjectRow(&p, func(dest ...any) error { return row.Scan(dest...) })
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan project: %w", err)
	}
	return &p, nil
}

func scanProjects(rows *sql.Rows) ([]*model.Project, error) {
	var out []*model.Project
	for rows.Next() {
		var p model.Project
		if err := scanProjectRow(&p, rows.Scan); err != nil {
			return nil, fmt.Errorf("scan project row: %w", err)
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

func resolveBudgetPeriod(p string) string {
	switch p {
	case "day", "week", "month":
		return p
	default:
		return "total"
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// marshalTags encodes a tag slice to a JSON array string. Never errors — returns '[]' on nil.
func marshalTags(tags []string) string {
	return marshalStringSlice(tags)
}

// unmarshalTags decodes a JSON array string to a tag slice. Returns nil on empty / invalid input.
func unmarshalTags(raw string) []string {
	return unmarshalStringSlice(raw)
}

// marshalStringSlice encodes a string slice to a JSON array string. Never errors — returns '[]' on nil.
func marshalStringSlice(vals []string) string {
	if len(vals) == 0 {
		return "[]"
	}
	b, err := json.Marshal(vals)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// unmarshalStringSlice decodes a JSON array string to a slice. Returns nil on empty / invalid input.
func unmarshalStringSlice(raw string) []string {
	if raw == "" || raw == "[]" || raw == "null" {
		return nil
	}
	var vals []string
	if err := json.Unmarshal([]byte(raw), &vals); err != nil {
		return nil
	}
	return vals
}
