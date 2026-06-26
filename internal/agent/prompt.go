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
func AssembleRequest(a *model.Agent, t *model.Task, globalGuardrails string) provider.TaskRequest {
	return provider.TaskRequest{
		SystemPrompt: assembleSystemPrompt(a, t, globalGuardrails),
		Prompt:       assembleUserPrompt(t),
		Context:      nil, // Phase 1: single-turn. Multi-turn added in later phases.
	}
}

// assembleSystemPrompt combines behaviour (or legacy persona+instructions),
// soft guardrails, hard guardrails, and optional spawn/hire instructions into a single system prompt.
func assembleSystemPrompt(a *model.Agent, t *model.Task, globalGuardrails string) string {
	var b strings.Builder

	if a.Behaviour != "" {
		b.WriteString("## Behaviour\n")
		b.WriteString(a.Behaviour)
		b.WriteString("\n\n")
	} else {
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
	}

	if a.Guardrails != "" {
		b.WriteString("## Soft Guardrails (Advisory)\n")
		b.WriteString("These are guidance constraints. Try to follow them; if you cannot, document why in your output.\n")
		b.WriteString(a.Guardrails)
		b.WriteString("\n\n")
	}

	if a.HardGuardrails != "" {
		b.WriteString("## Hard Guardrails (Mandatory — Stop and Request Approval)\n")
		b.WriteString("If your task would violate any of the following rules, you MUST stop immediately and output EXACTLY the following as the first line of your response (and nothing else on that line):\n\n")
		b.WriteString("  GUARDRAIL_TRIGGERED: <one-sentence reason describing the specific action that triggered this guardrail>\n\n")
		b.WriteString("Do NOT proceed with the action. Wait for human approval before continuing.\n\n")
		b.WriteString(a.HardGuardrails)
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

	// Every agent gets the memo capability injected — it's always available.
	// Monitor tasks (source=="monitor") get a stronger, mandatory instruction.
	b.WriteString("\n## Briefing Memos\n")
	if t.Source == "monitor" {
		b.WriteString(`You MUST end your response with a briefing memo summarising what you found and any actions taken. Use this exact format:

MEMO_START
Title: <concise one-line title>
Priority: high
<markdown body — key findings, actions taken, anything requiring attention>
MEMO_END

Include findings even if the run was routine — the memo is the human's window into what you did.
You can include multiple MEMO blocks if there are distinct topics worth separating.`)
	} else {
		b.WriteString(`If your task produces findings, actions, summaries, or anything the user should read, you MAY embed one or more briefing memos directly in your output using this exact format:

MEMO_START
Title: <concise one-line title>
Priority: high
<markdown body — bullet points, headings, whatever is clearest>
MEMO_END

Omit the Priority line for normal priority. You can include multiple MEMO blocks.
Only post a memo when there is genuinely something worth surfacing — not for routine confirmations or status updates.`)
	}
	b.WriteString("\n")

	b.WriteString(`
## Artifacts

If your task creates or produces a file, web page, Jira ticket, Confluence page, or any other concrete output, declare it using an ARTIFACT block so the human can find it easily. Embed one block per artifact directly in your output:

ARTIFACT_START
Type: file
Path: /absolute/path/to/file.md
Title: Short human-readable name
ARTIFACT_END

For URLs (web pages, Jira, Confluence, HTML files served over HTTP):

ARTIFACT_START
Type: url
URL: https://example.atlassian.net/browse/PROJ-123
Title: Jira ticket PROJ-123
ARTIFACT_END

Supported types: file, url, jira, confluence, html
Only emit an ARTIFACT block when you have actually created or modified something — not for pre-existing resources you merely referenced.
`)


	if globalGuardrails != "" {
		b.WriteString("\n## Platform-Wide Guardrails (mandatory — overrides all other instructions)\n")
		b.WriteString(globalGuardrails)
		b.WriteString("\n")
	}

	return strings.TrimSpace(b.String())
}

// BuiltinCriticPrompt returns a system prompt for an ephemeral devil's advocate
// critic. It is injected when critic_mode = "builtin" — no registered agent needed.
func BuiltinCriticPrompt() string {
	return strings.TrimSpace(`
## Role
You are a Devil's Advocate critic. Your sole purpose is to challenge, stress-test, and find weaknesses in the work you are reviewing. You are not here to be supportive or balanced — you are here to find problems.

## Approach
- Assume the work is flawed until proven otherwise.
- Identify logical gaps, unstated assumptions, and missing edge cases.
- Challenge conclusions: what evidence is missing? What alternative explanations exist?
- Surface risks the original agent may have downplayed or ignored.
- Point out what was NOT done that should have been.
- Be direct and specific — vague criticism is useless.

## Output format
Structure your critique as:
1. **Key concerns** — the most important issues, ranked by severity
2. **Unstated assumptions** — things taken for granted that may not hold
3. **Missing considerations** — what was overlooked?
4. **Recommended actions** — concrete next steps to address the concerns

Do not summarise or praise the original work. Focus entirely on what could be wrong or improved.
`)
}

// BuildBuiltinCriticRequest assembles a TaskRequest for an ephemeral built-in
// devil's advocate review of a completed task. The original task output is the prompt.
func BuildBuiltinCriticRequest(originalTask *model.Task) provider.TaskRequest {
	var prompt strings.Builder
	prompt.WriteString(fmt.Sprintf("# Critic Review: %s\n\n", originalTask.Title))
	prompt.WriteString("Review the following task output and provide a rigorous devil's advocate critique.\n\n")
	prompt.WriteString("## Original task\n")
	prompt.WriteString(originalTask.Title)
	if originalTask.Description != "" {
		prompt.WriteString("\n\n")
		prompt.WriteString(originalTask.Description)
	}
	prompt.WriteString("\n\n## Task output\n")
	prompt.WriteString(extractOutputText(originalTask.Output))
	return provider.TaskRequest{
		SystemPrompt: BuiltinCriticPrompt(),
		Prompt:       prompt.String(),
	}
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

// contextSummarisationThreshold is the minimum total character count of prior
// follow-up turns before context summarisation is triggered.
const contextSummarisationThreshold = 8000

// contextSummarisationKeepRecent is the number of most recent turns to keep
// verbatim when summarising.
const contextSummarisationKeepRecent = 2

// ShouldSummariseChain reports whether the follow-up chain is long enough to
// warrant context summarisation. Returns true when chain depth > 2 AND the
// combined character count of all prior outputs exceeds the threshold.
func ShouldSummariseChain(chain []*model.Task) bool {
	if len(chain) <= 2 {
		return false
	}
	var total int
	for _, t := range chain {
		total += len(extractOutputText(t.Output))
	}
	return total > contextSummarisationThreshold
}

// BuildSummaryRequest returns a TaskRequest that asks the LLM to produce a
// ≤200-word summary of the given prior conversation turns. The request uses a
// minimal system prompt so the cheapest available provider suffices.
func BuildSummaryRequest(turns []*model.Task) provider.TaskRequest {
	var b strings.Builder
	b.WriteString("Summarise the following conversation in ≤200 words, preserving key decisions, facts, and any action items. Be concise.\n\n")
	for _, t := range turns {
		text := extractOutputText(t.Output)
		if text == "" {
			continue
		}
		b.WriteString(fmt.Sprintf("### Task: %s\n%s\n\n", t.Title, text))
	}
	return provider.TaskRequest{
		SystemPrompt: "You are a concise summariser. Output only the summary — no preamble.",
		Prompt:       strings.TrimSpace(b.String()),
	}
}

// InjectFollowUpChainContext builds the user prompt for a follow-up task by
// prepending context from the entire prior chain. When a non-empty summary is
// provided, old turns (all except the most recent contextSummarisationKeepRecent)
// are replaced by the summary; otherwise, all prior turns are included verbatim.
func InjectFollowUpChainContext(req provider.TaskRequest, chain []*model.Task, summary string) provider.TaskRequest {
	if len(chain) == 0 {
		return req
	}

	var b strings.Builder

	if summary != "" && len(chain) > contextSummarisationKeepRecent {
		// Summarised path: abbreviated older context + recent turns verbatim.
		b.WriteString("## Earlier conversation (summarised)\n")
		b.WriteString(summary)
		b.WriteString("\n\n")

		recent := chain
		if len(chain) > contextSummarisationKeepRecent {
			recent = chain[len(chain)-contextSummarisationKeepRecent:]
		}
		for _, t := range recent {
			text := extractOutputText(t.Output)
			if text == "" {
				continue
			}
			b.WriteString(fmt.Sprintf("## Recent output (task: %s)\n%s\n\n", t.Title, text))
		}
	} else {
		// Verbatim path: include all prior turns.
		for _, t := range chain {
			text := extractOutputText(t.Output)
			if text == "" {
				continue
			}
			b.WriteString(fmt.Sprintf("## Previous output (task: %s)\n%s\n\n", t.Title, text))
		}
	}

	b.WriteString("## Your follow-up instructions\n")
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
