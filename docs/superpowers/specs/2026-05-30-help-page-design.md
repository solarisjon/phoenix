# Help Page Design

**Date:** 2026-05-30  
**Status:** Approved  
**Feature:** In-app Help & API Reference sidebar entry

---

## Overview

Add a **Help** entry to the sidebar that opens a rich reference page for new users and developers integrating with Phoenix's REST API. The page uses tabs to separate concerns without requiring scrolling past unrelated content.

---

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Style | Concept Map + Reference Docs | Passive reference that stays useful past onboarding; no interactive checklist state to manage |
| Layout | Tabbed sections | Clean separation; each tab is self-contained; no scrolling past unrelated content |
| API reference | Expandable endpoint rows | Reveals request body fields and query params on demand; actually useful for integration, not just a route listing |
| Providers tab | Brief table + pointer to Settings | Setup is done in the UI; a deep guide would duplicate what the form already explains |

---

## Sidebar Entry

- **Label:** `Help`
- **Icon:** `?` (question mark, consistent with the existing symbolic icon style)
- **Route:** `/help`
- **Position:** Below Settings (bottom of nav)

---

## Page Structure

Four tabs rendered as a horizontal tab bar at the top of the page content area:

```
[ Concepts ]  [ API Reference ]  [ Providers ]  [ Tips & Gotchas ]
```

Active tab is highlighted with the accent colour. Tab state is local component state (no URL routing per-tab needed).

---

## Tab: Concepts

A 2×N grid of concept cards. Each card has:
- Icon + bold name heading
- 2–3 sentence plain-English explanation
- A "→ Go to X" link that navigates to the relevant page

Concepts to cover (in order):

| Icon | Name | Key points |
|------|------|------------|
| 🔌 | Provider | An LLM or coding-agent backend. Phoenix talks to it to run tasks. Configured in Settings → Providers. |
| 🤖 | Agent | An AI worker with a persona, instructions, guardrails, and a provider. Agents don't run on their own — they need a task. |
| ⊞ | Project | A human-driven workbench. You create tasks; assigned agents execute them. Full thread history per task. |
| ⟳ | Monitor | A schedule-driven project. Fires automatically on a heartbeat interval. Good for recurring checks and dispatching tasks into other projects. |
| ✦ | Task | A single unit of work. Has an input, output, cost, status, and optional source provenance. Can be followed up or retried. |
| ⊡ | Inbox | Holds tasks needing human attention: failed runs, tasks awaiting approval, and agent hire proposals. |
| ⬡⬡ | Team | A named group of agents that can be assigned to a project together. Useful for reusable agent lineups. |
| ⚙ | Global Guardrails | Platform-wide rules injected into every agent's system prompt, regardless of per-agent settings. Set in Settings → System. |

A short introductory paragraph above the grid explains the mental model: *"Phoenix is built around Providers, Agents, and Projects. Providers supply the intelligence; Agents apply it; Projects and Monitors direct it."*

---

## Tab: API Reference

All endpoints are served at `http://localhost:8080/api/...` by default. A note at the top states this and mentions CORS is open for local development.

Endpoints are grouped by resource. Each group has a bold heading. Within a group, each endpoint is a row that expands on click.

### Collapsed row layout
```
[METHOD]  /api/path/...          Short description                    ▶
```
- Method badge is colour-coded: GET=green, POST=blue, PUT=amber, DELETE=red
- Path in monospace, accent colour
- Arrow toggles to ▼ when expanded

### Expanded row layout
Shows below the collapsed header (no separate panel):
- **Query params** (if any): table of `name | type | description`
- **Request body** (POST/PUT): table of `field | type | required | description`
- **Notes** (if anything non-obvious): plain text

Expanded state is per-row, independent — multiple rows can be open simultaneously.

### Resource groups and endpoints

**🔌 Providers**
| Method | Path | Description |
|--------|------|-------------|
| GET | /api/providers | List all providers |
| POST | /api/providers | Create a provider |
| GET | /api/providers/{id} | Get a provider |
| PUT | /api/providers/{id} | Update a provider |
| DELETE | /api/providers/{id} | Delete a provider |
| GET | /api/providers/{id}/models | List available models (Ollama) |

**🤖 Agents**
| Method | Path | Description |
|--------|------|-------------|
| GET | /api/agents | List all agents |
| POST | /api/agents | Create an agent |
| GET | /api/agents/{id} | Get an agent |
| PUT | /api/agents/{id} | Update an agent |
| DELETE | /api/agents/{id} | Delete an agent |
| POST | /api/agents/generate | AI-generate agent config from a description |
| POST | /api/agents/spawn | Dispatch a task to an agent (programmatic trigger) |

**⊞ Projects**
| Method | Path | Description |
|--------|------|-------------|
| GET | /api/projects | List projects — `?kind=project\|monitor` |
| POST | /api/projects | Create a project |
| GET | /api/projects/{id} | Get a project |
| PUT | /api/projects/{id} | Update a project |
| DELETE | /api/projects/{id} | Delete a project |
| GET | /api/projects/{id}/agents | List agents assigned to project |
| POST | /api/projects/{id}/agents | Assign an agent to a project |
| DELETE | /api/projects/{id}/agents/{agentId} | Remove agent from project |
| POST | /api/projects/{id}/teams | Assign a team to a project |

**✦ Tasks**
| Method | Path | Description |
|--------|------|-------------|
| GET | /api/tasks | List tasks — `?project_id=` filter |
| POST | /api/tasks | Create a task |
| POST | /api/tasks/quick | Run a task in the Quick Tasks sandbox |
| GET | /api/tasks/running | List currently running tasks |
| GET | /api/tasks/attention | List tasks needing attention |
| GET | /api/tasks/{id} | Get a task |
| PUT | /api/tasks/{id} | Update a task |
| DELETE | /api/tasks/{id} | Delete a task |
| POST | /api/tasks/{id}/retry | Retry a failed task |
| POST | /api/tasks/{id}/dismiss | Dismiss a task from inbox |
| POST | /api/tasks/{id}/followup | Send a follow-up prompt to a completed task |

**⊡ Inbox**
| Method | Path | Description |
|--------|------|-------------|
| GET | /api/inbox | List inbox items (failed + awaiting approval) |
| POST | /api/inbox/dismiss-all | Bulk dismiss — `?filter=failed\|awaiting\|all` |
| POST | /api/inbox/{taskId}/approve | Approve a task awaiting human sign-off |
| POST | /api/inbox/{taskId}/reject | Reject a task |
| POST | /api/inbox/{taskId}/revise | Revise task input and re-run |

**⬡⬡ Teams**
| Method | Path | Description |
|--------|------|-------------|
| GET | /api/teams | List teams |
| POST | /api/teams | Create a team |
| GET | /api/teams/{id} | Get a team |
| PUT | /api/teams/{id} | Update a team |
| DELETE | /api/teams/{id} | Delete a team |
| POST | /api/teams/{id}/agents | Add agent to team |
| DELETE | /api/teams/{id}/agents/{agentId} | Remove agent from team |
| GET | /api/teams/{id}/export | Export team as portable bundle |
| POST | /api/import/team | Import a team bundle |

**📋 Agent Drafts** *(hire proposals from agents)*
| Method | Path | Description |
|--------|------|-------------|
| GET | /api/agent-drafts | List pending hire proposals |
| POST | /api/agent-drafts | Create a draft (programmatic) |
| PUT | /api/agent-drafts/{id} | Update a draft |
| POST | /api/agent-drafts/{id}/approve | Approve — creates the agent |
| POST | /api/agent-drafts/{id}/reject | Reject the proposal |
| POST | /api/agent-drafts/{id}/dismiss | Dismiss from inbox |

**📊 Stats**
| Method | Path | Description |
|--------|------|-------------|
| GET | /api/stats/costs | Cost breakdown by agent and time period |

**⚙ Admin**
| Method | Path | Description |
|--------|------|-------------|
| GET | /api/admin/backup | Download a live DB snapshot (VACUUM INTO) |
| GET | /api/admin/settings | Get system settings (global guardrails) |
| PUT | /api/admin/settings | Update system settings |
| POST | /api/admin/settings/generate-guardrails | AI-generate guardrail text from plain English |

**🔌 WebSocket**
| Method | Path | Description |
|--------|------|-------------|
| WS | /api/ws | Real-time event stream (task updates, inbox changes) |

WebSocket events are JSON objects with a `type` field. Document event types: `task_updated`, `task_created`, `inbox_changed`, `agent_draft_created`.

---

## Tab: Providers

A brief intro: *"Providers are the AI backends Phoenix uses to run tasks. Configure them in Settings → Providers."*

Simple table:

| Kind | Type | Notes |
|------|------|-------|
| `llm` | LLM | OpenAI-compatible HTTP API. Point at any URL (OpenAI, Anthropic, local proxy). |
| `ollama` | LLM | Local Ollama instance. Set base URL (default `http://localhost:11434`). |
| `opencode` | Coding agent | opencode CLI. Set binary path. |
| `pi` | Coding agent | pi CLI. Set binary path. |
| `claudecode` | Coding agent | claude CLI. Set binary path. |
| `crush` | Coding agent | crush CLI. Set binary path. |

No setup instructions — the UI form guides configuration. Link to Settings → Providers at the bottom.

---

## Tab: Tips & Gotchas

A short bulleted list of things that trip up new users. Plain text, no expandable rows needed.

Suggested tips:
- **Route ordering matters if you call the API directly** — static paths (`/api/tasks/running`) must be called exactly; they won't match `{id}` routes.
- **Agents don't run without a task** — create a task in a Project and assign an agent to it, or use Quick Tasks (⌘K) for one-offs.
- **Monitors vs Projects** — if an agent has a `heartbeat_interval`, set the project `kind=monitor`; it will then appear under Monitors, not Projects.
- **Global Guardrails override per-agent guardrails** — they are appended to every system prompt when enabled.
- **Task source provenance** — when a Monitor dispatches a task into another Project, set the `source` field so recipients know where it came from.
- **Backup before migrations** — use `GET /api/admin/backup` before upgrading; the download is a live WAL-consolidated snapshot.
- **WebSocket reconnects automatically** — the frontend reconnects on disconnect with exponential backoff; events missed during disconnect are not replayed.
- **Can hire vs can spawn** — `can_spawn_agents` lets an agent create tasks for *existing* agents; `can_hire_agents` lets it *propose new agents* via the draft/approval flow.

---

## Implementation Notes

### Files to create
- `web/src/pages/HelpPage.tsx` — main tabbed page component

### Files to modify
- `web/src/components/layout/Sidebar.tsx` — add Help entry
- `web/src/App.tsx` — add `/help` route

### Component structure

```
HelpPage
├── TabBar (local state: activeTab)
├── ConceptsTab
│   └── ConceptCard[]
├── ApiReferenceTab
│   └── ResourceGroup[]
│       └── EndpointRow[] (local expanded state per row)
├── ProvidersTab
└── TipsTab
```

All data (concepts, endpoints, provider table, tips) is **static** — no API calls needed. The page is fully static content, React only for tab switching and expand/collapse.

### Endpoint row expand/collapse
Each `EndpointRow` manages its own `expanded` boolean in local state. Click anywhere on the collapsed row to toggle. No accordion behaviour (multiple rows can be open simultaneously).

### Styling
Follow existing Phoenix UI conventions:
- Tab bar: same style as `SettingsPage` tabs
- Method badges: coloured `<span>` with monospace font (green/blue/amber/red)
- Paths: monospace, accent colour (`text-[--ph-accent]`)
- Expanded section: slightly inset background, `text-xs` for field tables
- Concept cards: `border border-[--ph-border] rounded-lg p-4` grid, same as existing card patterns

---

## Out of Scope

- Interactive "try it" / curl runner in the API tab
- Search/filter within the API reference
- Versioning or changelog section
- Video or animated walkthroughs
