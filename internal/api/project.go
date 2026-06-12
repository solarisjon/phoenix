package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/provider"
)

type createProjectRequest struct {
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	WorkingDir       string   `json:"working_dir"`
	Kind             string   `json:"kind"`
	Status           string   `json:"status"`
	ScheduleInterval *int     `json:"schedule_interval"` // seconds; nil = no schedule (monitors only)
	ScheduleKind     string   `json:"schedule_kind"`     // "interval" | "daily"
	ScheduleTimes    []string `json:"schedule_times"`    // ["07:00","12:00"] when schedule_kind == "daily"
	ScheduleCatchUp  bool     `json:"schedule_catch_up"` // daily only: catch up a missed run same calendar day
	CriticAgentID    *string  `json:"critic_agent_id"`   // deprecated: prefer critic_mode
	CriticMode       string   `json:"critic_mode"`       // "none" | "builtin" | "agent:<id>"
	Tags             []string `json:"tags"`
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
		r.Status != string(model.ProjectStatusArchived) {
		return "status must be 'active' or 'archived'"
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
	status := r.URL.Query().Get("status") // optional: "active" | "archived" — defaults to "active"
	if status == "" {
		status = string(model.ProjectStatusActive)
	}
	list, err := s.projects.ListByStatus(r.Context(), kind, status)
	if err != nil {
		respondInternalErr(w, err)
		return
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

	user, err := s.users.GetDefault(r.Context())
	if err != nil || user == nil {
		respondInternalErr(w, err)
		return
	}

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
		ID:               uuid.New().String(),
		Name:             strings.TrimSpace(req.Name),
		Description:      req.Description,
		WorkingDir:       strings.TrimSpace(req.WorkingDir),
		Kind:             kind,
		ScheduleInterval: req.ScheduleInterval,
		ScheduleKind:     resolveScheduleKind(req.ScheduleKind),
		ScheduleTimes:    normaliseScheduleTimes(req.ScheduleTimes),
		ScheduleCatchUp:  req.ScheduleCatchUp,
		Owner:            user.ID,
		Status:           status,
		CriticAgentID:    req.CriticAgentID,
		CriticMode:       criticMode,
		Tags:             normaliseTags(req.Tags),
		CreatedAt:        time.Now(),
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
	existing.Description = req.Description
	existing.WorkingDir = strings.TrimSpace(req.WorkingDir)
	existing.ScheduleInterval = req.ScheduleInterval
	existing.ScheduleKind = resolveScheduleKind(req.ScheduleKind)
	existing.ScheduleTimes = normaliseScheduleTimes(req.ScheduleTimes)
	existing.ScheduleCatchUp = req.ScheduleCatchUp
	existing.CriticAgentID = req.CriticAgentID
	existing.CriticMode = resolveCriticMode(req.CriticMode, req.CriticAgentID)

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
		providers, err := s.providers.List(r.Context())
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

// ---- File browser ----

// projectFileEntry is a single entry returned by listProjectFiles.
type projectFileEntry struct {
	Name       string    `json:"name"`
	RelPath    string    `json:"rel_path"` // relative to working_dir
	SizeBytes  int64     `json:"size_bytes"`
	ModifiedAt time.Time `json:"modified_at"`
	Ext        string    `json:"ext"` // e.g. ".md", ".html", ".txt"
	IsArtifact bool      `json:"is_artifact"` // true when tagged in task output
}

// listProjectFiles lists regular files under the project's working_dir.
// Files are returned sorted by modification time (newest first).
// Hidden files and directories are excluded. Walk depth is capped at 3.
func (s *Server) listProjectFiles(w http.ResponseWriter, r *http.Request) {
	proj, err := s.projects.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if proj == nil {
		respondErr(w, http.StatusNotFound, "project not found")
		return
	}
	if proj.WorkingDir == "" {
		respond(w, http.StatusOK, []projectFileEntry{})
		return
	}

	root := filepath.Clean(proj.WorkingDir)
	if _, err := os.Stat(root); os.IsNotExist(err) {
		respond(w, http.StatusOK, []projectFileEntry{})
		return
	}

	// Collect artifact paths from task outputs so we can badge them.
	artifactPaths := collectArtifactPaths(r.Context(), s, proj.ID)

	var entries []projectFileEntry
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		// Skip hidden files/dirs.
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			// Limit depth to 3 levels below root.
			rel, _ := filepath.Rel(root, path)
			if strings.Count(rel, string(os.PathSeparator)) >= 3 {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		_, isArtifact := artifactPaths[path]
		entries = append(entries, projectFileEntry{
			Name:       d.Name(),
			RelPath:    rel,
			SizeBytes:  info.Size(),
			ModifiedAt: info.ModTime(),
			Ext:        strings.ToLower(filepath.Ext(d.Name())),
			IsArtifact: isArtifact,
		})
		return nil
	})

	// Sort newest-first.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].ModifiedAt.After(entries[j].ModifiedAt)
	})

	respond(w, http.StatusOK, entries)
}

// getProjectFileContent returns the text content of a file inside the project's
// working_dir. The file path is passed as the URL wildcard segment after /files/.
// Read is limited to 256 KB for safety.
func (s *Server) getProjectFileContent(w http.ResponseWriter, r *http.Request) {
	proj, err := s.projects.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if proj == nil {
		respondErr(w, http.StatusNotFound, "project not found")
		return
	}
	if proj.WorkingDir == "" {
		respondErr(w, http.StatusBadRequest, "project has no working directory")
		return
	}

	// Decode the relative path from the URL wildcard.
	relPath := chi.URLParam(r, "*")
	if relPath == "" {
		respondErr(w, http.StatusBadRequest, "file path required")
		return
	}

	root := filepath.Clean(proj.WorkingDir)
	abs := filepath.Clean(filepath.Join(root, relPath))

	// Guard: resolved path must be within the project root.
	if !strings.HasPrefix(abs, root+string(os.PathSeparator)) && abs != root {
		respondErr(w, http.StatusForbidden, "path outside project directory")
		return
	}

	f, err := os.Open(abs)
	if os.IsNotExist(err) {
		respondErr(w, http.StatusNotFound, "file not found")
		return
	}
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	defer f.Close()

	const maxRead = 256 * 1024
	lr := io.LimitReader(f, maxRead)
	data, err := io.ReadAll(lr)
	if err != nil {
		respondInternalErr(w, err)
		return
	}

	respond(w, http.StatusOK, map[string]any{
		"content":  string(data),
		"ext":      strings.ToLower(filepath.Ext(abs)),
		"truncated": int64(len(data)) == maxRead,
	})
}

// collectArtifactPaths queries recent task outputs for the project and returns
// a set of absolute paths that were declared as ARTIFACT_START file artifacts.
func collectArtifactPaths(ctx context.Context, s *Server, projectID string) map[string]struct{} {
	out := map[string]struct{}{}
	tasks, err := s.tasks.ListByProject(ctx, projectID, "", 500)
	if err != nil {
		return out
	}
	for _, t := range tasks {
		for _, a := range parseArtifactBlocks(t.Output) {
			if a.artType == "file" && a.path != "" {
				out[filepath.Clean(a.path)] = struct{}{}
			}
		}
	}
	return out
}

// parsedArtifact holds a single parsed ARTIFACT_START…ARTIFACT_END block.
type parsedArtifact struct {
	artType string // "file" | "url" | "jira" | "confluence" | "html"
	path    string // file path or URL
	title   string
}

// parseArtifactBlocks extracts ARTIFACT_START … ARTIFACT_END blocks from text.
//
//	ARTIFACT_START
//	Type: file
//	Path: /abs/path/to/file.md
//	Title: My Document
//	ARTIFACT_END
func parseArtifactBlocks(output string) []parsedArtifact {
	var results []parsedArtifact
	lines := strings.Split(output, "\n")
	i := 0
	for i < len(lines) {
		if strings.TrimSpace(lines[i]) != "ARTIFACT_START" {
			i++
			continue
		}
		i++
		var a parsedArtifact
		for i < len(lines) {
			if strings.TrimSpace(lines[i]) == "ARTIFACT_END" {
				i++
				break
			}
			line := lines[i]
			switch {
			case strings.HasPrefix(line, "Type:"):
				a.artType = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "Type:")))
			case strings.HasPrefix(line, "Path:"):
				a.path = strings.TrimSpace(strings.TrimPrefix(line, "Path:"))
			case strings.HasPrefix(line, "URL:"):
				a.path = strings.TrimSpace(strings.TrimPrefix(line, "URL:"))
			case strings.HasPrefix(line, "Title:"):
				a.title = strings.TrimSpace(strings.TrimPrefix(line, "Title:"))
			}
			i++
		}
		if a.artType != "" && a.path != "" {
			results = append(results, a)
		}
	}
	return results
}
