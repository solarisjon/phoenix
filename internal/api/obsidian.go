package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/provider"
)

// ---- Obsidian vault CRUD ----

func (s *Server) listObsidianVaults(w http.ResponseWriter, r *http.Request) {
	vaults, err := s.obsidianVaults.List(r.Context())
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if vaults == nil {
		vaults = []*model.ObsidianVault{}
	}
	respond(w, http.StatusOK, vaults)
}

func (s *Server) getObsidianVault(w http.ResponseWriter, r *http.Request) {
	v, err := s.obsidianVaults.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if v == nil {
		respondErr(w, http.StatusNotFound, "vault not found")
		return
	}
	respond(w, http.StatusOK, v)
}

func (s *Server) createObsidianVault(w http.ResponseWriter, r *http.Request) {
	var v model.ObsidianVault
	if !decode(w, r, &v) {
		return
	}
	if strings.TrimSpace(v.Name) == "" || strings.TrimSpace(v.Path) == "" {
		respondErr(w, http.StatusBadRequest, "name and path are required")
		return
	}
	if v.ID == "" {
		v.ID = uuid.New().String()
	}
	v.Enabled = true
	v.CreatedAt = time.Now().UTC()
	if err := s.obsidianVaults.Create(r.Context(), &v); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusCreated, v)
}

func (s *Server) updateObsidianVault(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := s.obsidianVaults.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if existing == nil {
		respondErr(w, http.StatusNotFound, "vault not found")
		return
	}
	var v model.ObsidianVault
	if !decode(w, r, &v) {
		return
	}
	v.ID = id
	v.CreatedAt = existing.CreatedAt
	if err := s.obsidianVaults.Update(r.Context(), &v); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusOK, v)
}

func (s *Server) deleteObsidianVault(w http.ResponseWriter, r *http.Request) {
	if err := s.obsidianVaults.Delete(r.Context(), chi.URLParam(r, "id")); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// discoverObsidianVaults scans the configured root directory for vault subdirectories
// and returns them. Vaults already in the DB are marked as configured.
func (s *Server) discoverObsidianVaults(w http.ResponseWriter, r *http.Request) {
	root := r.URL.Query().Get("root")
	if root == "" {
		settings, err := s.systemSettings.Get(r.Context())
		if err != nil {
			respondInternalErr(w, err)
			return
		}
		root = settings.ObsidianRoot
	}
	root = strings.TrimSpace(root)
	if root == "" {
		respondErr(w, http.StatusBadRequest, "obsidian_root not configured and no root query param provided")
		return
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		respondErr(w, http.StatusBadRequest, fmt.Sprintf("cannot read directory: %v", err))
		return
	}

	existing, err := s.obsidianVaults.List(r.Context())
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	configured := make(map[string]bool)
	for _, v := range existing {
		configured[v.Path] = true
	}

	type vaultInfo struct {
		Name       string `json:"name"`
		Path       string `json:"path"`
		Configured bool   `json:"configured"`
	}
	var results []vaultInfo
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		p := filepath.Join(root, entry.Name())
		// Obsidian vaults contain a .obsidian directory.
		if _, err := os.Stat(filepath.Join(p, ".obsidian")); err != nil {
			continue
		}
		results = append(results, vaultInfo{
			Name:       entry.Name(),
			Path:       p,
			Configured: configured[p],
		})
	}
	if results == nil {
		results = []vaultInfo{}
	}
	respond(w, http.StatusOK, results)
}

// generateObsidianVaultContext uses an LLM to suggest a context description for
// a vault based on its name, similar to the agent persona generation flow.
func (s *Server) generateObsidianVaultContext(w http.ResponseWriter, r *http.Request) {
	var req struct {
		VaultName  string `json:"vault_name"`
		ProviderID string `json:"provider_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.VaultName) == "" {
		respondErr(w, http.StatusBadRequest, "vault_name is required")
		return
	}

	providerID := req.ProviderID
	if providerID == "" {
		providers, err := s.providers.List(r.Context())
		if err != nil || len(providers) == 0 {
			respondErr(w, http.StatusBadRequest, "no providers available")
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

	prompt := fmt.Sprintf(`You are helping configure an Obsidian vault integration for an AI agent platform.

The vault is named: %q

Write a short 1–2 sentence description of what this vault is likely used for, based solely on its name. This description will be used to route agent-generated notes to the correct vault.

Be specific and practical. Examples:
- "Project planning notes, active engineering tasks, design documents, and technical decisions."
- "On-call incidents, customer escalations, SEV tickets, and post-incident reviews."
- "Technology research, competitive analysis, and exploratory spike notes."

Output only the description — no preamble, no quotes, no bullet points.`, req.VaultName)

	resp, err := prov.Execute(r.Context(), provider.TaskRequest{
		SystemPrompt: "You are a concise technical writer. Output only the requested content.",
		Prompt:       prompt,
	})
	if err != nil {
		respondErr(w, http.StatusInternalServerError, fmt.Sprintf("generation failed: %v", err))
		return
	}
	respond(w, http.StatusOK, map[string]string{"context": strings.TrimSpace(resp.Output)})
}

// writeTaskToObsidian generates and writes an Obsidian note for a completed task.
// Called manually from the task detail view or automatically post-completion.
func (s *Server) writeTaskToObsidian(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "id")
	task, err := s.tasks.Get(r.Context(), taskID)
	if err != nil || task == nil {
		respondErr(w, http.StatusNotFound, "task not found")
		return
	}
	if task.Status != model.TaskStatusCompleted {
		respondErr(w, http.StatusBadRequest, "only completed tasks can be written to Obsidian")
		return
	}

	var req struct {
		VaultID    string `json:"vault_id"`    // optional: if empty, use LLM to pick vault
		ProviderID string `json:"provider_id"` // optional: use cheapest LLM if empty
	}
	if !decode(w, r, &req) {
		return
	}

	settings, err := s.systemSettings.Get(r.Context())
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if !settings.ObsidianEnabled {
		respondErr(w, http.StatusServiceUnavailable, "Obsidian integration is disabled — enable it in Settings → Obsidian")
		return
	}
	if settings.ObsidianRoot == "" {
		respondErr(w, http.StatusBadRequest, "Obsidian root directory not configured")
		return
	}

	vaults, err := s.obsidianVaults.ListEnabled(r.Context())
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if len(vaults) == 0 {
		respondErr(w, http.StatusBadRequest, "no Obsidian vaults configured")
		return
	}

	var targetVault *model.ObsidianVault
	if req.VaultID != "" {
		for _, v := range vaults {
			if v.ID == req.VaultID {
				targetVault = v
				break
			}
		}
		if targetVault == nil {
			respondErr(w, http.StatusBadRequest, "vault not found")
			return
		}
	}

	// Get agent and project info for metadata.
	agent, _ := s.agents.Get(r.Context(), task.AgentID)
	project, _ := s.projects.Get(r.Context(), task.ProjectID)

	providerID := req.ProviderID
	if providerID == "" {
		providers, err := s.providers.List(r.Context())
		if err == nil {
			for _, p := range providers {
				if p.Type == model.ProviderTypeLLM {
					providerID = p.ID
					break
				}
			}
		}
	}

	if providerID == "" {
		respondErr(w, http.StatusBadRequest, "no LLM provider available for note generation")
		return
	}

	prov, err := s.registry.Get(r.Context(), providerID)
	if err != nil {
		respondErr(w, http.StatusBadRequest, fmt.Sprintf("provider load failed: %v", err))
		return
	}

	agentName := "unknown"
	if agent != nil {
		agentName = agent.Name
	}
	projectName := "unknown"
	if project != nil {
		projectName = project.Name
	}

	taskOutput := extractTaskOutputText(task.Output)
	dateStr := time.Now().Format("2006-01-02")

	// If no vault specified, ask LLM to pick one.
	if targetVault == nil {
		var vaultRouting strings.Builder
		for _, v := range vaults {
			vaultRouting.WriteString(fmt.Sprintf("- %s: %s\n", v.Name, v.Context))
		}
		pickPrompt := fmt.Sprintf(`Choose the most appropriate Obsidian vault for this task output.

Task title: %s
Agent: %s
Project: %s

Task output summary:
%s

Available vaults:
%s

Reply with ONLY the vault name (exactly as shown above) that best matches the content.`,
			task.Title, agentName, projectName,
			truncate(taskOutput, 1000), vaultRouting.String())

		pickResp, err := prov.Execute(r.Context(), provider.TaskRequest{
			SystemPrompt: "You are a precise classifier. Output only the vault name, nothing else.",
			Prompt:       pickPrompt,
		})
		if err == nil {
			picked := strings.TrimSpace(pickResp.Output)
			for _, v := range vaults {
				if strings.EqualFold(v.Name, picked) {
					targetVault = v
					break
				}
			}
		}
		// If still no match, use the first vault.
		if targetVault == nil {
			targetVault = vaults[0]
		}
	}

	// Generate the note content.
	notePrompt := fmt.Sprintf(`Convert this Phoenix agent task output into a well-formatted Obsidian note.

Task: %s
Agent: %s
Project: %s
Date: %s

Task output:
%s

Format requirements:
1. Include YAML front matter with: date, tags (phoenix, %s, %s), source (phoenix-task), task_id (%s), agent, project
2. Use a clean H1 heading as the title
3. Reformat the content for Obsidian — use proper Markdown, headings, bullet points
4. End with a horizontal rule and a footer: "Generated by Phoenix agent: %s on %s"
5. Output ONLY the complete Markdown file content — no explanation, no preamble`,
		task.Title, agentName, projectName, dateStr,
		taskOutput,
		agentName, projectName, task.ID, agentName, dateStr)

	noteResp, err := prov.Execute(r.Context(), provider.TaskRequest{
		SystemPrompt: "You are a technical writer producing clean Obsidian Markdown notes. Output only the Markdown content.",
		Prompt:       notePrompt,
	})
	if err != nil {
		respondErr(w, http.StatusInternalServerError, fmt.Sprintf("note generation failed: %v", err))
		return
	}

	noteContent := noteResp.Output

	// Write the file.
	slug := slugify(task.Title)
	filename := fmt.Sprintf("%s-%s.md", dateStr, slug)
	filePath := filepath.Join(targetVault.Path, filename)

	// Handle collisions.
	if _, err := os.Stat(filePath); err == nil {
		for i := 2; i <= 99; i++ {
			candidate := filepath.Join(targetVault.Path, fmt.Sprintf("%s-%s-%d.md", dateStr, slug, i))
			if _, err := os.Stat(candidate); os.IsNotExist(err) {
				filePath = candidate
				filename = filepath.Base(filePath)
				break
			}
		}
	}

	if err := os.WriteFile(filePath, []byte(noteContent), 0644); err != nil {
		respondErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to write file: %v", err))
		return
	}

	respond(w, http.StatusOK, map[string]string{
		"vault":    targetVault.Name,
		"path":     filePath,
		"filename": filename,
	})
}

// extractTaskOutputText extracts the text field from a task output JSON blob.
func extractTaskOutputText(output string) string {
	if output == "" || output == "{}" {
		return ""
	}
	// Try to extract "text" from JSON.
	var m map[string]string
	if err := json.Unmarshal([]byte(output), &m); err == nil {
		if t, ok := m["text"]; ok {
			return t
		}
	}
	return output
}

// slugify converts a title to a lowercase-kebab-case filename-safe string.
func slugify(title string) string {
	title = strings.ToLower(title)
	var b strings.Builder
	for _, r := range title {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteByte('-')
		}
	}
	s := strings.Trim(b.String(), "-")
	// Collapse multiple dashes.
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	if len(s) > 60 {
		s = s[:60]
	}
	if s == "" {
		s = "untitled"
	}
	return s
}

// truncate limits a string to maxLen runes, appending "…" if truncated.
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "…"
}
