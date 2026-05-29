# Agent Hiring Flow — Design Spec
*2026-05-29*

## Overview

Agents with the `can_hire_agents` permission can dynamically recruit and create
new agents during task execution. The hiring agent scopes the new agent's
persona, instructions, and guardrails, then submits a draft to the human Inbox
for review and approval. A human approves (or rejects/edits) the draft; on
approval the agent is created exactly as if a human had done it via the UI.

This is distinct from the existing `can_spawn_agents` capability (which creates
tasks for *existing* agents). `can_hire_agents` creates *brand-new* agents.

---

## Permission Flag

### New field: `can_hire_agents`
- Boolean on `model.Agent`, stored in `agents` table (migration 008)
- Default `false`
- Displayed in AgentsPage UI as a separate checkbox: **"Can hire new agents"**
- `can_spawn_agents` remains unchanged — two distinct flags, two distinct capabilities

---

## Data Model

### New table: `agent_drafts` (migration 008)

```sql
CREATE TABLE agent_drafts (
  id                   TEXT PRIMARY KEY,
  created_by_agent_id  TEXT NOT NULL REFERENCES agents(id),
  created_by_task_id   TEXT REFERENCES tasks(id),
  name                 TEXT NOT NULL,
  persona              TEXT NOT NULL DEFAULT '',
  instructions         TEXT NOT NULL DEFAULT '',
  guardrails           TEXT NOT NULL DEFAULT '',
  provider_id          TEXT NOT NULL REFERENCES providers(id),
  status               TEXT NOT NULL DEFAULT 'pending_approval',
  dismissed            INTEGER NOT NULL DEFAULT 0,
  created_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

**Status values:** `pending_approval` → `approved` | `rejected`

`provider_id` is inherited from the hiring agent's provider at draft-creation
time. The human may change it in the approval step.

---

## API Endpoints

### `POST /api/agent-drafts`
Called by the hiring agent from within its task.

**Request body:**
```json
{
  "created_by_agent_id": "<hiring agent id>",
  "created_by_task_id":  "<current task id>",
  "name":         "Senior Operations Manager",
  "persona":      "...",
  "instructions": "...",
  "guardrails":   "..."
}
```

**Behaviour:**
- Validates `created_by_agent_id` exists and has `can_hire_agents = true`
- Looks up hiring agent's `provider_id` and stores it on the draft
- Creates draft with status `pending_approval`
- Emits WebSocket event `agent_draft_created` so inbox badge updates immediately
- Returns created draft (201)

**Errors:** 400 if agent not found or lacks permission; 422 if name/persona/instructions empty.

---

### `GET /api/agent-drafts`
Returns all non-dismissed drafts, newest first. Used by Inbox page.

Query params: `?status=pending_approval` (default), `?status=all`

---

### `PUT /api/agent-drafts/:id`
Human edits name, persona, instructions, guardrails, and/or provider_id before approving.
Only allowed when status = `pending_approval`.

---

### `POST /api/agent-drafts/:id/approve`
**Request body:**
```json
{
  "provider_id": "<chosen provider id>"
}
```
provider_id is optional — falls back to the draft's stored provider_id.

**Behaviour:**
- Creates a new `Agent` record (status=`active`) using draft fields + chosen provider
- Marks draft `approved` + `dismissed = 1`
- Returns the newly created agent (201)

---

### `POST /api/agent-drafts/:id/reject`
Marks draft `rejected` + `dismissed = 1`. Returns 204.

---

### `POST /api/agent-drafts/:id/dismiss`
Marks draft `dismissed = 1` without changing status. Returns 204.

---

## System Prompt Injection

When `can_hire_agents = true`, the runner injects this section into the agent's
system prompt (agent ID and task ID are baked in at assembly time):

```
## Hiring New Agents

You are permitted to recruit and create new agents by calling the Phoenix API.

**Step 1 — Check existing agents first:**
Before proposing a hire, call GET http://localhost:8080/api/agents to list all
existing agents. Review their names and personas. Only propose a new hire if no
existing agent can fulfill the required role.

**Step 2 — Submit a hire proposal:**
If no suitable agent exists, make an HTTP POST to http://localhost:8080/api/agent-drafts
with this JSON body:

{
  "created_by_agent_id": "<AGENT_ID>",
  "created_by_task_id":  "<TASK_ID>",
  "name":         "<full role title, e.g. Senior Operations Manager>",
  "persona":      "<2-3 sentences: who they are, personality, communication style>",
  "instructions": "<detailed operational instructions, 4-8 paragraphs or bullets>",
  "guardrails":   "<constraints and boundaries, 3-5 items>"
}

The draft will be sent to a human for review and approval before the agent is
activated. You do not need to assign a provider or project — the human handles
that at approval time.

Only propose a hire when explicitly asked to recruit, or when your task requires
a capability that no existing agent can fulfill.
```

---

## Inbox Presentation

Pending hire drafts appear in the Inbox page alongside failed tasks and
awaiting-approval tasks, as a visually distinct card type.

### Card anatomy
- **Badge:** `🧑‍💼 Pending Hire` (amber/purple accent, distinct from task badges)
- **Title:** Proposed agent name (e.g. "Senior Operations Manager")
- **Body:** Three collapsible/editable sections — Persona · Instructions · Guardrails
- **Meta:** "Proposed by [Recruiter] · via task [task title] · [timestamp]"
- **Provider selector:** Dropdown of all providers, pre-selected to hiring agent's provider
- **Actions:**
  - ✓ **Approve** — calls `POST /api/agent-drafts/:id/approve` with chosen provider
  - ✏️ **Edit** — inline editing of all three fields + name, then re-save
  - ✗ **Reject** — calls `POST /api/agent-drafts/:id/reject`

### Inbox badge
The existing amber badge count includes pending hire drafts (same 30s poll +
WebSocket event already in place).

---

## AgentsPage UI Changes

- New checkbox on agent create/edit form: **"Can hire new agents"** (below existing
  "Can spawn tasks for other agents" checkbox)
- Tooltip/hint: "Allows this agent to propose new agent hires via the API, subject
  to human approval in the Inbox"

---

## Traceability

Every created agent has a `created_by` field (already exists on `model.Agent`).
On approval, `created_by` is set to `"agent:<hiring_agent_id>"` so it's clear
in the UI which agent originated the hire.

---

## What Is NOT in Scope

- The new agent is never auto-assigned to a project at hire time — human does this
- No heartbeat is set on the new agent at hire time — human configures this
- `can_hire_agents` does not imply `can_spawn_agents` — they are independent
- No nested hiring (hired agents cannot themselves hire on first creation — they
  start with `can_hire_agents = false` by default)

---

## Migration Summary

**Migration 008:**
```sql
ALTER TABLE agents ADD COLUMN can_hire_agents INTEGER NOT NULL DEFAULT 0;

CREATE TABLE agent_drafts (
  id                   TEXT PRIMARY KEY,
  created_by_agent_id  TEXT NOT NULL REFERENCES agents(id),
  created_by_task_id   TEXT REFERENCES tasks(id),
  name                 TEXT NOT NULL,
  persona              TEXT NOT NULL DEFAULT '',
  instructions         TEXT NOT NULL DEFAULT '',
  guardrails           TEXT NOT NULL DEFAULT '',
  provider_id          TEXT NOT NULL REFERENCES providers(id),
  status               TEXT NOT NULL DEFAULT 'pending_approval',
  dismissed            INTEGER NOT NULL DEFAULT 0,
  created_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

---

## File Changes Summary

| File | Change |
|------|--------|
| `internal/store/sqlite/migrations/008_agent_hiring.sql` | New migration |
| `internal/model/model.go` | `Agent.CanHireAgents`, new `AgentDraft` struct |
| `internal/store/store.go` | New `AgentDraftRepo` interface |
| `internal/store/sqlite/agent_draft.go` | AgentDraftRepo implementation |
| `internal/store/sqlite/sqlite.go` | Wire AgentDraftRepo |
| `internal/agent/prompt.go` | `assembleSystemPrompt` — inject hiring instructions |
| `internal/api/agent_draft.go` | New handler file for all draft endpoints |
| `internal/api/server.go` | Register new routes; add `AgentDrafts` repo to Server |
| `web/src/pages/AgentsPage.tsx` | `can_hire_agents` checkbox |
| `web/src/pages/InboxPage.tsx` | Pending hire cards |
| `web/src/lib/api.ts` | New API functions for drafts |
