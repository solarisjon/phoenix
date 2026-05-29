package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/solarisjon/phoenix/internal/model"
)

// listAgentDrafts returns all pending (non-dismissed) agent hire drafts.
func (s *Server) listAgentDrafts(w http.ResponseWriter, r *http.Request) {
	drafts, err := s.agentDrafts.List(r.Context())
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if drafts == nil {
		drafts = []*model.AgentDraft{}
	}
	respond(w, http.StatusOK, drafts)
}

// createAgentDraft is called by a hiring agent (via HTTP from within its task)
// to submit a new agent hire proposal for human review.
func (s *Server) createAgentDraft(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CreatedByAgentID string `json:"created_by_agent_id"`
		CreatedByTaskID  string `json:"created_by_task_id"`
		Name             string `json:"name"`
		Persona          string `json:"persona"`
		Instructions     string `json:"instructions"`
		Guardrails       string `json:"guardrails"`
	}
	if !decode(w, r, &req) {
		return
	}

	// Validate required fields.
	if strings.TrimSpace(req.CreatedByAgentID) == "" {
		respondErr(w, http.StatusBadRequest, "created_by_agent_id is required")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		respondErr(w, http.StatusBadRequest, "name is required")
		return
	}
	if strings.TrimSpace(req.Persona) == "" {
		respondErr(w, http.StatusBadRequest, "persona is required")
		return
	}
	if strings.TrimSpace(req.Instructions) == "" {
		respondErr(w, http.StatusBadRequest, "instructions is required")
		return
	}

	// Verify hiring agent exists and has permission.
	hiringAgent, err := s.agents.Get(r.Context(), req.CreatedByAgentID)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if hiringAgent == nil {
		respondErr(w, http.StatusBadRequest, "hiring agent not found")
		return
	}
	if !hiringAgent.CanHireAgents {
		respondErr(w, http.StatusForbidden, "agent does not have permission to hire new agents (can_hire_agents must be enabled)")
		return
	}

	// Inherit provider from the hiring agent.
	providerID := hiringAgent.ProviderID

	// Build draft.
	d := &model.AgentDraft{
		ID:               uuid.New().String(),
		CreatedByAgentID: req.CreatedByAgentID,
		Name:             strings.TrimSpace(req.Name),
		Persona:          strings.TrimSpace(req.Persona),
		Instructions:     strings.TrimSpace(req.Instructions),
		Guardrails:       strings.TrimSpace(req.Guardrails),
		ProviderID:       providerID,
		Status:           model.AgentDraftPending,
		CreatedAt:        time.Now(),
	}
	if tid := strings.TrimSpace(req.CreatedByTaskID); tid != "" {
		d.CreatedByTaskID = &tid
	}

	if err := s.agentDrafts.Create(r.Context(), d); err != nil {
		respondInternalErr(w, err)
		return
	}

	// Broadcast to connected clients so the inbox badge updates immediately.
	s.hub.Broadcast(Event{
		Type:    EventAgentDraftCreated,
		Payload: d,
	})

	respond(w, http.StatusCreated, d)
}

// updateAgentDraft allows a human to edit the draft fields before approving.
func (s *Server) updateAgentDraft(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	draft, err := s.agentDrafts.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if draft == nil {
		respondErr(w, http.StatusNotFound, "draft not found")
		return
	}
	if draft.Status != model.AgentDraftPending {
		respondErr(w, http.StatusConflict, "only pending drafts can be edited")
		return
	}

	var req struct {
		Name         string `json:"name"`
		Persona      string `json:"persona"`
		Instructions string `json:"instructions"`
		Guardrails   string `json:"guardrails"`
		ProviderID   string `json:"provider_id"`
	}
	if !decode(w, r, &req) {
		return
	}

	if strings.TrimSpace(req.Name) != "" {
		draft.Name = strings.TrimSpace(req.Name)
	}
	if req.Persona != "" {
		draft.Persona = req.Persona
	}
	if req.Instructions != "" {
		draft.Instructions = req.Instructions
	}
	if req.Guardrails != "" {
		draft.Guardrails = req.Guardrails
	}
	if strings.TrimSpace(req.ProviderID) != "" {
		draft.ProviderID = strings.TrimSpace(req.ProviderID)
	}

	if err := s.agentDrafts.Update(r.Context(), draft); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusOK, draft)
}

// approveAgentDraft creates a real agent from the draft and marks it approved.
func (s *Server) approveAgentDraft(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	draft, err := s.agentDrafts.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if draft == nil {
		respondErr(w, http.StatusNotFound, "draft not found")
		return
	}
	if draft.Status != model.AgentDraftPending {
		respondErr(w, http.StatusConflict, fmt.Sprintf("draft is already %s", draft.Status))
		return
	}

	// Allow provider override at approval time.
	var req struct {
		ProviderID string `json:"provider_id"`
	}
	_ = decode(w, r, &req) // optional body — ignore decode errors
	providerID := draft.ProviderID
	if strings.TrimSpace(req.ProviderID) != "" {
		providerID = strings.TrimSpace(req.ProviderID)
	}

	// Verify provider exists.
	prov, err := s.providers.Get(r.Context(), providerID)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if prov == nil {
		respondErr(w, http.StatusBadRequest, "provider not found")
		return
	}

	// Create the agent — identical to a human creating it via the UI.
	newAgent := &model.Agent{
		ID:             uuid.New().String(),
		Name:           draft.Name,
		Persona:        draft.Persona,
		Instructions:   draft.Instructions,
		Guardrails:     draft.Guardrails,
		ProviderID:     providerID,
		CanSpawnAgents: false,
		CanHireAgents:  false,
		CreatedBy:      fmt.Sprintf("agent:%s", draft.CreatedByAgentID),
		Status:         model.AgentStatusActive,
		CreatedAt:      time.Now(),
	}
	if err := s.agents.Create(r.Context(), newAgent); err != nil {
		respondInternalErr(w, err)
		return
	}

	// Mark draft approved + dismissed.
	draft.Status = model.AgentDraftApproved
	draft.Dismissed = true
	draft.ProviderID = providerID
	if err := s.agentDrafts.Update(r.Context(), draft); err != nil {
		respondInternalErr(w, err)
		return
	}

	respond(w, http.StatusCreated, newAgent)
}

// rejectAgentDraft marks a draft rejected and dismissed.
func (s *Server) rejectAgentDraft(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	draft, err := s.agentDrafts.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if draft == nil {
		respondErr(w, http.StatusNotFound, "draft not found")
		return
	}
	if draft.Status != model.AgentDraftPending {
		respondErr(w, http.StatusConflict, fmt.Sprintf("draft is already %s", draft.Status))
		return
	}

	draft.Status = model.AgentDraftRejected
	draft.Dismissed = true
	if err := s.agentDrafts.Update(r.Context(), draft); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusNoContent, nil)
}

// dismissAgentDraft hides a draft from the inbox without changing its status.
func (s *Server) dismissAgentDraft(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.agentDrafts.Dismiss(r.Context(), id); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusNoContent, nil)
}
