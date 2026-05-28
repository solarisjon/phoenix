package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/solarisjon/phoenix/internal/model"
)

type createAgentRequest struct {
	Name              string `json:"name"`
	Persona           string `json:"persona"`
	Instructions      string `json:"instructions"`
	Guardrails        string `json:"guardrails"`
	ProviderID        string `json:"provider_id"`
	HeartbeatInterval *int   `json:"heartbeat_interval"`
	Status            string `json:"status"`
}

func (r createAgentRequest) validate() string {
	if strings.TrimSpace(r.Name) == "" {
		return "name is required"
	}
	if strings.TrimSpace(r.ProviderID) == "" {
		return "provider_id is required"
	}
	if r.Status != "" &&
		r.Status != string(model.AgentStatusActive) &&
		r.Status != string(model.AgentStatusPaused) &&
		r.Status != string(model.AgentStatusDisabled) {
		return "status must be 'active', 'paused', or 'disabled'"
	}
	return ""
}

func (s *Server) listAgents(w http.ResponseWriter, r *http.Request) {
	list, err := s.agents.List(r.Context())
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if list == nil {
		list = []*model.Agent{}
	}
	respond(w, http.StatusOK, list)
}

func (s *Server) getAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	a, err := s.agents.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if a == nil {
		respondErr(w, http.StatusNotFound, "agent not found")
		return
	}
	respond(w, http.StatusOK, a)
}

func (s *Server) createAgent(w http.ResponseWriter, r *http.Request) {
	var req createAgentRequest
	if !decode(w, r, &req) {
		return
	}
	if msg := req.validate(); msg != "" {
		respondErr(w, http.StatusBadRequest, msg)
		return
	}

	// Verify provider exists.
	p, err := s.providers.Get(r.Context(), req.ProviderID)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if p == nil {
		respondErr(w, http.StatusBadRequest, "provider not found")
		return
	}

	user, err := s.users.GetDefault(r.Context())
	if err != nil || user == nil {
		respondInternalErr(w, err)
		return
	}

	status := model.AgentStatusActive
	if req.Status != "" {
		status = model.AgentStatus(req.Status)
	}

	a := &model.Agent{
		ID:                uuid.New().String(),
		Name:              strings.TrimSpace(req.Name),
		Persona:           req.Persona,
		Instructions:      req.Instructions,
		Guardrails:        req.Guardrails,
		ProviderID:        req.ProviderID,
		HeartbeatInterval: req.HeartbeatInterval,
		CreatedBy:         user.ID,
		Status:            status,
		CreatedAt:         time.Now(),
	}
	if err := s.agents.Create(r.Context(), a); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusCreated, a)
}

func (s *Server) updateAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := s.agents.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if existing == nil {
		respondErr(w, http.StatusNotFound, "agent not found")
		return
	}

	var req createAgentRequest
	if !decode(w, r, &req) {
		return
	}
	if msg := req.validate(); msg != "" {
		respondErr(w, http.StatusBadRequest, msg)
		return
	}

	// Verify new provider exists if changed.
	if req.ProviderID != existing.ProviderID {
		p, err := s.providers.Get(r.Context(), req.ProviderID)
		if err != nil {
			respondInternalErr(w, err)
			return
		}
		if p == nil {
			respondErr(w, http.StatusBadRequest, "provider not found")
			return
		}
	}

	existing.Name = strings.TrimSpace(req.Name)
	existing.Persona = req.Persona
	existing.Instructions = req.Instructions
	existing.Guardrails = req.Guardrails
	existing.ProviderID = req.ProviderID
	existing.HeartbeatInterval = req.HeartbeatInterval
	if req.Status != "" {
		existing.Status = model.AgentStatus(req.Status)
	}

	if err := s.agents.Update(r.Context(), existing); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusOK, existing)
}

func (s *Server) deleteAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := s.agents.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if existing == nil {
		respondErr(w, http.StatusNotFound, "agent not found")
		return
	}
	if err := s.agents.Delete(r.Context(), id); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusNoContent, nil)
}
