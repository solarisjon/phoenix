package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/store"
)

type AgentRepo struct{ db *DB }

func NewAgentRepo(db *DB) *AgentRepo { return &AgentRepo{db} }

func (r *AgentRepo) List(ctx context.Context, userID string) ([]*model.Agent, error) {
	var rows *sql.Rows
	var err error
	if userID == "" {
		rows, err = r.db.QueryContext(ctx, `
			SELECT id, name, persona, instructions, guardrails, behaviour, hard_guardrails,
			       provider_id, model_override, can_spawn_agents, can_hire_agents,
			       max_concurrent, max_cost_per_run, fallback_model, is_orchestrator,
			       created_by, status, created_at, template_id,
			       agent_health_status, agent_health_latency_ms, agent_health_error, agent_health_checked_at
			FROM agents ORDER BY created_at ASC`)
	} else {
		rows, err = r.db.QueryContext(ctx, `
			SELECT id, name, persona, instructions, guardrails, behaviour, hard_guardrails,
			       provider_id, model_override, can_spawn_agents, can_hire_agents,
			       max_concurrent, max_cost_per_run, fallback_model, is_orchestrator,
			       created_by, status, created_at, template_id,
			       agent_health_status, agent_health_latency_ms, agent_health_error, agent_health_checked_at
			FROM agents WHERE created_by = ? ORDER BY created_at ASC`, userID)
	}
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	defer rows.Close()
	return scanAgents(rows)
}

func (r *AgentRepo) Get(ctx context.Context, id string) (*model.Agent, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, name, persona, instructions, guardrails, behaviour, hard_guardrails,
		       provider_id, model_override, can_spawn_agents, can_hire_agents,
		       max_concurrent, max_cost_per_run, fallback_model, is_orchestrator,
		       created_by, status, created_at, template_id,
		       agent_health_status, agent_health_latency_ms, agent_health_error, agent_health_checked_at
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
	isOrchestrator := 0
	if a.IsOrchestrator {
		isOrchestrator = 1
	}
	healthStatus := "unknown"
	if a.AgentHealthStatus != "" {
		healthStatus = a.AgentHealthStatus
	}
	healthError := ""
	if a.AgentHealthError != "" {
		healthError = a.AgentHealthError
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO agents
		  (id, name, persona, instructions, guardrails, behaviour, hard_guardrails, provider_id, model_override, can_spawn_agents, can_hire_agents, max_concurrent, max_cost_per_run, fallback_model, is_orchestrator, created_by, status, template_id, agent_health_status, agent_health_latency_ms, agent_health_error, agent_health_checked_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.Name, a.Persona, a.Instructions, a.Guardrails, a.Behaviour, a.HardGuardrails,
		a.ProviderID, a.ModelOverride, canSpawn, canHire, a.MaxConcurrent, a.MaxCostPerRun, a.FallbackModel, isOrchestrator, a.CreatedBy, string(a.Status), nullString(a.TemplateID),
		healthStatus, a.AgentHealthLatencyMs, healthError, a.AgentHealthCheckedAt)
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
	isOrchestrator := 0
	if a.IsOrchestrator {
		isOrchestrator = 1
	}
	healthStatus := "unknown"
	if a.AgentHealthStatus != "" {
		healthStatus = a.AgentHealthStatus
	}
	healthError := ""
	if a.AgentHealthError != "" {
		healthError = a.AgentHealthError
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE agents SET
		  name = ?, persona = ?, instructions = ?, guardrails = ?, behaviour = ?, hard_guardrails = ?,
		  provider_id = ?, model_override = ?, can_spawn_agents = ?, can_hire_agents = ?, max_concurrent = ?, max_cost_per_run = ?, fallback_model = ?, is_orchestrator = ?, status = ?, template_id = ?, agent_health_status = ?, agent_health_latency_ms = ?, agent_health_error = ?, agent_health_checked_at = ?
		WHERE id = ?`,
		a.Name, a.Persona, a.Instructions, a.Guardrails, a.Behaviour, a.HardGuardrails,
		a.ProviderID, a.ModelOverride, canSpawn, canHire, a.MaxConcurrent, a.MaxCostPerRun, a.FallbackModel, isOrchestrator, string(a.Status), nullString(a.TemplateID),
		healthStatus, a.AgentHealthLatencyMs, healthError, a.AgentHealthCheckedAt, a.ID)
	if err != nil {
		return fmt.Errorf("update agent: %w", err)
	}
	return nil
}

func (r *AgentRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM agents WHERE id = ?`, id)
	if err != nil {
		if isForeignKeyViolation(err) {
			var taskCount int
			_ = r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE agent_id = ?`, id).Scan(&taskCount)
			if taskCount > 0 {
				return fmt.Errorf("%w: %d task(s) still reference this agent", store.ErrInUse, taskCount)
			}
			return fmt.Errorf("%w: other records still reference this agent", store.ErrInUse)
		}
		return fmt.Errorf("delete agent: %w", err)
	}
	return nil
}

// UpdateHealth records the outcome of an agent self-test.
func (r *AgentRepo) UpdateHealth(ctx context.Context, id, status string, latencyMs *int64, errMsg string) error {
	healthCheckedAt := time.Now().UTC()
	_, err := r.db.ExecContext(ctx, `
		UPDATE agents SET
		  agent_health_status = ?,
		  agent_health_latency_ms = ?,
		  agent_health_error = ?,
		  agent_health_checked_at = ?
		WHERE id = ?`,
		status, latencyMs, errMsg, healthCheckedAt, id)
	if err != nil {
		return fmt.Errorf("update agent health: %w", err)
	}
	return nil
}

func (r *AgentRepo) Search(ctx context.Context, query, userID string) ([]*model.Agent, error) {
	var rows *sql.Rows
	var err error
	if userID == "" {
		rows, err = r.db.QueryContext(ctx, `
			SELECT id, name, persona, instructions, guardrails, behaviour, hard_guardrails,
			       provider_id, model_override, can_spawn_agents, can_hire_agents,
			       max_concurrent, max_cost_per_run, fallback_model, is_orchestrator,
			       created_by, status, created_at, template_id,
			       agent_health_status, agent_health_latency_ms, agent_health_error, agent_health_checked_at
			FROM agents
			WHERE rowid IN (SELECT rowid FROM agents_fts WHERE agents_fts MATCH ?)
			ORDER BY created_at DESC LIMIT 50`, query)
	} else {
		rows, err = r.db.QueryContext(ctx, `
			SELECT id, name, persona, instructions, guardrails, behaviour, hard_guardrails,
			       provider_id, model_override, can_spawn_agents, can_hire_agents,
			       max_concurrent, max_cost_per_run, fallback_model, is_orchestrator,
			       created_by, status, created_at, template_id,
			       agent_health_status, agent_health_latency_ms, agent_health_error, agent_health_checked_at
			FROM agents
			WHERE created_by = ? AND rowid IN (SELECT rowid FROM agents_fts WHERE agents_fts MATCH ?)
			ORDER BY created_at DESC LIMIT 50`, userID, query)
	}
	if err != nil {
		return nil, fmt.Errorf("search agents: %w", err)
	}
	defer rows.Close()
	return scanAgents(rows)
}

func scanAgent(row *sql.Row) (*model.Agent, error) {
	var a model.Agent
	var status string
	var templateID sql.NullString
	var healthStatus string
	var healthLatencyMs sql.NullInt64
	var healthError string
	var healthCheckedAt sql.NullTime
	var canSpawn, canHire, isOrchestrator int
	err := row.Scan(&a.ID, &a.Name, &a.Persona, &a.Instructions, &a.Guardrails, &a.Behaviour, &a.HardGuardrails,
		&a.ProviderID, &a.ModelOverride, &canSpawn, &canHire, &a.MaxConcurrent, &a.MaxCostPerRun, &a.FallbackModel,
		&isOrchestrator, &a.CreatedBy, &status, &a.CreatedAt, &templateID, &healthStatus, &healthLatencyMs, &healthError, &healthCheckedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan agent: %w", err)
	}
	a.Status = model.AgentStatus(status)
	a.CanSpawnAgents = canSpawn != 0
	a.CanHireAgents = canHire != 0
	a.IsOrchestrator = isOrchestrator != 0
	a.AgentHealthStatus = healthStatus
	a.AgentHealthError = healthError
	if healthLatencyMs.Valid {
		latency := int64(healthLatencyMs.Int64)
		a.AgentHealthLatencyMs = &latency
	}
	if healthCheckedAt.Valid {
		t := healthCheckedAt.Time
		a.AgentHealthCheckedAt = &t
	}
	synthesiseBehaviour(&a)
	return &a, nil
}

func scanAgents(rows *sql.Rows) ([]*model.Agent, error) {
	var out []*model.Agent
	for rows.Next() {
		var a model.Agent
		var status string
		var templateID sql.NullString
		var healthStatus string
		var healthLatencyMs sql.NullInt64
		var healthError string
		var healthCheckedAt sql.NullTime
		var canSpawn, canHire, isOrchestrator int
		if err := rows.Scan(&a.ID, &a.Name, &a.Persona, &a.Instructions, &a.Guardrails, &a.Behaviour, &a.HardGuardrails,
			&a.ProviderID, &a.ModelOverride, &canSpawn, &canHire, &a.MaxConcurrent, &a.MaxCostPerRun, &a.FallbackModel,
			&isOrchestrator, &a.CreatedBy, &status, &a.CreatedAt, &templateID, &healthStatus, &healthLatencyMs, &healthError, &healthCheckedAt); err != nil {
			return nil, fmt.Errorf("scan agent row: %w", err)
		}
		a.Status = model.AgentStatus(status)
		a.CanSpawnAgents = canSpawn != 0
		a.CanHireAgents = canHire != 0
		a.IsOrchestrator = isOrchestrator != 0
		a.AgentHealthStatus = healthStatus
		a.AgentHealthError = healthError
		if healthLatencyMs.Valid {
			latency := int64(healthLatencyMs.Int64)
			a.AgentHealthLatencyMs = &latency
		}
		if healthCheckedAt.Valid {
			t := healthCheckedAt.Time
			a.AgentHealthCheckedAt = &t
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
