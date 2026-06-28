package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
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
	created_at, started_at, completed_at, is_critic_review, reviewed_task_id, critic_mode, prompt_hash, summary_cache, priority, depends_on `

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

func (r *TaskRepo) HasActiveTaskForProject(ctx context.Context, projectID string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM tasks WHERE project_id = ? AND status IN ('running','queued'))`,
		projectID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("has active task for project: %w", err)
	}
	return exists, nil
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
	var dependsOnJSON *string
	if len(t.DependsOn) > 0 {
		b, _ := json.Marshal(t.DependsOn)
		s := string(b)
		dependsOnJSON = &s
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO tasks
		  (id, project_id, agent_id, parent_task_id, follow_up_of, title, description, status, input, output, cost_usd, tokens_in, tokens_out, source, is_critic_review, reviewed_task_id, critic_mode, prompt_hash, depends_on)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.ProjectID, t.AgentID, nullString(t.ParentTaskID), nullString(t.FollowUpOf),
		t.Title, t.Description, string(t.Status), t.Input, t.Output, t.CostUSD, t.TokensIn, t.TokensOut, t.Source, isCriticReview, nullString(t.ReviewedTaskID), criticMode, t.PromptHash, dependsOnJSON)
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
	// Only return tasks with no pending dependencies; UnlockDependents clears
	// depends_on once all prereqs complete, promoting the task to runnable.
	row := r.db.QueryRowContext(ctx,
		`SELECT`+taskSelectCols+`FROM tasks
		 WHERE agent_id = ? AND status = 'queued'
		   AND (depends_on IS NULL OR depends_on = '[]')
		 ORDER BY priority DESC, created_at ASC LIMIT 1`,
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

// ListFollowUpChain returns all tasks in the follow-up chain that includes rootTaskID,
// ordered oldest first. It walks follow_up_of links starting from rootTaskID forward
// (i.e. it fetches tasks whose follow_up_of points back to rootTaskID transitively).
// The root task itself is always the first element.
func (r *TaskRepo) ListFollowUpChain(ctx context.Context, rootTaskID string) ([]*model.Task, error) {
	// Use a recursive CTE to walk the chain from root to current leaf.
	rows, err := r.db.QueryContext(ctx,
		`WITH RECURSIVE chain(id) AS (
		   SELECT ? AS id
		   UNION ALL
		   SELECT t.id FROM tasks t JOIN chain c ON t.follow_up_of = c.id
		 )
		 SELECT`+taskSelectCols+`FROM tasks
		 WHERE id IN (SELECT id FROM chain)
		 ORDER BY created_at ASC`, rootTaskID)
	if err != nil {
		return nil, fmt.Errorf("list follow-up chain: %w", err)
	}
	defer rows.Close()
	return scanTasks(rows)
}

// SaveSummaryCache persists a summary string on a task (typically the root of a follow-up
// chain). This avoids re-summarising on every subsequent follow-up turn.
func (r *TaskRepo) SaveSummaryCache(ctx context.Context, taskID, summary string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE tasks SET summary_cache = ? WHERE id = ?`, summary, taskID)
	if err != nil {
		return fmt.Errorf("save summary cache: %w", err)
	}
	return nil
}

func (r *TaskRepo) BumpPriority(ctx context.Context, taskID string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE tasks SET priority = priority + 10 WHERE id = ?`, taskID)
	if err != nil {
		return fmt.Errorf("bump priority: %w", err)
	}
	return nil
}

func scanTaskRow(dest *model.Task, scanFn func(...any) error) error {
	var status string
	var parentID, followUpOf, healthSignal, guardrailReason, lastError, reviewedTaskID, dependsOn sql.NullString
	var dismissed, isCriticReview int
	var runnerPID sql.NullInt64
	var timeoutAt, startedAt, completedAt sql.NullTime

	if err := scanFn(
		&dest.ID, &dest.ProjectID, &dest.AgentID, &parentID, &followUpOf,
		&dest.Title, &dest.Description, &status,
		&dest.Input, &dest.Output, &dest.CostUSD, &dest.TokensIn, &dest.TokensOut, &dismissed,
		&runnerPID, &timeoutAt,
		&dest.Source, &healthSignal, &guardrailReason, &lastError,
		&dest.CreatedAt, &startedAt, &completedAt, &isCriticReview, &reviewedTaskID,
		&dest.CriticMode, &dest.PromptHash, &dest.SummaryCache, &dest.Priority, &dependsOn,
	); err != nil {
		return err
	}
	dest.Status = model.TaskStatus(status)
	dest.Dismissed = dismissed != 0
	dest.IsCriticReview = isCriticReview != 0
	if parentID.Valid {
		dest.ParentTaskID = &parentID.String
	}
	if followUpOf.Valid {
		dest.FollowUpOf = &followUpOf.String
	}
	if healthSignal.Valid {
		dest.HealthSignal = &healthSignal.String
	}
	if guardrailReason.Valid {
		dest.GuardrailReason = &guardrailReason.String
	}
	if lastError.Valid {
		dest.LastError = lastError.String
	}
	if reviewedTaskID.Valid {
		dest.ReviewedTaskID = &reviewedTaskID.String
	}
	if runnerPID.Valid {
		dest.RunnerPID = int(runnerPID.Int64)
	}
	if timeoutAt.Valid {
		dest.TimeoutAt = &timeoutAt.Time
	}
	if startedAt.Valid {
		dest.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		dest.CompletedAt = &completedAt.Time
	}
	if dest.CriticMode == "" {
		dest.CriticMode = model.CriticModeInherit
	}
	if dependsOn.Valid && dependsOn.String != "" && dependsOn.String != "[]" {
		_ = json.Unmarshal([]byte(dependsOn.String), &dest.DependsOn)
	}
	return nil
}

func scanTask(row *sql.Row) (*model.Task, error) {
	var t model.Task
	err := scanTaskRow(&t, func(dest ...any) error { return row.Scan(dest...) })
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan task: %w", err)
	}
	return &t, nil
}

func scanTasks(rows *sql.Rows) ([]*model.Task, error) {
	var out []*model.Task
	for rows.Next() {
		var t model.Task
		if err := scanTaskRow(&t, rows.Scan); err != nil {
			return nil, fmt.Errorf("scan task row: %w", err)
		}
		out = append(out, &t)
	}
	return out, rows.Err()
}

// UnlockDependents finds queued tasks that were waiting on completedTaskID and
// whose remaining deps are all now completed. It clears their depends_on so
// NextQueuedTask can pick them up, and returns the agent IDs to drain.
func (r *TaskRepo) UnlockDependents(ctx context.Context, completedTaskID string) ([]string, error) {
	// Rough filter: find queued tasks that mention the completed ID in depends_on.
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, agent_id, depends_on FROM tasks
		WHERE status = 'queued'
		  AND depends_on IS NOT NULL
		  AND depends_on != '[]'
		  AND depends_on LIKE ?`,
		"%"+completedTaskID+"%")
	if err != nil {
		return nil, fmt.Errorf("unlock dependents: %w", err)
	}
	defer rows.Close()

	type candidate struct {
		id      string
		agentID string
		deps    []string
	}
	var candidates []candidate
	for rows.Next() {
		var id, agentID string
		var depsJSON string
		if err := rows.Scan(&id, &agentID, &depsJSON); err != nil {
			continue
		}
		var deps []string
		if err := json.Unmarshal([]byte(depsJSON), &deps); err != nil {
			continue
		}
		// Exact check: completedTaskID must actually be in the list.
		found := false
		for _, d := range deps {
			if d == completedTaskID {
				found = true
				break
			}
		}
		if found {
			candidates = append(candidates, candidate{id, agentID, deps})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("unlock dependents scan: %w", err)
	}

	var agentIDs []string
	for _, c := range candidates {
		// Check all deps are completed.
		allDone := true
		for _, depID := range c.deps {
			var s string
			err := r.db.QueryRowContext(ctx, `SELECT status FROM tasks WHERE id = ?`, depID).Scan(&s)
			if err != nil || s != string(model.TaskStatusCompleted) {
				allDone = false
				break
			}
		}
		if !allDone {
			continue
		}
		// Clear depends_on so NextQueuedTask can pick it up.
		if _, err := r.db.ExecContext(ctx, `UPDATE tasks SET depends_on = NULL WHERE id = ?`, c.id); err != nil {
			continue
		}
		agentIDs = append(agentIDs, c.agentID)
	}
	return agentIDs, nil
}
