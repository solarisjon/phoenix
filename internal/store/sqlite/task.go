package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/solarisjon/phoenix/internal/model"
)

type TaskRepo struct{ db *DB }

func NewTaskRepo(db *DB) *TaskRepo { return &TaskRepo{db} }

const taskSelectCols = ` id, project_id, agent_id, parent_task_id, follow_up_of, title, description,
	status, input, output, cost_usd, tokens_in, tokens_out, dismissed,
	runner_pid, timeout_at,
	source,
	created_at, started_at, completed_at `

func (r *TaskRepo) List(ctx context.Context, projectID string) ([]*model.Task, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT`+taskSelectCols+`FROM tasks WHERE project_id = ? ORDER BY created_at DESC`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()
	return scanTasks(rows)
}

func (r *TaskRepo) ListAll(ctx context.Context) ([]*model.Task, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT`+taskSelectCols+`FROM tasks WHERE dismissed = 0 ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list all tasks: %w", err)
	}
	defer rows.Close()
	return scanTasks(rows)
}

func (r *TaskRepo) ListByStatuses(ctx context.Context, statuses []model.TaskStatus) ([]*model.Task, error) {
	if len(statuses) == 0 {
		return nil, nil
	}
	placeholders := strings.Repeat("?,", len(statuses))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(statuses))
	for i, s := range statuses {
		args[i] = string(s)
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT`+taskSelectCols+`FROM tasks WHERE status IN (`+placeholders+`) AND dismissed = 0 ORDER BY created_at DESC`,
		args...)
	if err != nil {
		return nil, fmt.Errorf("list tasks by statuses: %w", err)
	}
	defer rows.Close()
	return scanTasks(rows)
}

func (r *TaskRepo) ListByAgent(ctx context.Context, agentID string) ([]*model.Task, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT`+taskSelectCols+`FROM tasks WHERE agent_id = ? ORDER BY created_at DESC`, agentID)
	if err != nil {
		return nil, fmt.Errorf("list tasks by agent: %w", err)
	}
	defer rows.Close()
	return scanTasks(rows)
}

func (r *TaskRepo) ListByStatus(ctx context.Context, status model.TaskStatus) ([]*model.Task, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT`+taskSelectCols+`FROM tasks WHERE status = ? AND dismissed = 0 ORDER BY created_at ASC`,
		string(status))
	if err != nil {
		return nil, fmt.Errorf("list tasks by status: %w", err)
	}
	defer rows.Close()
	return scanTasks(rows)
}

func (r *TaskRepo) Get(ctx context.Context, id string) (*model.Task, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT`+taskSelectCols+`FROM tasks WHERE id = ?`, id)
	return scanTask(row)
}

func (r *TaskRepo) Create(ctx context.Context, t *model.Task) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO tasks
		  (id, project_id, agent_id, parent_task_id, follow_up_of, title, description, status, input, output, cost_usd, tokens_in, tokens_out, source)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.ProjectID, t.AgentID, nullString(t.ParentTaskID), nullString(t.FollowUpOf),
		t.Title, t.Description, string(t.Status), t.Input, t.Output, t.CostUSD, t.TokensIn, t.TokensOut, t.Source)
	if err != nil {
		return fmt.Errorf("create task: %w", err)
	}
	return nil
}

func (r *TaskRepo) Update(ctx context.Context, t *model.Task) error {
	dismissed := 0
	if t.Dismissed {
		dismissed = 1
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE tasks SET
		  status = ?, output = ?, cost_usd = ?, tokens_in = ?, tokens_out = ?, dismissed = ?,
		  runner_pid = ?, timeout_at = ?,
		  started_at = ?, completed_at = ?
		WHERE id = ?`,
		string(t.Status), t.Output, t.CostUSD, t.TokensIn, t.TokensOut, dismissed,
		t.RunnerPID, t.TimeoutAt,
		t.StartedAt, t.CompletedAt, t.ID)
	if err != nil {
		return fmt.Errorf("update task: %w", err)
	}
	return nil
}

func (r *TaskRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM tasks WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete task: %w", err)
	}
	return nil
}

// ---- Scanning helpers ----

func scanTask(row *sql.Row) (*model.Task, error) {
	var t model.Task
	var status string
	var parentID, followUpOf sql.NullString
	var dismissed int
	var runnerPID sql.NullInt64
	var timeoutAt, startedAt, completedAt sql.NullTime

	err := row.Scan(
		&t.ID, &t.ProjectID, &t.AgentID, &parentID, &followUpOf,
		&t.Title, &t.Description, &status,
		&t.Input, &t.Output, &t.CostUSD, &t.TokensIn, &t.TokensOut, &dismissed,
		&runnerPID, &timeoutAt,
		&t.Source,
		&t.CreatedAt, &startedAt, &completedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan task: %w", err)
	}
	t.Status = model.TaskStatus(status)
	t.Dismissed = dismissed != 0
	if parentID.Valid {
		t.ParentTaskID = &parentID.String
	}
	if followUpOf.Valid {
		t.FollowUpOf = &followUpOf.String
	}
	if runnerPID.Valid {
		t.RunnerPID = int(runnerPID.Int64)
	}
	if timeoutAt.Valid {
		t.TimeoutAt = &timeoutAt.Time
	}
	if startedAt.Valid {
		t.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		t.CompletedAt = &completedAt.Time
	}
	return &t, nil
}

func scanTasks(rows *sql.Rows) ([]*model.Task, error) {
	var out []*model.Task
	for rows.Next() {
		var t model.Task
		var status string
		var parentID, followUpOf sql.NullString
		var dismissed int
		var runnerPID sql.NullInt64
		var timeoutAt, startedAt, completedAt sql.NullTime

		if err := rows.Scan(
			&t.ID, &t.ProjectID, &t.AgentID, &parentID, &followUpOf,
			&t.Title, &t.Description, &status,
			&t.Input, &t.Output, &t.CostUSD, &t.TokensIn, &t.TokensOut, &dismissed,
			&runnerPID, &timeoutAt,
			&t.Source,
			&t.CreatedAt, &startedAt, &completedAt,
		); err != nil {
			return nil, fmt.Errorf("scan task row: %w", err)
		}
		t.Status = model.TaskStatus(status)
		t.Dismissed = dismissed != 0
		if parentID.Valid {
			t.ParentTaskID = &parentID.String
		}
		if followUpOf.Valid {
			t.FollowUpOf = &followUpOf.String
		}
		if runnerPID.Valid {
			t.RunnerPID = int(runnerPID.Int64)
		}
		if timeoutAt.Valid {
			t.TimeoutAt = &timeoutAt.Time
		}
		if startedAt.Valid {
			t.StartedAt = &startedAt.Time
		}
		if completedAt.Valid {
			t.CompletedAt = &completedAt.Time
		}
		out = append(out, &t)
	}
	return out, rows.Err()
}
