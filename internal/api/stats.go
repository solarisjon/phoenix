package api

import (
	"net/http"

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
