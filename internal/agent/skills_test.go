package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/provider"
)

func TestMatchSkills_BySlugAndDefault(t *testing.T) {
	skills := []*model.Skill{
		{ID: "1", Name: "Morning Coffee", Slug: "morning_coffee", Enabled: true},
		{ID: "2", Name: "Outlook Sync", Slug: "outlook-to-gcal", Enabled: true},
	}
	defaultID := "1"
	proj := &model.Project{
		Objective:      "Run the skill called morning_coffee.",
		DefaultSkillID: &defaultID,
	}
	task := &model.Task{Title: "Scheduled run", Description: proj.Objective}

	matched := MatchSkills(skills, task, proj)
	if len(matched) != 1 {
		t.Fatalf("matched %d skills, want 1", len(matched))
	}
	if matched[0].Slug != "morning_coffee" {
		t.Fatalf("matched slug = %q, want morning_coffee", matched[0].Slug)
	}

	task2 := &model.Task{Description: "Use the outlook-to-gcal skill today"}
	matched2 := MatchSkills(skills, task2, nil)
	if len(matched2) != 1 || matched2[0].Slug != "outlook-to-gcal" {
		t.Fatalf("expected outlook-to-gcal match, got %#v", matched2)
	}
}

func TestTaskHasSkillIntent_WithoutRegisteredSkill(t *testing.T) {
	proj := &model.Project{Objective: "Run the skill called morning_coffee."}
	task := &model.Task{Description: proj.Objective}
	if !TaskHasSkillIntent(nil, task, proj) {
		t.Fatal("expected skill intent from objective text")
	}
}

func TestResolveSkillExecutionStrategy_Orchestrate(t *testing.T) {
	skills := []*model.Skill{{
		ID: "1", Name: "Morning Coffee", Slug: "morning_coffee", Enabled: true,
		ExecutionMode: model.SkillExecutionOrchestrate,
	}}
	proj := &model.Project{Objective: "Run the skill called morning_coffee."}
	task := &model.Task{Description: proj.Objective}
	got := ResolveSkillExecutionStrategy(skills, task, proj)
	if got != SkillStrategyOrchestrate {
		t.Fatalf("strategy = %q, want orchestrate", got)
	}
}

func TestResolveSkillExecutionStrategy_Direct(t *testing.T) {
	skills := []*model.Skill{{
		ID: "1", Name: "Outlook Sync", Slug: "outlook-to-gcal", Enabled: true,
		ExecutionMode: model.SkillExecutionDirect,
	}}
	task := &model.Task{Description: "run the outlook-to-gcal skill"}
	got := ResolveSkillExecutionStrategy(skills, task, nil)
	if got != SkillStrategyDirect {
		t.Fatalf("strategy = %q, want direct", got)
	}
}

func TestInjectSkillExecutionMode_OverridesRoutingPersona(t *testing.T) {
	req := provider.TaskRequest{SystemPrompt: "You never attempt to perform the task yourself."}
	out := InjectSkillExecutionMode(req, nil, "run the morning_coffee skill")
	if !strings.Contains(out.SystemPrompt, "## Skill Execution Mode") {
		t.Fatal("missing skill execution mode section")
	}
	if !strings.Contains(out.SystemPrompt, "Do NOT route, delegate, decompose") {
		t.Fatal("missing direct execution instruction")
	}
}

func TestInjectSkillOrchestrationMode(t *testing.T) {
	skill := &model.Skill{Name: "Morning Coffee", ExecutionMode: model.SkillExecutionOrchestrate, Steps: []model.SkillStep{{Slug: "fetch-data", Title: "Fetch data"}}}
	req := provider.TaskRequest{SystemPrompt: "You are an orchestrator."}
	out := InjectSkillOrchestrationMode(req, skill)
	if !strings.Contains(out.SystemPrompt, "## Skill Orchestration Mode") {
		t.Fatal("missing orchestration mode section")
	}
	if !strings.Contains(out.SystemPrompt, "fetch-data") {
		t.Fatal("expected step slug in orchestration instructions")
	}
}

func TestBuildSkillOrchestrationPlan(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	skill := &model.Skill{
		Name: "demo",
		Steps: []model.SkillStep{
			{Slug: "fresh", Title: "Fresh step", Outputs: []string{path}},
			{Slug: "run-me", Title: "Needs run"},
		},
	}
	plan, skipped := BuildSkillOrchestrationPlan(skill, dir)
	if len(skipped) != 1 || skipped[0] != "fresh" {
		t.Fatalf("skipped = %#v, want [fresh]", skipped)
	}
	if len(plan.Subtasks) != 1 || plan.Subtasks[0].Title != "Needs run" {
		t.Fatalf("plan subtasks = %#v", plan.Subtasks)
	}
}

func TestDeriveWorkflowHealth(t *testing.T) {
	root := &model.Task{Status: model.TaskStatusCompleted}
	subtasks := []*model.Task{
		{Status: model.TaskStatusCompleted},
		{Status: model.TaskStatusFailed},
	}
	if got := DeriveWorkflowHealth(root, subtasks, nil, "all_clear"); got != "failed" {
		t.Fatalf("health = %q, want failed", got)
	}
}

func TestTaskRequestsSkillExecution(t *testing.T) {
	proj := &model.Project{Objective: "Run the skill called morning_coffee."}
	task := &model.Task{Description: proj.Objective}
	if !TaskRequestsSkillExecution(context.Background(), nil, nil, "", task, proj) {
		t.Fatal("expected skill execution request")
	}
}

func TestDiscoverFilesystemSkills(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "demo-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: demo-skill\ndescription: Demo skill for tests\nexecution_mode: orchestrate\nsteps_json: [{\"slug\":\"step-a\",\"title\":\"Step A\"}]\n---\n\nDo the demo thing.\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	skills := DiscoverFilesystemSkillsIn([]string{dir})
	if len(skills) != 1 {
		t.Fatalf("discovered %d skills, want 1", len(skills))
	}
	if skills[0].ExecutionMode != model.SkillExecutionOrchestrate {
		t.Fatalf("execution_mode = %q", skills[0].ExecutionMode)
	}
	if len(skills[0].Steps) != 1 {
		t.Fatalf("steps = %#v", skills[0].Steps)
	}
}

func TestExpandSkillPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	got := ExpandSkillPath("~/notes/skills")
	want := filepath.Join(home, "notes/skills")
	if got != want {
		t.Fatalf("ExpandSkillPath = %q, want %q", got, want)
	}
}
