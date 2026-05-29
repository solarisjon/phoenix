package sqlite

import (
	"context"
	"fmt"

	"github.com/solarisjon/phoenix/internal/store"
)

type StatsRepo struct{ db *DB }

func NewStatsRepo(db *DB) *StatsRepo { return &StatsRepo{db} }

func (r *StatsRepo) CostByAgent(ctx context.Context) ([]*store.CostSummary, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT a.id, a.name, COALESCE(SUM(t.cost_usd), 0)
		FROM agents a
		LEFT JOIN tasks t ON t.agent_id = a.id
		GROUP BY a.id, a.name
		ORDER BY SUM(t.cost_usd) DESC`)
	if err != nil {
		return nil, fmt.Errorf("cost by agent: %w", err)
	}
	defer rows.Close()
	return scanCostSummaries(rows)
}

func (r *StatsRepo) CostByProject(ctx context.Context) ([]*store.CostSummary, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT p.id, p.name, COALESCE(SUM(t.cost_usd), 0)
		FROM projects p
		LEFT JOIN tasks t ON t.project_id = p.id
		GROUP BY p.id, p.name
		ORDER BY SUM(t.cost_usd) DESC`)
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
		if err := rows.Scan(&s.ID, &s.Name, &s.Total); err != nil {
			return nil, fmt.Errorf("scan cost summary: %w", err)
		}
		out = append(out, &s)
	}
	return out, rows.Err()
}
