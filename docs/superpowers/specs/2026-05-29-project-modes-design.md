# Project Modes & Task Refinement Design
**Date:** 2026-05-29  
**Status:** Approved for implementation

---

## Problem

Phoenix projects fall into two very different usage patterns, but both use the same UI:

1. **Human-driven** — a human creates tasks manually, an agent works them, the human reviews and iterates. Think: "write this slide deck, review it, ask for revisions."
2. **Autonomous** — a heartbeat agent wakes on a schedule, decides what to do, does it, and logs results. A human occasionally checks in but isn't in the loop per task. Think: "every 2 min, scan Jira for new escalations and triage them."

Additionally, agents dispatch work to other agents (spawn tasks). Currently all tasks look identical in the UI: a flat, chronological list. As autonomous projects run 24/7, that list becomes hundreds of entries with no way to distinguish heartbeat sessions from specialist work or to see queue health at a glance.

There is also no task refinement flow — a human can retry a completed task (run it again identically) but cannot say "good start, now do it again but make it shorter" with the previous output automatically included as context.

---

## Goals

- Auto-adapt the project detail page based on whether a heartbeat agent is assigned — zero extra setup from the user
- Show queue health (done / active / stuck counts) as the primary signal on autonomous projects
- Surface "needs attention" exceptions prominently; hide routine completions
- Add a reply-thread refinement flow to human-driven tasks — conversational iteration with context carried forward
- Keep Phoenix as a **task runner**, not a ticket tracker — agents report summaries as task output; Phoenix displays them
- Design must be **generic** — the Escalation Queue is one example; any agent watching any external system (GitHub issues, Zendesk, a spreadsheet) should work the same way

---

## What Is NOT Changing

- No new "project type" field — mode is inferred at render time from whether any assigned agent has `heartbeat_interval > 0`
- No ticket tracker — Phoenix never pulls from Jira/GitHub/etc directly; that is the agent's job via its own tools/MCP
- No new navigation — fits within the existing Projects → Project Detail page
- No changes to how heartbeat tasks are created or scheduled

---

## Visual Direction

**Light & Airy** — white cards, subtle drop shadows, warm stone tones (Tailwind stone palette), indigo accent. Approachable, closer to Notion/Linear than a terminal tool. Applied to the new project detail layout only; global theme system (dark/midnight/forest/ember/light) is unchanged and continues to work via CSS custom properties.

---

## Data Model Changes

### New task field: `follow_up_of`

```sql
-- migration 007
ALTER TABLE tasks ADD COLUMN follow_up_of TEXT REFERENCES tasks(id);
```

- `follow_up_of` — UUID of the task this is a follow-up to. `NULL` for original tasks.
- `parent_task_id` already exists for agent-spawned sub-tasks and is not changed.
- These are distinct concepts: `parent_task_id` = agent dispatch chain; `follow_up_of` = human refinement chain.

### Go model

```go
// model.Task gains:
FollowUpOf *string `json:"follow_up_of"` // nil = original task
```

### Context injection

When a follow-up task runs, the runner automatically prepends the parent task's output to the prompt as context. This happens in `runner.go` — no schema change needed for this.

---

## New API Endpoint

### `POST /api/tasks/:id/followup`

Creates a new task that is a follow-up to the given task. The follow-up task runs with the original task's output automatically injected as context.

**Request body:**
```json
{
  "description": "Make the intro punchier — lead with a provocative question. Cut slide 4.",
  "agent_id": "optional — defaults to same agent as parent"
}
```

**Behaviour:**
1. Load the parent task; 404 if not found or dismissed
2. 409 if parent task is currently `running` or `queued`
3. Create a new task with:
   - `project_id` = parent's project
   - `agent_id` = request body agent_id or parent's agent_id
   - `title` = parent's title (follow-up implied by chain)
   - `description` = request body description
   - `follow_up_of` = parent task ID
4. Enqueue the task immediately (same as `POST /api/tasks`)
5. Return the new task object

**Context injection in runner:** Before building the prompt, if `task.FollowUpOf != nil`, load that task's output and prepend it as a context block:

```
## Previous output (task: <title>)
<output text>

## Your follow-up instructions
<description>
```

---

## Project Detail Page — Auto-Adapting Layout

### Mode detection

```ts
const isAutonomous = projectAgents.some(a => (a.heartbeat_interval ?? 0) > 0)
```

Computed at render time. No API change needed.

### Autonomous mode layout

Replaces the current flat task list when `isAutonomous === true`.

**Header:**
- Project name + assigned agent names
- "● live" pill (green) with "next in Xm Xs" countdown — derived from last heartbeat task `created_at + interval` (same approximation as Team Detail page already uses)
- No "+ New Task" button in primary position (can be in a secondary "..." menu for manual overrides)

**Progress bar:**
- Single thin bar showing done/(done+active+stuck) ratio, green fill

**Stats row — 4 cards:**
- Done (green count) — tasks with `status = 'completed'` and `follow_up_of IS NULL` and not a heartbeat task title
- Active (orange count) — tasks with `status = 'running'` or `'queued'`
- Stuck (red count) — tasks with `status = 'failed'` and `dismissed = 0`
- Open — heartbeat tasks with `status = 'completed'` in last 24h (i.e. sessions run)

**"Needs attention" panel** (shown only when stuck tasks exist):
- Red-tinted card, pinned below stats
- Lists each failed/stuck task by title
- "Add note" → opens follow-up input (same mechanism as human-driven follow-up, pre-filled with "Note: ")
- "Dismiss" → calls existing `POST /api/tasks/:id/dismiss`

**Two-column body:**
- Left: "Last session" — most recent heartbeat task's output text, truncated to ~3 lines with "View full output →" link opening existing task detail modal
- Right: "Activity" — last 10 tasks in reverse chronological order, each showing status icon + title snippet + relative time. Click any row → task detail modal.

### Human-driven mode layout

Shown when `isAutonomous === false`. Replaces current flat task list.

**Header:**
- Project name + assigned agent names
- "+ New Task" button (indigo, top right)

**Task thread cards:**
Each task is a card showing:
- Title + status badge (Done / Running / Failed / Queued)
- Agent name + relative time + cost
- Output preview — first 2 lines of output text, truncated, with "read more" expanding inline
- Follow-up thread entries below the output (if any `follow_up_of` tasks exist for this task), displayed as an indented conversation thread
- Reply input at the bottom of each card — a text field placeholder "Follow up on this task..." with a send button. On submit, calls `POST /api/tasks/:id/followup`

**Task ordering:** newest first (same as current). Follow-up tasks are nested under their parent, not shown as top-level items.

**GuidedSetup:** unchanged — shown only when the project has zero tasks total.

---

## Component Plan

### New components

**`ProjectAutonomousView`** (`web/src/components/project/ProjectAutonomousView.tsx`)
- Props: `project`, `tasks`, `agents`, `onUpdate`
- Computes stats, next-heartbeat countdown, last session summary
- Renders header, progress bar, stats row, attention panel, two-column body

**`ProjectHumanView`** (`web/src/components/project/ProjectHumanView.tsx`)
- Props: `project`, `tasks`, `agents`, `onUpdate`
- Renders task thread cards with reply inputs
- Manages follow-up submission via `POST /api/tasks/:id/followup`

**`TaskThreadCard`** (`web/src/components/project/TaskThreadCard.tsx`)
- Props: `task`, `followUps: Task[]`, `agents`, `onUpdate`
- Renders a single task card with its follow-up thread and reply input

### Modified

**`ProjectDetailPage.tsx`**
- After loading tasks and agents, compute `isAutonomous`
- Render `<ProjectAutonomousView>` or `<ProjectHumanView>` accordingly
- Remove inline `TaskCard` and `GuidedSetup` (move GuidedSetup to its own file or keep inline but only used in human view)

### API client (`lib/api.ts`)

```ts
followUpTask(taskId: string, description: string, agentId?: string): Promise<Task>
```

---

## Light & Airy Visual Tokens

The new components use these Tailwind classes (which already map to theme-aware values via the existing CSS custom property system):

| Element | Classes |
|---|---|
| Page background | `bg-stone-50` |
| Card | `bg-white border border-stone-200 rounded-xl shadow-sm` |
| Card hover | `hover:shadow-md transition-shadow` |
| Primary number | `text-2xl font-bold text-stone-900` |
| Secondary text | `text-xs text-stone-400` |
| Done count | `text-green-600` |
| Active count | `text-orange-600` |
| Stuck count | `text-red-600` |
| Live pill | `bg-green-50 border border-green-200 text-green-700 text-xs rounded-full px-2 py-0.5` |
| Attention panel | `bg-red-50 border border-red-200 rounded-xl` |
| Reply input | `bg-stone-50 border border-stone-200 rounded-lg` |
| Send button | `bg-indigo-600 text-white rounded-md text-xs px-2 py-1` |
| Output preview | `bg-stone-50 border-l-2 border-stone-200 text-stone-500 text-xs` |
| Follow-up thread | `border-l-2 border-stone-200 pl-3` |

These work correctly in all 5 themes because the global `index.css` overrides map Tailwind's hardcoded colours to `--ph-*` custom properties. No new CSS variables needed.

---

## Migration

```sql
-- internal/store/sqlite/migrations/007_task_followup.sql
ALTER TABLE tasks ADD COLUMN follow_up_of TEXT REFERENCES tasks(id);
```

Store layer: add `follow_up_of` to `taskSelectCols`, `Update()`, and `Create()` in `internal/store/sqlite/task.go`.

---

## Spec Self-Review

**Placeholders:** None.

**Internal consistency:**
- Mode detection (`isAutonomous`) is computed client-side from agent data already fetched by `ProjectDetailPage` — no extra API call needed ✓
- `follow_up_of` vs `parent_task_id` are distinct and non-overlapping ✓
- Context injection happens in `runner.go` using the existing `req.Context []ContextEntry` mechanism already used by spawned tasks ✓
- Light & Airy tokens use existing Tailwind classes already in the codebase ✓

**Scope:** Focused. Two new React components, one new API endpoint, one migration, one new model field, one modified page. No new navigation, no new DB tables, no new providers.

**Ambiguity resolved:**
- "Stuck" = `status = 'failed' AND dismissed = 0`. Does not include `awaiting_approval` (that goes to Inbox).
- "Open" stat in autonomous mode = count of heartbeat sessions (tasks whose title starts with "Heartbeat —") completed in last 24h. Approximation is fine — it's a pulse indicator, not an audit.
- Follow-up tasks appear **nested under their parent** in human view, not as top-level cards. This prevents the list growing confusingly.
- The "next heartbeat in Xm" countdown does not require a new API — derived from `last_heartbeat_task.created_at + agent.heartbeat_interval` in the frontend, same as Team Detail page.
