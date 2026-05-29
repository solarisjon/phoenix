# Phoenix UX Clarity & Team Bundle Design
**Date:** 2026-05-29  
**Status:** Approved

---

## Problem Statement

Phoenix has grown organically and the UI now has several friction points that make it hard for non-technical users (and even technical ones) to understand:

1. **Heartbeats are a black box** — set as raw seconds on an agent, no visibility into which projects are affected or when the next fire is
2. **No guided flow** — creating your first task requires 4+ manual steps across multiple pages with no guidance
3. **Duplicate task surfaces** — failed/attention tasks appear on Dashboard, Inbox, and project pages inconsistently
4. **Teams feel orphaned** — Teams page exists but doesn't show team activity, schedules, or projects
5. **Nav has too much noise** — Providers and Agents are always visible even for users who never need to touch them
6. **No cross-project task view** — you must click into each project individually to see what's running
7. **No portability** — a configured team cannot be shared with another user

## North Star

**Phoenix should work for a non-technical user** who receives a pre-built team bundle, imports it, and can immediately create projects and run tasks — without touching agent config, providers, or any technical internals. Power users retain full access to everything through Settings.

The core mental model: **Team → Project → Tasks**. Everything else is configuration.

## Scope

This spec covers:
1. Navigation restructure
2. Team page redesign (the primary user-facing view)
3. Guided project setup (3-step flow)
4. Global Tasks page
5. Team bundle export/import
6. Dashboard simplification

It does NOT cover: authentication, multi-user access control, billing, or cloud hosting.

---

## 1. Navigation Restructure

### Before
```
Dashboard · Inbox · Projects · Agents · Teams · Providers
```

### After
```
◈  Dashboard
⊡  Inbox          [attention badge]
⊞  Projects
⬡⬡ Teams
⚙  Settings
```

**Settings** is a new page (not a modal) containing:
- Agents — full CRUD, same as current AgentsPage
- Providers — full CRUD, same as current ProvidersPage
- (Future: Backup, System info)

Settings uses an internal tab/sub-nav. The URL structure is `/settings/agents` and `/settings/providers`.

**Teams** moves from position 5 to position 4 — it's a primary user concept, not an admin one.

**Agents** and **Providers** are removed from top-level nav. They remain fully functional in Settings.

No permissions system. Any user can open Settings and modify anything. The nav restructure is purely about reducing visual noise.

---

## 2. Team Page Redesign

The Team detail page becomes the primary "home base" for a team. It has three sections.

### 2a. Members section

A card grid showing each agent in the team:

```
MEMBERS (3)                                      [+ Add Member]
┌──────────────────┐ ┌──────────────────┐ ┌──────────────────┐
│ 🤖 Strategist    │ │ 🤖 Researcher    │ │ 🤖 Writer        │
│ Defines goals    │ │ Gathers data     │ │ Drafts content   │
│                  │ │                  │ │                  │
│ ● running task   │ │   idle           │ │   idle           │
│ Q3 OKRs          │ │                  │ │                  │
└──────────────────┘ └──────────────────┘ └──────────────────┘
```

Each card shows:
- Agent name and first line of persona (truncated)
- Live status: "● running — [task title]" if the agent has an active task, otherwise "idle"
- Clicking the card opens the agent in Settings → Agents (edit mode)

### 2b. Projects section

```
PROJECTS (2)                                  [+ New Project]
┌────────────────────────────────────────────────────────────┐
│ Q3 OKRs          3 tasks  ·  2 done  ·  1 running    [→]  │
│ NLS Report       5 tasks  ·  5 done  ·  0 running    [→]  │
└────────────────────────────────────────────────────────────┘
```

"New Project" from here pre-assigns the whole team to the new project, then drops the user into the guided 3-step setup (see section 3). Projects are shown if any team member is assigned to them.

### 2c. Schedule section

Surfaces heartbeat config in human-readable form:

```
SCHEDULE
┌────────────────────────────────────────────────────────────┐
│ Strategist  →  Q3 OKRs      every 1h   next: 11:30 AM     │
│ Researcher  →  NLS Report   every 4h   next: 3:00 PM      │
└────────────────────────────────────────────────────────────┘
```

Each row is one (agent × project) heartbeat pair. "Next" is computed as `last_fired + interval` (stored on the task; if never fired, `started_at + interval`). If an agent has no heartbeat interval set, they don't appear here.

Clicking a schedule row opens the agent in Settings so the interval can be changed.

Empty state: "No scheduled activity. Set a heartbeat interval on an agent to enable automatic check-ins."

### 2d. Header actions

```
[Export Team ↓]                    [Edit Team]  [Delete]
```

Export is covered in section 5.

---

## 3. Guided Project Setup

When a project has no tasks yet (regardless of agent assignment), the tasks column shows a 3-step guided flow instead of an empty state.

```
  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  ①  Choose who works on this  ②  Describe the goal  ③  Run
  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

**Step 1 — Choose agents:**
- If navigated from a Team page: team members are pre-selected with checkboxes to deselect
- Otherwise: shows all agents with checkboxes, grouped by team if teams exist
- "Select all" / "Clear" shortcuts

**Step 2 — Describe the goal:**
- Single `<textarea>` labelled "What do you need done?"
- Optional "Task title" field (auto-generated from first line if left blank)
- No other fields — keep it simple

**Step 3 — Review & Run:**
- Summary: "Creating [N] task(s) for [agent names]"
- "Start" button — creates tasks and immediately runs them
- After run: transitions to the normal task list view

This flow is for first-time setup. Once a project has tasks, "New Task" button opens the existing modal (which already supports agent/team mode from the recent fix).

---

## 4. Global Tasks Page

A new page at `/tasks` — "All Tasks" — accessible from Projects in the nav (sub-link or tab).

```
TASKS                                    [+ New Task]

[All ▾]  [Running]  [Completed]  [Failed]  [Needs Attention]

Search: [________________________]

Task                    Project         Agent          Status      Cost    When
Research Q3 OKRs        Life of Mgr     Strategist     ✓ done      $0.02   2h ago
Draft NLS report        NLS Report      Researcher     ● running   —       now
Heartbeat 2026-05-29    Q3 OKRs         Strategist     ✓ done      $0.01   1h ago
```

Features:
- Status filter tabs (All / Running / Completed / Failed / Needs Attention)
- Text search on task title
- Click row → opens task detail modal (same as Dashboard modal)
- Retry / Dismiss inline on Failed rows
- "New Task" opens the task creation modal (project and agent selectable from dropdowns)
- Pagination or virtual scroll for large lists

This replaces the need to dig into each project to find tasks. Projects page becomes a list of workspaces; tasks become a first-class browsable entity.

---

## 5. Team Bundle Export / Import

### 5a. Export format

A single JSON file. Human-readable, version-stamped.

```json
{
  "phoenix_bundle_version": "1",
  "exported_at": "2026-05-29T10:00:00Z",
  "team": {
    "name": "Product Management",
    "description": "..."
  },
  "agents": [
    {
      "name": "Strategist",
      "persona": "...",
      "instructions": "...",
      "guardrails": "...",
      "heartbeat_interval": 3600,
      "can_spawn_agents": false,
      "provider_ref": "primary_llm"
    }
  ],
  "providers": [
    {
      "ref": "primary_llm",
      "name": "LLM Provider",
      "type": "llm",
      "kind": "openai-compatible",
      "config": {
        "base_url": "https://...",
        "model": "claude-sonnet-4-5",
        "api_key": ""
      }
    }
  ]
}
```

Key points:
- `api_key` is always exported as empty string — never included
- `provider_ref` links agents to providers by a bundle-local name, not a UUID
- No project data is included (projects are user-specific workspaces)
- No task history is included
- File named `[team-name-slugified]-bundle.json`

### 5b. Export endpoint

`GET /api/teams/{id}/export` → returns the JSON file with `Content-Disposition: attachment`.

UI: "Export Team ↓" button on team detail page.

### 5c. Import flow

Entry point: Settings → "Import Team" button, or an "Import" link on the Teams page empty state.

**Step 1 — Upload:**
Drop zone or file picker. Accepts `.json`. Validates `phoenix_bundle_version` field. Shows parsed summary before doing anything.

```
Found: "Product Management" team
  · 3 agents: Strategist, Researcher, Writer
  · 1 provider template: LLM Provider (openai-compatible)
```

**Step 2 — Configure provider:**
For each provider in the bundle:
```
Provider: LLM Provider
Endpoint: https://...          [pre-filled, editable]
Model:    claude-sonnet-4-5   [pre-filled, editable]
API Key:  [____________________]  ← required to run tasks
          [Skip for now — I'll add this later]
```

If skipped, the provider is created with an empty API key. Tasks will fail with a clear "provider not configured" error until the key is added in Settings.

**Step 3 — Confirm & Import:**
```
Ready to import:
  ✓ Create provider "LLM Provider"
  ✓ Create 3 agents
  ✓ Create team "Product Management"

[Import]   [Cancel]
```

On success: navigates to the new team's page. Toast: "Team imported. Create a project to get started."

**Conflict handling:**
- If a provider with the same endpoint+model already exists: offer to reuse it instead of creating a duplicate
- If an agent with the same name already exists: offer to skip or create as a copy ("Strategist (imported)")
- Partial failures: import what succeeded, report what failed

### 5d. Import endpoint

`POST /api/import/team` — accepts the JSON bundle, returns `{ team_id, agent_ids[], provider_ids[], skipped[] }`.

---

## 6. Dashboard Simplification

The dashboard currently duplicates task lists that exist elsewhere. After this redesign:

**Keep:**
- Stat cards: Active Projects · Tasks Running · Needs Attention · This week's cost
- "Your Teams" quick-view: card per team, member count, active task count, link to team page
- Running tasks live panel (triggered by "Tasks Running" stat card click)

**Remove:**
- Recent Activity task list — this moves to the Global Tasks page
- Cost & Activity charts section — moves to a dedicated Stats page (future) or stays but is collapsed by default

**Add:**
- "Get started" empty state when no teams and no projects exist — single CTA: "Import a team" or "Build a team"

---

## 7. Data Model Changes

### 7a. No new DB migrations required for core UX changes

The Schedule section on the Team page derives "next fire time" from existing data:
- The scheduler already tracks when heartbeats fire (it creates tasks with timestamp titles)
- "Next" = most recent heartbeat task `created_at` + `heartbeat_interval`
- If no heartbeat task exists yet: `agent.created_at` + `heartbeat_interval`

This computation happens in the frontend from data already returned by the API.

### 7b. Import endpoint creates standard records

The import endpoint uses existing `AgentRepo.Create()`, `ProviderRepo.Create()`, `TeamRepo.Create()` and `TeamRepo.AddAgent()`. No new store methods needed beyond what already exists.

---

## 8. Implementation Order

1. **Navigation restructure + Settings page** — unblocks everything, low risk
2. **Global Tasks page** — high value, self-contained
3. **Team page redesign** — members/projects/schedule sections
4. **Export endpoint + UI button**
5. **Import endpoint + wizard UI**
6. **Guided project setup** — replaces empty state on project detail

Each item is independently shippable. Items 1-3 improve the existing user experience immediately. Items 4-5 unlock the "hand a team to someone" use case.

---

## Success Criteria

- A non-technical user can receive a bundle file, import it, create a project, and run their first task without reading any documentation
- Heartbeat schedules are visible and human-readable without opening any agent config
- All tasks across all projects are browsable in one place
- Providers and agent internals are accessible but not in the way for everyday use
- A team builder (technical user) can export any team they've built and hand it to anyone
