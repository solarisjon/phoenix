package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/solarisjon/phoenix/internal/model"
)

type ProjectRepo struct{ db *DB }

func NewProjectRepo(db *DB) *ProjectRepo { return &ProjectRepo{db} }

// ListByKind returns projects filtered by kind. An empty kind returns all.
func (r *ProjectRepo) ListByKind(ctx context.Context, kind string) ([]*model.Project, error) {
	var rows *sql.Rows
	var err error
	if kind == "" {
		rows, err = r.db.QueryContext(ctx,
			`SELECT id, name, description, working_dir, kind, schedule_interval, owner, status, created_at FROM projects ORDER BY created_at ASC`)
	} else {
		rows, err = r.db.QueryContext(ctx,
			`SELECT id, name, description, working_dir, kind, schedule_interval, owner, status, created_at FROM projects WHERE kind = ? ORDER BY created_at ASC`, kind)
	}
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()
	return scanProjects(rows)
}

func (r *ProjectRepo) List(ctx context.Context) ([]*model.Project, error) {
	return r.ListByKind(ctx, "")
}

func (r *ProjectRepo) Get(ctx context.Context, id string) (*model.Project, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, name, description, working_dir, kind, schedule_interval, owner, status, created_at FROM projects WHERE id = ?`, id)
	return scanProject(row)
}

func (r *ProjectRepo) Create(ctx context.Context, p *model.Project) error {
	kind := string(p.Kind)
	if kind == "" {
		kind = string(model.ProjectKindProject)
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO projects (id, name, description, working_dir, kind, schedule_interval, owner, status) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.Description, p.WorkingDir, kind, p.ScheduleInterval, p.Owner, string(p.Status))
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
	_, err := r.db.ExecContext(ctx,
		`UPDATE projects SET name = ?, description = ?, working_dir = ?, kind = ?, schedule_interval = ?, status = ? WHERE id = ?`,
		p.Name, p.Description, p.WorkingDir, kind, p.ScheduleInterval, string(p.Status), p.ID)
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

func (r *ProjectRepo) AssignAgent(ctx context.Context, projectID, agentID string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO project_agents (project_id, agent_id) VALUES (?, ?)`,
		projectID, agentID)
	if err != nil {
		return fmt.Errorf("assign agent: %w", err)
	}
	return nil
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
		SELECT a.id, a.name, a.persona, a.instructions, a.guardrails,
		       a.provider_id, a.model_override, a.can_spawn_agents, a.can_hire_agents, a.heartbeat_interval,
		       a.max_concurrent, a.created_by, a.status, a.created_at
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

func scanProject(row *sql.Row) (*model.Project, error) {
	var p model.Project
	var status, kind string
	err := row.Scan(&p.ID, &p.Name, &p.Description, &p.WorkingDir, &kind, &p.ScheduleInterval, &p.Owner, &status, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan project: %w", err)
	}
	p.Status = model.ProjectStatus(status)
	p.Kind = model.ProjectKind(kind)
	if p.Kind == "" {
		p.Kind = model.ProjectKindProject
	}
	return &p, nil
}

func scanProjects(rows *sql.Rows) ([]*model.Project, error) {
	var out []*model.Project
	for rows.Next() {
		var p model.Project
		var status, kind string
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.WorkingDir, &kind, &p.ScheduleInterval, &p.Owner, &status, &p.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan project row: %w", err)
		}
		p.Status = model.ProjectStatus(status)
		p.Kind = model.ProjectKind(kind)
		if p.Kind == "" {
			p.Kind = model.ProjectKindProject
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}
