package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/solarisjon/phoenix/internal/model"
)

type TaskRepo struct{ db *DB }

func NewTaskRepo(db *DB) *TaskRepo { return &TaskRepo{db} }

const taskSelectCols = ` id, project_id, agent_id, parent_task_id, follow_up_of, title, description,
	status, input, output, cost_usd, tokens_in, tokens_out, dismissed,
	runner_pid, timeout_at,
	source, health_signal, guardrail_reason, last_error,
	created_at, started_at, completed_at, is_critic_review, reviewed_task_id, critic_mode, prompt_hash `

func (r *TaskRepo) List(ctx context.Context, projectID string) ([]*model.Task, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT`+taskSelectCols+`FROM tasks WHERE project_id = ? AND dismissed = 0 ORDER BY created_at DESC`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()
	return scanTasks(rows)
}

func (r *TaskRepo) ListByProject(ctx context.Context, projectID string, status model.TaskStatus, limit int) ([]*model.Task, error) {
	query := `SELECT` + taskSelectCols + `FROM tasks WHERE project_id = ? AND dismissed = 0`
	args := []any{projectID}
	if status != "" {
		query += ` AND status = ?`
		args = append(args, string(status))
	}
	query += ` ORDER BY created_at DESC`
	if limit > 0 {
		query += fmt.Sprintf(` LIMIT %d`, limit)
	}
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list tasks by project: %w", err)
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

func (r *TaskRepo) Search(ctx context.Context, query string) ([]*model.Task, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT`+taskSelectCols+`FROM tasks
         WHERE rowid IN (SELECT rowid FROM tasks_fts WHERE tasks_fts MATCH ?)
         ORDER BY created_at DESC
         LIMIT 100`, query)
	if err != nil {
		return nil, fmt.Errorf("search tasks: %w", err)
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
	isCriticReview := 0
	if t.IsCriticReview {
		isCriticReview = 1
	}
	criticMode := t.CriticMode
	if criticMode == "" {
		criticMode = model.CriticModeInherit
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO tasks
		  (id, project_id, agent_id, parent_task_id, follow_up_of, title, description, status, input, output, cost_usd, tokens_in, tokens_out, source, is_critic_review, reviewed_task_id, critic_mode, prompt_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.ProjectID, t.AgentID, nullString(t.ParentTaskID), nullString(t.FollowUpOf),
		t.Title, t.Description, string(t.Status), t.Input, t.Output, t.CostUSD, t.TokensIn, t.TokensOut, t.Source, isCriticReview, nullString(t.ReviewedTaskID), criticMode, t.PromptHash)
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
	isCriticReview := 0
	if t.IsCriticReview {
		isCriticReview = 1
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE tasks SET
		  status = ?, output = ?, cost_usd = ?, tokens_in = ?, tokens_out = ?, dismissed = ?,
		  runner_pid = ?, timeout_at = ?,
		  started_at = ?, completed_at = ?,
		  health_signal = ?, guardrail_reason = ?, last_error = ?,
		  is_critic_review = ?, reviewed_task_id = ?, prompt_hash = ?
		WHERE id = ?`,
		string(t.Status), t.Output, t.CostUSD, t.TokensIn, t.TokensOut, dismissed,
		t.RunnerPID, t.TimeoutAt,
		t.StartedAt, t.CompletedAt,
		t.HealthSignal, t.GuardrailReason, t.LastError, isCriticReview, nullString(t.ReviewedTaskID), t.PromptHash, t.ID)
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

func (r *TaskRepo) ListCompletedForInbox(ctx context.Context, limit int) ([]*model.Task, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT`+taskSelectCols+`FROM tasks WHERE status = 'completed' AND dismissed = 0 ORDER BY completed_at DESC LIMIT ?`,
		limit)
	if err != nil {
		return nil, fmt.Errorf("list completed for inbox: %w", err)
	}
	defer rows.Close()
	return scanTasks(rows)
}

func (r *TaskRepo) FindByPromptHash(ctx context.Context, projectID, hash string) (*model.Task, error) {
	if hash == "" {
		return nil, nil
	}
	row := r.db.QueryRowContext(ctx,
		`SELECT`+taskSelectCols+`FROM tasks
		 WHERE project_id = ? AND prompt_hash = ? AND status = 'completed'
		 ORDER BY completed_at DESC LIMIT 1`,
		projectID, hash)
	return scanTask(row)
}

// LastMonitorRunAt returns the creation time of the most recent monitor-sourced
// task for the project, regardless of dismissed status. Dismissed tasks must
// count so that a user clearing the inbox doesn't cause the monitor to re-fire.
func (r *TaskRepo) LastMonitorRunAt(ctx context.Context, projectID string) (*time.Time, error) {
	var ts time.Time
	err := r.db.QueryRowContext(ctx,
		`SELECT created_at FROM tasks
		 WHERE project_id = ? AND source = 'monitor'
		 ORDER BY created_at DESC LIMIT 1`,
		projectID).Scan(&ts)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("last monitor run at: %w", err)
	}
	return &ts, nil
}

func (r *TaskRepo) ProjectSpendForPeriod(ctx context.Context, projectID, period string) (float64, error) {
	var query string
	switch period {
	case "day":
		query = `SELECT COALESCE(SUM(cost_usd), 0) FROM tasks
		         WHERE project_id = ? AND date(created_at) = date('now')`
	case "week":
		query = `SELECT COALESCE(SUM(cost_usd), 0) FROM tasks
		         WHERE project_id = ? AND created_at >= date('now', '-6 days')`
	case "month":
		query = `SELECT COALESCE(SUM(cost_usd), 0) FROM tasks
		         WHERE project_id = ? AND strftime('%Y-%m', created_at) = strftime('%Y-%m', 'now')`
	default: // "total"
		query = `SELECT COALESCE(SUM(cost_usd), 0) FROM tasks WHERE project_id = ?`
	}
	var cost float64
	if err := r.db.QueryRowContext(ctx, query, projectID).Scan(&cost); err != nil {
		return 0, fmt.Errorf("project spend for period: %w", err)
	}
	return cost, nil
}

func (r *TaskRepo) NextQueuedTask(ctx context.Context, agentID string) (*model.Task, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT`+taskSelectCols+`FROM tasks WHERE agent_id = ? AND status = 'queued' ORDER BY created_at ASC LIMIT 1`,
		agentID)
	return scanTask(row)
}

func (r *TaskRepo) CancelQueuedTask(ctx context.Context, taskID string) (bool, error) {
	now := time.Now()
	errJSON := `{"error":"task cancelled by user"}`
	res, err := r.db.ExecContext(ctx,
		`UPDATE tasks SET status='failed', output=?, completed_at=? WHERE id=? AND status='queued'`,
		errJSON, now, taskID)
	if err != nil {
		return false, fmt.Errorf("cancel queued task: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("cancel queued task rows: %w", err)
	}
	return n > 0, nil
}

// ForceFailTask unconditionally marks a task as failed if it is not already terminal.
// It also clears runner_pid so orphaned subprocess records are not left around.
func (r *TaskRepo) ForceFailTask(ctx context.Context, taskID string) (bool, error) {
	now := time.Now()
	errJSON := `{"error":"task force-cancelled by user"}`
	res, err := r.db.ExecContext(ctx,
		`UPDATE tasks SET status='failed', output=?, completed_at=?, runner_pid=0
		 WHERE id=? AND status NOT IN ('completed','failed')`,
		errJSON, now, taskID)
	if err != nil {
		return false, fmt.Errorf("force fail task: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("force fail task rows: %w", err)
	}
	return n > 0, nil
}

// ListTimedOut returns tasks that are still active (running or queued) but whose
// timeout_at has already passed. Used by the watchdog goroutine to reap tasks
// whose runner goroutine exited without updating the DB.
func (r *TaskRepo) ListTimedOut(ctx context.Context) ([]*model.Task, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT`+taskSelectCols+`FROM tasks
		 WHERE status IN ('running','queued')
		   AND timeout_at IS NOT NULL
		   AND timeout_at < datetime('now')`)
	if err != nil {
		return nil, fmt.Errorf("list timed out tasks: %w", err)
	}
	defer rows.Close()
	return scanTasks(rows)
}

// ListProjectHistory returns all completed tasks for a project regardless of dismissed state.
// Used by the project view to show full history including inbox-dismissed tasks.
func (r *TaskRepo) ListProjectHistory(ctx context.Context, projectID string) ([]*model.Task, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT`+taskSelectCols+`FROM tasks WHERE project_id = ? AND status = 'completed'
		 ORDER BY created_at DESC`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list project history: %w", err)
	}
	defer rows.Close()
	return scanTasks(rows)
}

func scanTask(row *sql.Row) (*model.Task, error) {
	var t model.Task
	var status string
	var parentID, followUpOf, healthSignal, guardrailReason, lastError, reviewedTaskID sql.NullString
	var dismissed int
	var isCriticReview int
	var runnerPID sql.NullInt64
	var timeoutAt, startedAt, completedAt sql.NullTime

	err := row.Scan(
		&t.ID, &t.ProjectID, &t.AgentID, &parentID, &followUpOf,
		&t.Title, &t.Description, &status,
		&t.Input, &t.Output, &t.CostUSD, &t.TokensIn, &t.TokensOut, &dismissed,
		&runnerPID, &timeoutAt,
		&t.Source, &healthSignal, &guardrailReason, &lastError,
		&t.CreatedAt, &startedAt, &completedAt, &isCriticReview, &reviewedTaskID, &t.CriticMode, &t.PromptHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan task: %w", err)
	}
	t.Status = model.TaskStatus(status)
	t.Dismissed = dismissed != 0
	t.IsCriticReview = isCriticReview != 0
	if parentID.Valid {
		t.ParentTaskID = &parentID.String
	}
	if followUpOf.Valid {
		t.FollowUpOf = &followUpOf.String
	}
	if healthSignal.Valid {
		t.HealthSignal = &healthSignal.String
	}
	if guardrailReason.Valid {
		t.GuardrailReason = &guardrailReason.String
	}
	if lastError.Valid {
		t.LastError = lastError.String
	}
	if reviewedTaskID.Valid {
		t.ReviewedTaskID = &reviewedTaskID.String
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
	if t.CriticMode == "" {
		t.CriticMode = model.CriticModeInherit
	}
	return &t, nil
}

func scanTasks(rows *sql.Rows) ([]*model.Task, error) {
	var out []*model.Task
	for rows.Next() {
		var t model.Task
		var status string
		var parentID, followUpOf, healthSignal, guardrailReason, lastError, reviewedTaskID sql.NullString
		var dismissed int
		var isCriticReview int
		var runnerPID sql.NullInt64
		var timeoutAt, startedAt, completedAt sql.NullTime

		if err := rows.Scan(
			&t.ID, &t.ProjectID, &t.AgentID, &parentID, &followUpOf,
			&t.Title, &t.Description, &status,
			&t.Input, &t.Output, &t.CostUSD, &t.TokensIn, &t.TokensOut, &dismissed,
			&runnerPID, &timeoutAt,
			&t.Source, &healthSignal, &guardrailReason, &lastError,
			&t.CreatedAt, &startedAt, &completedAt, &isCriticReview, &reviewedTaskID, &t.CriticMode, &t.PromptHash,
		); err != nil {
			return nil, fmt.Errorf("scan task row: %w", err)
		}
		t.Status = model.TaskStatus(status)
		t.Dismissed = dismissed != 0
		t.IsCriticReview = isCriticReview != 0
		if parentID.Valid {
			t.ParentTaskID = &parentID.String
		}
		if followUpOf.Valid {
			t.FollowUpOf = &followUpOf.String
		}
		if healthSignal.Valid {
			t.HealthSignal = &healthSignal.String
		}
		if guardrailReason.Valid {
			t.GuardrailReason = &guardrailReason.String
		}
		if lastError.Valid {
			t.LastError = lastError.String
		}
		if reviewedTaskID.Valid {
			t.ReviewedTaskID = &reviewedTaskID.String
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
		if t.CriticMode == "" {
			t.CriticMode = model.CriticModeInherit
		}
		out = append(out, &t)
	}
	return out, rows.Err()
}
