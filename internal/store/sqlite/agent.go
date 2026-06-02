package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/solarisjon/phoenix/internal/model"
)

type AgentRepo struct{ db *DB }

func NewAgentRepo(db *DB) *AgentRepo { return &AgentRepo{db} }

func (r *AgentRepo) List(ctx context.Context) ([]*model.Agent, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, persona, instructions, guardrails, behaviour, hard_guardrails,
		       provider_id, model_override, can_spawn_agents, can_hire_agents, heartbeat_interval,
		       max_concurrent, created_by, status, created_at
		FROM agents ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	defer rows.Close()
	return scanAgents(rows)
}

func (r *AgentRepo) Get(ctx context.Context, id string) (*model.Agent, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, name, persona, instructions, guardrails, behaviour, hard_guardrails,
		       provider_id, model_override, can_spawn_agents, can_hire_agents, heartbeat_interval,
		       max_concurrent, created_by, status, created_at
		FROM agents WHERE id = ?`, id)
	return scanAgent(row)
}

func (r *AgentRepo) Create(ctx context.Context, a *model.Agent) error {
	canSpawn := 0
	if a.CanSpawnAgents {
		canSpawn = 1
	}
	canHire := 0
	if a.CanHireAgents {
		canHire = 1
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO agents
		  (id, name, persona, instructions, guardrails, behaviour, hard_guardrails, provider_id, model_override, can_spawn_agents, can_hire_agents, heartbeat_interval, max_concurrent, created_by, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.Name, a.Persona, a.Instructions, a.Guardrails, a.Behaviour, a.HardGuardrails,
		a.ProviderID, a.ModelOverride, canSpawn, canHire, nullInt(a.HeartbeatInterval), a.MaxConcurrent, a.CreatedBy, string(a.Status))
	if err != nil {
		return fmt.Errorf("create agent: %w", err)
	}
	return nil
}

func (r *AgentRepo) Update(ctx context.Context, a *model.Agent) error {
	canSpawn := 0
	if a.CanSpawnAgents {
		canSpawn = 1
	}
	canHire := 0
	if a.CanHireAgents {
		canHire = 1
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE agents SET
		  name = ?, persona = ?, instructions = ?, guardrails = ?, behaviour = ?, hard_guardrails = ?,
		  provider_id = ?, model_override = ?, can_spawn_agents = ?, can_hire_agents = ?, heartbeat_interval = ?, max_concurrent = ?, status = ?
		WHERE id = ?`,
		a.Name, a.Persona, a.Instructions, a.Guardrails, a.Behaviour, a.HardGuardrails,
		a.ProviderID, a.ModelOverride, canSpawn, canHire, nullInt(a.HeartbeatInterval), a.MaxConcurrent, string(a.Status), a.ID)
	if err != nil {
		return fmt.Errorf("update agent: %w", err)
	}
	return nil
}

func (r *AgentRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM agents WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete agent: %w", err)
	}
	return nil
}

func scanAgent(row *sql.Row) (*model.Agent, error) {
	var a model.Agent
	var status string
	var hb sql.NullInt64
	var canSpawn, canHire int
	err := row.Scan(&a.ID, &a.Name, &a.Persona, &a.Instructions, &a.Guardrails, &a.Behaviour, &a.HardGuardrails,
		&a.ProviderID, &a.ModelOverride, &canSpawn, &canHire, &hb, &a.MaxConcurrent, &a.CreatedBy, &status, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan agent: %w", err)
	}
	a.Status = model.AgentStatus(status)
	a.CanSpawnAgents = canSpawn != 0
	a.CanHireAgents = canHire != 0
	if hb.Valid {
		v := int(hb.Int64)
		a.HeartbeatInterval = &v
	}
	synthesiseBehaviour(&a)
	return &a, nil
}

func scanAgents(rows *sql.Rows) ([]*model.Agent, error) {
	var out []*model.Agent
	for rows.Next() {
		var a model.Agent
		var status string
		var hb sql.NullInt64
		var canSpawn, canHire int
		if err := rows.Scan(&a.ID, &a.Name, &a.Persona, &a.Instructions, &a.Guardrails, &a.Behaviour, &a.HardGuardrails,
			&a.ProviderID, &a.ModelOverride, &canSpawn, &canHire, &hb, &a.MaxConcurrent, &a.CreatedBy, &status, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan agent row: %w", err)
		}
		a.Status = model.AgentStatus(status)
		a.CanSpawnAgents = canSpawn != 0
		a.CanHireAgents = canHire != 0
		if hb.Valid {
			v := int(hb.Int64)
			a.HeartbeatInterval = &v
		}
		synthesiseBehaviour(&a)
		out = append(out, &a)
	}
	return out, rows.Err()
}

// synthesiseBehaviour populates a.Behaviour for agents that predate the field.
// If behaviour is already set, it is left unchanged.
// If only legacy persona/instructions exist, they are merged.
func synthesiseBehaviour(a *model.Agent) {
	if a.Behaviour != "" {
		return
	}
	var parts []string
	if a.Persona != "" {
		parts = append(parts, a.Persona)
	}
	if a.Instructions != "" {
		parts = append(parts, a.Instructions)
	}
	if len(parts) > 0 {
		a.Behaviour = strings.Join(parts, "\n\n")
	}
}
