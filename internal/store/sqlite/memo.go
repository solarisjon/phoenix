package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/solarisjon/phoenix/internal/model"
)

// MemoRepo implements store.MemoRepo backed by SQLite.
type MemoRepo struct{ db *DB }

func NewMemoRepo(db *DB) *MemoRepo { return &MemoRepo{db} }

const memoSelectCols = `id, project_id, project_name, task_id, agent_id, agent_name,
	title, body, priority, status, created_at`

// List returns memos. status="" returns all non-archived; otherwise filters to that status.
func (r *MemoRepo) List(ctx context.Context, status string) ([]*model.Memo, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if status == "" {
		rows, err = r.db.QueryContext(ctx,
			`SELECT `+memoSelectCols+` FROM memos WHERE status != 'archived' ORDER BY created_at DESC`)
	} else {
		rows, err = r.db.QueryContext(ctx,
			`SELECT `+memoSelectCols+` FROM memos WHERE status = ? ORDER BY created_at DESC`, status)
	}
	if err != nil {
		return nil, fmt.Errorf("list memos: %w", err)
	}
	defer rows.Close()
	return scanMemos(rows)
}

func (r *MemoRepo) Get(ctx context.Context, id string) (*model.Memo, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+memoSelectCols+` FROM memos WHERE id = ?`, id)
	return scanMemo(row)
}

func (r *MemoRepo) Create(ctx context.Context, m *model.Memo) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO memos (id, project_id, project_name, task_id, agent_id, agent_name,
		 title, body, priority, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.ProjectID, m.ProjectName, m.TaskID, m.AgentID, m.AgentName,
		m.Title, m.Body, string(m.Priority), string(m.Status), m.CreatedAt)
	if err != nil {
		return fmt.Errorf("create memo: %w", err)
	}
	return nil
}

func (r *MemoRepo) UpdateStatus(ctx context.Context, id string, status model.MemoStatus) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE memos SET status = ? WHERE id = ?`, string(status), id)
	if err != nil {
		return fmt.Errorf("update memo status: %w", err)
	}
	return nil
}

func (r *MemoRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM memos WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete memo: %w", err)
	}
	return nil
}

func (r *MemoRepo) UnreadCount(ctx context.Context) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memos WHERE status IN ('unread', 'flagged')`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("memo unread count: %w", err)
	}
	return count, nil
}

// ---- scan helpers ----

func scanMemo(row *sql.Row) (*model.Memo, error) {
	var m model.Memo
	var priority, status string
	err := row.Scan(
		&m.ID, &m.ProjectID, &m.ProjectName, &m.TaskID, &m.AgentID, &m.AgentName,
		&m.Title, &m.Body, &priority, &status, &m.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan memo: %w", err)
	}
	m.Priority = model.MemoPriority(priority)
	m.Status = model.MemoStatus(status)
	return &m, nil
}

func scanMemos(rows *sql.Rows) ([]*model.Memo, error) {
	var out []*model.Memo
	for rows.Next() {
		var m model.Memo
		var priority, status string
		if err := rows.Scan(
			&m.ID, &m.ProjectID, &m.ProjectName, &m.TaskID, &m.AgentID, &m.AgentName,
			&m.Title, &m.Body, &priority, &status, &m.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan memo row: %w", err)
		}
		m.Priority = model.MemoPriority(priority)
		m.Status = model.MemoStatus(status)
		out = append(out, &m)
	}
	return out, rows.Err()
}
