# Project View Redesign — Spec

**Date:** 2026-06-22  
**Status:** Approved  
**Scope:** ProjectsWorkspace middle pane, project data model, dismiss behaviour, AI next-action suggestion

---

## Problem

The current project middle pane has four compounding problems:

1. **Dismissed tasks disappear from the project.** Clearing a task from Inbox sets `dismissed = 0` in the DB query for `ListByProject`, making the project's task history silently incomplete. Users who dismiss tasks from Inbox lose visibility into what work has been done.
2. **No goal or objective visible.** Projects only have a name. Looking at "Life of Manager" you cannot see what the project is trying to accomplish.
3. **Flat chronological list with no grouping.** Failed, running, completed, and awaiting-approval tasks are interleaved. There is no way to quickly see what needs attention vs what is done.
4. **No sense of progress or next steps.** Tasks feel like a log rather than a purposeful todo list. There is no mechanism to ask "what should I do next?"

---

## Design Decision

**Option B — Status-Grouped Sections** was selected.

A project is a durable, high-level container (like "Life of Manager" or "Escalations") that groups related tasks over time. It has an objective (the overall intent) and accumulates tasks indefinitely. The view should answer: *"What are we trying to do, how far along are we, and what needs attention?"*

---

## Changes

### 1. Data model — `projects` table

Add an `objective` column:

```sql
ALTER TABLE projects ADD COLUMN objective TEXT NOT NULL DEFAULT '';
```

Exposed on the `Project` model and all project API responses. No existing data is affected (defaults to empty string).

### 2. Dismiss behaviour — decouple Inbox from project history

**Current:** `dismissed = 1` hides the task from both Inbox AND `ListByProject`.

**New:** `dismissed = 1` still hides from Inbox. `ListByProject` is split into two queries:
- **Active query** (used for grouped sections): `dismissed = 0` — same as today.
- **History query** (used for the Completed section): `dismissed IN (0, 1)` AND `status = 'completed'` — returns all completed tasks regardless of dismiss state.

The frontend fetches two lists: the existing active list (`dismissed=0`, all non-completed statuses) and a separate completed-history list loaded lazily when the user expands the Completed section. The backend adds `GET /api/projects/:id/history` returning all completed tasks for the project regardless of `dismissed` state.

### 3. AI next-action suggestion

**New endpoint:** `POST /api/projects/:id/suggest`

- No request body required.
- Server assembles a prompt containing: project name, objective, and a summary of recent tasks (title + status, last 20).
- Calls the project's assigned agent's provider (or falls back to any available LLM provider) with a system prompt asking for 1–3 specific actionable next tasks.
- Returns JSON: `{ suggestions: [{ title: string, description: string }] }`
- If no provider is available, returns a 422 with a clear error message.

The suggestion is **ephemeral** — it is not persisted. Each click of the Suggest button makes a fresh call.

### 4. Project API — objective field

`PUT /api/projects/:id` gains support for `objective` in the request body. Existing create (`POST /api/projects`) also accepts `objective` (optional, defaults to `""`).

`GET /api/projects/:id` and the list endpoints return `objective` in all project responses.

---

## Frontend — ProjectsWorkspace Middle Pane

### Project header (replaces simple title + button strip)

```
┌─────────────────────────────────────────────────┐
│ Life of Manager                                  │
│ ┌─────────────────────────────────────────────┐ │
│ │ Objective  · click to edit                  │ │
│ │ Build out articles and resources to help    │ │
│ │ managers grow — delegation, feedback, 1:1s. │ │
│ └─────────────────────────────────────────────┘ │
│ ✓ 12 done  ✗ 2 failed  ● 1 running   [✦ Suggest] [+ Task] │
└─────────────────────────────────────────────────┘
```

- **Objective block:** Clicking the text makes it an inline `<textarea>`. On blur or Enter, calls `PUT /api/projects/:id` with the new objective. If no objective is set, shows a placeholder: *"Click to add an objective..."*
- **Status counts:** Derived from the loaded task list. Clicking a count badge scrolls to that section.
- **✦ Suggest button:** Calls `POST /api/projects/:id/suggest`, shows a loading state, then renders the suggestion card inline at the top of the task list.
- **+ Task button:** Opens the existing compose panel (unchanged).

### Task sections — priority order

Sections render in this fixed order. Empty sections are hidden entirely (not shown as empty headings).

| Priority | Section | Status filter | Colour |
|---|---|---|---|
| 1 | **Needs Attention** | `awaiting_approval` | Amber `#f59e0b` |
| 2 | **Running** | `running`, `queued` | Violet `#8b5cf6` |
| 3 | **Failed** | `failed` | Red `#ef4444` |
| 4 | **Completed** | `completed` (all, incl. dismissed) | Emerald `#10b981` |

Each section has a collapsible header showing the section name and count. **Needs Attention, Running, and Failed are expanded by default. Completed is collapsed by default** (but persists open/closed state in `localStorage` per project).

### AI Suggestion card

Rendered at the very top of the task list, above all sections, after clicking ✦ Suggest:

```
┌────────────────────────────────────────────────┐
│ ✦ Suggested next action                        │
│ Write article: "Running 1:1s that matter"      │
│ Completes your series on core manager skills   │
│ [▶ Run this]  [✕ Dismiss]                      │
└────────────────────────────────────────────────┘
```

- **▶ Run this:** Creates a new task with the suggested title and description via `POST /api/tasks`. The card collapses. The new task appears in Running after WS event.
- **✕ Dismiss:** Removes the card. No state persisted.
- If the suggestion call fails, show an inline error message in the card location with a retry link.
- Only one suggestion card shown at a time. Clicking Suggest again while a card is visible replaces it.

### Task rows (unchanged visual style)

Individual task rows within each section are identical to the existing `TaskRow` — left colour border, title, status badge, agent name, time ago. Clicking opens the right-pane detail view as today.

### Completed section — history fetch

When the user expands the Completed section for the first time, the frontend fires `GET /api/projects/:id/history` and appends those results to the completed list (de-duplicated by ID). A loading spinner shows during fetch. The section label updates to show the total count including dismissed.

### WebSocket refresh

The middle pane subscribes to `task.status_changed` events (it currently does not). When a status change arrives for a task in the current project, the task list re-fetches so sections update live (a running task moves to Completed, a queued task moves to Running, etc.).

---

## Backend — New Endpoint Summary

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/projects/:id/history` | All completed tasks for project regardless of `dismissed` flag |
| `POST` | `/api/projects/:id/suggest` | AI-generated next-action suggestions |

Both require the project to exist and belong to the authenticated context (same auth as existing project endpoints).

---

## What Does Not Change

- Three-pane layout structure
- Right-pane task detail view (TaskDetailView / TaskDetailModal)
- Task compose panel
- Working directory / Files tab
- Follow-up threads
- Inbox dismiss behaviour (dismissed still clears from Inbox — only the project view changes)
- Monitor projects (they use the same workspace; the objective field is optional)

---

## Migration & Rollout

1. DB migration: `ALTER TABLE projects ADD COLUMN objective TEXT NOT NULL DEFAULT ''` — safe, additive, no data loss.
2. Backend: add `objective` to model, store read/write, API handlers.
3. Backend: add `/api/projects/:id/history` and `/api/projects/:id/suggest`.
4. Frontend: update project header component, task list grouping, suggestion card.
5. No data migration needed for tasks or dismiss state — the history endpoint reads existing data.

---

## Out of Scope

- Drag-and-drop task reordering
- Manual task status changes from the project view
- Pinned / starred tasks within a project
- Project-level completion percentage or progress bar
- Per-project agent routing rules
