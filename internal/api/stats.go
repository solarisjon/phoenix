package api

import (
	"net/http"

	"github.com/solarisjon/phoenix/internal/store"
)

type costsResponse struct {
	Total     float64              `json:"total_cost_usd"`
	ByAgent   []*store.CostSummary `json:"by_agent"`
	ByProject []*store.CostSummary `json:"by_project"`
}

func (s *Server) getCosts(w http.ResponseWriter, r *http.Request) {
	total, err := s.stats.TotalCost(r.Context())
	if err != nil {
		respondInternalErr(w, err)
		return
	}

	byAgent, err := s.stats.CostByAgent(r.Context())
	if err != nil {
		respondInternalErr(w, err)
		return
	}

	byProject, err := s.stats.CostByProject(r.Context())
	if err != nil {
		respondInternalErr(w, err)
		return
	}

	if byAgent == nil {
		byAgent = []*store.CostSummary{}
	}
	if byProject == nil {
		byProject = []*store.CostSummary{}
	}

	respond(w, http.StatusOK, costsResponse{
		Total:     total,
		ByAgent:   byAgent,
		ByProject: byProject,
	})
}
