// Package model defines the core domain types shared across Phoenix.
package model

import "time"

// ---- Enums ----

type AgentStatus string

const (
	AgentStatusActive   AgentStatus = "active"
	AgentStatusPaused   AgentStatus = "paused"
	AgentStatusDisabled AgentStatus = "disabled"
)

type ProjectStatus string

const (
	ProjectStatusActive   ProjectStatus = "active"
	ProjectStatusArchived ProjectStatus = "archived"
)

type TaskStatus string

const (
	TaskStatusPending          TaskStatus = "pending"
	TaskStatusQueued           TaskStatus = "queued"
	TaskStatusRunning          TaskStatus = "running"
	TaskStatusCompleted        TaskStatus = "completed"
	TaskStatusFailed           TaskStatus = "failed"
	TaskStatusAwaitingApproval TaskStatus = "awaiting_approval"
)

type ProviderType string

const (
	ProviderTypeLLM         ProviderType = "llm"
	ProviderTypeCodingAgent ProviderType = "coding_agent"
)

// ---- Domain Types ----

// User represents a Phoenix user. Single-user for Phase 1 but FK-ready for multi-user.
type User struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Settings  string    `json:"settings"` // JSON blob
	CreatedAt time.Time `json:"created_at"`
}

// Provider holds configuration for an LLM endpoint or coding agent tool.
type Provider struct {
	ID         string       `json:"id"`
	Name       string       `json:"name"`
	Type       ProviderType `json:"type"`
	Config     string       `json:"config"` // JSON blob
	CreatedBy  string       `json:"created_by"`
	CreatedAt  time.Time    `json:"created_at"`
}

// Agent is an AI agent with a persona, instructions, guardrails, and a provider.
type Agent struct {
	ID                string      `json:"id"`
	Name              string      `json:"name"`
	Persona           string      `json:"persona"`
	Instructions      string      `json:"instructions"`
	Guardrails        string      `json:"guardrails"`
	ProviderID        string      `json:"provider_id"`
	ModelOverride     string      `json:"model_override"`     // if set, overrides the provider's default model
	CanSpawnAgents    bool        `json:"can_spawn_agents"`   // if true, agent may create tasks for other agents
	CanHireAgents     bool        `json:"can_hire_agents"`    // if true, agent may submit new agent hire proposals
	HeartbeatInterval *int        `json:"heartbeat_interval"` // seconds, nil = manual only
	CreatedBy         string      `json:"created_by"`
	Status            AgentStatus `json:"status"`
	CreatedAt         time.Time   `json:"created_at"`
}

// ProjectKind distinguishes human-driven workbenches from autonomous daemons.
type ProjectKind string

const (
	ProjectKindProject ProjectKind = "project" // human-driven workbench
	ProjectKindMonitor ProjectKind = "monitor" // autonomous heartbeat daemon
)

// Project is a workspace containing tasks assigned to agents.
type Project struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	WorkingDir  string        `json:"working_dir"` // optional: filesystem path passed to coding agents
	Kind        ProjectKind   `json:"kind"`        // "project" | "monitor"
	Owner       string        `json:"owner"`
	Status      ProjectStatus `json:"status"`
	CreatedAt   time.Time     `json:"created_at"`
}

// ProjectAgent links an agent to a project.
type ProjectAgent struct {
	ProjectID string `json:"project_id"`
	AgentID   string `json:"agent_id"`
}

// Task is a unit of work assigned to an agent within a project.
type Task struct {
	ID           string     `json:"id"`
	ProjectID    string     `json:"project_id"`
	AgentID      string     `json:"agent_id"`
	ParentTaskID *string    `json:"parent_task_id"` // nil = top-level task
	FollowUpOf   *string    `json:"follow_up_of"`   // nil = original task; set on human refinement follow-ups
	Title        string     `json:"title"`
	Description  string     `json:"description"`
	Status       TaskStatus `json:"status"`
	Input        string     `json:"input"`        // JSON blob
	Output       string     `json:"output"`       // JSON blob
	CostUSD      float64    `json:"cost_usd"`
	Source       string     `json:"source"`       // free-text provenance, empty if human-created
	Dismissed    bool       `json:"dismissed"`    // hidden from inbox but kept for audit
	RunnerPID    int        `json:"runner_pid"`   // OS PID of the subprocess, 0 if not running
	TimeoutAt    *time.Time `json:"timeout_at"`   // when the task will be force-killed
	CreatedAt    time.Time  `json:"created_at"`
	StartedAt    *time.Time `json:"started_at"`
	CompletedAt  *time.Time `json:"completed_at"`
}

// Team is a named group of agents that can be assigned to projects as a unit.
type Team struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	Agents      []*Agent  `json:"agents,omitempty"` // populated on Get/List
}

// SystemSettings holds platform-wide configuration that overrides per-agent settings.
type SystemSettings struct {
	GlobalGuardrailsEnabled bool   `json:"global_guardrails_enabled"`
	GlobalGuardrails        string `json:"global_guardrails"`
}

// AgentDraftStatus represents the lifecycle of a pending agent hire.
type AgentDraftStatus string

const (
	AgentDraftPending  AgentDraftStatus = "pending_approval"
	AgentDraftApproved AgentDraftStatus = "approved"
	AgentDraftRejected AgentDraftStatus = "rejected"
)

// AgentDraft is a proposed new agent submitted by a hiring agent for human
// review and approval. On approval it becomes a live Agent.
type AgentDraft struct {
	ID                 string           `json:"id"`
	CreatedByAgentID   string           `json:"created_by_agent_id"`
	CreatedByAgentName string           `json:"created_by_agent_name"` // denormalised for display
	CreatedByTaskID    *string          `json:"created_by_task_id"`
	CreatedByTaskTitle string           `json:"created_by_task_title"` // denormalised for display
	Name               string           `json:"name"`
	Persona            string           `json:"persona"`
	Instructions       string           `json:"instructions"`
	Guardrails         string           `json:"guardrails"`
	ProviderID         string           `json:"provider_id"`
	Status             AgentDraftStatus `json:"status"`
	Dismissed          bool             `json:"dismissed"`
	CreatedAt          time.Time        `json:"created_at"`
}
