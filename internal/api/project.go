package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/provider"
)

type createProjectRequest struct {
	Name                 string   `json:"name"`
	Objective            string   `json:"objective"`
	WorkingDir           string   `json:"working_dir"`
	Kind                 string   `json:"kind"`
	Status               string   `json:"status"`
	ScheduleInterval     *int     `json:"schedule_interval"`     // seconds; nil = no schedule (monitors only)
	ScheduleKind         string   `json:"schedule_kind"`         // "interval" | "daily"
	ScheduleTimes        []string `json:"schedule_times"`        // ["07:00","12:00"] when schedule_kind == "daily"
	ScheduleCatchUp      bool     `json:"schedule_catch_up"`     // daily only: catch up a missed run same calendar day
	CriticAgentID        *string  `json:"critic_agent_id"`       // deprecated: prefer critic_mode
	CriticMode           string   `json:"critic_mode"`           // "none" | "builtin" | "agent:<id>"
	MonitorModel         string   `json:"monitor_model"`         // if set, overrides the agent's model for monitor tasks
	BudgetUSD            float64  `json:"budget_usd"`            // 0 = no limit
	BudgetPeriod         string   `json:"budget_period"`         // "day" | "week" | "month" | "total"
	ContextSummarisation bool     `json:"context_summarisation"` // opt-in: summarise long follow-up chains
	Tags                 []string `json:"tags"`

	// Heartbeat reaction (monitors only)
	HeartbeatOnAttention   string  `json:"heartbeat_on_attention"`   // "" | "spawn" | "notify" | "escalate"
	HeartbeatOnFailed      string  `json:"heartbeat_on_failed"`      // same options
	LinkedProjectID        *string `json:"linked_project_id"`        // project to spawn remediation tasks in
	HeartbeatEscalateAfter int     `json:"heartbeat_escalate_after"` // N consecutive bad before escalate fires; 0 = immediately
	MonitorCacheTTL        int     `json:"monitor_cache_ttl"`        // seconds; 0 = cache indefinitely

	// ReAct autonomous loop
	ReactMode     bool `json:"react_mode"`
	MaxIterations int  `json:"max_iterations"` // 0 = system default (10)
}

func (r createProjectRequest) validate() string {
	if strings.TrimSpace(r.Name) == "" {
		return "name is required"
	}
	if r.Kind != "" && r.Kind != string(model.ProjectKindProject) && r.Kind != string(model.ProjectKindMonitor) {
		return "kind must be 'project' or 'monitor'"
	}
	if r.Status != "" &&
		r.Status != string(model.ProjectStatusActive) &&
		r.Status != string(model.ProjectStatusArchived) &&
		r.Status != string(model.ProjectStatusPaused) {
		return "status must be 'active', 'archived', or 'paused'"
	}
	if r.ScheduleKind != "" &&
		r.ScheduleKind != model.ScheduleKindInterval &&
		r.ScheduleKind != model.ScheduleKindDaily {
		return "schedule_kind must be 'interval' or 'daily'"
	}
	if r.ScheduleKind == model.ScheduleKindDaily {
		for _, t := range r.ScheduleTimes {
			if !validHHMM(t) {
				return "schedule_times entries must be in HH:MM 24-hour format (e.g. '07:00')"
			}
		}
	}
	return ""
}

// validHHMM reports whether s is a valid 24-hour time of day "HH:MM".
func validHHMM(s string) bool {
	if _, err := time.Parse("15:04", strings.TrimSpace(s)); err != nil {
		return false
	}
	return true
}

// normaliseScheduleTimes trims, validates, de-duplicates, and sorts daily
// schedule times into canonical "HH:MM" form.
func normaliseScheduleTimes(in []string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, raw := range in {
		t, err := time.Parse("15:04", strings.TrimSpace(raw))
		if err != nil {
			continue
		}
		hhmm := t.Format("15:04")
		if _, dup := seen[hhmm]; dup {
			continue
		}
		seen[hhmm] = struct{}{}
		out = append(out, hhmm)
	}
	sort.Strings(out)
	return out
}

type assignAgentRequest struct {
	AgentID string `json:"agent_id"`
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")     // optional: "project" | "monitor"
	status := r.URL.Query().Get("status") // optional: "active" | "archived" | "paused" — defaults to all non-archived
	userID := userFromCtx(r.Context()).ID
	var list []*model.Project
	if status == "" {
		// Default: return all non-archived (active + paused) so paused monitors remain visible.
		all, err := s.projects.ListByStatus(r.Context(), kind, "", userID)
		if err != nil {
			respondInternalErr(w, err)
			return
		}
		for _, p := range all {
			if p.Status != model.ProjectStatusArchived {
				list = append(list, p)
			}
		}
	} else {
		var err error
		list, err = s.projects.ListByStatus(r.Context(), kind, status, userID)
		if err != nil {
			respondInternalErr(w, err)
			return
		}
	}
	if list == nil {
		list = []*model.Project{}
	}
	respond(w, http.StatusOK, list)
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, err := s.projects.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if p == nil {
		respondErr(w, http.StatusNotFound, "project not found")
		return
	}
	respond(w, http.StatusOK, p)
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var req createProjectRequest
	if !decode(w, r, &req) {
		return
	}
	if msg := req.validate(); msg != "" {
		respondErr(w, http.StatusBadRequest, msg)
		return
	}

	user := userFromCtx(r.Context())

	status := model.ProjectStatusActive
	if req.Status != "" {
		status = model.ProjectStatus(req.Status)
	}
	kind := model.ProjectKindProject
	if req.Kind != "" {
		kind = model.ProjectKind(req.Kind)
	}

	criticMode := resolveCriticMode(req.CriticMode, req.CriticAgentID)
	if msg, err := s.validateCriticAgent(r.Context(), criticMode, req.CriticAgentID); err != nil {
		respondInternalErr(w, err)
		return
	} else if msg != "" {
		respondErr(w, http.StatusBadRequest, msg)
		return
	}
	p := &model.Project{
		ID:                   uuid.New().String(),
		Name:                 strings.TrimSpace(req.Name),
		Objective:            req.Objective,
		WorkingDir:           strings.TrimSpace(req.WorkingDir),
		Kind:                 kind,
		ScheduleInterval:     req.ScheduleInterval,
		ScheduleKind:         resolveScheduleKind(req.ScheduleKind),
		ScheduleTimes:        normaliseScheduleTimes(req.ScheduleTimes),
		ScheduleCatchUp:      req.ScheduleCatchUp,
		Owner:                user.ID,
		Status:               status,
		CriticAgentID:        req.CriticAgentID,
		CriticMode:           criticMode,
		MonitorModel:         strings.TrimSpace(req.MonitorModel),
		BudgetUSD:            req.BudgetUSD,
		BudgetPeriod:         resolveBudgetPeriod(req.BudgetPeriod),
		ContextSummarisation: req.ContextSummarisation,
		Tags:                 normaliseTags(req.Tags),
		HeartbeatOnAttention:   req.HeartbeatOnAttention,
		HeartbeatOnFailed:      req.HeartbeatOnFailed,
		LinkedProjectID:        req.LinkedProjectID,
		HeartbeatEscalateAfter: req.HeartbeatEscalateAfter,
		MonitorCacheTTL:        req.MonitorCacheTTL,
		ReactMode:              req.ReactMode,
		MaxIterations:        req.MaxIterations,
		CreatedAt:            time.Now(),
	}
	if err := s.projects.Create(r.Context(), p); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusCreated, p)
}

func (s *Server) updateProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := s.projects.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if existing == nil {
		respondErr(w, http.StatusNotFound, "project not found")
		return
	}

	var req createProjectRequest
	if !decode(w, r, &req) {
		return
	}
	if msg := req.validate(); msg != "" {
		respondErr(w, http.StatusBadRequest, msg)
		return
	}

	existing.Name = strings.TrimSpace(req.Name)
	existing.Objective = req.Objective
	existing.WorkingDir = strings.TrimSpace(req.WorkingDir)
	existing.ScheduleInterval = req.ScheduleInterval
	existing.ScheduleKind = resolveScheduleKind(req.ScheduleKind)
	existing.ScheduleTimes = normaliseScheduleTimes(req.ScheduleTimes)
	existing.ScheduleCatchUp = req.ScheduleCatchUp
	existing.CriticAgentID = req.CriticAgentID
	existing.CriticMode = resolveCriticMode(req.CriticMode, req.CriticAgentID)
	existing.MonitorModel = strings.TrimSpace(req.MonitorModel)
	existing.BudgetUSD = req.BudgetUSD
	existing.BudgetPeriod = resolveBudgetPeriod(req.BudgetPeriod)
	existing.ContextSummarisation = req.ContextSummarisation
	existing.HeartbeatOnAttention = req.HeartbeatOnAttention
	existing.HeartbeatOnFailed = req.HeartbeatOnFailed
	existing.LinkedProjectID = req.LinkedProjectID
	existing.HeartbeatEscalateAfter = req.HeartbeatEscalateAfter
	existing.MonitorCacheTTL = req.MonitorCacheTTL
	existing.ReactMode = req.ReactMode
	existing.MaxIterations = req.MaxIterations

	if msg, err := s.validateCriticAgent(r.Context(), existing.CriticMode, req.CriticAgentID); err != nil {
		respondInternalErr(w, err)
		return
	} else if msg != "" {
		respondErr(w, http.StatusBadRequest, msg)
		return
	}

	existing.Tags = normaliseTags(req.Tags)
	if req.Kind != "" {
		existing.Kind = model.ProjectKind(req.Kind)
	}
	if req.Status != "" {
		existing.Status = model.ProjectStatus(req.Status)
	}

	if err := s.projects.Update(r.Context(), existing); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusOK, existing)
}

func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := s.projects.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if existing == nil {
		respondErr(w, http.StatusNotFound, "project not found")
		return
	}

	// Refuse deletion while any task is actively running or queued.
	tasks, err := s.tasks.List(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	for _, t := range tasks {
		if t.Status == model.TaskStatusRunning || t.Status == model.TaskStatusQueued {
			respondErr(w, http.StatusConflict,
				"cannot delete project while tasks are running or queued — wait for them to finish or cancel them first")
			return
		}
	}

	// Hard-delete the project and all its tasks.
	if err := s.projects.DeleteWithTasks(r.Context(), id); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusNoContent, nil)
}

// archiveProject sets a project's status to 'archived', hiding it from active views.
func (s *Server) archiveProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, err := s.projects.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if p == nil {
		respondErr(w, http.StatusNotFound, "project not found")
		return
	}
	if p.Status == model.ProjectStatusArchived {
		respond(w, http.StatusOK, p) // idempotent
		return
	}

	// Refuse archiving while tasks are still running.
	tasks, err := s.tasks.List(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	for _, t := range tasks {
		if t.Status == model.TaskStatusRunning || t.Status == model.TaskStatusQueued {
			respondErr(w, http.StatusConflict,
				"cannot archive project while tasks are running or queued — wait for them to finish first")
			return
		}
	}

	p.Status = model.ProjectStatusArchived
	if err := s.projects.Update(r.Context(), p); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusOK, p)
}

// restoreProject sets a project's status back to 'active'.
func (s *Server) restoreProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, err := s.projects.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if p == nil {
		respondErr(w, http.StatusNotFound, "project not found")
		return
	}
	p.Status = model.ProjectStatusActive
	if err := s.projects.Update(r.Context(), p); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusOK, p)
}

// pauseProject suspends a monitor's schedule without archiving it.
func (s *Server) pauseProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, err := s.projects.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if p == nil {
		respondErr(w, http.StatusNotFound, "project not found")
		return
	}
	if p.Status == model.ProjectStatusPaused {
		respond(w, http.StatusOK, p) // idempotent
		return
	}
	p.Status = model.ProjectStatusPaused
	if err := s.projects.Update(r.Context(), p); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusOK, p)
}

func (s *Server) assignAgent(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")

	proj, err := s.projects.Get(r.Context(), projectID)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if proj == nil {
		respondErr(w, http.StatusNotFound, "project not found")
		return
	}

	var req assignAgentRequest
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.AgentID) == "" {
		respondErr(w, http.StatusBadRequest, "agent_id is required")
		return
	}

	agent, err := s.agents.Get(r.Context(), req.AgentID)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if agent == nil {
		respondErr(w, http.StatusBadRequest, "agent not found")
		return
	}

	if _, err := s.projects.AssignAgent(r.Context(), projectID, req.AgentID); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusNoContent, nil)
}

func (s *Server) removeAgent(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	agentID := chi.URLParam(r, "agentId")

	if err := s.projects.RemoveAgent(r.Context(), projectID, agentID); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusNoContent, nil)
}

func (s *Server) listProjectAgents(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")

	proj, err := s.projects.Get(r.Context(), projectID)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if proj == nil {
		respondErr(w, http.StatusNotFound, "project not found")
		return
	}

	agents, err := s.projects.ListAgents(r.Context(), projectID)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if agents == nil {
		agents = []*model.Agent{}
	}
	respond(w, http.StatusOK, agents)
}

// validateCriticAgent checks that the agent referenced in critic_mode or
// critic_agent_id actually exists. Returns an empty string if OK.
func (s *Server) validateCriticAgent(ctx context.Context, criticMode string, legacyAgentID *string) (string, error) {
	agentID := ""
	if len(criticMode) > 6 && criticMode[:6] == "agent:" {
		agentID = criticMode[6:]
	} else if legacyAgentID != nil && *legacyAgentID != "" {
		agentID = *legacyAgentID
	}
	if agentID == "" {
		return "", nil
	}
	a, err := s.agents.Get(ctx, agentID)
	if err != nil {
		return "", err
	}
	if a == nil {
		return "critic agent not found: " + agentID, nil
	}
	return "", nil
}

// resolveCriticMode normalises the critic_mode value from an API request.
// If criticMode is already set and valid, it is returned as-is.
// If criticMode is empty but a legacy critic_agent_id is provided, we synthesise "agent:<id>".
// Falls back to "none".
func resolveCriticMode(criticMode string, legacyAgentID *string) string {
	switch criticMode {
	case model.CriticModeBuiltin:
		return model.CriticModeBuiltin
	case model.CriticModeNone, "":
		// fall through to legacy check
	default:
		if len(criticMode) > 6 && criticMode[:6] == "agent:" {
			return criticMode
		}
	}
	if legacyAgentID != nil && *legacyAgentID != "" {
		return "agent:" + *legacyAgentID
	}
	return model.CriticModeNone
}

// resolveScheduleKind normalises the schedule_kind value, defaulting to
// "interval" for empty or unrecognised input (backward compatible).
func resolveScheduleKind(kind string) string {
	if kind == model.ScheduleKindDaily {
		return model.ScheduleKindDaily
	}
	return model.ScheduleKindInterval
}

func resolveBudgetPeriod(p string) string {
	switch p {
	case "day", "week", "month":
		return p
	default:
		return "total"
	}
}

// normaliseTags trims whitespace and removes empty/duplicate tags.
func normaliseTags(in []string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, t := range in {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			continue
		}
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}

// generateProjectDescription uses an LLM to generate a description for a project/monitor.
func (s *Server) generateProjectDescription(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name       string `json:"name"`
		Hint       string `json:"hint"`        // optional extra context from the user
		ProviderID string `json:"provider_id"` // optional; falls back to first LLM provider
	}
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		respondErr(w, http.StatusBadRequest, "name is required")
		return
	}

	providerID := req.ProviderID
	if providerID == "" {
		providers, err := s.providers.List(r.Context(), userFromCtx(r.Context()).ID)
		if err != nil || len(providers) == 0 {
			respondErr(w, http.StatusBadRequest, "no providers available for generation")
			return
		}
		for _, p := range providers {
			if p.Type == model.ProviderTypeLLM {
				providerID = p.ID
				break
			}
		}
		if providerID == "" {
			providerID = providers[0].ID
		}
	}

	prov, err := s.registry.Get(r.Context(), providerID)
	if err != nil {
		respondErr(w, http.StatusBadRequest, fmt.Sprintf("provider load failed: %v", err))
		return
	}

	hintSection := ""
	if strings.TrimSpace(req.Hint) != "" {
		hintSection = "\nAdditional context: " + strings.TrimSpace(req.Hint)
	}

	prompt := fmt.Sprintf(`You are a monitoring configuration assistant.
Write a clear, concise description for an AI monitoring job named "%s".%s

The description will be sent as the task prompt to an AI agent on each scheduled run.
It should explain: what to check or monitor, what data to collect, and what to report back.
Be specific and actionable. Return ONLY the description text — no JSON, no markdown, no headings.`, req.Name, hintSection)

	resp, err := prov.Execute(r.Context(), provider.TaskRequest{
		SystemPrompt: "You are a concise technical writer. Return only plain text, no markdown, no JSON.",
		Prompt:       prompt,
	})
	if err != nil {
		respondErr(w, http.StatusInternalServerError, fmt.Sprintf("generation failed: %v", err))
		return
	}

	description := strings.TrimSpace(resp.Output)
	respond(w, http.StatusOK, map[string]string{"description": description})
}

func (s *Server) getProjectSummary(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	project, err := s.projects.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if project == nil {
		respondErr(w, http.StatusNotFound, "project not found")
		return
	}
	summary, err := s.stats.ProjectTaskSummary(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusOK, summary)
}

// getProjectSpend returns the current spend for the project's budget period.
// Response: { spent_usd, budget_usd, budget_period, remaining_usd }
func (s *Server) getProjectSpend(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	project, err := s.projects.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if project == nil {
		respondErr(w, http.StatusNotFound, "project not found")
		return
	}
	period := project.BudgetPeriod
	if period == "" {
		period = "total"
	}
	spent, err := s.tasks.ProjectSpendForPeriod(r.Context(), id, period)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	remaining := project.BudgetUSD - spent
	if project.BudgetUSD == 0 {
		remaining = 0
	}
	respond(w, http.StatusOK, map[string]interface{}{
		"spent_usd":     spent,
		"budget_usd":    project.BudgetUSD,
		"budget_period": period,
		"remaining_usd": remaining,
	})
}

// listProjectSummaries returns task summaries for all projects in a single
// response, keyed by project ID. Useful for rendering status dots in the left pane.
func (s *Server) listProjectSummaries(w http.ResponseWriter, r *http.Request) {
	summaries, err := s.stats.AllProjectTaskSummaries(r.Context())
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusOK, summaries)
}

// listProjectHistory returns all completed tasks for a project regardless of dismissed state.
// Used by the project view to show full history including inbox-dismissed tasks.
func (s *Server) listProjectHistory(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, err := s.projects.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if p == nil {
		respondErr(w, http.StatusNotFound, "project not found")
		return
	}
	tasks, err := s.tasks.ListProjectHistory(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if tasks == nil {
		tasks = []*model.Task{}
	}
	respond(w, http.StatusOK, tasks)
}

// suggestProjectNextAction uses an LLM to suggest 1–3 next tasks for a project
// based on its objective and recent task history.
func (s *Server) suggestProjectNextAction(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	project, err := s.projects.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if project == nil {
		respondErr(w, http.StatusNotFound, "project not found")
		return
	}

	// Gather recent task history (last 20) for context.
	recentTasks, err := s.tasks.ListByProject(r.Context(), id, "", 20)
	if err != nil {
		respondInternalErr(w, err)
		return
	}

	// Find a suitable LLM provider.
	providers, err := s.providers.List(r.Context(), userFromCtx(r.Context()).ID)
	if err != nil || len(providers) == 0 {
		respondErr(w, http.StatusUnprocessableEntity, "no providers available for suggestions")
		return
	}
	var providerID string
	for _, p := range providers {
		if p.Type == model.ProviderTypeLLM {
			providerID = p.ID
			break
		}
	}
	if providerID == "" {
		// Fall back to any provider (may be a coding agent — best-effort).
		providerID = providers[0].ID
	}

	prov, err := s.registry.Get(r.Context(), providerID)
	if err != nil {
		respondErr(w, http.StatusUnprocessableEntity, fmt.Sprintf("provider load failed: %v", err))
		return
	}

	// Build task history summary.
	var historyLines []string
	for _, t := range recentTasks {
		historyLines = append(historyLines, fmt.Sprintf("- [%s] %s", t.Status, t.Title))
	}
	historyText := strings.Join(historyLines, "\n")
	if historyText == "" {
		historyText = "(no tasks yet)"
	}

	objective := strings.TrimSpace(project.Objective)
	if objective == "" {
		objective = "(no objective set)"
	}

	prompt := fmt.Sprintf(`You are a project planning assistant helping with: "%s".

Project objective: %s

Recent tasks:
%s

Suggest 1 to 3 specific, actionable next tasks for this project. Each suggestion should be something an AI agent could execute — concrete enough to be run as a task.

Return a JSON array. Each item must have "title" (short, max 80 chars) and "description" (1-2 sentences explaining what to do). Example:
[{"title": "Write article on giving feedback", "description": "Draft a 500-word article covering how to deliver constructive feedback to team members."}]

Return ONLY the JSON array, no prose, no markdown.`, project.Name, objective, historyText)

	resp, err := prov.Execute(r.Context(), provider.TaskRequest{
		SystemPrompt: "You are a concise project planning assistant. Return only valid JSON arrays, no markdown.",
		Prompt:       prompt,
	})
	if err != nil {
		respondErr(w, http.StatusInternalServerError, fmt.Sprintf("suggestion generation failed: %v", err))
		return
	}

	// Parse the JSON response — extract array even if wrapped in markdown fences.
	raw := strings.TrimSpace(resp.Output)
	if idx := strings.Index(raw, "["); idx >= 0 {
		raw = raw[idx:]
	}
	if idx := strings.LastIndex(raw, "]"); idx >= 0 {
		raw = raw[:idx+1]
	}

	type suggestion struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	var suggestions []suggestion
	if err := json.Unmarshal([]byte(raw), &suggestions); err != nil || len(suggestions) == 0 {
		// Return a graceful fallback rather than an error.
		suggestions = []suggestion{{
			Title:       "Review project progress",
			Description: "Review recent task outputs and identify the next most impactful action for this project.",
		}}
	}

	respond(w, http.StatusOK, map[string]any{"suggestions": suggestions})
}
