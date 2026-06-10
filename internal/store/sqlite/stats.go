package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/solarisjon/phoenix/internal/store"
)

type StatsRepo struct{ db *DB }

func NewStatsRepo(db *DB) *StatsRepo { return &StatsRepo{db} }

func (r *StatsRepo) CostByAgent(ctx context.Context) ([]*store.CostSummary, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT a.id, a.name, COALESCE(SUM(t.cost_usd), 0), COUNT(t.id)
		FROM agents a
		INNER JOIN tasks t ON t.agent_id = a.id
		GROUP BY a.id, a.name
		ORDER BY SUM(t.cost_usd) DESC, COUNT(t.id) DESC`)
	if err != nil {
		return nil, fmt.Errorf("cost by agent: %w", err)
	}
	defer rows.Close()
	return scanCostSummaries(rows)
}

func (r *StatsRepo) CostByProject(ctx context.Context) ([]*store.CostSummary, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT p.id, p.name, COALESCE(SUM(t.cost_usd), 0), COUNT(t.id)
		FROM projects p
		INNER JOIN tasks t ON t.project_id = p.id
		GROUP BY p.id, p.name
		ORDER BY SUM(t.cost_usd) DESC, COUNT(t.id) DESC`)
	if err != nil {
		return nil, fmt.Errorf("cost by project: %w", err)
	}
	defer rows.Close()
	return scanCostSummaries(rows)
}

func (r *StatsRepo) TotalCost(ctx context.Context) (float64, error) {
	var total float64
	err := r.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(cost_usd), 0) FROM tasks`).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("total cost: %w", err)
	}
	return total, nil
}

func (r *StatsRepo) CostByDay(ctx context.Context, days int) ([]*store.DailyCost, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT date(created_at) AS day, COALESCE(SUM(cost_usd), 0)
		FROM tasks
		WHERE created_at >= date('now', ?)
		  AND cost_usd > 0
		GROUP BY day
		ORDER BY day ASC`,
		fmt.Sprintf("-%d days", days))
	if err != nil {
		return nil, fmt.Errorf("cost by day: %w", err)
	}
	defer rows.Close()
	var out []*store.DailyCost
	for rows.Next() {
		var d store.DailyCost
		if err := rows.Scan(&d.Date, &d.Cost); err != nil {
			return nil, fmt.Errorf("scan daily cost: %w", err)
		}
		out = append(out, &d)
	}
	return out, rows.Err()
}

func (r *StatsRepo) TaskCountByStatus(ctx context.Context) ([]*store.TaskCountByStatus, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT status, COUNT(*) FROM tasks GROUP BY status ORDER BY COUNT(*) DESC`)
	if err != nil {
		return nil, fmt.Errorf("task count by status: %w", err)
	}
	defer rows.Close()
	var out []*store.TaskCountByStatus
	for rows.Next() {
		var s store.TaskCountByStatus
		if err := rows.Scan(&s.Status, &s.Count); err != nil {
			return nil, fmt.Errorf("scan task status count: %w", err)
		}
		out = append(out, &s)
	}
	return out, rows.Err()
}

func (r *StatsRepo) TotalTaskCount(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("total task count: %w", err)
	}
	return n, nil
}

func scanCostSummaries(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]*store.CostSummary, error) {
	var out []*store.CostSummary
	for rows.Next() {
		var s store.CostSummary
		if err := rows.Scan(&s.ID, &s.Name, &s.Total, &s.TaskCount); err != nil {
			return nil, fmt.Errorf("scan cost summary: %w", err)
		}
		out = append(out, &s)
	}
	return out, rows.Err()
}

// AllProjectTaskSummaries returns a map of project ID → summary for every
// project that has at least one task. Projects with no tasks are omitted.
func (r *StatsRepo) AllProjectTaskSummaries(ctx context.Context) (map[string]*store.ProjectSummary, error) {
	out := map[string]*store.ProjectSummary{}

	// Task counts grouped by (project_id, status).
	rows, err := r.db.QueryContext(ctx,
		`SELECT project_id, status, COUNT(*)
		 FROM tasks
		 WHERE project_id IS NOT NULL
		 GROUP BY project_id, status`)
	if err != nil {
		return nil, fmt.Errorf("all project task summaries: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var projectID, status string
		var count int
		if err := rows.Scan(&projectID, &status, &count); err != nil {
			return nil, fmt.Errorf("scan all project summaries: %w", err)
		}
		if _, ok := out[projectID]; !ok {
			out[projectID] = &store.ProjectSummary{TasksByStatus: map[string]int{}}
		}
		out[projectID].TasksByStatus[status] = count
		out[projectID].TotalTasks += count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Total cost and last activity per project.
	actRows, err := r.db.QueryContext(ctx,
		`SELECT project_id,
		        COALESCE(SUM(cost_usd), 0),
		        MAX(COALESCE(completed_at, started_at, created_at))
		 FROM tasks
		 WHERE project_id IS NOT NULL
		 GROUP BY project_id`)
	if err != nil {
		return nil, fmt.Errorf("all project summaries cost: %w", err)
	}
	defer actRows.Close()
	for actRows.Next() {
		var projectID string
		var cost float64
		var lastActivity sql.NullTime
		if err := actRows.Scan(&projectID, &cost, &lastActivity); err != nil {
			return nil, fmt.Errorf("scan all project summaries cost: %w", err)
		}
		if _, ok := out[projectID]; !ok {
			out[projectID] = &store.ProjectSummary{TasksByStatus: map[string]int{}}
		}
		out[projectID].TotalCostUSD = cost
		if lastActivity.Valid {
			t := lastActivity.Time
			out[projectID].LastActivity = &t
		}
	}
	return out, actRows.Err()
}

// ProjectTaskSummary returns task counts by status, total cost, and last
// activity time for the given project. It never returns nil (always an empty
// map on a project with no tasks).
func (r *StatsRepo) ProjectTaskSummary(ctx context.Context, projectID string) (*store.ProjectSummary, error) {
	summary := &store.ProjectSummary{TasksByStatus: map[string]int{}}

	rows, err := r.db.QueryContext(ctx,
		`SELECT status, COUNT(*) FROM tasks WHERE project_id = ? GROUP BY status`,
		projectID)
	if err != nil {
		return nil, fmt.Errorf("project task summary counts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("scan project task summary: %w", err)
		}
		summary.TasksByStatus[status] = count
		summary.TotalTasks += count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var lastActivity sql.NullTime
	err = r.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(cost_usd), 0),
		        MAX(COALESCE(completed_at, started_at, created_at))
		 FROM tasks WHERE project_id = ?`,
		projectID).Scan(&summary.TotalCostUSD, &lastActivity)
	if err != nil {
		return nil, fmt.Errorf("project task summary cost: %w", err)
	}
	if lastActivity.Valid {
		summary.LastActivity = &lastActivity.Time
	}
	return summary, nil
}
