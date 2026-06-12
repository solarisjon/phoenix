// Package store defines repository interfaces for all Phoenix domain types.
// Implementations (e.g. SQLite) live in sub-packages.
package store

import (
	"context"
	"time"

	"github.com/solarisjon/phoenix/internal/model"
)

// UserRepo manages user records.
type UserRepo interface {
	Get(ctx context.Context, id string) (*model.User, error)
	GetDefault(ctx context.Context) (*model.User, error)
	Create(ctx context.Context, u *model.User) error
	Update(ctx context.Context, u *model.User) error
}

// ProviderRepo manages provider configurations.
type ProviderRepo interface {
	List(ctx context.Context) ([]*model.Provider, error)
	Get(ctx context.Context, id string) (*model.Provider, error)
	Create(ctx context.Context, p *model.Provider) error
	Update(ctx context.Context, p *model.Provider) error
	Delete(ctx context.Context, id string) error
}

// AgentRepo manages agent records.
type AgentRepo interface {
	List(ctx context.Context) ([]*model.Agent, error)
	Get(ctx context.Context, id string) (*model.Agent, error)
	Create(ctx context.Context, a *model.Agent) error
	Update(ctx context.Context, a *model.Agent) error
	Delete(ctx context.Context, id string) error
}

// ProjectRepo manages projects and project-agent assignments.
type ProjectRepo interface {
	List(ctx context.Context) ([]*model.Project, error)
	ListByKind(ctx context.Context, kind string) ([]*model.Project, error)
	// ListByStatus returns projects filtered by status ('active' or 'archived').
	// An empty status returns all projects regardless of status.
	ListByStatus(ctx context.Context, kind, status string) ([]*model.Project, error)
	Get(ctx context.Context, id string) (*model.Project, error)
	Create(ctx context.Context, p *model.Project) error
	Update(ctx context.Context, p *model.Project) error
	Delete(ctx context.Context, id string) error
	// DeleteWithTasks hard-deletes a project and all its tasks.
	DeleteWithTasks(ctx context.Context, id string) error

	AssignAgent(ctx context.Context, projectID, agentID string) (bool, error)
	IsAgentAssigned(ctx context.Context, projectID, agentID string) (bool, error)
	RemoveAgent(ctx context.Context, projectID, agentID string) error
	ListAgents(ctx context.Context, projectID string) ([]*model.Agent, error)
}

// TaskRepo manages task records.
type TaskRepo interface {
	List(ctx context.Context, projectID string) ([]*model.Task, error)
	// ListByProject is like List but supports optional status filter and row limit.
	// status="" means all statuses. limit<=0 means no limit.
	ListByProject(ctx context.Context, projectID string, status model.TaskStatus, limit int) ([]*model.Task, error)
	ListAll(ctx context.Context) ([]*model.Task, error)
	ListByStatus(ctx context.Context, status model.TaskStatus) ([]*model.Task, error)
	ListByStatuses(ctx context.Context, statuses []model.TaskStatus) ([]*model.Task, error)
	ListByAgent(ctx context.Context, agentID string) ([]*model.Task, error)
	Search(ctx context.Context, query string) ([]*model.Task, error)
	Get(ctx context.Context, id string) (*model.Task, error)
	Create(ctx context.Context, t *model.Task) error
	Update(ctx context.Context, t *model.Task) error
	Delete(ctx context.Context, id string) error

	// NextQueuedTask returns the oldest queued task for the given agent, or nil if none.
	NextQueuedTask(ctx context.Context, agentID string) (*model.Task, error)
	// CancelQueuedTask atomically sets a task to failed only if it is still queued.
	// Returns true if the task was cancelled, false if it was already in another state.
	CancelQueuedTask(ctx context.Context, taskID string) (bool, error)
	// ListCompletedForInbox returns up to limit recently completed, undismissed tasks, newest first.
	ListCompletedForInbox(ctx context.Context, limit int) ([]*model.Task, error)
}

// TeamRepo manages agent teams.
type TeamRepo interface {
	List(ctx context.Context) ([]*model.Team, error)
	Get(ctx context.Context, id string) (*model.Team, error)
	Create(ctx context.Context, t *model.Team) error
	Update(ctx context.Context, t *model.Team) error
	Delete(ctx context.Context, id string) error
	AddAgent(ctx context.Context, teamID, agentID string) error
	RemoveAgent(ctx context.Context, teamID, agentID string) error
	ListAgents(ctx context.Context, teamID string) ([]*model.Agent, error)
}

// CostSummary holds aggregated cost and token data.
type CostSummary struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Total     float64 `json:"total_cost_usd"`
	TaskCount int     `json:"task_count"`
	TokensIn  int     `json:"tokens_in"`
	TokensOut int     `json:"tokens_out"`
}

// UsageSummary holds aggregated usage by provider or model.
type UsageSummary struct {
	Label     string  `json:"label"`      // provider name or model string
	Total     float64 `json:"total_cost_usd"`
	TaskCount int     `json:"task_count"`
	TokensIn  int     `json:"tokens_in"`
	TokensOut int     `json:"tokens_out"`
}

// DailyCost holds the total cost and tokens for a single calendar day.
type DailyCost struct {
	Date      string  `json:"date"` // YYYY-MM-DD
	Cost      float64 `json:"cost_usd"`
	TokensIn  int     `json:"tokens_in"`
	TokensOut int     `json:"tokens_out"`
}

// TotalUsage holds cluster-wide token and cost totals.
type TotalUsage struct {
	CostUSD   float64 `json:"total_cost_usd"`
	TokensIn  int     `json:"total_tokens_in"`
	TokensOut int     `json:"total_tokens_out"`
}

// TaskCountByStatus holds task counts grouped by status.
type TaskCountByStatus struct {
	Status string `json:"status"`
	Count  int    `json:"count"`
}

// AgentDraftRepo manages pending agent hire proposals.
type AgentDraftRepo interface {
	List(ctx context.Context) ([]*model.AgentDraft, error)
	Get(ctx context.Context, id string) (*model.AgentDraft, error)
	Create(ctx context.Context, d *model.AgentDraft) error
	Update(ctx context.Context, d *model.AgentDraft) error
	Dismiss(ctx context.Context, id string) error
}

// SystemSettingsRepo manages platform-wide settings stored as key/value pairs.
type SystemSettingsRepo interface {
	Get(ctx context.Context) (*model.SystemSettings, error)
	Save(ctx context.Context, s *model.SystemSettings) error
}

// MemoRepo manages briefing memos.
type MemoRepo interface {
	// List returns memos filtered by status. Empty string = all non-archived.
	List(ctx context.Context, status string) ([]*model.Memo, error)
	Get(ctx context.Context, id string) (*model.Memo, error)
	Create(ctx context.Context, m *model.Memo) error
	UpdateStatus(ctx context.Context, id string, status model.MemoStatus) error
	Delete(ctx context.Context, id string) error
	// UnreadCount returns the count of unread + flagged memos for the sidebar badge.
	UnreadCount(ctx context.Context) (int, error)
}

// ProjectSummary holds aggregated task counts and cost for a single project.
type ProjectSummary struct {
	TasksByStatus map[string]int `json:"tasks_by_status"`
	TotalTasks    int            `json:"total_tasks"`
	TotalCostUSD  float64        `json:"total_cost_usd"`
	LastActivity  *time.Time     `json:"last_activity"`
}

// StatsRepo provides aggregated cost and usage queries.
type StatsRepo interface {
	CostByAgent(ctx context.Context) ([]*CostSummary, error)
	CostByProject(ctx context.Context) ([]*CostSummary, error)
	TotalUsage(ctx context.Context) (*TotalUsage, error)
	CostByDay(ctx context.Context, days int) ([]*DailyCost, error)
	TaskCountByStatus(ctx context.Context) ([]*TaskCountByStatus, error)
	TotalTaskCount(ctx context.Context) (int, error)
	UsageByProvider(ctx context.Context) ([]*UsageSummary, error)
	UsageByModel(ctx context.Context) ([]*UsageSummary, error)
	ProjectTaskSummary(ctx context.Context, projectID string) (*ProjectSummary, error)
	// AllProjectTaskSummaries returns a map of project ID → task summary for
	// all projects that have at least one task. Projects with no tasks are omitted.
	AllProjectTaskSummaries(ctx context.Context) (map[string]*ProjectSummary, error)
}
