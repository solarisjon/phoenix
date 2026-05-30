# Projects & Monitors Split — Design Spec

**Date:** 2026-05-30  
**Status:** Approved for implementation  
**Author:** Brainstorming session with Jon

---

## Problem

Phoenix currently has a single "Project" concept that conflates two fundamentally different working patterns:

1. **Human-driven workbenches** — a human (or another agent) creates discrete tasks and assigns them to agents. The human is directing the work. The task list *is* the product.

2. **Autonomous daemons** — a heartbeat agent wakes on a schedule, surveys an external system (Jira, a queue, a repo), does its analysis, optionally dispatches tasks elsewhere, then goes back to sleep. No human drives it. It's a crontab with intelligence.

Mixing these in a single "Projects" list creates confusion: autonomous heartbeat runs appear alongside human task threads, the dispatch-style agents feel out of place in a workbench view, and there's no clear mental model for what each project type is for.

---

## Solution

Split into two clearly separated concepts with distinct sidebar navigation entries:

- **Projects** — human-driven workbenches (existing behaviour, refined)
- **Monitors** — autonomous schedule-driven daemons (new concept, new views)

Plus one small data model addition: a free-text `source` field on tasks to carry provenance when a Monitor dispatches into a Project.

---

## Definitions

### Project
A named workspace where a human (or another agent) creates discrete tasks and assigns them to agents. The human is in the driving seat. Agents respond to tasks given to them.

- May have multiple agents assigned
- Tasks are created by humans or by other agents via `spawn`
- The task thread view is the primary UI — ordered chronologically
- Heartbeat agents can be assigned but their role is to assist, not to self-direct
- Mode detection: `isAutonomous` flag removed from Projects — all Projects use the human/thread view

### Monitor
An autonomous daemon driven by a single heartbeat agent. It wakes on a schedule, does its work (analysis, triage, routing), optionally dispatches tasks into Projects or other Monitors, and sleeps until next fire.

- Has exactly **one** heartbeat agent (the primary agent)
- May have additional non-heartbeat agents assigned as execution helpers (optional, future)
- The UI focuses on: run log, last/next fire time, agent status
- Created separately from Projects, listed under a new "Monitors" sidebar entry
- A Monitor that dispatches tasks elsewhere uses the agent's instructions to encode the target project ID — no structural wiring needed

### Task Source (provenance)
A new optional free-text field `source TEXT DEFAULT ''` on the `tasks` table. When a heartbeat agent dispatches a task to another project, it populates this field with a human-readable origin string, e.g.:

```
"Jira triage 2026-05-30 — PHOEN-42, PHOEN-43"
```

- Written by the dispatching agent as part of its instructions
- Displayed on the task card as a small "origin" label (muted, below the task title)
- No FK relationship, no click-through — purely informational
- Empty for tasks created by humans or via the UI task form

---

## Data Model Changes

### Migration 010: `tasks.source` field

```sql
ALTER TABLE tasks ADD COLUMN source TEXT NOT NULL DEFAULT '';
```

### Migration 011: `projects.kind` field

```sql
ALTER TABLE tasks ADD COLUMN source TEXT NOT NULL DEFAULT '';
ALTER TABLE projects ADD COLUMN kind TEXT NOT NULL DEFAULT 'project'
    CHECK (kind IN ('project', 'monitor'));
```

The `kind` field drives routing in the UI and API. Existing projects default to `'project'`. A project becomes a Monitor by setting `kind = 'monitor'`.

> **Note:** This is intentionally minimal — both kinds remain in the same `projects` table. No schema duplication, no separate table. The kind field is the only structural difference.

---

## API Changes

### Projects

- `GET /api/projects` — add optional `?kind=project|monitor` filter. Without filter, returns all (backwards compatible).
- `POST /api/projects` — accepts `kind` field (default `'project'`)
- `PUT /api/projects/:id` — accepts `kind` field

### Tasks

- All task endpoints: `source` field passes through transparently (create, update, list, get)
- `POST /api/tasks` and `POST /api/tasks/quick` — accept optional `source` string
- `POST /api/agents/spawn` — accepts optional `source` string, passes to created task

### Model

```go
// Project gains:
Kind string `json:"kind"` // "project" | "monitor"

// Task gains:
Source string `json:"source"` // free-text provenance, empty if human-created
```

---

## Frontend Changes

### Sidebar

Add **Monitors** entry between Projects and Tasks:

```
◈  Dashboard
⊡  Inbox          [badge]
⊞  Projects
⟳  Monitors
✦  Tasks
⬡⬡ Teams
⚙  Settings
```

Icon: `⟳` (cycle/repeat) — conveys the scheduled, recurring nature.

### Projects Page (`/projects`)

- Filters to `kind=project` only — no Monitors shown here
- "New Project" form: no heartbeat agent option exposed (heartbeat agents can still be assigned manually, but the create form doesn't surface this — keeps the mental model clean)
- All existing behaviour preserved

### Monitors Page (`/monitors`) — new

List view showing all `kind=monitor` projects. Each Monitor card shows:
- Name and description
- Primary heartbeat agent name + interval (e.g. "every 30 min")
- Last run: time ago + status (completed / failed)
- Next run: countdown
- Quick "View runs" link to detail

Empty state: "No monitors yet. Create one to start watching." with a Create button.

### Monitor Detail Page (`/monitors/:id`) — new

Replaces the current `ProjectAutonomousView` (which is retired from Projects). Focused layout:

**Header section:**
- Monitor name + description
- Primary agent name, heartbeat interval
- Status pill: Active / Paused
- Next fire countdown (live, updates every second)
- "Run now" button (triggers immediate heartbeat task)

**Run log section:**
- Chronological list of heartbeat task runs (newest first)
- Each run card: timestamp, duration, status, collapsed output preview
- Expandable to full markdown output
- Shows `source` field if the task was dispatched from elsewhere

**No "New Task" button** — Monitors are not human-driven. If you need to trigger a run, use "Run now".

### Project Detail Page (`/projects/:id`)

- Always uses `ProjectHumanView` — the `isAutonomous` computed flag and `ProjectAutonomousView` component are retired
- `ProjectAutonomousView` component is deleted (its useful parts — countdown, run display — move to Monitor detail)
- Task cards: show `source` field as a small muted label beneath the title when non-empty, e.g. `↳ Jira triage 2026-05-30 — PHOEN-42`

### Create Monitor Flow

New "Create Monitor" modal/form (accessed from Monitors page):
1. Name + description
2. Select heartbeat agent (dropdown filtered to agents with `heartbeat_interval > 0`)
3. Working directory (optional)
4. Save → creates project with `kind=monitor`, assigns selected agent

> If no heartbeat agents exist yet, the form shows a prompt: "You'll need an agent with a heartbeat interval set. Go to Settings → Agents to configure one."

---

## Routing

```
/projects          → ProjectsPage (kind=project only)
/projects/:id      → ProjectDetailPage (human view always)
/monitors          → MonitorsPage (kind=monitor only)
/monitors/:id      → MonitorDetailPage (run log view)
```

The existing `/projects/:id` route continues to work for projects. Any existing heartbeat-assigned projects that are `kind=project` continue to show in Projects — admins can migrate them to Monitors by editing (a `kind` selector in the project edit form).

---

## Migration Path for Existing Data

- All existing projects default to `kind='project'`
- Existing heartbeat projects appear in Projects as before (human view, which now always applies)
- Users can re-classify them as Monitors via the project edit form — a `kind` toggle (Project / Monitor) in the edit modal
- No automated migration — user decides which existing projects should become Monitors

---

## What Is Removed / Retired

| What | Why |
|------|-----|
| `ProjectAutonomousView` component | Replaced by `MonitorDetailPage` |
| `isAutonomous` computed flag on ProjectDetailPage | All projects use human view |
| Heartbeat agent UI in Project create form | Moved to Monitor create flow |

---

## Out of Scope

- Structural wiring between Monitors and Projects (loose coupling by design)
- Monitor → Monitor dependency graphs
- Pausing/resuming individual Monitors from the UI (agent `status=paused` already handles this at the agent level)
- Run history pagination (load all for now, paginate later if needed)
- Multi-agent Monitors (primary heartbeat agent only for now)

---

## Success Criteria

1. A user landing on Projects sees only human-driven workbenches — no heartbeat clutter
2. A user landing on Monitors sees only autonomous daemons with a run-log focused view
3. Tasks dispatched by a Monitor into a Project show their `source` label on the task card
4. Existing projects and data are unaffected — kind defaults to `'project'`
5. The `ProjectAutonomousView` component is gone; the Monitor detail view replaces it cleanly
