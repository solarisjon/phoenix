// Package agent handles agent lifecycle, prompt assembly, and task execution.
package agent

import (
	"encoding/json"
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
		b.WriteString("## Delegating to Existing Agents\n")
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
		b.WriteString("\n\n")
	}

	if a.CanHireAgents {
		b.WriteString("## Hiring New Agents\n")
		b.WriteString(fmt.Sprintf(
			`You are permitted to recruit and create new agents by calling the Phoenix API.`+"\n\n"+
			`**Step 1 — Check existing agents first:**`+"\n"+
			`Before proposing a hire, call GET http://localhost:8080/api/agents to list all existing agents.`+"\n"+
			`Review their names and personas. Only propose a new hire if no existing agent can fulfill the required role.`+"\n\n"+
			`**Step 2 — Submit a hire proposal:**`+"\n"+
			`If no suitable agent exists, make an HTTP POST to http://localhost:8080/api/agent-drafts with this JSON body:`+"\n\n"+
			"```json\n"+
			"{\n"+
			`  "created_by_agent_id": "%s",`+"\n"+
			`  "created_by_task_id":  "%s",`+"\n"+
			`  "name":         "<full role title, e.g. Senior Operations Manager>",`+"\n"+
			`  "persona":      "<2-3 sentences: who they are, personality, communication style>",`+"\n"+
			`  "instructions": "<detailed operational instructions, 4-8 paragraphs or bullets>",`+"\n"+
			`  "guardrails":   "<constraints and boundaries, 3-5 items>"`+"\n"+
			"}\n"+
			"```\n"+
			`The draft will be sent to a human for review and approval before the agent is activated.`+"\n"+
			`You do not need to assign a provider or project — the human handles that at approval time.`+"\n"+
			`Only propose a hire when explicitly asked to recruit, or when your task requires a capability that no existing agent can fulfill.`,
			a.ID, t.ID))
		b.WriteString("\n")
	}

	return strings.TrimSpace(b.String())
}

// InjectFollowUpContext prepends the parent task's output to the request prompt
// so the agent has full context when processing a human refinement follow-up.
func InjectFollowUpContext(req provider.TaskRequest, parent *model.Task) provider.TaskRequest {
	if parent.Output == "" || parent.Output == "{}" {
		return req
	}
	// Extract text from the parent output JSON if possible.
	parentText := extractOutputText(parent.Output)
	if parentText == "" {
		return req
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("## Previous output (task: %s)\n", parent.Title))
	b.WriteString(parentText)
	b.WriteString("\n\n## Your follow-up instructions\n")
	b.WriteString(req.Prompt)
	req.Prompt = b.String()
	return req
}

// extractOutputText pulls the "text" field from a task output JSON blob,
// falling back to the raw string if it's not JSON.
func extractOutputText(output string) string {
	var m map[string]string
	if err := json.Unmarshal([]byte(output), &m); err == nil {
		if t, ok := m["text"]; ok {
			return t
		}
		// error key means the task failed — not useful as context
		if _, ok := m["error"]; ok {
			return ""
		}
	}
	return output
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
