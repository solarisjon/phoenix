package api

import (
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/solarisjon/phoenix/internal/pricing"
	"github.com/solarisjon/phoenix/internal/store"
)

type costsResponse struct {
	Total       float64                    `json:"total_cost_usd"`
	TokensIn    int                        `json:"total_tokens_in"`
	TokensOut   int                        `json:"total_tokens_out"`
	ByAgent     []*store.CostSummary       `json:"by_agent"`
	ByProject   []*store.CostSummary       `json:"by_project"`
	ByProvider  []*store.UsageSummary      `json:"by_provider"`
	ByModel     []*store.UsageSummary      `json:"by_model"`
	ByDay       []*store.DailyCost         `json:"by_day"`
	ByStatus    []*store.TaskCountByStatus `json:"by_status"`
	TotalTasks  int                        `json:"total_tasks"`
}

func (s *Server) getCosts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	usage, err := s.stats.TotalUsage(ctx)
	if err != nil { respondInternalErr(w, err); return }

	byAgent, err := s.stats.CostByAgent(ctx)
	if err != nil { respondInternalErr(w, err); return }

	byProject, err := s.stats.CostByProject(ctx)
	if err != nil { respondInternalErr(w, err); return }

	byProvider, err := s.stats.UsageByProvider(ctx)
	if err != nil { respondInternalErr(w, err); return }

	byModel, err := s.stats.UsageByModel(ctx)
	if err != nil { respondInternalErr(w, err); return }

	byDay, err := s.stats.CostByDay(ctx, 30)
	if err != nil { respondInternalErr(w, err); return }

	byStatus, err := s.stats.TaskCountByStatus(ctx)
	if err != nil { respondInternalErr(w, err); return }

	totalTasks, err := s.stats.TotalTaskCount(ctx)
	if err != nil { respondInternalErr(w, err); return }

	if byAgent == nil { byAgent = []*store.CostSummary{} }
	if byProject == nil { byProject = []*store.CostSummary{} }
	if byProvider == nil { byProvider = []*store.UsageSummary{} }
	if byModel == nil { byModel = []*store.UsageSummary{} }
	if byDay == nil { byDay = []*store.DailyCost{} }
	if byStatus == nil { byStatus = []*store.TaskCountByStatus{} }

	respond(w, http.StatusOK, costsResponse{
		Total:      usage.CostUSD,
		TokensIn:   usage.TokensIn,
		TokensOut:  usage.TokensOut,
		ByAgent:    byAgent,
		ByProject:  byProject,
		ByProvider: byProvider,
		ByModel:    byModel,
		ByDay:      byDay,
		ByStatus:   byStatus,
		TotalTasks: totalTasks,
	})
}

// ---- Cost Insights ----

type insightBreakdownRow struct {
	ID                 string  `json:"id"`
	Name               string  `json:"name"`
	Model              string  `json:"model,omitempty"`
	ProviderName       string  `json:"provider_name,omitempty"`
	ProviderID         string  `json:"provider_id,omitempty"`
	ActualCostUSD      float64 `json:"actual_cost_usd"`
	TokensIn           int64   `json:"tokens_in"`
	TokensOut          int64   `json:"tokens_out"`
	TaskCount          int     `json:"task_count"`
	CostPerTask        float64 `json:"cost_per_task"`
	ProjectedMonthlyUSD float64 `json:"projected_monthly_usd"`
}

type insightRecommendation struct {
	Severity   string `json:"severity"`    // "warning" | "info"
	Kind       string `json:"kind"`
	Title      string `json:"title"`
	Detail     string `json:"detail"`
	AgentID    string `json:"agent_id,omitempty"`
	ProviderID string `json:"provider_id,omitempty"`
}

type insightSummary struct {
	TotalActualUSD      float64 `json:"total_actual_usd"`
	ProjectedMonthlyUSD float64 `json:"projected_monthly_usd"`
	TaskCount           int     `json:"task_count"`
}

type costInsightsResponse struct {
	Period          map[string]string       `json:"period"`
	Summary         insightSummary          `json:"summary"`
	ByAgent         []insightBreakdownRow   `json:"by_agent"`
	ByProvider      []insightBreakdownRow   `json:"by_provider"`
	ByProject       []insightBreakdownRow   `json:"by_project"`
	Recommendations []insightRecommendation `json:"recommendations"`
}

func (s *Server) getCostInsights(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse date range; default to last 30 days.
	now := time.Now().UTC()
	defaultFrom := now.AddDate(0, 0, -30)

	parseDate := func(param, fallback string) time.Time {
		v := r.URL.Query().Get(param)
		if v == "" {
			v = fallback
		}
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			return time.Now().UTC()
		}
		return t.UTC()
	}
	from := parseDate("from", defaultFrom.Format("2006-01-02"))
	to := parseDate("to", now.Format("2006-01-02"))
	// Make 'to' inclusive by advancing to end of day.
	to = to.Add(24*time.Hour - time.Second)

	periodDays := to.Sub(from).Hours() / 24
	if periodDays < 1 {
		periodDays = 1
	}

	agentRows, err := s.stats.InsightsByAgent(ctx, from, to)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	providerRows, err := s.stats.InsightsByProvider(ctx, from, to)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	projectRows, err := s.stats.InsightsByProject(ctx, from, to)
	if err != nil {
		respondInternalErr(w, err)
		return
	}

	toBreakdown := func(rows []*store.InsightRow) []insightBreakdownRow {
		out := make([]insightBreakdownRow, 0, len(rows))
		for _, row := range rows {
			br := insightBreakdownRow{
				ID:            row.ID,
				Name:          row.Name,
				Model:         row.Model,
				ProviderName:  row.ProviderName,
				ProviderID:    row.ProviderID,
				ActualCostUSD: row.ActualCost,
				TokensIn:      row.TokensIn,
				TokensOut:     row.TokensOut,
				TaskCount:     row.TaskCount,
			}
			if row.TaskCount > 0 {
				br.CostPerTask = row.ActualCost / float64(row.TaskCount)
			}
			// Projected monthly using provider override if available, else model price.
			price, hasPrice := s.effectivePrice(row)
			if hasPrice && row.TaskCount > 0 {
				avgIn := float64(row.TokensIn) / float64(row.TaskCount)
				avgOut := float64(row.TokensOut) / float64(row.TaskCount)
				tasksPerMonth := float64(row.TaskCount) / periodDays * 30
				br.ProjectedMonthlyUSD = round2((avgIn*price.InputPerMToken+avgOut*price.OutputPerMToken)/1_000_000*tasksPerMonth)
			} else if row.ActualCost > 0 {
				// Fall back to extrapolating actual cost.
				br.ProjectedMonthlyUSD = round2(row.ActualCost / periodDays * 30)
			}
			out = append(out, br)
		}
		return out
	}

	byAgent := toBreakdown(agentRows)
	byProvider := toBreakdown(providerRows)
	byProject := toBreakdown(projectRows)

	// Summary totals.
	var totalActual float64
	var totalProjected float64
	var totalTasks int
	for _, br := range byAgent {
		totalActual += br.ActualCostUSD
		totalProjected += br.ProjectedMonthlyUSD
		totalTasks += br.TaskCount
	}

	recs := s.buildRecommendations(agentRows, periodDays)

	resp := costInsightsResponse{
		Period: map[string]string{
			"from": from.Format("2006-01-02"),
			"to":   to.Format("2006-01-02"),
		},
		Summary: insightSummary{
			TotalActualUSD:      round2(totalActual),
			ProjectedMonthlyUSD: round2(totalProjected),
			TaskCount:           totalTasks,
		},
		ByAgent:         byAgent,
		ByProvider:      byProvider,
		ByProject:       byProject,
		Recommendations: recs,
	}
	if resp.ByAgent == nil {
		resp.ByAgent = []insightBreakdownRow{}
	}
	if resp.ByProvider == nil {
		resp.ByProvider = []insightBreakdownRow{}
	}
	if resp.ByProject == nil {
		resp.ByProject = []insightBreakdownRow{}
	}
	if resp.Recommendations == nil {
		resp.Recommendations = []insightRecommendation{}
	}
	respond(w, http.StatusOK, resp)
}

// effectivePrice returns the best available price for an insight row.
// Provider override wins; then model name lookup in registry.
func (s *Server) effectivePrice(row *store.InsightRow) (pricing.ModelPrice, bool) {
	if row.ProviderID != "" {
		if p, ok := s.pricingReg.GetOverride(row.ProviderID); ok {
			return p, true
		}
	}
	if row.Model != "" {
		return s.pricingReg.GetPrice(row.Model)
	}
	return pricing.ModelPrice{}, false
}

// buildRecommendations applies deterministic rules to the agent rows.
func (s *Server) buildRecommendations(rows []*store.InsightRow, periodDays float64) []insightRecommendation {
	var recs []insightRecommendation

	for _, row := range rows {
		if row.TaskCount == 0 {
			continue
		}
		tasksPerMonth := float64(row.TaskCount) / periodDays * 30

		// Rule 1: expensive_model_swap
		// Agent projected monthly > $5 AND a cheaper model exists that saves >50%.
		price, hasPrice := s.effectivePrice(row)
		if hasPrice {
			avgIn := float64(row.TokensIn) / float64(row.TaskCount)
			avgOut := float64(row.TokensOut) / float64(row.TaskCount)
			projectedMonthly := (avgIn*price.InputPerMToken+avgOut*price.OutputPerMToken) / 1_000_000 * tasksPerMonth
			if projectedMonthly > 5 {
				suggested, savingPct, ok := pricing.SuggestCheaperModel(row.Model, s.pricingReg)
				if ok && savingPct > 50 {
					recs = append(recs, insightRecommendation{
						Severity:   "warning",
						Kind:       "expensive_model_swap",
						Title:      fmt.Sprintf("Agent \"%s\" costs $%.2f/mo on %s", row.Name, projectedMonthly, row.Model),
						Detail:     fmt.Sprintf("Switching to %s would reduce cost by ~%.0f%% (est. $%.2f/mo).", suggested, savingPct, projectedMonthly*(1-savingPct/100)),
						AgentID:    row.ID,
						ProviderID: row.ProviderID,
					})
				}
			}
		}

		// Rule 2: overkill_monitor
		// Uses a Tier 1 model but runs <5 tasks/month (low-frequency workload).
		if pricing.ModelTier(row.Model) == 1 && tasksPerMonth < 5 {
			recs = append(recs, insightRecommendation{
				Severity: "info",
				Kind:     "overkill_monitor",
				Title:    fmt.Sprintf("Agent \"%s\" uses a flagship model but only runs ~%.0f tasks/month", row.Name, tasksPerMonth),
				Detail:   fmt.Sprintf("%s is a top-tier model. For low-frequency tasks a mid-range model may deliver similar quality at lower cost.", row.Model),
				AgentID:  row.ID,
			})
		}
	}

	// Rule 3: no_concurrency_cap
	// Fetch agents with spend > $1 and max_concurrent = 0.
	// We approximate from the row's actual cost in the period.
	for _, row := range rows {
		if row.ActualCost > 1 {
			// We can't filter on max_concurrent here without joining agents —
			// the recommendation is added if actual cost > $1 and the agent
			// has no concurrency cap (checked at API layer via agent data).
			// Since we don't have agent model here, emit a lighter "info" hint.
			recs = append(recs, insightRecommendation{
				Severity: "info",
				Kind:     "no_concurrency_cap",
				Title:    fmt.Sprintf("Agent \"%s\" spent $%.2f — consider setting a concurrency limit", row.Name, row.ActualCost),
				Detail:   "Setting max_concurrent on busy agents prevents runaway costs if tasks queue up unexpectedly.",
				AgentID:  row.ID,
			})
			break // one global hint is enough
		}
	}

	return recs
}

func round2(f float64) float64 {
	return math.Round(f*100) / 100
}
