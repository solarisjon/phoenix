package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/provider"
)

type createProviderRequest struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Config string `json:"config"`
}

func (r createProviderRequest) validate() string {
	if strings.TrimSpace(r.Name) == "" {
		return "name is required"
	}
	if r.Type != string(model.ProviderTypeLLM) && r.Type != string(model.ProviderTypeCodingAgent) {
		return "type must be 'llm' or 'coding_agent'"
	}
	return ""
}

func (s *Server) listProviders(w http.ResponseWriter, r *http.Request) {
	list, err := s.providers.List(r.Context())
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if list == nil {
		list = []*model.Provider{}
	}
	respond(w, http.StatusOK, list)
}

func (s *Server) getProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, err := s.providers.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if p == nil {
		respondErr(w, http.StatusNotFound, "provider not found")
		return
	}
	respond(w, http.StatusOK, p)
}

func (s *Server) createProvider(w http.ResponseWriter, r *http.Request) {
	var req createProviderRequest
	if !decode(w, r, &req) {
		return
	}
	if msg := req.validate(); msg != "" {
		respondErr(w, http.StatusBadRequest, msg)
		return
	}

	user, err := s.users.GetDefault(r.Context())
	if err != nil || user == nil {
		respondInternalErr(w, err)
		return
	}

	config := req.Config
	if config == "" {
		config = "{}"
	}

	p := &model.Provider{
		ID:        uuid.New().String(),
		Name:      strings.TrimSpace(req.Name),
		Type:      model.ProviderType(req.Type),
		Config:    config,
		CreatedBy: user.ID,
		CreatedAt: time.Now(),
	}
	if err := s.providers.Create(r.Context(), p); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusCreated, p)
}

func (s *Server) updateProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := s.providers.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if existing == nil {
		respondErr(w, http.StatusNotFound, "provider not found")
		return
	}

	var req createProviderRequest
	if !decode(w, r, &req) {
		return
	}
	if msg := req.validate(); msg != "" {
		respondErr(w, http.StatusBadRequest, msg)
		return
	}

	existing.Name = strings.TrimSpace(req.Name)
	existing.Type = model.ProviderType(req.Type)
	if req.Config != "" {
		existing.Config = req.Config
	}

	if err := s.providers.Update(r.Context(), existing); err != nil {
		respondInternalErr(w, err)
		return
	}

	// Invalidate the registry cache so next execution picks up new config.
	s.registry.Invalidate(id)

	respond(w, http.StatusOK, existing)
}

// listProviderModels calls the provider's ListModels() if it supports it,
// and returns {"models":[...]} or {"supported":false}.
func (s *Server) listProviderModels(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, err := s.providers.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if p == nil {
		respondErr(w, http.StatusNotFound, "provider not found")
		return
	}

	prov, err := s.registry.Get(r.Context(), id)
	if err != nil {
		respondErr(w, http.StatusBadRequest, "could not build provider: "+err.Error())
		return
	}

	lister, ok := prov.(provider.ModelLister)
	if !ok {
		respond(w, http.StatusOK, map[string]any{"supported": false, "models": []string{}})
		return
	}

	models, err := lister.ListModels(r.Context())
	if err != nil {
		// Return partial failure as a soft error — don't 500, let UI show free-text fallback
		respond(w, http.StatusOK, map[string]any{"supported": true, "error": err.Error(), "models": []string{}})
		return
	}

	respond(w, http.StatusOK, map[string]any{"supported": true, "models": models})
}

func (s *Server) resyncProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := s.providers.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if existing == nil {
		respondErr(w, http.StatusNotFound, "provider not found")
		return
	}
	s.registry.Invalidate(id)
	respond(w, http.StatusOK, map[string]string{"status": "ok", "message": "provider cache cleared — next task will reload config from DB"})
}

func (s *Server) deleteProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := s.providers.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if existing == nil {
		respondErr(w, http.StatusNotFound, "provider not found")
		return
	}
	if err := s.providers.Delete(context.Background(), id); err != nil {
		respondInternalErr(w, err)
		return
	}
	s.registry.Invalidate(id)
	respond(w, http.StatusNoContent, nil)
}
