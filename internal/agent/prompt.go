// Package agent handles agent lifecycle, prompt assembly, and task execution.
package agent

import (
	"fmt"
	"strings"

	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/provider"
)

// AssembleRequest builds a provider.TaskRequest from an agent and task.
// The system prompt combines persona, instructions, and guardrails.
// Prior conversation turns from the task input are included as context.
func AssembleRequest(a *model.Agent, t *model.Task) provider.TaskRequest {
	return provider.TaskRequest{
		SystemPrompt: assembleSystemPrompt(a, t),
		Prompt:       assembleUserPrompt(t),
		Context:      nil, // Phase 1: single-turn. Multi-turn added in later phases.
	}
}

// assembleSystemPrompt combines persona, instructions, guardrails, and optional
// spawn-agent instructions into a single system prompt string.
func assembleSystemPrompt(a *model.Agent, t *model.Task) string {
	var b strings.Builder

	if a.Persona != "" {
		b.WriteString("## Persona\n")
		b.WriteString(a.Persona)
		b.WriteString("\n\n")
	}

	if a.Instructions != "" {
		b.WriteString("## Instructions\n")
		b.WriteString(a.Instructions)
		b.WriteString("\n\n")
	}

	if a.Guardrails != "" {
		b.WriteString("## Guardrails\n")
		b.WriteString(a.Guardrails)
		b.WriteString("\n\n")
	}

	if a.CanSpawnAgents {
		b.WriteString("## Agent Spawning\n")
		b.WriteString(fmt.Sprintf(`You are permitted to delegate work to other agents by calling the Phoenix API.`+"\n"+
			`To spawn a task for another agent, make an HTTP POST to http://localhost:8080/api/agents/spawn with JSON body:`+"\n\n"+
			"```json\n"+
			"{\n"+
			`  "source_agent_id": "%s",`+"\n"+
			`  "target_agent_id": "<agent-id>",`+"\n"+
			`  "project_id": "%s",`+"\n"+
			`  "title": "<task title>",`+"\n"+
			`  "description": "<detailed instructions for the target agent>"`+"\n"+
			"}\n"+
			"```\n"+
			`The API returns the created task. Only spawn tasks when explicitly needed to complete your work.`,
			a.ID, t.ProjectID))
		b.WriteString("\n")
	}

	return strings.TrimSpace(b.String())
}

// assembleUserPrompt builds the user-facing prompt from the task.
func assembleUserPrompt(t *model.Task) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("# Task: %s\n\n", t.Title))

	if t.Description != "" {
		b.WriteString(t.Description)
		b.WriteString("\n")
	}

	// If the task has structured input beyond an empty JSON object, include it.
	if t.Input != "" && t.Input != "{}" {
		b.WriteString("\n## Input\n")
		b.WriteString(t.Input)
		b.WriteString("\n")
	}

	return strings.TrimSpace(b.String())
}
