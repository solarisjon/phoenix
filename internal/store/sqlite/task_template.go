package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/solarisjon/phoenix/internal/model"
)

type TaskTemplateRepo struct{ db *DB }

func NewTaskTemplateRepo(db *DB) *TaskTemplateRepo { return &TaskTemplateRepo{db} }

func (r *TaskTemplateRepo) List(ctx context.Context, projectID string) ([]*model.TaskTemplate, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if projectID == "" {
		rows, err = r.db.QueryContext(ctx,
			`SELECT id, name, description, title, body, project_id, agent_id, created_at
			 FROM task_templates ORDER BY created_at ASC`)
	} else {
		rows, err = r.db.QueryContext(ctx,
			`SELECT id, name, description, title, body, project_id, agent_id, created_at
			 FROM task_templates
			 WHERE project_id IS NULL OR project_id = ?
			 ORDER BY created_at ASC`, projectID)
	}
	if err != nil {
		return nil, fmt.Errorf("list task templates: %w", err)
	}
	defer rows.Close()

	var out []*model.TaskTemplate
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *TaskTemplateRepo) Get(ctx context.Context, id string) (*model.TaskTemplate, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, name, description, title, body, project_id, agent_id, created_at
		 FROM task_templates WHERE id = ?`, id)
	var t model.TaskTemplate
	var projectID, agentID sql.NullString
	err := row.Scan(&t.ID, &t.Name, &t.Description, &t.Title, &t.Body,
		&projectID, &agentID, &t.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get task template: %w", err)
	}
	if projectID.Valid {
		t.ProjectID = &projectID.String
	}
	if agentID.Valid {
		t.AgentID = &agentID.String
	}
	return &t, nil
}

func (r *TaskTemplateRepo) Create(ctx context.Context, t *model.TaskTemplate) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO task_templates (id, name, description, title, body, project_id, agent_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.Name, t.Description, t.Title, t.Body, t.ProjectID, t.AgentID)
	if err != nil {
		return fmt.Errorf("create task template: %w", err)
	}
	return nil
}

func (r *TaskTemplateRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM task_templates WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete task template: %w", err)
	}
	return nil
}

func scanTemplate(rows *sql.Rows) (*model.TaskTemplate, error) {
	var t model.TaskTemplate
	var projectID, agentID sql.NullString
	if err := rows.Scan(&t.ID, &t.Name, &t.Description, &t.Title, &t.Body,
		&projectID, &agentID, &t.CreatedAt); err != nil {
		return nil, fmt.Errorf("scan task template: %w", err)
	}
	if projectID.Valid {
		t.ProjectID = &projectID.String
	}
	if agentID.Valid {
		t.AgentID = &agentID.String
	}
	return &t, nil
}
