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

const projectSelectCols = `id, name, description, objective, working_dir, kind, schedule_interval,
	schedule_kind, schedule_times, schedule_catch_up,
	owner, status, critic_agent_id, critic_mode, monitor_model, budget_usd, budget_period, tags, created_at`

// ListByKind returns projects filtered by kind, active only.
func (r *ProjectRepo) ListByKind(ctx context.Context, kind string) ([]*model.Project, error) {
	return r.ListByStatus(ctx, kind, string(model.ProjectStatusActive))
}

// ListByStatus returns projects filtered by kind and status. Empty strings match all values.
func (r *ProjectRepo) ListByStatus(ctx context.Context, kind, status string) ([]*model.Project, error) {
	var rows *sql.Rows
	var err error
	switch {
	case kind == "" && status == "":
		rows, err = r.db.QueryContext(ctx,
			`SELECT `+projectSelectCols+` FROM projects ORDER BY created_at ASC`)
	case kind == "":
		rows, err = r.db.QueryContext(ctx,
			`SELECT `+projectSelectCols+` FROM projects WHERE status = ? ORDER BY created_at ASC`, status)
	case status == "":
		rows, err = r.db.QueryContext(ctx,
			`SELECT `+projectSelectCols+` FROM projects WHERE kind = ? ORDER BY created_at ASC`, kind)
	default:
		rows, err = r.db.QueryContext(ctx,
			`SELECT `+projectSelectCols+` FROM projects WHERE kind = ? AND status = ? ORDER BY created_at ASC`, kind, status)
	}
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()
	return scanProjects(rows)
}

// List returns all projects regardless of status (scheduler, stats, etc.).
func (r *ProjectRepo) List(ctx context.Context) ([]*model.Project, error) {
	return r.ListByStatus(ctx, "", "")
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
		`INSERT INTO projects (id, name, description, objective, working_dir, kind, schedule_interval,
		 schedule_kind, schedule_times, schedule_catch_up,
		 owner, status, critic_agent_id, critic_mode, monitor_model, budget_usd, budget_period, tags)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.Description, p.Objective, p.WorkingDir, kind, p.ScheduleInterval,
		scheduleKind, timesJSON, boolToInt(p.ScheduleCatchUp),
		p.Owner, string(p.Status), nullString(p.CriticAgentID), p.CriticMode, p.MonitorModel,
		p.BudgetUSD, resolveBudgetPeriod(p.BudgetPeriod), tagsJSON)
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
		`UPDATE projects SET name = ?, description = ?, objective = ?, working_dir = ?, kind = ?,
		 schedule_interval = ?, schedule_kind = ?, schedule_times = ?, schedule_catch_up = ?,
		 status = ?, critic_agent_id = ?, critic_mode = ?, monitor_model = ?,
		 budget_usd = ?, budget_period = ?, tags = ? WHERE id = ?`,
		p.Name, p.Description, p.Objective, p.WorkingDir, kind,
		p.ScheduleInterval, scheduleKind, timesJSON, boolToInt(p.ScheduleCatchUp),
		string(p.Status), nullString(p.CriticAgentID), p.CriticMode, p.MonitorModel,
		p.BudgetUSD, resolveBudgetPeriod(p.BudgetPeriod), tagsJSON, p.ID)
	if err != nil {
		return fmt.Errorf("update project: %w", err)
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
		       a.max_concurrent, a.max_cost_per_run, a.fallback_model, a.created_by, a.status, a.created_at, a.template_id
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

// ---- scan helpers ----

func scanProject(row *sql.Row) (*model.Project, error) {
	var p model.Project
	var status, kind, tagsJSON string
	var scheduleKind, timesJSON string
	var catchUp int
	var criticAgentID sql.NullString
	err := row.Scan(
		&p.ID, &p.Name, &p.Description, &p.Objective, &p.WorkingDir, &kind, &p.ScheduleInterval,
		&scheduleKind, &timesJSON, &catchUp,
		&p.Owner, &status, &criticAgentID, &p.CriticMode, &p.MonitorModel,
		&p.BudgetUSD, &p.BudgetPeriod, &tagsJSON, &p.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan project: %w", err)
	}
	p.Status = model.ProjectStatus(status)
	p.Kind = model.ProjectKind(kind)
	if criticAgentID.Valid {
		p.CriticAgentID = &criticAgentID.String
	}
	if p.Kind == "" {
		p.Kind = model.ProjectKindProject
	}
	if p.CriticMode == "" {
		p.CriticMode = model.CriticModeNone
	}
	p.ScheduleKind = scheduleKind
	if p.ScheduleKind == "" {
		p.ScheduleKind = model.ScheduleKindInterval
	}
	p.ScheduleTimes = unmarshalStringSlice(timesJSON)
	p.ScheduleCatchUp = catchUp != 0
	p.Tags = unmarshalTags(tagsJSON)
	return &p, nil
}

func scanProjects(rows *sql.Rows) ([]*model.Project, error) {
	var out []*model.Project
	for rows.Next() {
		var p model.Project
		var status, kind, tagsJSON string
		var scheduleKind, timesJSON string
		var catchUp int
		var criticAgentID sql.NullString
		if err := rows.Scan(
			&p.ID, &p.Name, &p.Description, &p.Objective, &p.WorkingDir, &kind, &p.ScheduleInterval,
			&scheduleKind, &timesJSON, &catchUp,
			&p.Owner, &status, &criticAgentID, &p.CriticMode, &p.MonitorModel,
			&p.BudgetUSD, &p.BudgetPeriod, &tagsJSON, &p.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan project row: %w", err)
		}
		p.Status = model.ProjectStatus(status)
		p.Kind = model.ProjectKind(kind)
		if criticAgentID.Valid {
			p.CriticAgentID = &criticAgentID.String
		}
		if p.Kind == "" {
			p.Kind = model.ProjectKindProject
		}
		if p.CriticMode == "" {
			p.CriticMode = model.CriticModeNone
		}
		p.ScheduleKind = scheduleKind
		if p.ScheduleKind == "" {
			p.ScheduleKind = model.ScheduleKindInterval
		}
		p.ScheduleTimes = unmarshalStringSlice(timesJSON)
		p.ScheduleCatchUp = catchUp != 0
		p.Tags = unmarshalTags(tagsJSON)
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
