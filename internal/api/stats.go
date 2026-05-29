package api

import (
	"net/http"

	"github.com/solarisjon/phoenix/internal/store"
)

type costsResponse struct {
	Total      float64                   `json:"total_cost_usd"`
	ByAgent    []*store.CostSummary      `json:"by_agent"`
	ByProject  []*store.CostSummary      `json:"by_project"`
	ByDay      []*store.DailyCost        `json:"by_day"`
	ByStatus   []*store.TaskCountByStatus `json:"by_status"`
	TotalTasks int                       `json:"total_tasks"`
}

func (s *Server) getCosts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	total, err := s.stats.TotalCost(ctx)
	if err != nil { respondInternalErr(w, err); return }

	byAgent, err := s.stats.CostByAgent(ctx)
	if err != nil { respondInternalErr(w, err); return }

	byProject, err := s.stats.CostByProject(ctx)
	if err != nil { respondInternalErr(w, err); return }

	byDay, err := s.stats.CostByDay(ctx, 30)
	if err != nil { respondInternalErr(w, err); return }

	byStatus, err := s.stats.TaskCountByStatus(ctx)
	if err != nil { respondInternalErr(w, err); return }

	totalTasks, err := s.stats.TotalTaskCount(ctx)
	if err != nil { respondInternalErr(w, err); return }

	if byAgent == nil { byAgent = []*store.CostSummary{} }
	if byProject == nil { byProject = []*store.CostSummary{} }
	if byDay == nil { byDay = []*store.DailyCost{} }
	if byStatus == nil { byStatus = []*store.TaskCountByStatus{} }

	respond(w, http.StatusOK, costsResponse{
		Total:      total,
		ByAgent:    byAgent,
		ByProject:  byProject,
		ByDay:      byDay,
		ByStatus:   byStatus,
		TotalTasks: totalTasks,
	})
}
