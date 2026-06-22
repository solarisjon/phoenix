package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/plugin/notifiers"
	"github.com/solarisjon/phoenix/internal/plugin/notifiers/telegram"
)

// ---- Plugin CRUD ----

func (s *Server) listPlugins(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	typeFilter := r.URL.Query().Get("type")

	var plugins []*model.Plugin
	var err error
	if typeFilter != "" {
		plugins, err = s.pluginRepo.ListByType(ctx, model.PluginType(typeFilter))
	} else {
		plugins, err = s.pluginRepo.List(ctx)
	}
	if err != nil {
		respondErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if plugins == nil {
		plugins = []*model.Plugin{}
	}
	respond(w, http.StatusOK, plugins)
}

func (s *Server) getPlugin(w http.ResponseWriter, r *http.Request) {
	p, err := s.pluginRepo.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		respondErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if p == nil {
		respondErr(w, http.StatusNotFound, "plugin not found")
		return
	}
	respond(w, http.StatusOK, p)
}

func (s *Server) createPlugin(w http.ResponseWriter, r *http.Request) {
	var p model.Plugin
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		respondErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if p.ID == "" {
		p.ID = uuid.New().String()
	}
	p.IsCore = false // community plugins only
	p.CreatedAt = time.Now().UTC()
	p.UpdatedAt = p.CreatedAt

	// Validate notifier config if applicable.
	if p.Type == model.PluginTypeNotifier {
		n := notifiers.Get(p.Kind)
		if n != nil {
			if err := n.ValidateConfig(json.RawMessage(p.Config)); err != nil {
				respondErr(w, http.StatusBadRequest, err.Error())
				return
			}
		}
	}

	if err := s.pluginRepo.Create(r.Context(), &p); err != nil {
		respondErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	respond(w, http.StatusCreated, p)
}

func (s *Server) updatePlugin(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := s.pluginRepo.Get(r.Context(), id)
	if err != nil {
		respondErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if existing == nil {
		respondErr(w, http.StatusNotFound, "plugin not found")
		return
	}

	var update model.Plugin
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		respondErr(w, http.StatusBadRequest, err.Error())
		return
	}

	// Validate notifier config if applicable.
	if existing.Type == model.PluginTypeNotifier && update.Config != "" {
		n := notifiers.Get(existing.Kind)
		if n != nil {
			if err := n.ValidateConfig(json.RawMessage(update.Config)); err != nil {
				respondErr(w, http.StatusBadRequest, err.Error())
				return
			}
		}
	}

	// Apply updates to existing record.
	if update.Name != "" {
		existing.Name = update.Name
	}
	if update.Config != "" {
		existing.Config = update.Config
	}
	existing.Enabled = update.Enabled

	if err := s.pluginRepo.Update(r.Context(), existing); err != nil {
		respondErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	respond(w, http.StatusOK, existing)
}

func (s *Server) deletePlugin(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := s.pluginRepo.Get(r.Context(), id)
	if err != nil {
		respondErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if existing == nil {
		respondErr(w, http.StatusNotFound, "plugin not found")
		return
	}
	if existing.IsCore {
		respondErr(w, http.StatusForbidden, "cannot delete core plugins")
		return
	}

	if err := s.pluginRepo.Delete(r.Context(), id); err != nil {
		respondErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) enablePlugin(w http.ResponseWriter, r *http.Request) {
	s.setPluginEnabled(w, r, true)
}

func (s *Server) disablePlugin(w http.ResponseWriter, r *http.Request) {
	s.setPluginEnabled(w, r, false)
}

func (s *Server) setPluginEnabled(w http.ResponseWriter, r *http.Request, enabled bool) {
	id := chi.URLParam(r, "id")
	p, err := s.pluginRepo.Get(r.Context(), id)
	if err != nil {
		respondErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if p == nil {
		respondErr(w, http.StatusNotFound, "plugin not found")
		return
	}
	p.Enabled = enabled
	if err := s.pluginRepo.Update(r.Context(), p); err != nil {
		respondErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	respond(w, http.StatusOK, p)
}

func (s *Server) testPlugin(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.pluginManager.TestPlugin(r.Context(), id); err != nil {
		respondErr(w, http.StatusBadRequest, err.Error())
		return
	}
	respond(w, http.StatusOK, map[string]string{"status": "ok", "message": "Test notification sent"})
}

// ---- Notification Rules ----

func (s *Server) listPluginRules(w http.ResponseWriter, r *http.Request) {
	pluginID := chi.URLParam(r, "id")
	rules, err := s.ruleRepo.ListByPlugin(r.Context(), pluginID)
	if err != nil {
		respondErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if rules == nil {
		rules = []*model.NotificationRule{}
	}
	respond(w, http.StatusOK, rules)
}

func (s *Server) createPluginRule(w http.ResponseWriter, r *http.Request) {
	pluginID := chi.URLParam(r, "id")
	var rule model.NotificationRule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		respondErr(w, http.StatusBadRequest, err.Error())
		return
	}
	rule.PluginID = pluginID
	if rule.ID == "" {
		rule.ID = uuid.New().String()
	}
	rule.CreatedAt = time.Now().UTC()

	if err := s.ruleRepo.Create(r.Context(), &rule); err != nil {
		respondErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	respond(w, http.StatusCreated, rule)
}

func (s *Server) updatePluginRule(w http.ResponseWriter, r *http.Request) {
	ruleID := chi.URLParam(r, "rid")
	existing, err := s.ruleRepo.Get(r.Context(), ruleID)
	if err != nil {
		respondErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if existing == nil {
		respondErr(w, http.StatusNotFound, "rule not found")
		return
	}

	var update model.NotificationRule
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		respondErr(w, http.StatusBadRequest, err.Error())
		return
	}

	existing.EventType = update.EventType
	existing.ProjectID = update.ProjectID
	existing.Enabled = update.Enabled
	existing.Template = update.Template

	if err := s.ruleRepo.Update(r.Context(), existing); err != nil {
		respondErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	respond(w, http.StatusOK, existing)
}

func (s *Server) deletePluginRule(w http.ResponseWriter, r *http.Request) {
	ruleID := chi.URLParam(r, "rid")
	if err := s.ruleRepo.Delete(r.Context(), ruleID); err != nil {
		respondErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- Plugin Config Schema ----

func (s *Server) getPluginSchema(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, err := s.pluginRepo.Get(r.Context(), id)
	if err != nil {
		respondErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if p == nil {
		respondErr(w, http.StatusNotFound, "plugin not found")
		return
	}

	n := notifiers.Get(p.Kind)
	if n == nil {
		respondErr(w, http.StatusNotFound, "no schema available for this plugin kind")
		return
	}
	respond(w, http.StatusOK, n.ConfigSchema())
}

// ---- Telegram Chat Discovery ----

func (s *Server) discoverTelegramChats(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, err := s.pluginRepo.Get(r.Context(), id)
	if err != nil {
		respondErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if p == nil {
		respondErr(w, http.StatusNotFound, "plugin not found")
		return
	}
	if p.Kind != "telegram" {
		respondErr(w, http.StatusBadRequest, "chat discovery is only available for Telegram plugins")
		return
	}

	var cfg struct {
		BotToken string `json:"bot_token"`
	}
	if err := json.Unmarshal([]byte(p.Config), &cfg); err != nil || cfg.BotToken == "" {
		respondErr(w, http.StatusBadRequest, "configure a bot token first")
		return
	}

	chats, err := telegram.GetChats(cfg.BotToken)
	if err != nil {
		respondErr(w, http.StatusBadRequest, err.Error())
		return
	}
	respond(w, http.StatusOK, chats)
}
