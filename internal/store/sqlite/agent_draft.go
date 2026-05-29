package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/solarisjon/phoenix/internal/model"
)

// AgentDraftRepo implements store.AgentDraftRepo using SQLite.
type AgentDraftRepo struct{ db *DB }

// NewAgentDraftRepo creates a new AgentDraftRepo.
func NewAgentDraftRepo(db *DB) *AgentDraftRepo { return &AgentDraftRepo{db} }

// List returns all non-dismissed drafts, newest first.
// Denormalises agent name and task title via joins.
func (r *AgentDraftRepo) List(ctx context.Context) ([]*model.AgentDraft, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT
			d.id, d.created_by_agent_id, COALESCE(a.name,''),
			d.created_by_task_id, COALESCE(t.title,''),
			d.name, d.persona, d.instructions, d.guardrails,
			d.provider_id, d.status, d.dismissed, d.created_at
		FROM agent_drafts d
		LEFT JOIN agents a ON a.id = d.created_by_agent_id
		LEFT JOIN tasks  t ON t.id = d.created_by_task_id
		WHERE d.dismissed = 0
		ORDER BY d.created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list agent drafts: %w", err)
	}
	defer rows.Close()
	return scanDrafts(rows)
}

// Get fetches a single draft by ID (including dismissed).
func (r *AgentDraftRepo) Get(ctx context.Context, id string) (*model.AgentDraft, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT
			d.id, d.created_by_agent_id, COALESCE(a.name,''),
			d.created_by_task_id, COALESCE(t.title,''),
			d.name, d.persona, d.instructions, d.guardrails,
			d.provider_id, d.status, d.dismissed, d.created_at
		FROM agent_drafts d
		LEFT JOIN agents a ON a.id = d.created_by_agent_id
		LEFT JOIN tasks  t ON t.id = d.created_by_task_id
		WHERE d.id = ?
	`, id)
	return scanDraft(row)
}

// Create inserts a new draft.
func (r *AgentDraftRepo) Create(ctx context.Context, d *model.AgentDraft) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO agent_drafts
		  (id, created_by_agent_id, created_by_task_id, name, persona, instructions, guardrails, provider_id, status, dismissed, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?)`,
		d.ID, d.CreatedByAgentID, nullString(d.CreatedByTaskID),
		d.Name, d.Persona, d.Instructions, d.Guardrails,
		d.ProviderID, string(d.Status), d.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("create agent draft: %w", err)
	}
	return nil
}

// Update saves changes to an existing draft (name, persona, instructions,
// guardrails, provider_id, status).
func (r *AgentDraftRepo) Update(ctx context.Context, d *model.AgentDraft) error {
	dismissed := 0
	if d.Dismissed {
		dismissed = 1
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE agent_drafts SET
		  name = ?, persona = ?, instructions = ?, guardrails = ?,
		  provider_id = ?, status = ?, dismissed = ?
		WHERE id = ?`,
		d.Name, d.Persona, d.Instructions, d.Guardrails,
		d.ProviderID, string(d.Status), dismissed, d.ID,
	)
	if err != nil {
		return fmt.Errorf("update agent draft: %w", err)
	}
	return nil
}

// Dismiss marks a draft as dismissed without changing its status.
func (r *AgentDraftRepo) Dismiss(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE agent_drafts SET dismissed = 1 WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("dismiss agent draft: %w", err)
	}
	return nil
}

// ---- scanners ----

func scanDraft(row *sql.Row) (*model.AgentDraft, error) {
	var d model.AgentDraft
	var taskID sql.NullString
	var taskTitle string
	var status string
	var dismissed int
	err := row.Scan(
		&d.ID, &d.CreatedByAgentID, &d.CreatedByAgentName,
		&taskID, &taskTitle,
		&d.Name, &d.Persona, &d.Instructions, &d.Guardrails,
		&d.ProviderID, &status, &dismissed, &d.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan agent draft: %w", err)
	}
	d.Status = model.AgentDraftStatus(status)
	d.Dismissed = dismissed != 0
	d.CreatedByTaskTitle = taskTitle
	if taskID.Valid {
		s := taskID.String
		d.CreatedByTaskID = &s
	}
	return &d, nil
}

func scanDrafts(rows *sql.Rows) ([]*model.AgentDraft, error) {
	var out []*model.AgentDraft
	for rows.Next() {
		var d model.AgentDraft
		var taskID sql.NullString
		var taskTitle string
		var status string
		var dismissed int
		if err := rows.Scan(
			&d.ID, &d.CreatedByAgentID, &d.CreatedByAgentName,
			&taskID, &taskTitle,
			&d.Name, &d.Persona, &d.Instructions, &d.Guardrails,
			&d.ProviderID, &status, &dismissed, &d.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan agent draft row: %w", err)
		}
		d.Status = model.AgentDraftStatus(status)
		d.Dismissed = dismissed != 0
		d.CreatedByTaskTitle = taskTitle
		if taskID.Valid {
			s := taskID.String
			d.CreatedByTaskID = &s
		}
		out = append(out, &d)
	}
	return out, rows.Err()
}
