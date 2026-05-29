package crush

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/solarisjon/phoenix/internal/provider"
)

func TestNew_Defaults(t *testing.T) {
	a, err := New(`{}`)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a.cfg.BinaryPath != "crush" {
		t.Errorf("BinaryPath = %q, want %q", a.cfg.BinaryPath, "crush")
	}
}

func TestNew_InvalidJSON(t *testing.T) {
	_, err := New(`not-json`)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestBuildArgs_Minimal(t *testing.T) {
	a, _ := New(`{}`)
	req := provider.TaskRequest{Prompt: "hello"}
	args := a.buildArgs(req, "")
	joined := strings.Join(args, " ")

	if !strings.HasPrefix(joined, "run --quiet") {
		t.Errorf("args should start with 'run --quiet', got: %s", joined)
	}
	if args[len(args)-1] != "hello" {
		t.Errorf("last arg = %q, want %q", args[len(args)-1], "hello")
	}
	if strings.Contains(joined, "--model") {
		t.Error("should not include --model when empty")
	}
	if strings.Contains(joined, "--yolo") {
		t.Error("should not include --yolo by default")
	}
}

func TestBuildArgs_WithModel(t *testing.T) {
	a, _ := New(`{"model":"anthropic/claude-sonnet-4-5"}`)
	args := a.buildArgs(provider.TaskRequest{Prompt: "p"}, "")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--model anthropic/claude-sonnet-4-5") {
		t.Errorf("missing --model, got: %s", joined)
	}
}

func TestBuildArgs_WithWorkDir(t *testing.T) {
	a, _ := New(`{}`)
	args := a.buildArgs(provider.TaskRequest{Prompt: "p"}, "/tmp/mydir")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--cwd /tmp/mydir") {
		t.Errorf("missing --cwd, got: %s", joined)
	}
}

func TestBuildArgs_Yolo(t *testing.T) {
	a, _ := New(`{"yolo":true}`)
	args := a.buildArgs(provider.TaskRequest{Prompt: "p"}, "")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--yolo") {
		t.Errorf("missing --yolo, got: %s", joined)
	}
}

func TestBuildArgs_ExtraArgs(t *testing.T) {
	a, _ := New(`{"extra_args":["--debug","--small-model","haiku"]}`)
	args := a.buildArgs(provider.TaskRequest{Prompt: "p"}, "")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--debug") {
		t.Errorf("missing extra args, got: %s", joined)
	}
}

func TestPrepareWorkDir_NoPromptNoBaseDir(t *testing.T) {
	a, _ := New(`{}`)
	req := provider.TaskRequest{Prompt: "hello"} // no system prompt
	workDir, cleanup, err := a.prepareWorkDir(req)
	if err != nil {
		t.Fatalf("prepareWorkDir: %v", err)
	}
	defer cleanup()
	if workDir == "" {
		t.Error("expected a workDir, got empty")
	}
	if _, err := os.Stat(workDir); err != nil {
		t.Errorf("workDir does not exist: %v", err)
	}
	// AGENTS.md should NOT be created when there's no system prompt.
	if _, err := os.Stat(filepath.Join(workDir, "AGENTS.md")); err == nil {
		t.Error("AGENTS.md should not exist with no system prompt")
	}
}

func TestPrepareWorkDir_WithPromptNoBaseDir(t *testing.T) {
	a, _ := New(`{}`)
	req := provider.TaskRequest{
		Prompt:       "do stuff",
		SystemPrompt: "You are a test assistant.",
	}
	workDir, cleanup, err := a.prepareWorkDir(req)
	if err != nil {
		t.Fatalf("prepareWorkDir: %v", err)
	}

	// AGENTS.md should be written.
	agentsPath := filepath.Join(workDir, "AGENTS.md")
	content, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatalf("AGENTS.md not created: %v", err)
	}
	if !strings.Contains(string(content), "You are a test assistant.") {
		t.Errorf("AGENTS.md content wrong: %s", content)
	}

	// Cleanup should remove the temp dir.
	cleanup()
	if _, err := os.Stat(workDir); err == nil {
		t.Error("temp workDir should be removed after cleanup")
	}
}

func TestPrepareWorkDir_WithPromptAndBaseDir_Fresh(t *testing.T) {
	// Base dir exists but has no AGENTS.md.
	baseDir := t.TempDir()
	a, _ := New(`{}`)
	req := provider.TaskRequest{
		Prompt:       "do stuff",
		SystemPrompt: "You are agent X.",
		WorkingDir:   baseDir,
	}
	workDir, cleanup, err := a.prepareWorkDir(req)
	if err != nil {
		t.Fatalf("prepareWorkDir: %v", err)
	}
	if workDir != baseDir {
		t.Errorf("workDir = %q, want %q", workDir, baseDir)
	}

	agentsPath := filepath.Join(baseDir, "AGENTS.md")
	content, _ := os.ReadFile(agentsPath)
	if !strings.Contains(string(content), "You are agent X.") {
		t.Errorf("AGENTS.md content wrong: %s", content)
	}

	// Cleanup should delete the AGENTS.md we created.
	cleanup()
	if _, err := os.Stat(agentsPath); err == nil {
		t.Error("AGENTS.md should be removed after cleanup")
	}
}

func TestPrepareWorkDir_WithPromptAndBaseDir_ExistingAGENTS(t *testing.T) {
	// Base dir has an existing AGENTS.md — we should append and restore.
	baseDir := t.TempDir()
	agentsPath := filepath.Join(baseDir, "AGENTS.md")
	original := "# Existing project context\nDo not remove this."
	os.WriteFile(agentsPath, []byte(original), 0644)

	a, _ := New(`{}`)
	req := provider.TaskRequest{
		Prompt:       "do stuff",
		SystemPrompt: "You are agent Y.",
		WorkingDir:   baseDir,
	}
	_, cleanup, err := a.prepareWorkDir(req)
	if err != nil {
		t.Fatalf("prepareWorkDir: %v", err)
	}

	// During task: AGENTS.md should contain both original and appended content.
	content, _ := os.ReadFile(agentsPath)
	if !strings.Contains(string(content), original) {
		t.Error("original AGENTS.md content lost")
	}
	if !strings.Contains(string(content), "You are agent Y.") {
		t.Error("system prompt not appended to AGENTS.md")
	}

	// After cleanup: AGENTS.md should be restored to original.
	cleanup()
	restored, _ := os.ReadFile(agentsPath)
	if string(restored) != original {
		t.Errorf("AGENTS.md not restored; got:\n%s\nwant:\n%s", restored, original)
	}
}

func TestEstimateCost(t *testing.T) {
	a, _ := New(`{}`)
	est := a.EstimateCost(provider.TaskRequest{Prompt: "p"})
	if est.EstimatedCostUSD != 0 {
		t.Errorf("EstimateCost = %v, want 0 (crush doesn't expose cost)", est.EstimatedCostUSD)
	}
}
