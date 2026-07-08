package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/solarisjon/phoenix/internal/store"
)

type StatsRepo struct{ db *DB }

func NewStatsRepo(db *DB) *StatsRepo { return &StatsRepo{db} }

func (r *StatsRepo) CostByAgent(ctx context.Context) ([]*store.CostSummary, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT a.id, a.name,
		       COALESCE(SUM(t.cost_usd), 0),
		       COUNT(t.id),
		       COALESCE(SUM(t.tokens_in), 0),
		       COALESCE(SUM(t.tokens_out), 0)
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
		SELECT p.id, p.name,
		       COALESCE(SUM(t.cost_usd), 0),
		       COUNT(t.id),
		       COALESCE(SUM(t.tokens_in), 0),
		       COALESCE(SUM(t.tokens_out), 0)
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

func (r *StatsRepo) TotalUsage(ctx context.Context) (*store.TotalUsage, error) {
	var u store.TotalUsage
	err := r.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(cost_usd), 0),
		        COALESCE(SUM(tokens_in), 0),
		        COALESCE(SUM(tokens_out), 0)
		 FROM tasks`).Scan(&u.CostUSD, &u.TokensIn, &u.TokensOut)
	if err != nil {
		return nil, fmt.Errorf("total usage: %w", err)
	}
	return &u, nil
}

func (r *StatsRepo) UsageByProvider(ctx context.Context) ([]*store.UsageSummary, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT p.name,
		       COALESCE(SUM(t.cost_usd), 0),
		       COUNT(t.id),
		       COALESCE(SUM(t.tokens_in), 0),
		       COALESCE(SUM(t.tokens_out), 0)
		FROM tasks t
		JOIN agents a ON t.agent_id = a.id
		JOIN providers p ON a.provider_id = p.id
		GROUP BY p.id, p.name
		ORDER BY SUM(t.cost_usd) DESC, SUM(t.tokens_in) DESC`)
	if err != nil {
		return nil, fmt.Errorf("usage by provider: %w", err)
	}
	defer rows.Close()
	return scanUsageSummaries(rows)
}

func (r *StatsRepo) UsageByModel(ctx context.Context) ([]*store.UsageSummary, error) {
	// Effective model: agent.model_override → providers.config.model → provider name
	rows, err := r.db.QueryContext(ctx, `
		SELECT COALESCE(
		         NULLIF(a.model_override, ''),
		         NULLIF(json_extract(p.config, '$.model'), ''),
		         p.name
		       ) AS model,
		       COALESCE(SUM(t.cost_usd), 0),
		       COUNT(t.id),
		       COALESCE(SUM(t.tokens_in), 0),
		       COALESCE(SUM(t.tokens_out), 0)
		FROM tasks t
		JOIN agents a ON t.agent_id = a.id
		JOIN providers p ON a.provider_id = p.id
		GROUP BY model
		ORDER BY SUM(t.cost_usd) DESC, SUM(t.tokens_in) DESC`)
	if err != nil {
		return nil, fmt.Errorf("usage by model: %w", err)
	}
	defer rows.Close()
	return scanUsageSummaries(rows)
}

func (r *StatsRepo) CostByDay(ctx context.Context, days int) ([]*store.DailyCost, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT date(created_at) AS day,
		       COALESCE(SUM(cost_usd), 0),
		       COALESCE(SUM(tokens_in), 0),
		       COALESCE(SUM(tokens_out), 0)
		FROM tasks
		WHERE created_at >= date('now', ?)
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
		if err := rows.Scan(&d.Date, &d.Cost, &d.TokensIn, &d.TokensOut); err != nil {
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
		if err := rows.Scan(&s.ID, &s.Name, &s.Total, &s.TaskCount, &s.TokensIn, &s.TokensOut); err != nil {
			return nil, fmt.Errorf("scan cost summary: %w", err)
		}
		out = append(out, &s)
	}
	return out, rows.Err()
}

func scanUsageSummaries(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]*store.UsageSummary, error) {
	var out []*store.UsageSummary
	for rows.Next() {
		var s store.UsageSummary
		if err := rows.Scan(&s.Label, &s.Total, &s.TaskCount, &s.TokensIn, &s.TokensOut); err != nil {
			return nil, fmt.Errorf("scan usage summary: %w", err)
		}
		out = append(out, &s)
	}
	return out, rows.Err()
}

// InsightsByAgent returns per-agent cost/token aggregates for completed tasks
// in the given date range, ordered by actual cost descending.
func (r *StatsRepo) InsightsByAgent(ctx context.Context, from, to time.Time) ([]*store.InsightRow, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT
			a.id,
			a.name,
			COALESCE(NULLIF(a.model_override,''), NULLIF(json_extract(p.config,'$.model'),''), '') AS model,
			p.name AS provider_name,
			p.id   AS provider_id,
			COALESCE(SUM(t.cost_usd), 0),
			COALESCE(SUM(t.tokens_in), 0),
			COALESCE(SUM(t.tokens_out), 0),
			COUNT(t.id)
		FROM tasks t
		JOIN agents a ON t.agent_id = a.id
		JOIN providers p ON a.provider_id = p.id
		WHERE t.completed_at BETWEEN ? AND ?
		  AND t.dismissed = 0
		GROUP BY a.id
		ORDER BY SUM(t.cost_usd) DESC`,
		from.UTC().Format("2006-01-02 15:04:05"),
		to.UTC().Format("2006-01-02 15:04:05"))
	if err != nil {
		return nil, fmt.Errorf("insights by agent: %w", err)
	}
	defer rows.Close()
	return scanInsightRows(rows)
}

// InsightsByProvider returns per-provider cost/token aggregates.
func (r *StatsRepo) InsightsByProvider(ctx context.Context, from, to time.Time) ([]*store.InsightRow, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT
			p.id,
			p.name,
			COALESCE(NULLIF(json_extract(p.config,'$.model'),''), '') AS model,
			p.name AS provider_name,
			p.id   AS provider_id,
			COALESCE(SUM(t.cost_usd), 0),
			COALESCE(SUM(t.tokens_in), 0),
			COALESCE(SUM(t.tokens_out), 0),
			COUNT(t.id)
		FROM tasks t
		JOIN agents a ON t.agent_id = a.id
		JOIN providers p ON a.provider_id = p.id
		WHERE t.completed_at BETWEEN ? AND ?
		  AND t.dismissed = 0
		GROUP BY p.id
		ORDER BY SUM(t.cost_usd) DESC`,
		from.UTC().Format("2006-01-02 15:04:05"),
		to.UTC().Format("2006-01-02 15:04:05"))
	if err != nil {
		return nil, fmt.Errorf("insights by provider: %w", err)
	}
	defer rows.Close()
	return scanInsightRows(rows)
}

// InsightsByProject returns per-project cost/token aggregates.
func (r *StatsRepo) InsightsByProject(ctx context.Context, from, to time.Time) ([]*store.InsightRow, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT
			pr.id,
			pr.name,
			'' AS model,
			'' AS provider_name,
			'' AS provider_id,
			COALESCE(SUM(t.cost_usd), 0),
			COALESCE(SUM(t.tokens_in), 0),
			COALESCE(SUM(t.tokens_out), 0),
			COUNT(t.id)
		FROM tasks t
		JOIN projects pr ON t.project_id = pr.id
		WHERE t.completed_at BETWEEN ? AND ?
		  AND t.dismissed = 0
		GROUP BY pr.id
		ORDER BY SUM(t.cost_usd) DESC`,
		from.UTC().Format("2006-01-02 15:04:05"),
		to.UTC().Format("2006-01-02 15:04:05"))
	if err != nil {
		return nil, fmt.Errorf("insights by project: %w", err)
	}
	defer rows.Close()
	return scanInsightRows(rows)
}

func scanInsightRows(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]*store.InsightRow, error) {
	var out []*store.InsightRow
	for rows.Next() {
		var r store.InsightRow
		if err := rows.Scan(&r.ID, &r.Name, &r.Model, &r.ProviderName, &r.ProviderID,
			&r.ActualCost, &r.TokensIn, &r.TokensOut, &r.TaskCount); err != nil {
			return nil, fmt.Errorf("scan insight row: %w", err)
		}
		out = append(out, &r)
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
		var lastActivityStr sql.NullString
		if err := actRows.Scan(&projectID, &cost, &lastActivityStr); err != nil {
			return nil, fmt.Errorf("scan all project summaries cost: %w", err)
		}
		if _, ok := out[projectID]; !ok {
			out[projectID] = &store.ProjectSummary{TasksByStatus: map[string]int{}}
		}
		out[projectID].TotalCostUSD = cost
		if lastActivityStr.Valid {
			if t := parseNullableTime(lastActivityStr.String); t != nil {
				out[projectID].LastActivity = t
			}
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

	var lastActivityStr sql.NullString
	err = r.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(cost_usd), 0),
		        MAX(COALESCE(completed_at, started_at, created_at))
		 FROM tasks WHERE project_id = ?`,
		projectID).Scan(&summary.TotalCostUSD, &lastActivityStr)
	if err != nil {
		return nil, fmt.Errorf("project task summary cost: %w", err)
	}
	if lastActivityStr.Valid {
		summary.LastActivity = parseNullableTime(lastActivityStr.String)
	}
	return summary, nil
}

// parseNullableTime parses a datetime string returned by SQLite aggregate
// expressions (e.g. MAX(COALESCE(...))). SQLite returns these as raw strings
// rather than typed time values, so sql.NullTime cannot scan them directly.
func parseNullableTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	// Try formats in order of likelihood: Go's default time.String() format,
	// then RFC3339 variants, then plain SQLite datetime.
	formats := []string{
		"2006-01-02 15:04:05.999999999 -0700 MST",
		"2006-01-02 15:04:05 -0700 MST",
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return &t
		}
	}
	return nil
}
