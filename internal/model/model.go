// Package model defines the core domain types shared across Phoenix.
package model

import (
	"encoding/json"
	"time"
)

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
	ProjectStatusPaused   ProjectStatus = "paused"
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

// ModelCapabilityTier classifies a model by its cost/capability tradeoff.
type ModelCapabilityTier string

const (
	ModelTierFast      ModelCapabilityTier = "fast"      // cheap, quick, simple tasks
	ModelTierStandard  ModelCapabilityTier = "standard"  // mid-tier general purpose
	ModelTierPowerful  ModelCapabilityTier = "powerful"  // top-tier for complex tasks
	ModelTierPlanning  ModelCapabilityTier = "planning"  // good at reasoning/orchestration
)

// ModelEntry describes a single model in a provider's whitelisted pool.
// Stored as a JSON array in providers.allowed_models.
type ModelEntry struct {
	ModelID             string              `json:"model_id"`
	Label               string              `json:"label"`
	CapabilityTier      ModelCapabilityTier `json:"capability_tier"`
	CapabilityDesc      string              `json:"capability_description"`
	UserDescription     string              `json:"user_description,omitempty"`
	InputCostPer1K      float64             `json:"input_cost_per_1k"`
	OutputCostPer1K     float64             `json:"output_cost_per_1k"`
	ProbedAt            *time.Time          `json:"probed_at,omitempty"`
}

// TaskType distinguishes the role of a task in the orchestration pipeline.
type TaskType string

const (
	TaskTypeStandard      TaskType = "standard"      // normal task (default)
	TaskTypeOrchestration TaskType = "orchestration" // orchestrator planning task
	TaskTypeSubtask       TaskType = "subtask"       // subtask spawned by orchestrator
)

// OrchestrationSubtask is one item in an orchestrator's decomposition plan.
type OrchestrationSubtask struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Domain      string `json:"domain"`     // e.g. "code", "write", "analyse", "research"
	Complexity  string `json:"complexity"` // "low" | "medium" | "high"
}

// OrchestrationPlan is the structured output produced by the orchestrator agent.
// It is stored as JSON in tasks.orchestration_plan.
type OrchestrationPlan struct {
	Confidence float64                `json:"confidence"` // 0–1
	Rationale  string                 `json:"rationale"`
	Subtasks   []OrchestrationSubtask `json:"subtasks"`
	SingleTask *OrchestrationSubtask  `json:"single_task,omitempty"` // set when no decomposition needed
}

// ParseOrchestrationPlan decodes a JSON plan string, returning nil on empty input.
func ParseOrchestrationPlan(raw string) (*OrchestrationPlan, error) {
	if raw == "" {
		return nil, nil
	}
	var p OrchestrationPlan
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// ---- Domain Types ----

// User represents a Phoenix user.
type User struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Email        string    `json:"email"`
	Settings     string    `json:"settings"`      // JSON blob
	PasswordHash string    `json:"-"`             // bcrypt hash; never serialised
	CreatedAt    time.Time `json:"created_at"`
}

// Session is an authenticated browser session tied to a user.
type Session struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// Provider holds configuration for an LLM endpoint or coding agent tool.
type Provider struct {
	ID              string       `json:"id"`
	Name            string       `json:"name"`
	Type            ProviderType `json:"type"`
	Config          string       `json:"config"` // JSON blob
	AllowedModels   []ModelEntry `json:"allowed_models"` // curated model pool; empty = unrestricted
	CreatedBy       string       `json:"created_by"`
	CreatedAt       time.Time    `json:"created_at"`
	HealthStatus    string       `json:"health_status"`              // "ok" | "error" | "unknown"
	HealthLatencyMs *int64       `json:"health_latency_ms,omitempty"` // nil if never checked
	HealthError     string       `json:"health_error,omitempty"`
	HealthCheckedAt *time.Time   `json:"health_checked_at,omitempty"`
}

// Agent is an AI agent with a behaviour description, guardrails, and a provider.
type Agent struct {
	ID                string      `json:"id"`
	Name              string      `json:"name"`
	Behaviour         string      `json:"behaviour"`       // unified persona + instructions field
	Persona           string      `json:"persona"`         // legacy — kept for backwards compat
	Instructions      string      `json:"instructions"`    // legacy — kept for backwards compat
	Guardrails        string      `json:"guardrails"`      // soft (advisory) constraints
	HardGuardrails    string      `json:"hard_guardrails"` // mandatory — triggers awaiting_approval
	ProviderID        string      `json:"provider_id"`
	ModelOverride     string      `json:"model_override"`     // if set, overrides the provider's default model
	CanSpawnAgents    bool        `json:"can_spawn_agents"`   // if true, agent may create tasks for other agents
	CanHireAgents     bool        `json:"can_hire_agents"`    // if true, agent may submit new agent hire proposals
	MaxConcurrent     int         `json:"max_concurrent"`     // 0 = unlimited
	MaxCostPerRun     float64     `json:"max_cost_per_run"`   // 0 = unlimited; USD ceiling per run (estimated pre-execution)
	FallbackModel     string      `json:"fallback_model"`     // model to use when cost budget overflows after context truncation; empty = fail
	IsOrchestrator    bool        `json:"is_orchestrator"`    // if true, this agent is the global task orchestrator
	CreatedBy         string      `json:"created_by"`
	Status            AgentStatus `json:"status"`
	CreatedAt         time.Time   `json:"created_at"`
	TemplateID        *string     `json:"template_id"`
}

// ProjectKind distinguishes human-driven workbenches from autonomous daemons.
type ProjectKind string

const (
	ProjectKindProject ProjectKind = "project" // human-driven workbench
	ProjectKindMonitor ProjectKind = "monitor" // autonomous heartbeat daemon
)

// ScheduleKind selects how a monitor's automatic runs are timed. A monitor uses
// exactly one kind at a time.
type ScheduleKind = string

const (
	// ScheduleKindInterval fires every ScheduleInterval seconds (default).
	ScheduleKindInterval ScheduleKind = "interval"
	// ScheduleKindDaily fires at each HH:MM listed in ScheduleTimes, in the
	// server's local timezone.
	ScheduleKindDaily ScheduleKind = "daily"
)

// CriticMode controls whether and how a critic/devil's-advocate review is run
// after a task completes.
//
//   "none"       — no critic (default)
//   "builtin"    — ephemeral devil's advocate; same provider as the original agent,
//                  hardcoded contrarian system prompt, no DB agent record required
//   "agent:<id>" — delegate to a specific registered agent
//
// On tasks, the special value "inherit" means "use the project's setting".
type CriticMode = string

const (
	CriticModeNone    CriticMode = "none"
	CriticModeBuiltin CriticMode = "builtin"
	CriticModeInherit CriticMode = "inherit" // task-level only
)

// Project is a workspace containing tasks assigned to agents.
type Project struct {
	ID               string        `json:"id"`
	Name             string        `json:"name"`
	Objective        string        `json:"objective"`         // goal statement injected into every task prompt
	WorkingDir       string        `json:"working_dir"`       // optional: filesystem path passed to coding agents
	Kind             ProjectKind   `json:"kind"`              // "project" | "monitor"
	ScheduleInterval *int          `json:"schedule_interval"` // seconds; nil = no automatic schedule (monitors only)
	ScheduleKind     ScheduleKind  `json:"schedule_kind"`     // "interval" | "daily" (monitors only)
	ScheduleTimes    []string      `json:"schedule_times"`    // ["07:00","12:00"] local time, used when ScheduleKind == "daily"
	ScheduleCatchUp  bool          `json:"schedule_catch_up"` // daily only: run a missed time at next opportunity (same calendar day)
	Owner            string        `json:"owner"`
	Status           ProjectStatus `json:"status"`
	CriticAgentID    *string       `json:"critic_agent_id"` // deprecated: use CriticMode
	CriticMode       string        `json:"critic_mode"`     // "none" | "builtin" | "agent:<id>"
	MonitorModel     string        `json:"monitor_model"`   // if set, overrides the agent's model for monitor runs
	BudgetUSD              float64  `json:"budget_usd"`              // 0 = no limit; positive = max spend for the period
	BudgetPeriod           string   `json:"budget_period"`           // "day" | "week" | "month" | "total"
	ContextSummarisation   bool     `json:"context_summarisation"`   // if true, long follow-up chains are summarised before injection
	Tags                   []string `json:"tags"`                    // free-text labels for grouping/filtering

	// Heartbeat reaction fields (monitors only — migration 045)
	HeartbeatOnAttention    string  `json:"heartbeat_on_attention"`     // "" | "spawn" | "notify" | "escalate"
	HeartbeatOnFailed       string  `json:"heartbeat_on_failed"`        // same options
	LinkedProjectID         *string `json:"linked_project_id"`          // project to spawn remediation tasks in
	HeartbeatConsecutiveBad  int    `json:"heartbeat_consecutive_bad"`   // consecutive non-clear signal count
	HeartbeatLastSignal      string `json:"heartbeat_last_signal"`       // last signal value
	HeartbeatEscalateAfter   int    `json:"heartbeat_escalate_after"`    // fire escalate action only after N consecutive bad; 0 = immediately

	// Monitor cache TTL (migration 048)
	MonitorCacheTTL int `json:"monitor_cache_ttl"` // seconds; 0 = cache indefinitely (original behaviour)

	// ReAct autonomous loop fields (migration 046)
	ReactMode     bool `json:"react_mode"`     // enable autonomous NEXT_ACTION iteration
	MaxIterations int  `json:"max_iterations"` // safety cap; 0 = use default (10)

	DefaultSkillID *string `json:"default_skill_id"` // skill (migration 053) auto-injected into every task for this project; nil = none

	CreatedAt time.Time `json:"created_at"`
}

// ProjectAgent links an agent to a project.
type ProjectAgent struct {
	ProjectID string `json:"project_id"`
	AgentID   string `json:"agent_id"`
}

// Task is a unit of work assigned to an agent within a project.
type Task struct {
	ID              string     `json:"id"`
	ProjectID       string     `json:"project_id"`
	AgentID         string     `json:"agent_id"`
	ParentTaskID    *string    `json:"parent_task_id"` // nil = top-level task
	FollowUpOf      *string    `json:"follow_up_of"`   // nil = original task; set on human refinement follow-ups
	Title           string     `json:"title"`
	Description     string     `json:"description"`
	Status          TaskStatus `json:"status"`
	Input           string     `json:"input"`  // JSON blob
	Output          string     `json:"output"` // JSON blob
	CostUSD         float64    `json:"cost_usd"`
	TokensIn        int        `json:"tokens_in"`
	TokensOut       int        `json:"tokens_out"`
	Source          string     `json:"source"`           // free-text provenance, empty if human-created
	HealthSignal    *string    `json:"health_signal"`    // monitor runs: "all_clear" | "needs_attention" | "failed"
	GuardrailReason *string    `json:"guardrail_reason"` // set when task is paused by a hard guardrail
	LastError       string     `json:"last_error"`       // most recent failure message; preserved across retries
	Dismissed       bool       `json:"dismissed"`        // hidden from inbox but kept for audit
	RunnerPID       int        `json:"runner_pid"`       // OS PID of the subprocess, 0 if not running
	TimeoutAt       *time.Time `json:"timeout_at"`       // when the task will be force-killed
	IsCriticReview  bool       `json:"is_critic_review"`
	ReviewedTaskID  *string    `json:"reviewed_task_id"`
	CriticMode      string     `json:"critic_mode"` // "inherit" | "none" | "builtin" | "agent:<id>"
	Priority             int        `json:"priority"`             // higher = runs first; default 0 = FIFO
	DependsOn            []string   `json:"depends_on"`           // task IDs that must complete before this task runs; nil = no deps
	LoopIteration        int        `json:"loop_iteration"`       // iteration index within a ReAct loop (0 = first/only)
	PromptHash           string     `json:"prompt_hash"`          // SHA-256 of the assembled prompt; used for monitor diffing
	SummaryCache         string     `json:"summary_cache"`        // cached summary of older follow-up turns (stored on the root task)
	TaskType             TaskType   `json:"task_type"`            // "standard" | "orchestration" | "subtask"
	OrchestrationPlan    string     `json:"orchestration_plan"`   // JSON blob: plan produced by orchestrator
	CreatedAt            time.Time  `json:"created_at"`
	StartedAt            *time.Time `json:"started_at"`
	CompletedAt          *time.Time `json:"completed_at"`
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
	GlobalGuardrailsEnabled  bool   `json:"global_guardrails_enabled"`
	GlobalGuardrails         string `json:"global_guardrails"`
	CorePluginsEnabled       bool   `json:"core_plugins_enabled"`
	CommunityPluginsEnabled  bool   `json:"community_plugins_enabled"`
	ObsidianEnabled          bool   `json:"obsidian_enabled"`    // master on/off switch for the Obsidian plugin
	ObsidianRoot             string `json:"obsidian_root"`       // filesystem path of vaults directory
	ObsidianAutoWrite        bool   `json:"obsidian_auto_write"` // auto-write MD to vault after task completion
	Theme                    string `json:"theme"`               // active UI theme id, e.g. "dracula"

	// Dynamic orchestration settings (migration 051)
	DynamicOrchestrationEnabled    bool    `json:"dynamic_orchestration_enabled"`
	OrchestratorAgentID            string  `json:"orchestrator_agent_id"`
	MaxSubtaskDepth                int     `json:"max_subtask_depth"`               // default 2
	MaxSubtasksPerLevel            int     `json:"max_subtasks_per_level"`          // default 5
	OrchestratorConfidenceThreshold float64 `json:"orchestrator_confidence_threshold"` // default 0.75; below = approval required

	// SkillImportDirs lists filesystem paths scanned for SKILL.md files. Each path
	// may be a skills container (subdirs with SKILL.md) or a single skill directory.
	SkillImportDirs []string `json:"skill_import_dirs"`
}

// ObsidianVault represents a single Obsidian vault directory with user-provided context
// describing what the vault is for, used to route agent output to the right vault.
type ObsidianVault struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`      // directory name
	Path      string    `json:"path"`      // absolute path to vault root
	Context   string    `json:"context"`   // human description of vault purpose
	Enabled   bool      `json:"enabled"`
	SortOrder int       `json:"sort_order"`
	CreatedAt time.Time `json:"created_at"`
}

// Skill is a reusable, named instruction set. A skill can be bound to a
// project as its default (Project.DefaultSkillID), or invoked ad hoc by
// mentioning its Slug in a task's title/description or a project's objective.
// Skills are injected into the system prompt at prompt-assembly time, so they
// work identically regardless of which provider/CLI executes the task.
type Skill struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Slug         string    `json:"slug"` // lowercase token matched against task text, e.g. "morning_coffee"
	Description  string    `json:"description"`
	Instructions string    `json:"instructions"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
}

// AgentDraftStatus represents the lifecycle of a pending agent hire.
type AgentDraftStatus string

const (
	AgentDraftPending  AgentDraftStatus = "pending_approval"
	AgentDraftApproved AgentDraftStatus = "approved"
	AgentDraftRejected AgentDraftStatus = "rejected"
)

// MemoStatus represents the read/flag/archive lifecycle of a memo.
type MemoStatus string

const (
	MemoStatusUnread   MemoStatus = "unread"
	MemoStatusRead     MemoStatus = "read"
	MemoStatusFlagged  MemoStatus = "flagged"
	MemoStatusArchived MemoStatus = "archived"
)

// MemoPriority flags whether a memo is high-priority.
type MemoPriority string

const (
	MemoPriorityNormal MemoPriority = "normal"
	MemoPriorityHigh   MemoPriority = "high"
)

// Memo is a briefing note posted by an agent (auto-extracted from output) or
// pinned manually by the user from a completed task. Memos surface important
// findings without cluttering the inbox task-lifecycle view.
type Memo struct {
	ID           string       `json:"id"`
	ProjectID    string       `json:"project_id"`
	ProjectName  string       `json:"project_name"` // denormalised
	TaskID       string       `json:"task_id"`
	AgentID      string       `json:"agent_id"`
	AgentName    string       `json:"agent_name"` // denormalised
	Title        string       `json:"title"`
	Body         string       `json:"body"`          // markdown
	ArtifactPath string       `json:"artifact_path"` // absolute path to a .md file artifact, if any
	Priority     MemoPriority `json:"priority"`      // "normal" | "high"
	Status       MemoStatus   `json:"status"`        // "unread" | "read" | "flagged" | "archived"
	CreatedAt    time.Time    `json:"created_at"`
}

// ---- Plugin Types ----

// PluginType identifies which subsystem handles a plugin.
type PluginType string

const (
	PluginTypeNotifier PluginType = "notifier"
	PluginTypeTheme    PluginType = "theme"
	PluginTypeMemory   PluginType = "memory"
)

// NotifyEventType identifies events that can trigger notifications.
type NotifyEventType string

const (
	NotifyTaskCompleted  NotifyEventType = "task.completed"
	NotifyTaskFailed     NotifyEventType = "task.failed"
	NotifyNeedsApproval  NotifyEventType = "task.needs_approval"
	NotifyGuardrailFired NotifyEventType = "task.guardrail_triggered"
)

// Plugin represents a core or community plugin (notifier, theme, etc.).
type Plugin struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Type      PluginType `json:"type"`
	Kind      string     `json:"kind"`    // e.g. "telegram", "webhook", "custom"
	IsCore    bool       `json:"is_core"` // true = shipped with Phoenix, can't delete
	Enabled   bool       `json:"enabled"`
	Config    string     `json:"config"` // JSON blob, schema depends on type+kind
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// NotificationRule maps an event type to a notifier plugin, optionally
// scoped to a specific project.
type NotificationRule struct {
	ID        string          `json:"id"`
	PluginID  string          `json:"plugin_id"`
	EventType NotifyEventType `json:"event_type"`
	ProjectID *string         `json:"project_id"` // nil = all projects
	Enabled   bool            `json:"enabled"`
	Template  *string         `json:"template"` // nil = use default template
	CreatedAt time.Time       `json:"created_at"`
}

// TaskTemplate is a reusable prompt scaffold for quick task creation.
// project_id nil = global (available in all projects).
// agent_id nil = user picks the agent at creation time.
type TaskTemplate struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"` // what the template is for
	Title       string    `json:"title"`        // task title (may contain {{vars}})
	Body        string    `json:"body"`         // task description (may contain {{vars}})
	ProjectID   *string   `json:"project_id"`   // nil = global
	AgentID     *string   `json:"agent_id"`     // nil = inherits from project
	CreatedAt   time.Time `json:"created_at"`
}

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
