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
	prompt := assembleSystemPrompt(a)

	for _, want := range []string{"## Persona", "You are an expert.", "## Instructions", "Always be concise.", "## Guardrails", "Never fabricate data."} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestAssembleSystemPrompt_EmptyFields(t *testing.T) {
	a := &model.Agent{Persona: "Only persona"}
	prompt := assembleSystemPrompt(a)
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
	req := AssembleRequest(a, task)
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
