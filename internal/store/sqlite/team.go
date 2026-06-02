package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/solarisjon/phoenix/internal/model"
)

type TeamRepo struct{ db *DB }

func NewTeamRepo(db *DB) *TeamRepo { return &TeamRepo{db} }

func (r *TeamRepo) List(ctx context.Context) ([]*model.Team, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, description, created_by, created_at FROM teams ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("list teams: %w", err)
	}
	defer rows.Close()

	var out []*model.Team
	for rows.Next() {
		t, err := scanTeamRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Load agents for each team.
	for _, t := range out {
		agents, err := r.ListAgents(ctx, t.ID)
		if err != nil {
			return nil, err
		}
		t.Agents = agents
	}
	return out, nil
}

func (r *TeamRepo) Get(ctx context.Context, id string) (*model.Team, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, name, description, created_by, created_at FROM teams WHERE id = ?`, id)
	var t model.Team
	err := row.Scan(&t.ID, &t.Name, &t.Description, &t.CreatedBy, &t.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get team: %w", err)
	}
	agents, err := r.ListAgents(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	t.Agents = agents
	return &t, nil
}

func (r *TeamRepo) Create(ctx context.Context, t *model.Team) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO teams (id, name, description, created_by) VALUES (?, ?, ?, ?)`,
		t.ID, t.Name, t.Description, t.CreatedBy)
	if err != nil {
		return fmt.Errorf("create team: %w", err)
	}
	return nil
}

func (r *TeamRepo) Update(ctx context.Context, t *model.Team) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE teams SET name = ?, description = ? WHERE id = ?`,
		t.Name, t.Description, t.ID)
	if err != nil {
		return fmt.Errorf("update team: %w", err)
	}
	return nil
}

func (r *TeamRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM teams WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete team: %w", err)
	}
	return nil
}

func (r *TeamRepo) AddAgent(ctx context.Context, teamID, agentID string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO team_agents (team_id, agent_id) VALUES (?, ?)`,
		teamID, agentID)
	if err != nil {
		return fmt.Errorf("add agent to team: %w", err)
	}
	return nil
}

func (r *TeamRepo) RemoveAgent(ctx context.Context, teamID, agentID string) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM team_agents WHERE team_id = ? AND agent_id = ?`,
		teamID, agentID)
	if err != nil {
		return fmt.Errorf("remove agent from team: %w", err)
	}
	return nil
}

func (r *TeamRepo) ListAgents(ctx context.Context, teamID string) ([]*model.Agent, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT a.id, a.name, a.persona, a.instructions, a.guardrails, a.behaviour, a.hard_guardrails,
		       a.provider_id, a.model_override, a.can_spawn_agents, a.can_hire_agents,
		       a.max_concurrent, a.created_by, a.status, a.created_at, a.template_id
		FROM agents a
		JOIN team_agents ta ON ta.agent_id = a.id
		WHERE ta.team_id = ?
		ORDER BY a.created_at ASC`, teamID)
	if err != nil {
		return nil, fmt.Errorf("list team agents: %w", err)
	}
	defer rows.Close()
	return scanAgents(rows)
}

func scanTeamRow(rows *sql.Rows) (*model.Team, error) {
	var t model.Team
	if err := rows.Scan(&t.ID, &t.Name, &t.Description, &t.CreatedBy, &t.CreatedAt); err != nil {
		return nil, fmt.Errorf("scan team: %w", err)
	}
	return &t, nil
}
