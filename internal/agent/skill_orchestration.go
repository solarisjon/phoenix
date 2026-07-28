package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/provider"
)

const skillCheckpointMaxAge = 4 * time.Hour

// SkillExecutionStrategy describes how Phoenix should run a matched skill.
type SkillExecutionStrategy string

const (
	SkillStrategyNone        SkillExecutionStrategy = "none"
	SkillStrategyDirect      SkillExecutionStrategy = "direct"
	SkillStrategyOrchestrate SkillExecutionStrategy = "orchestrate"
)

// SkillContext bundles matched skills and the resolved execution strategy.
type SkillContext struct {
	AllSkills []*model.Skill
	Matched   []*model.Skill
	Strategy  SkillExecutionStrategy
}

// ResolveSkillContext loads skills and determines the execution strategy.
func ResolveSkillContext(ctx context.Context, repo SkillRepoReader, importDirs []string, workingDir string, t *model.Task, proj *model.Project) SkillContext {
	var allSkills []*model.Skill
	if repo != nil {
		if dbSkills, err := repo.ListEnabled(ctx); err == nil {
			allSkills = MergeSkills(dbSkills, DiscoverFilesystemSkills(importDirs, workingDir))
		}
	}
	if len(allSkills) == 0 {
		allSkills = DiscoverFilesystemSkills(importDirs, workingDir)
	}
	matched := MatchSkills(allSkills, t, proj)
	return SkillContext{
		AllSkills: allSkills,
		Matched:   matched,
		Strategy:  ResolveSkillExecutionStrategy(allSkills, t, proj),
	}
}

// SkillRepoReader is the subset of store.SkillRepo needed by skill helpers.
type SkillRepoReader interface {
	ListEnabled(ctx context.Context) ([]*model.Skill, error)
}

// ResolveSkillExecutionStrategy returns how Phoenix should execute skill intent.
func ResolveSkillExecutionStrategy(allSkills []*model.Skill, t *model.Task, proj *model.Project) SkillExecutionStrategy {
	if !TaskHasSkillIntent(allSkills, t, proj) {
		return SkillStrategyNone
	}
	matched := MatchSkills(allSkills, t, proj)
	if len(matched) > 0 && matched[0].ExecutionMode == model.SkillExecutionOrchestrate {
		return SkillStrategyOrchestrate
	}
	return SkillStrategyDirect
}

// TaskShouldUseOrchestrationType reports whether a scheduled/manual task should
// be created as task_type orchestration.
func TaskShouldUseOrchestrationType(ctx context.Context, repo SkillRepoReader, importDirs []string, workingDir string, t *model.Task, proj *model.Project) bool {
	sc := ResolveSkillContext(ctx, repo, importDirs, workingDir, t, proj)
	return sc.Strategy == SkillStrategyOrchestrate
}

// TaskRequestsDirectSkillExecution reports whether direct skill execution mode applies.
func TaskRequestsDirectSkillExecution(ctx context.Context, repo SkillRepoReader, importDirs []string, workingDir string, t *model.Task, proj *model.Project) bool {
	sc := ResolveSkillContext(ctx, repo, importDirs, workingDir, t, proj)
	return sc.Strategy == SkillStrategyDirect
}

// PrimaryMatchedSkill returns the first matched skill, if any.
func PrimaryMatchedSkill(matched []*model.Skill) *model.Skill {
	if len(matched) == 0 {
		return nil
	}
	return matched[0]
}

// SkillOrchestrationModeSection instructs the orchestrator to plan subtasks from skill steps.
func SkillOrchestrationModeSection(skill *model.Skill) string {
	var b strings.Builder
	b.WriteString("## Skill Orchestration Mode\n\n")
	b.WriteString("This task invokes an orchestrate-mode skill. You MUST NOT execute the skill steps yourself.\n")
	b.WriteString("Your job is to confirm the decomposition plan and emit the required JSON orchestration output.\n")
	b.WriteString("Each subtask will be executed by a capable worker agent with the step's skill instructions injected.\n\n")
	if skill != nil && len(skill.Steps) > 0 {
		b.WriteString("The skill defines these steps in order:\n")
		for i, step := range skill.Steps {
			title := step.Title
			if title == "" {
				title = step.Slug
			}
			b.WriteString(fmt.Sprintf("%d. %s (%s)\n", i+1, title, step.Slug))
			if len(step.Outputs) > 0 {
				b.WriteString(fmt.Sprintf("   Expected outputs: %s\n", strings.Join(step.Outputs, ", ")))
			}
		}
		b.WriteString("\n")
	}
	b.WriteString("Respond with JSON only using the orchestration schema from Orchestration Mode.\n")
	b.WriteString("Create one subtask per skill step that still needs to run.\n")
	return b.String()
}

// InjectSkillOrchestrationMode prepends orchestration instructions for skill workflows.
func InjectSkillOrchestrationMode(req provider.TaskRequest, skill *model.Skill) provider.TaskRequest {
	section := SkillOrchestrationModeSection(skill)
	req.SystemPrompt = section + "\n" + req.SystemPrompt
	return req
}

// BuildSkillOrchestrationPlan creates a deterministic orchestration plan from skill steps,
// skipping steps whose checkpoint outputs are fresh enough.
func BuildSkillOrchestrationPlan(skill *model.Skill, workingDir string) (*routedPlan, []string) {
	plan := &routedPlan{
		Confidence: 1.0,
		Rationale:  fmt.Sprintf("Deterministic plan from skill %q", skill.Name),
	}
	var skipped []string
	for _, step := range skill.Steps {
		if stepCheckpointFresh(step, workingDir) {
			skipped = append(skipped, step.Slug)
			continue
		}
		title := step.Title
		if title == "" {
			title = step.Slug
		}
		desc := fmt.Sprintf("Execute skill step %q for %s.", step.Slug, skill.Name)
		if len(step.Outputs) > 0 {
			desc += fmt.Sprintf(" Expected outputs: %s.", strings.Join(step.Outputs, ", "))
		}
		plan.Subtasks = append(plan.Subtasks, routedSubtask{
			OrchestrationSubtask: model.OrchestrationSubtask{
				Title:       title,
				Description: desc,
				Domain:      skillStepDomain(step),
				Complexity:  "medium",
			},
		})
	}
	return plan, skipped
}

func skillStepDomain(step model.SkillStep) string {
	slug := strings.ToLower(step.Slug)
	switch {
	case strings.Contains(slug, "fetch"), strings.Contains(slug, "publish"), strings.Contains(slug, "ops"):
		return "ops"
	case strings.Contains(slug, "report"), strings.Contains(slug, "write"), strings.Contains(slug, "journal"):
		return "write"
	default:
		return "other"
	}
}

func stepCheckpointFresh(step model.SkillStep, workingDir string) bool {
	if len(step.Outputs) == 0 {
		return false
	}
	for _, out := range step.Outputs {
		if !outputCheckpointFresh(out, workingDir) {
			return false
		}
	}
	return true
}

func outputCheckpointFresh(path, workingDir string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return false
	}
	resolved := ExpandSkillPath(path)
	if !filepath.IsAbs(resolved) && workingDir != "" {
		resolved = filepath.Join(workingDir, resolved)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) < skillCheckpointMaxAge
}

// LoadStepInstructions reads sub-skill instructions for an orchestrated step.
func LoadStepInstructions(parentSkill *model.Skill, step model.SkillStep, importDirs []string, workingDir string) string {
	if body := readSubSkillFile(parentSkill.Slug, step.Slug, importDirs, workingDir); body != "" {
		return body
	}
	return stepDescriptionText(step, parentSkill.Name)
}

func stepDescriptionText(step model.SkillStep, parentName string) string {
	title := step.Title
	if title == "" {
		title = step.Slug
	}
	desc := fmt.Sprintf("Run step %q as part of %s.", title, parentName)
	if len(step.Outputs) > 0 {
		desc += fmt.Sprintf(" Produce: %s.", strings.Join(step.Outputs, ", "))
	}
	return desc
}

func readSubSkillFile(parentSlug, stepSlug string, importDirs []string, workingDir string) string {
	parentDir := findSkillDirectory(parentSlug, importDirs, workingDir)
	if parentDir == "" {
		return ""
	}
	candidates := []string{
		filepath.Join(parentDir, stepSlug, "SKILL.md"),
		filepath.Join(parentDir, strings.TrimPrefix(stepSlug, parentSlug+"/"), "SKILL.md"),
	}
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		_, _, _, body := parseSkillMarkdown(string(data))
		if strings.TrimSpace(body) != "" {
			return body
		}
	}
	return ""
}

func findSkillDirectory(slug string, importDirs []string, workingDir string) string {
	slug = NormalizeSkillSlug(slug)
	for _, root := range SkillSearchRoots(importDirs, workingDir) {
		for _, entry := range scanSkillRoot(root) {
			if NormalizeSkillSlug(entry.Skill.Slug) == slug {
				return filepath.Dir(entry.SourcePath)
			}
		}
	}
	return ""
}

// VerifyDeliverables checks expected outputs for a workflow run.
func VerifyDeliverables(steps []model.SkillStep, workingDir string, startedAt *time.Time) []model.WorkflowDeliverable {
	var out []model.WorkflowDeliverable
	seen := make(map[string]bool)
	for _, step := range steps {
		for _, path := range step.Outputs {
			path = strings.TrimSpace(path)
			if path == "" || seen[path] {
				continue
			}
			seen[path] = true
			d := model.WorkflowDeliverable{
				Path:     path,
				Title:    step.Title,
				StepSlug: step.Slug,
				Kind:     deliverableKind(path),
			}
			d.Verified = verifyDeliverable(path, workingDir, startedAt)
			out = append(out, d)
		}
	}
	return out
}

func deliverableKind(path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return "url"
	}
	return "file"
}

func verifyDeliverable(path, workingDir string, startedAt *time.Time) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return verifyURLDeliverable(path)
	}
	resolved := ExpandSkillPath(path)
	if !filepath.IsAbs(resolved) && workingDir != "" {
		resolved = filepath.Join(workingDir, resolved)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return false
	}
	if startedAt != nil && info.ModTime().Before(startedAt.Add(-time.Minute)) {
		return false
	}
	return true
}

func verifyURLDeliverable(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(rawURL)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 400
}

// DeriveWorkflowHealth computes run health from subtasks and deliverables.
func DeriveWorkflowHealth(root *model.Task, subtasks []*model.Task, deliverables []model.WorkflowDeliverable, agentSignal string) string {
	for _, st := range subtasks {
		switch st.Status {
		case model.TaskStatusFailed:
			return "failed"
		}
	}
	pending := false
	for _, st := range subtasks {
		if st.Status != model.TaskStatusCompleted {
			pending = true
			break
		}
	}
	if pending {
		if root != nil && (root.Status == model.TaskStatusRunning || root.Status == model.TaskStatusQueued || root.Status == model.TaskStatusPending) {
			return "needs_attention"
		}
	}
	if len(deliverables) > 0 {
		for _, d := range deliverables {
			if !d.Verified {
				if pending {
					continue
				}
				return "needs_attention"
			}
		}
	}
	if len(subtasks) == 0 && agentSignal == "failed" {
		return "failed"
	}
	if agentSignal == "needs_attention" {
		return "needs_attention"
	}
	if pending {
		return "needs_attention"
	}
	return "all_clear"
}

// MarshalDeliverables serialises deliverables for task storage.
func MarshalDeliverables(items []model.WorkflowDeliverable) string {
	if len(items) == 0 {
		return "[]"
	}
	b, err := json.Marshal(items)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// ParseDeliverables deserialises stored deliverables JSON.
func ParseDeliverables(raw string) []model.WorkflowDeliverable {
	if strings.TrimSpace(raw) == "" || raw == "[]" {
		return nil
	}
	var out []model.WorkflowDeliverable
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

// BuildWorkflowRun aggregates a workflow run for API responses.
func BuildWorkflowRun(root *model.Task, subtasks []*model.Task, skill *model.Skill, workingDir string) *model.WorkflowRun {
	if root == nil {
		return nil
	}
	steps := []model.SkillStep{}
	if skill != nil {
		steps = skill.Steps
	}
	deliverables := ParseDeliverables(root.DeliverablesJSON)
	if len(deliverables) == 0 && len(steps) > 0 {
		deliverables = VerifyDeliverables(steps, workingDir, root.StartedAt)
	}
	agentSignal := ""
	if root.HealthSignal != nil {
		agentSignal = *root.HealthSignal
	}
	derived := root.DerivedHealth
	if derived == "" {
		derived = DeriveWorkflowHealth(root, subtasks, deliverables, agentSignal)
	}
	totalCost := root.CostUSD
	for _, st := range subtasks {
		totalCost += st.CostUSD
	}
	stepsComplete := 0
	for _, st := range subtasks {
		if st.Status == model.TaskStatusCompleted {
			stepsComplete++
		}
	}
	durationSec := 0
	if root.StartedAt != nil && root.CompletedAt != nil {
		durationSec = int(root.CompletedAt.Sub(*root.StartedAt).Seconds())
	}
	return &model.WorkflowRun{
		RootTask:      root,
		Subtasks:      subtasks,
		Plan:          root.OrchestrationPlan,
		DerivedHealth: derived,
		Deliverables:  deliverables,
		TotalCost:     totalCost,
		DurationSec:   durationSec,
		StepsComplete: stepsComplete,
		StepsTotal:    len(subtasks),
	}
}
