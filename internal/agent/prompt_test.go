package agent

import (
	"strings"
	"testing"

	"github.com/solarisjon/phoenix/internal/model"
)

func TestAssembleSystemPrompt_AllSections(t *testing.T) {
	a := &model.Agent{
		Persona:      "You are an expert.",
		Instructions: "Always be concise.",
		Guardrails:   "Never fabricate data.",
	}
	task := &model.Task{ID: "t1", ProjectID: "p1"}
	prompt := assembleSystemPrompt(a, task, "")

	for _, want := range []string{"## Persona", "You are an expert.", "## Instructions", "Always be concise.", "## Guardrails", "Never fabricate data."} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestAssembleSystemPrompt_EmptyFields(t *testing.T) {
	a := &model.Agent{Persona: "Only persona"}
	task := &model.Task{ID: "t1", ProjectID: "p1"}
	prompt := assembleSystemPrompt(a, task, "")
	if strings.Contains(prompt, "## Instructions") {
		t.Error("should not include Instructions section when empty")
	}
	if strings.Contains(prompt, "## Guardrails") {
		t.Error("should not include Guardrails section when empty")
	}
}

func TestAssembleUserPrompt_WithDescription(t *testing.T) {
	task := &model.Task{
		Title:       "Research OKRs",
		Description: "Find best practices.",
		Input:       `{"query":"OKRs"}`,
	}
	prompt := assembleUserPrompt(task)
	if !strings.Contains(prompt, "Research OKRs") {
		t.Error("missing task title")
	}
	if !strings.Contains(prompt, "Find best practices.") {
		t.Error("missing description")
	}
	if !strings.Contains(prompt, `{"query":"OKRs"}`) {
		t.Error("missing input")
	}
}

func TestAssembleUserPrompt_EmptyInput(t *testing.T) {
	task := &model.Task{Title: "Simple task", Input: "{}"}
	prompt := assembleUserPrompt(task)
	if strings.Contains(prompt, "## Input") {
		t.Error("should not include Input section for empty JSON object")
	}
}

func TestAssembleSystemPrompt_GlobalGuardrails(t *testing.T) {
	a := &model.Agent{Persona: "Expert", Guardrails: "No hallucinations."}
	task := &model.Task{ID: "t1", ProjectID: "p1"}
	global := "• Never touch Jira\n• No git commits without approval"
	prompt := assembleSystemPrompt(a, task, global)

	if !strings.Contains(prompt, "Platform-Wide Guardrails") {
		t.Error("global guardrails section header missing")
	}
	if !strings.Contains(prompt, "Never touch Jira") {
		t.Error("global guardrails content missing")
	}
	// Global guardrails must appear AFTER per-agent guardrails
	perAgentIdx := strings.Index(prompt, "No hallucinations.")
	globalIdx := strings.Index(prompt, "Platform-Wide Guardrails")
	if perAgentIdx < 0 || globalIdx < 0 || globalIdx < perAgentIdx {
		t.Errorf("global guardrails should appear after per-agent guardrails (perAgent=%d, global=%d)", perAgentIdx, globalIdx)
	}
}

func TestAssembleRequest(t *testing.T) {
	a := &model.Agent{
		Persona:      "Expert",
		Instructions: "Be precise.",
		Guardrails:   "No hallucinations.",
	}
	task := &model.Task{
		Title:       "Test",
		Description: "Do the thing.",
		Input:       "{}",
	}
	req := AssembleRequest(a, task, "")
	if req.SystemPrompt == "" {
		t.Error("SystemPrompt should not be empty")
	}
	if req.Prompt == "" {
		t.Error("Prompt should not be empty")
	}
	if req.Context != nil {
		t.Error("Context should be nil in Phase 1")
	}
}
