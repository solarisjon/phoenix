// Package store defines repository interfaces for all Phoenix domain types.
// Implementations (e.g. SQLite) live in sub-packages.
package store

import (
	"context"

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
	Get(ctx context.Context, id string) (*model.Project, error)
	Create(ctx context.Context, p *model.Project) error
	Update(ctx context.Context, p *model.Project) error
	Delete(ctx context.Context, id string) error

	AssignAgent(ctx context.Context, projectID, agentID string) error
	RemoveAgent(ctx context.Context, projectID, agentID string) error
	ListAgents(ctx context.Context, projectID string) ([]*model.Agent, error)
}

// TaskRepo manages task records.
type TaskRepo interface {
	List(ctx context.Context, projectID string) ([]*model.Task, error)
	ListByStatus(ctx context.Context, status model.TaskStatus) ([]*model.Task, error)
	ListByStatuses(ctx context.Context, statuses []model.TaskStatus) ([]*model.Task, error)
	Get(ctx context.Context, id string) (*model.Task, error)
	Create(ctx context.Context, t *model.Task) error
	Update(ctx context.Context, t *model.Task) error
	Delete(ctx context.Context, id string) error
}

// CostSummary holds aggregated cost data.
type CostSummary struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Total float64 `json:"total_cost_usd"`
}

// StatsRepo provides aggregated cost queries.
type StatsRepo interface {
	CostByAgent(ctx context.Context) ([]*CostSummary, error)
	CostByProject(ctx context.Context) ([]*CostSummary, error)
	TotalCost(ctx context.Context) (float64, error)
}
