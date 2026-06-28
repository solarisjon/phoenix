package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/provider"
)

// ---- Memo extraction ----

// extractAndSaveMemos scans agent output for MEMO blocks and persists each one.
// A MEMO block looks like:
//
//	MEMO_START
//	Title: <single line title>
//	Priority: high          (optional; defaults to normal)
//	<body markdown — everything until MEMO_END>
//	MEMO_END
//
// Multiple blocks are supported in a single output.
// extractAndSaveMemos scans agent output for MEMO blocks and persists each one.
// Returns true if at least one memo was saved.
func (r *Runner) extractAndSaveMemos(task *model.Task, a *model.Agent, output string) bool {
	memoBlocks := parseMemoBlocks(output)
	if len(memoBlocks) == 0 {
		return false
	}

	// Look up project name for display (best-effort; empty string is fine).
	var projectName string
	if proj, err := r.projects.Get(r.bgCtx, task.ProjectID); err == nil && proj != nil {
		projectName = proj.Name
	}

	saved := false
	for _, block := range memoBlocks {
		memo := &model.Memo{
			ID:          uuid.New().String(),
			ProjectID:   task.ProjectID,
			ProjectName: projectName,
			TaskID:      task.ID,
			AgentID:     a.ID,
			AgentName:   a.Name,
			Title:       block.title,
			Body:        block.body,
			Priority:    block.priority,
			Status:      model.MemoStatusUnread,
			CreatedAt:   time.Now(),
		}
		if err := r.memos.Create(r.bgCtx, memo); err != nil {
			slog.Error("runner: save memo from task", "task_id", task.ID, "error", err)
			continue
		}
		slog.Info("runner: memo saved from task", "task_id", task.ID, "title", memo.Title)
		if r.onMemo != nil {
			r.onMemo(memo)
		}
		saved = true
	}
	return saved
}

// autoMemo creates a fallback memo for any task that completed successfully but
// whose agent didn't emit a MEMO_START block. This ensures the Briefing always
// reflects what every completed task did, even when the agent skips the memo.
func (r *Runner) autoMemo(task *model.Task, a *model.Agent, output string) {
	var projectName string
	if proj, err := r.projects.Get(r.bgCtx, task.ProjectID); err == nil && proj != nil {
		projectName = proj.Name
	}

	// Truncate very long outputs so the memo body is readable.
	body := output
	const maxBody = 4000
	if len(body) > maxBody {
		body = body[:maxBody] + "\n\n_[output truncated — open the task for the full run log]_"
	}

	memo := &model.Memo{
		ID:          uuid.New().String(),
		ProjectID:   task.ProjectID,
		ProjectName: projectName,
		TaskID:      task.ID,
		AgentID:     a.ID,
		AgentName:   a.Name,
		Title:       task.Title,
		Body:        body,
		Priority:    model.MemoPriorityNormal,
		Status:      model.MemoStatusUnread,
		CreatedAt:   time.Now(),
	}
	if err := r.memos.Create(r.bgCtx, memo); err != nil {
		slog.Error("runner: auto-memo for task", "task_id", task.ID, "error", err)
		return
	}
	slog.Info("runner: auto-memo created", "task_id", task.ID, "title", memo.Title)
	if r.onMemo != nil {
		r.onMemo(memo)
	}
}

// ---- Critic / Devil's Advocate ----

// maybeLaunchCritic resolves the effective critic mode for the completed task
// (task-level overrides project-level) and launches a critic task if needed.
func (r *Runner) maybeLaunchCritic(task *model.Task, originalAgent *model.Agent) {
	// Resolve effective critic mode: task setting, unless it says "inherit".
	mode := task.CriticMode
	if mode == "" || mode == model.CriticModeInherit {
		// Fall back to project setting.
		if proj, err := r.projects.Get(r.bgCtx, task.ProjectID); err == nil && proj != nil {
			mode = proj.CriticMode
		}
	}
	if mode == "" || mode == model.CriticModeNone {
		return
	}

	switch {
	case mode == model.CriticModeBuiltin:
		r.launchBuiltinCritic(task, originalAgent)
	case len(mode) > 6 && mode[:6] == "agent:":
		agentID := mode[6:]
		r.launchAgentCritic(task, agentID)
	}
}

// launchBuiltinCritic spawns an ephemeral devil's advocate review using the
// same provider as the original agent — no registered agent record needed.
// Routes through RunTask so MaxConcurrent is respected like any other task.
func (r *Runner) launchBuiltinCritic(task *model.Task, originalAgent *model.Agent) {
	criticTask := &model.Task{
		ID:             uuid.New().String(),
		ProjectID:      task.ProjectID,
		AgentID:        originalAgent.ID, // same agent; execute() swaps in the critic system prompt
		Title:          "Devil's Advocate: " + task.Title,
		Status:         model.TaskStatusPending,
		Source:         "critic",
		IsCriticReview: true,
		CriticMode:     model.CriticModeBuiltin,
		ReviewedTaskID: &task.ID,
		CreatedAt:      time.Now(),
	}
	if err := r.tasks.Create(r.bgCtx, criticTask); err != nil {
		slog.Error("runner: create builtin critic task", "error", err)
		return
	}
	if err := r.RunTask(r.bgCtx, criticTask.ID); err != nil {
		slog.Error("runner: run builtin critic task", "error", err)
	}
}

// launchAgentCritic spawns a critic task using a specific registered agent.
func (r *Runner) launchAgentCritic(task *model.Task, criticAgentID string) {
	criticAgent, err := r.agents.Get(r.bgCtx, criticAgentID)
	if err != nil || criticAgent == nil {
		slog.Error("runner: critic agent not found", "agent_id", criticAgentID, "error", err)
		return
	}
	criticTask := &model.Task{
		ID:             uuid.New().String(),
		ProjectID:      task.ProjectID,
		AgentID:        criticAgentID,
		Title:          "Critic Review: " + task.Title,
		Description:    "You are reviewing the output of a completed task. Provide an objective critique: what was done well, what could be improved, any risks or concerns.\n\nOriginal Task: " + task.Title + "\n\nTask Output:\n" + task.Output,
		Status:         model.TaskStatusPending,
		Source:         "critic",
		IsCriticReview: true,
		CriticMode:     "agent:" + criticAgentID,
		ReviewedTaskID: &task.ID,
		CreatedAt:      time.Now(),
	}
	if err := r.tasks.Create(r.bgCtx, criticTask); err != nil {
		slog.Error("runner: create agent critic task", "error", err)
		return
	}
	if err := r.RunTask(r.bgCtx, criticTask.ID); err != nil {
		slog.Error("runner: run agent critic task", "error", err)
	}
}

type parsedMemo struct {
	title    string
	body     string
	priority model.MemoPriority
}

// parseMemoBlocks extracts all MEMO_START … MEMO_END sections from text.
func parseMemoBlocks(output string) []parsedMemo {
	var results []parsedMemo
	lines := strings.Split(output, "\n")

	i := 0
	for i < len(lines) {
		if strings.TrimSpace(lines[i]) != "MEMO_START" {
			i++
			continue
		}
		// Found a block start — collect until MEMO_END.
		i++
		var title string
		priority := model.MemoPriorityNormal
		var bodyLines []string
		headerDone := false

		for i < len(lines) {
			if strings.TrimSpace(lines[i]) == "MEMO_END" {
				i++
				break
			}
			line := lines[i]
			if !headerDone {
				if strings.HasPrefix(line, "Title:") {
					title = strings.TrimSpace(strings.TrimPrefix(line, "Title:"))
					i++
					continue
				}
				if strings.HasPrefix(line, "Priority:") {
					pval := strings.TrimSpace(strings.ToLower(strings.TrimPrefix(line, "Priority:")))
					if pval == "high" {
						priority = model.MemoPriorityHigh
					}
					i++
					continue
				}
				// First non-header line starts the body.
				headerDone = true
			}
			bodyLines = append(bodyLines, line)
			i++
		}

		if title == "" || len(bodyLines) == 0 {
			continue // skip malformed blocks
		}
		results = append(results, parsedMemo{
			title:    title,
			body:     strings.TrimSpace(strings.Join(bodyLines, "\n")),
			priority: priority,
		})
	}
	return results
}

// ---- Artifact extraction ----

// extractAndSaveArtifacts scans agent output for ARTIFACT_START blocks and creates
// a briefing memo entry for each one so they appear in the Briefing panel.
// For obsidian-type artifacts the agent specifies the full path; the runner
// ensures the parent directory exists and writes the file content from the
// block body (if provided), then records a memo pointing to the written file.
func (r *Runner) extractAndSaveArtifacts(task *model.Task, a *model.Agent, output string) {
	artifacts := ParseArtifactBlocks(output)
	if len(artifacts) == 0 {
		return
	}
	var projectName string
	if proj, err := r.projects.Get(r.bgCtx, task.ProjectID); err == nil && proj != nil {
		projectName = proj.Name
	}
	for _, art := range artifacts {
		title := art.Title
		if title == "" {
			title = art.Path
		}

		// Skip obsidian-type artifacts when the plugin is disabled.
		if art.ArtType == "obsidian" {
			if sysSettings, err := r.settings.Get(r.bgCtx); err != nil || !sysSettings.ObsidianEnabled {
				continue
			}
			if err := os.MkdirAll(filepath.Dir(art.Path), 0755); err != nil {
				slog.Warn("runner: obsidian mkdirAll failed", "path", art.Path, "error", err)
			}
		}

		body := fmt.Sprintf("**Type:** %s\n\n**Location:** %s", art.ArtType, art.Path)
		if art.ArtType == "obsidian" && art.Vault != "" {
			body = fmt.Sprintf("**Vault:** %s\n\n**File:** %s", art.Vault, art.Path)
		}

		// Set artifact_path for file-type .md artifacts so the Briefing UI can render them inline.
		artifactPath := ""
		if art.ArtType == "file" && strings.HasSuffix(strings.ToLower(art.Path), ".md") {
			artifactPath = art.Path
		}

		memo := &model.Memo{
			ID:           uuid.New().String(),
			ProjectID:    task.ProjectID,
			ProjectName:  projectName,
			TaskID:       task.ID,
			AgentID:      a.ID,
			AgentName:    a.Name,
			Title:        "Artifact: " + title,
			Body:         body,
			ArtifactPath: artifactPath,
			Priority:     model.MemoPriorityNormal,
			Status:       model.MemoStatusUnread,
			CreatedAt:    time.Now(),
		}
		if err := r.memos.Create(r.bgCtx, memo); err != nil {
			slog.Error("runner: save artifact memo", "task_id", task.ID, "error", err)
			continue
		}
		slog.Info("runner: artifact memo saved", "task_id", task.ID, "title", memo.Title)
		if r.onMemo != nil {
			r.onMemo(memo)
		}
	}
}

// maybeAutoWriteObsidian fires a background goroutine that generates and writes
// an Obsidian note for the completed task when obsidian_auto_write=1 and at
// least one vault is configured. Errors are logged but never surface to the user.
func (r *Runner) maybeAutoWriteObsidian(task *model.Task, a *model.Agent, output string) {
	if r.obsidianVaults == nil || r.settings == nil {
		return
	}
	settings, err := r.settings.Get(r.bgCtx)
	if err != nil || !settings.ObsidianEnabled || !settings.ObsidianAutoWrite || settings.ObsidianRoot == "" {
		return
	}
	vaults, err := r.obsidianVaults.ListEnabled(r.bgCtx)
	if err != nil || len(vaults) == 0 {
		return
	}

	outputText := extractOutputText(output)
	if strings.TrimSpace(outputText) == "" {
		return
	}

	go func() {
		prov, err := r.registry.Get(r.bgCtx, a.ProviderID)
		if err != nil {
			slog.Warn("obsidian auto-write: provider load failed", "task_id", task.ID, "error", err)
			return
		}

		agentName := a.Name
		projectName := task.ProjectID
		if proj, err := r.projects.Get(r.bgCtx, task.ProjectID); err == nil && proj != nil {
			projectName = proj.Name
		}
		dateStr := time.Now().Format("2006-01-02")

		// Pick the best vault.
		targetVault := vaults[0]
		if len(vaults) > 1 {
			var routing strings.Builder
			for _, v := range vaults {
				routing.WriteString(fmt.Sprintf("- %s: %s\n", v.Name, v.Context))
			}
			pickPrompt := fmt.Sprintf(`Choose the most appropriate Obsidian vault for this task output.
Task: %s
Agent: %s
Output summary: %s
Vaults:
%s
Reply with ONLY the vault name.`, task.Title, agentName, truncateStr(outputText, 800), routing.String())

			resp, err := prov.Execute(r.bgCtx, provider.TaskRequest{
				SystemPrompt: "Output only the vault name.",
				Prompt:       pickPrompt,
			})
			if err == nil {
				picked := strings.TrimSpace(resp.Output)
				for _, v := range vaults {
					if strings.EqualFold(v.Name, picked) {
						targetVault = v
						break
					}
				}
			}
		}

		// Generate note content.
		notePrompt := fmt.Sprintf(`Convert this task output into a well-formatted Obsidian Markdown note.

Task: %s
Agent: %s
Project: %s
Date: %s

Task output:
%s

Requirements:
1. YAML front matter: date, tags (phoenix, %s, %s), source (phoenix-task), task_id (%s), agent, project
2. H1 heading as title
3. Clean Markdown body — use headings, bullets, tables as appropriate
4. Closing footer: "Generated by Phoenix agent: %s on %s"
Output ONLY the Markdown content.`,
			task.Title, agentName, projectName, dateStr,
			outputText, agentName, projectName, task.ID, agentName, dateStr)

		noteResp, err := prov.Execute(r.bgCtx, provider.TaskRequest{
			SystemPrompt: "You produce clean Obsidian Markdown notes. Output only the Markdown.",
			Prompt:       notePrompt,
		})
		if err != nil {
			slog.Warn("obsidian auto-write: note generation failed", "task_id", task.ID, "error", err)
			return
		}

		slug := slugifyStr(task.Title)
		filename := fmt.Sprintf("%s-%s.md", dateStr, slug)
		filePath := fmt.Sprintf("%s/%s", targetVault.Path, filename)
		// Handle collisions.
		if _, statErr := os.Stat(filePath); statErr == nil {
			for i := 2; i <= 99; i++ {
				candidate := fmt.Sprintf("%s/%s-%s-%d.md", targetVault.Path, dateStr, slug, i)
				if _, statErr := os.Stat(candidate); os.IsNotExist(statErr) {
					filePath = candidate
					filename = fmt.Sprintf("%s-%s-%d.md", dateStr, slug, i)
					break
				}
			}
		}

		if err := os.WriteFile(filePath, []byte(noteResp.Output), 0644); err != nil {
			slog.Warn("obsidian auto-write: write failed", "task_id", task.ID, "path", filePath, "error", err)
			return
		}
		slog.Info("obsidian auto-write: note written", "task_id", task.ID, "path", filePath)

		// Create a memo pointing to the note.
		if r.memos != nil {
			var projectNameForMemo string
			if proj, err := r.projects.Get(r.bgCtx, task.ProjectID); err == nil && proj != nil {
				projectNameForMemo = proj.Name
			}
			memo := &model.Memo{
				ID:          uuid.New().String(),
				ProjectID:   task.ProjectID,
				ProjectName: projectNameForMemo,
				TaskID:      task.ID,
				AgentID:     a.ID,
				AgentName:   a.Name,
				Title:       "Obsidian note: " + task.Title,
				Body:        fmt.Sprintf("**Vault:** %s\n\n**File:** %s", targetVault.Name, filePath),
				Priority:    model.MemoPriorityNormal,
				Status:      model.MemoStatusUnread,
				CreatedAt:   time.Now(),
			}
			if createErr := r.memos.Create(r.bgCtx, memo); createErr != nil {
				slog.Error("obsidian auto-write: save memo", "error", createErr)
			} else if r.onMemo != nil {
				r.onMemo(memo)
			}
		}
	}()
}

// slugifyStr converts a title to lowercase-kebab-case for use in filenames.
func slugifyStr(title string) string {
	title = strings.ToLower(title)
	var b strings.Builder
	for _, ch := range title {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= '0' && ch <= '9':
			b.WriteRune(ch)
		case ch == ' ' || ch == '-' || ch == '_':
			b.WriteByte('-')
		}
	}
	s := strings.Trim(b.String(), "-")
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

// truncateStr limits s to maxLen runes, appending "…" if truncated.
func truncateStr(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "…"
}

// deriveHealthSignal inspects the output text of a completed monitor task and
// returns one of three health signals:
//   - "all_clear"       — completed successfully, no issues detected
//   - "needs_attention" — completed but issues detected
//   - "failed"          — task itself failed (set separately in failTask)
//
// Structured output is checked first: if the agent emits a line starting with
// "HEALTH_SIGNAL:" that value is used directly. This avoids false positives from
// keyword matching (e.g. "no errors found" or "error rate: 0%").
// Falls back to keyword scanning when no structured marker is present.
func deriveHealthSignal(output string) string {
	const marker = "HEALTH_SIGNAL:"
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, marker) {
			val := strings.TrimSpace(strings.TrimPrefix(trimmed, marker))
			switch strings.ToLower(val) {
			case "all_clear":
				return "all_clear"
			case "needs_attention":
				return "needs_attention"
			case "failed":
				return "failed"
			}
		}
	}

	// Fallback: keyword scan for agents that predate structured output.
	lower := strings.ToLower(output)
	alertKeywords := []string{
		"error", "warning", "alert", "critical", "failure", "fail", "issue",
		"problem", "exception", "danger", "anomaly", "breach", "exceeded",
		"unavailable", "down", "offline", "unreachable", "timeout", "timed out",
	}
	for _, kw := range alertKeywords {
		if strings.Contains(lower, kw) {
			return "needs_attention"
		}
	}
	return "all_clear"
}

// extractGuardrailTrigger scans the agent output for a hard guardrail trigger.
// It looks for a line that starts exactly with "GUARDRAIL_TRIGGERED:" and returns
// the reason text. Returns "" if no trigger is found.
// The match is anchored to the start of a line to avoid false positives in explanatory text.
func extractGuardrailTrigger(output string) string {
	const marker = "GUARDRAIL_TRIGGERED:"
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, marker) {
			reason := strings.TrimSpace(strings.TrimPrefix(trimmed, marker))
			if reason == "" {
				reason = "Hard guardrail triggered (no reason provided)"
			}
			return reason
		}
	}
	return ""
}

// ---- Heartbeat reaction ----

// updateHeartbeatSignal updates the consecutive-bad counter and last signal
// on a monitor project. Returns the updated consecutive-bad count.
func (r *Runner) updateHeartbeatSignal(proj *model.Project, signal string) int {
	consecutive := 0
	if signal == "all_clear" {
		consecutive = 0
	} else {
		if proj.HeartbeatLastSignal != "all_clear" {
			consecutive = proj.HeartbeatConsecutiveBad + 1
		} else {
			consecutive = 1
		}
	}
	if err := r.projects.UpdateHeartbeatSignal(r.bgCtx, proj.ID, signal, consecutive); err != nil {
		slog.Error("runner: update heartbeat signal", "project_id", proj.ID, "error", err)
	}
	return consecutive
}

// maybeReactToHealthSignal checks whether a monitor project has a configured
// reaction for the given health signal and executes it.
//
//   - "spawn": creates a remediation task in the linked project, injecting the
//     monitor output as context so the fixer agent knows what happened.
//   - "notify": delegates to the notification plugin system (same events as
//     task.completed/failed, but with health_signal metadata).
//   - "escalate": promotes needs_attention to failed severity in the signal
//     (handled upstream by the caller if consecutive threshold is reached).
func (r *Runner) maybeReactToHealthSignal(task *model.Task, proj *model.Project, signal string, consecutiveBad int) {
	action := proj.HeartbeatOnAttention
	if signal == "failed" {
		action = proj.HeartbeatOnFailed
	}
	if action == "" {
		return
	}

	switch action {
	case "spawn":
		if proj.LinkedProjectID == nil || *proj.LinkedProjectID == "" {
			slog.Warn("runner: heartbeat spawn requested but no linked_project_id set", "monitor_id", proj.ID)
			return
		}
		r.spawnRemediationTask(task, proj, signal, consecutiveBad)
	case "escalate":
		// Only escalate once the consecutive-bad threshold is reached.
		// HeartbeatEscalateAfter == 0 means escalate on first non-clear signal.
		if proj.HeartbeatEscalateAfter > 0 && consecutiveBad < proj.HeartbeatEscalateAfter {
			slog.Debug("runner: heartbeat escalation pending threshold",
				"monitor", proj.Name, "consecutive_bad", consecutiveBad, "threshold", proj.HeartbeatEscalateAfter)
			return
		}
		slog.Warn("runner: heartbeat escalation triggered",
			"monitor", proj.Name, "signal", signal, "consecutive_bad", consecutiveBad)
	case "notify":
		// Notification plugins already fire on task.completed with the health_signal
		// field present in the task payload. No additional work needed here.
	}
}

// spawnRemediationTask creates a task in the linked project with the monitor
// output injected as context, so the remediation agent knows what triggered it.
func (r *Runner) spawnRemediationTask(task *model.Task, proj *model.Project, signal string, consecutiveBad int) {
	linkedProj, err := r.projects.Get(r.bgCtx, *proj.LinkedProjectID)
	if err != nil || linkedProj == nil {
		slog.Error("runner: heartbeat spawn: linked project not found",
			"linked_project_id", *proj.LinkedProjectID, "error", err)
		return
	}

	agents, err := r.projects.ListAgents(r.bgCtx, linkedProj.ID)
	if err != nil || len(agents) == 0 {
		slog.Error("runner: heartbeat spawn: no agents on linked project",
			"linked_project_id", linkedProj.ID, "error", err)
		return
	}
	var targetAgent *model.Agent
	for _, a := range agents {
		if a.Status == model.AgentStatusActive {
			targetAgent = a
			break
		}
	}
	if targetAgent == nil {
		slog.Error("runner: heartbeat spawn: no active agents on linked project", "linked_project_id", linkedProj.ID)
		return
	}

	monitorOutput := extractOutputText(task.Output)
	consecutiveNote := ""
	if consecutiveBad > 1 {
		consecutiveNote = fmt.Sprintf(" (consecutive alert #%d)", consecutiveBad)
	}

	description := fmt.Sprintf(`## Monitor Alert Context%s

**Monitor:** %s
**Signal:** %s
**Triggered at:** %s

### Monitor Output

%s

---

Investigate and remediate the issue described above. When done, emit:

TASK_COMPLETE: <brief summary of what you fixed or determined>`,
		consecutiveNote,
		proj.Name,
		signal,
		task.CreatedAt.Format("2006-01-02 15:04:05"),
		monitorOutput,
	)

	remTask := &model.Task{
		ID:          uuid.New().String(),
		ProjectID:   linkedProj.ID,
		AgentID:     targetAgent.ID,
		Title:       fmt.Sprintf("Remediate: %s (%s)", proj.Name, signal),
		Description: description,
		Status:      model.TaskStatusPending,
		Source:      "heartbeat:" + proj.Name,
		Input:       "{}",
		Output:      "{}",
		CreatedAt:   time.Now(),
	}
	if err := r.tasks.Create(r.bgCtx, remTask); err != nil {
		slog.Error("runner: heartbeat spawn: create task", "error", err)
		return
	}
	if err := r.RunTask(r.bgCtx, remTask.ID); err != nil {
		slog.Error("runner: heartbeat spawn: run task", "error", err)
	}
	slog.Info("runner: heartbeat spawned remediation task",
		"task_id", remTask.ID, "monitor", proj.Name, "signal", signal)
}

// ---- ReAct loop ----

const reactMaxIterationsDefault = 10

// parseNextAction extracts the content of a NEXT_ACTION block from agent output.
// Returns ("", false) when no block is found.
func parseNextAction(output string) (string, bool) {
	const start = "NEXT_ACTION:"
	const end = "END_NEXT_ACTION"
	startIdx := strings.Index(output, start)
	if startIdx < 0 {
		return "", false
	}
	body := output[startIdx+len(start):]
	endIdx := strings.Index(body, end)
	if endIdx >= 0 {
		body = body[:endIdx]
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return "", false
	}
	return body, true
}

// parseTaskComplete checks whether the agent signalled loop termination.
// Looks for "TASK_COMPLETE:" anywhere in the output.
func parseTaskComplete(output string) bool {
	return strings.Contains(output, "TASK_COMPLETE:")
}

// maybeSpawnReActIteration checks whether the completed task belongs to a
// react_mode project and the output contains a NEXT_ACTION directive. If so,
// it auto-creates and enqueues the next iteration as a follow-up task.
func (r *Runner) maybeSpawnReActIteration(task *model.Task, proj *model.Project, output string) {
	if proj == nil || !proj.ReactMode || task.IsCriticReview {
		return
	}

	maxIter := proj.MaxIterations
	if maxIter <= 0 {
		maxIter = reactMaxIterationsDefault
	}

	if task.LoopIteration+1 >= maxIter {
		slog.Info("runner: react loop reached max iterations — stopping",
			"task_id", task.ID, "iteration", task.LoopIteration, "max", maxIter)
		return
	}

	if parseTaskComplete(output) {
		slog.Info("runner: react loop TASK_COMPLETE — stopping",
			"task_id", task.ID, "iteration", task.LoopIteration)
		return
	}

	nextAction, ok := parseNextAction(output)
	if !ok {
		return
	}

	followUpID := task.ID
	nextTask := &model.Task{
		ID:            uuid.New().String(),
		ProjectID:     task.ProjectID,
		AgentID:       task.AgentID,
		FollowUpOf:    &followUpID,
		Title:         fmt.Sprintf("%s (iteration %d)", stripIterationSuffix(task.Title), task.LoopIteration+1),
		Description:   nextAction,
		Status:        model.TaskStatusPending,
		Source:        task.Source,
		Input:         "{}",
		Output:        "{}",
		LoopIteration: task.LoopIteration + 1,
		CreatedAt:     time.Now(),
	}
	if err := r.tasks.Create(r.bgCtx, nextTask); err != nil {
		slog.Error("runner: react loop: create next task", "error", err)
		return
	}
	if err := r.RunTask(r.bgCtx, nextTask.ID); err != nil {
		slog.Error("runner: react loop: run next task", "error", err)
		return
	}
	slog.Info("runner: react loop spawned next iteration",
		"task_id", nextTask.ID, "iteration", nextTask.LoopIteration)
}

// stripIterationSuffix removes a trailing " (iteration N)" from a title so
// the next iteration doesn't accumulate duplicate suffixes.
func stripIterationSuffix(title string) string {
	// e.g. "Fix the bug (iteration 2)" → "Fix the bug"
	if idx := strings.LastIndex(title, " (iteration "); idx > 0 {
		return title[:idx]
	}
	return title
}

// ---- Prompt hash ----

// promptHash returns a hex SHA-256 of the assembled prompt components.
// Used by monitor diffing to detect unchanged prompts and skip the LLM call.
func promptHash(req provider.TaskRequest) string {
	h := sha256.New()
	h.Write([]byte(req.SystemPrompt))
	h.Write([]byte{0})
	h.Write([]byte(req.Prompt))
	for _, m := range req.Context {
		h.Write([]byte{0})
		h.Write([]byte(m.Role))
		h.Write([]byte{0})
		h.Write([]byte(m.Content))
	}
	return hex.EncodeToString(h.Sum(nil))
}
