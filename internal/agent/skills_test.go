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

func TestInjectSkillExecutionMode_OverridesRoutingPersona(t *testing.T) {
	req := provider.TaskRequest{SystemPrompt: "You never attempt to perform the task yourself."}
	out := InjectSkillExecutionMode(req, nil, "run the morning_coffee skill")
	if !strings.Contains(out.SystemPrompt, "## Skill Execution Mode") {
		t.Fatal("missing skill execution mode section")
	}
	if !strings.Contains(out.SystemPrompt, "Do NOT route, delegate, decompose") {
		t.Fatal("missing direct execution instruction")
	}
	if !strings.Contains(out.SystemPrompt, "You never attempt to perform the task yourself.") {
		t.Fatal("original system prompt should remain after override section")
	}
}

func TestDiscoverFilesystemSkills(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, ".agents", "skills", "demo-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: demo-skill\ndescription: Demo skill for tests\n---\n\nDo the demo thing.\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	skills := DiscoverFilesystemSkills(nil, dir)
	if len(skills) != 1 {
		t.Fatalf("discovered %d skills, want 1", len(skills))
	}
	if skills[0].Slug != "demo-skill" {
		t.Fatalf("slug = %q, want demo-skill", skills[0].Slug)
	}
	if !strings.Contains(skills[0].Instructions, "Do the demo thing.") {
		t.Fatal("missing skill body")
	}
}

func TestTaskRequestsSkillExecution(t *testing.T) {
	proj := &model.Project{Objective: "Run the skill called morning_coffee."}
	task := &model.Task{Description: proj.Objective}
	if !TaskRequestsSkillExecution(context.Background(), nil, nil, "", task, proj) {
		t.Fatal("expected skill execution request")
	}
}

func TestDiscoverFilesystemSkills_CustomImportDir(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "morning_coffee")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: morning_coffee\ndescription: Brew coffee\n---\n\nMake coffee.\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	skills := DiscoverFilesystemSkills([]string{dir}, "")
	if len(skills) != 1 || skills[0].Slug != "morning_coffee" {
		t.Fatalf("got %#v, want morning_coffee", skills)
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
