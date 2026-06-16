# Phoenix — Active Development Context

**Last updated:** 2026-06-16 (v0.3)  
**Purpose:** Quick-load context for a coding agent resuming work on this project. Read this file first, then the GitHub Issues at https://github.com/solarisjon/phoenix/issues, then proceed.

---

## What Phoenix Is

A self-hosted AI agent orchestration platform. Single Go binary, SQLite, React frontend embedded via `embed.FS`. Users configure LLM or coding-agent providers, create agents with persona/instructions/guardrails, assign agents to projects, and run tasks. Full design in `docs/superpowers/specs/2026-05-28-phoenix-design.md`.

**Dev instance:** `http://localhost:8080`  
**Prod instance:** `http://172.29.72.127:8090` (Podman container on scs001166435)  
**Database (dev):** `~/.local/share/phoenix/phoenix.db`  
**Database (prod):** `~/Prod/phoenix/data/phoenix.db` on server  
**Binary:** `/Users/jbowman/src/phoenix/phoenix`  
**GitHub:** `https://github.com/solarisjon/phoenix`  
**Issues:** tracked at https://github.com/solarisjon/phoenix/issues

### Deploy to production
```bash
make deploy-remote          # build-web + cross-compile + scp + podman rebuild on server
make server-setup           # first-time server bootstrap (run once)
# Container: phoenix-app, port 8090, data at ~/Prod/phoenix/data/
# Logs: ssh jbowman@172.29.72.127 'podman logs -f phoenix-app'
```

---

## Tech Stack

- **Backend:** Go, `github.com/solarisjon/phoenix`
- **Router:** `github.com/go-chi/chi/v5`
- **DB:** SQLite via `modernc.org/sqlite`, migrations embedded in `internal/store/sqlite/migrations/`
- **Frontend:** React + TypeScript + Vite + Tailwind CSS, built into `web/dist/`, embedded via `embed.FS` in `internal/frontend/`
- **Real-time:** WebSocket hub in `internal/api/hub.go`, events in `internal/api/events.go`

---

## Repository Layout (actual)

```
cmd/phoenix/main.go                    # entry point; wires all repos, starts scheduler
internal/
  model/model.go                       # ALL domain types (Agent, Task, Project, Team, AgentDraft, SystemSettings…)
  store/store.go                       # repository interfaces
  store/sqlite/                        # SQLite impls + embedded migrations
    migrations/
      001_initial.sql
      002_agent_model_override.sql     # model_override, dismissed
      003_agent_spawn.sql              # can_spawn_agents
      004_project_working_dir.sql      # projects.working_dir
      005_teams.sql                    # teams + team_agents
      006_task_pid.sql                 # tasks.runner_pid, tasks.timeout_at
      007_task_followup.sql            # tasks.follow_up_of
      008_agent_hiring.sql             # agents.can_hire_agents + agent_drafts table
      009_system_settings.sql          # system_settings key/value table (global guardrails)
      010_task_source.sql              # tasks.source free-text provenance field
      011_project_kind.sql             # projects.kind ('project' | 'monitor')
    agent.go, task.go, project.go, team.go, agent_draft.go, stats.go, admin.go, sqlite.go
    system_settings.go                 # SystemSettingsRepo: Get/Save global guardrails
  api/
    server.go                          # router — ALL routes registered here
    agent.go                           # CRUD + generate + spawnTask (source field added)
    agent_draft.go                     # CRUD + approve + reject + dismiss
    task.go                            # CRUD + retry + dismiss + followup + quick + listRunning + listAttention
    inbox.go                           # approve + reject + revise + dismissAll
    settings.go                        # GET/PUT /admin/settings + generate-guardrails
    provider.go, project.go, team.go, stats.go, admin.go
    hub.go / ws.go / events.go         # WebSocket
  agent/
    runner.go                          # goroutine lifecycle, task execution, PID tracking, timeout
    prompt.go                          # system prompt assembly; global guardrails injection; hiring instructions
  scheduler/
    scheduler.go                       # scans every 60s; interval monitors use per-monitor tickers; daily monitors evaluated centrally against wall clock (catch-up + dedup)
  provider/
    provider.go                        # Provider interface + shared types (TaskRequest, StreamChunk, etc.)
    envexpand.go                       # ${ENV_VAR} expansion in configs
    llm/llm.go                         # OpenAI-compatible SSE streaming HTTP adapter
    ollama/ollama.go                   # Ollama local model adapter (/api/chat NDJSON)
    opencode/opencode.go               # opencode CLI adapter
    pi/pi.go                           # pi CLI adapter (stdin prompt delivery, --mode json)
    claudecode/claudecode.go           # claude CLI adapter (stream-json)
    crush/crush.go                     # crush CLI adapter (AGENTS.md lifecycle, stdin prompt)
    registry/registry.go               # builds Provider from DB record; dispatches on kind field
web/src/
  lib/api.ts                           # typed API client (all endpoints)
  lib/ws.ts                            # WebSocket client (EventType union)
  lib/utils.ts                         # taskStatusVariant, parseOutput, formatCost, timeAgo
  components/
    layout/AppLayout.tsx               # inbox + running count badges; WS lifecycle
    layout/Sidebar.tsx                 # nav: Dashboard, Inbox, Projects, Monitors, Tasks, Teams, Settings
    project/ProjectHumanView.tsx       # human-driven task thread view (ProjectAutonomousView DELETED)
    project/TaskThreadCard.tsx         # task card with reply input; shows task.source provenance label
    ui/follow-up-thread.tsx            # chat-bubble follow-up UI (used in all task modals)
    ui/markdown-output.tsx             # react-markdown with --ph-* var scoped CSS
    ui/quick-task.tsx                  # floating FAB + ⌘K modal
    ui/theme-picker.tsx                # theme switcher
  pages/
    DashboardPage.tsx, InboxPage.tsx, TasksPage.tsx
    ProjectsWorkspace.tsx              # THREE-PANE workspace: project list | task inbox | task detail/compose
                                       # replaces old ProjectsPage.tsx + ProjectDetailPage.tsx
                                       # includes: ProjectListItem, TaskRow, TaskDetailView, TaskComposeView,
                                       #           FileRow, FilePreviewView, AgentPickerModal
    MonitorsPage.tsx                   # lists kind=monitor; flat dark cards with health-signal dots
    MonitorDetailPage.tsx              # run log, countdown, run-now; RunCard with left-border status color
    AgentsPage.tsx, ProvidersPage.tsx
    SettingsPage.tsx                   # System tab: Global Guardrails section + DB backup
    TeamsPage.tsx, TeamDetailPage.tsx
```

---

## Data Model — Current State (migrations 001–024)

### agents
```sql
id, name, persona, instructions, guardrails, provider_id,
model_override TEXT DEFAULT '',
can_spawn_agents INTEGER DEFAULT 0,
can_hire_agents INTEGER DEFAULT 0,
behaviour TEXT DEFAULT '',           -- unified persona+instructions (migration 017)
hard_guardrails TEXT DEFAULT '',     -- mandatory stop-and-ask guardrails (migration 018)
max_concurrent INTEGER DEFAULT 0,    -- 0=unlimited (migration 014)
template_id TEXT,                    -- deprecated, not used functionally (migration 020)
created_by, status, created_at
```

> `heartbeat_interval` was removed (migration 021). `template_id` is stored but not acted on — UI no longer shows it.

### tasks
```sql
id, project_id, agent_id, parent_task_id, follow_up_of,
title, description, status, input, output,
cost_usd REAL DEFAULT 0,
tokens_in INTEGER DEFAULT 0,         -- migration 012
tokens_out INTEGER DEFAULT 0,        -- migration 012
dismissed INTEGER DEFAULT 0,
runner_pid INTEGER DEFAULT 0,
timeout_at DATETIME,
source TEXT DEFAULT '',              -- free-text provenance (migration 010)
health_signal TEXT,                  -- monitor runs: all_clear|needs_attention|failed (migration 016)
guardrail_reason TEXT,               -- set when hard guardrail fires
is_critic_review INTEGER DEFAULT 0,  -- critic tasks flagged to prevent critic loops (migration 019)
reviewed_task_id TEXT,               -- FK to original task this critic reviewed (migration 019)
critic_mode TEXT DEFAULT 'inherit',  -- inherit|none|builtin|agent:<id> (migration 024)
created_at, started_at, completed_at
```

### agent_drafts
```sql
id, created_by_agent_id, created_by_task_id, name, persona,
instructions, guardrails, provider_id,
status TEXT DEFAULT 'pending_approval',  -- pending_approval | approved | rejected
dismissed INTEGER DEFAULT 0,
created_at, updated_at
```

### projects
```sql
id, name, description,
working_dir TEXT DEFAULT '',
kind TEXT DEFAULT 'project' CHECK(kind IN ('project','monitor')),  -- migration 011
schedule_interval INTEGER,           -- seconds between monitor runs (migration 015); nil=no schedule
schedule_kind TEXT NOT NULL DEFAULT 'interval',  -- migration 026: 'interval' | 'daily'
schedule_times TEXT NOT NULL DEFAULT '[]',       -- migration 026: JSON array of "HH:MM" local times (daily only)
schedule_catch_up INTEGER NOT NULL DEFAULT 0,    -- migration 026: daily only; run a missed time once at next opportunity same day
critic_agent_id TEXT,                -- deprecated; use critic_mode (migration 019)
critic_mode TEXT DEFAULT 'none',     -- none|builtin|agent:<id> (migration 024)
owner, status, created_at,
tags TEXT NOT NULL DEFAULT '[]'      -- migration 023: JSON array of tag strings
```

### memos (migration 022)
```sql
id, project_id, project_name, task_id, agent_id, agent_name,
title, body,
priority TEXT DEFAULT 'normal' CHECK(priority IN ('normal','high')),
status TEXT DEFAULT 'unread' CHECK(status IN ('unread','read','flagged','archived')),
created_at
```

### system_settings (migration 009)
```sql
key TEXT PRIMARY KEY, value TEXT, updated_at DATETIME
-- rows: global_guardrails_enabled ('0'/'1'), global_guardrails (text)
```

### teams + team_agents + project_agents (from migration 005)

---

## API Routes (complete)

```
GET/POST           /api/providers
GET/PUT/DELETE     /api/providers/:id
GET                /api/providers/:id/models

GET/POST           /api/agents
POST               /api/agents/generate
POST               /api/agents/spawn              # body: source field optional
GET/PUT/DELETE     /api/agents/:id

GET/POST           /api/agent-drafts
PUT                /api/agent-drafts/:id
POST               /api/agent-drafts/:id/approve
POST               /api/agent-drafts/:id/reject
POST               /api/agent-drafts/:id/dismiss

GET/POST           /api/projects                  # GET: ?kind=project|monitor&status=active|archived (default status=active)
GET/PUT/DELETE     /api/projects/:id              # PUT: kind field accepted; DELETE hard-deletes project + all tasks
POST               /api/projects/:id/archive      # sets status=archived; blocks if tasks running
POST               /api/projects/:id/restore      # sets status=active
GET                /api/projects/summaries        # task health summary keyed by project ID
GET/POST           /api/projects/:id/agents
DELETE             /api/projects/:id/agents/:agentId
POST               /api/projects/:id/teams
GET                /api/projects/:id/files        # list files in working_dir (depth ≤3, no hidden)
GET                /api/projects/:id/files/*      # get file content (≤256KB)

GET/POST           /api/tasks                    # ?project_id= filter; POST: source field optional
POST               /api/tasks/quick              # sandbox project
GET                /api/tasks/running
GET                /api/tasks/attention
GET/PUT/DELETE     /api/tasks/:id
POST               /api/tasks/:id/retry
POST               /api/tasks/:id/dismiss
POST               /api/tasks/:id/followup

GET                /api/inbox
POST               /api/inbox/dismiss-all         # ?filter=failed|awaiting|all
POST               /api/inbox/:taskId/approve
POST               /api/inbox/:taskId/reject
POST               /api/inbox/:taskId/revise

GET/POST           /api/teams
GET/PUT/DELETE     /api/teams/:id
GET/POST           /api/teams/:id/agents
DELETE             /api/teams/:id/agents/:agentId
POST               /api/teams/:id/assign/:projectId
GET                /api/teams/:id/export
POST               /api/import/team

GET                /api/memos                    # ?status=unread|read|flagged|archived (default: all non-archived)
POST               /api/memos                    # create memo manually
GET                /api/memos/count              # {count} of unread+flagged (sidebar badge)
PUT                /api/memos/:id/status         # {status: unread|read|flagged|archived}
DELETE             /api/memos/:id

GET                /api/stats/costs
GET                /api/admin/backup
GET/PUT            /api/admin/settings            # global guardrails
POST               /api/admin/settings/generate-guardrails

WS                 /api/ws
```

**Route ordering:** static routes MUST be before `{id}` params in chi. `/tasks/quick`, `/tasks/running`, `/tasks/attention`, `/agents/generate`, `/agents/spawn`, `/inbox/dismiss-all` all registered before `/:id` params.

---

## Provider Kinds & Adapters

| Kind | Type | Binary | Stream format | Notes |
|------|------|--------|---------------|-------|
| `llm` | `llm` | HTTP | SSE `data: {"choices":[...]}` | OpenAI-compatible |
| `ollama` | `llm` | HTTP | NDJSON `/api/chat` | Local models; kind field in config |
| `opencode` | `coding_agent` | `opencode` | NDJSON `{"type":"text","part":{...}}` | |
| `pi` | `coding_agent` | `pi` | NDJSON `{"type":"message_update",...}` | Prompt via stdin |
| `claudecode` | `coding_agent` | `claude` | NDJSON `{"type":"assistant",...}` | |
| `crush` | `coding_agent` | `crush` | Plain text lines | Prompt via stdin; system prompt via AGENTS.md |

Registry dispatches: `coding_agent` type → `kind` field; `llm` type → `kind=ollama` → ollama adapter, else → llm adapter.

---

## Key Architectural Decisions

- **Project/Monitor Tags:** Stored as `tags TEXT DEFAULT '[]'` (JSON array) on the `projects` table (migration 023). Serialised/deserialised in `project.go` via `marshalTags`/`unmarshalTags`. API accepts `tags: string[]` on create/update; `normaliseTags()` lowercases and dedupes. Frontend: `TagInput` component (inline pill editor with autocomplete from existing tags), `TagPill` for display, `FilterSortBar` + `applyFilterSort`/`collectAllTags` utilities for filter/sort/group-by-tag. Both ProjectsPage and MonitorsPage have full filter bar.
- **Briefing / Memos:** Agents auto-post memos by embedding `MEMO_START / Title: / Priority: / body / MEMO_END` blocks in their output — runner extracts them on task completion and persists to `memos` table. Users can also manually pin any completed task via "📋 Pin to Briefing" button on task cards. `/briefing` page shows all memos with Read/Flag/Archive/Delete actions. Sidebar shows unread+flagged count badge (violet). WS event `memo.created` triggers badge refresh.
- **Memo system prompt:** Every agent gets a `## Briefing Memos` section injected into its system prompt explaining the `MEMO_START…MEMO_END` format. Agents are instructed to use it only for genuinely important findings.
- **Archive vs Delete:** Archive sets `projects.status='archived'` — project disappears from active views but all tasks/history preserved; recoverable from Settings → Archived tab. Delete (`DELETE /api/projects/:id`) calls `DeleteWithTasks` which hard-deletes the project AND all its tasks in a transaction. Active projects filtered by `status='active'` by default; `?status=archived` to list archived.
- **Dismiss vs delete (tasks):** `dismissed=1`, preserved for audit; all list queries filter `AND dismissed = 0`
- **Follow-up context:** `InjectFollowUpContext()` in `prompt.go` prepends parent output as `## Previous output` block
- **Prompt delivery:** pi and crush receive prompt via **stdin** (not CLI arg) — avoids ARG_MAX issues with long follow-up prompts
- **pi adapter:** always uses stdin; `buildArgs()` no longer appends prompt as positional arg
- **crush adapter:** uses stdin when prompt > 8192 bytes; short prompts still as CLI arg
- **Ollama thinking tokens:** stripped by default (`message.thinking` field skipped); `keep_thinking=true` to surface
- **Agent hiring:** `can_hire_agents` agents get hiring instructions injected into system prompt; proposals go to `agent_drafts`, never directly create agents; approval creates Agent with `created_by="agent:<id>"`
- **Quick Tasks sandbox:** fixed UUID `00000000-0000-0000-0000-000000000002`; `ensureSandboxProject()` is idempotent
- **Backup:** `VACUUM INTO` for WAL-consolidated snapshot safe during live operation
- **Projects vs Monitors:** `projects.kind` field distinguishes human-driven workbenches (`'project'`) from autonomous heartbeat daemons (`'monitor'`). Projects always use `ProjectHumanView`. Monitors use `MonitorDetailPage` (run log). `ProjectAutonomousView` is deleted. `isAutonomous` flag is gone.
- **Task source provenance:** `tasks.source` is a free-text string set by dispatching agents (e.g. Monitors) when creating tasks in other projects. Empty for human-created tasks. Displayed on task cards as `↳ <source>`.
- **Global guardrails:** stored in `system_settings` table. When `global_guardrails_enabled=1`, the text is appended to every agent's system prompt under `## Platform-Wide Guardrails (mandatory)`. Loaded per-task in `runner.go`, injected in `prompt.go`. Takes precedence over per-agent guardrails.
- **Devil's Advocate / Critic Mode:** `projects.critic_mode` sets the default for all tasks in the project. `tasks.critic_mode` can override per-task (`"inherit"` = use project setting). `"builtin"` launches an ephemeral DA review using `BuildBuiltinCriticRequest()` and `executeBuiltinCritic()` in `runner.go` — same provider as the original agent, hardcoded contrarian system prompt, no registered agent needed. `"agent:<id>"` uses a specific registered agent. Critic tasks are flagged `is_critic_review=true` to prevent loops. Old `critic_agent_id` FK migrated to `"agent:<id>"` string via migration 024.
- **Agents page:** Renamed from "Agent Templates" to "Agents". `template_id` / Base Template field removed from Edit Agent form. Template/Instance badge removed from agent list. The `template_id` column remains in DB but is not surfaced in UI.
- **Bulk inbox dismiss:** `POST /api/inbox/dismiss-all?filter=failed|awaiting|all` marks matching tasks dismissed. Static route registered before `/:taskId`.
- **Inbox badge:** counts failed + awaiting_approval tasks AND pending agent_drafts
- **Model override:** patched into provider config JSON at runtime via `GetWithOverride()`, not cached
- **Team export api_key:** always exported as empty string — never included in bundle
- **Orphaned tasks on restart:** `ResetOrphanedTasks()` marks queued/running → failed; kills subprocesses via `runner_pid`
- **Projects workspace (v0.2):** `ProjectsWorkspace.tsx` is a single-file three-pane component. Left pane = project list (`ProjectListItem`). Centre pane = task inbox (`TaskRow`) + tabs (Tasks / Files). Right pane = task detail (`TaskDetailView`), task compose (`TaskComposeView`), or file preview (`FilePreviewView`). Old `ProjectsPage.tsx` and `ProjectDetailPage.tsx` are DELETED. Route `/projects` and `/projects/:id` both render `ProjectsWorkspace` — the component selects the project from URL param if present.
- **File browser:** `GET /api/projects/:id/files` lists working_dir contents (depth ≤3, hidden files skipped, dirs marked). `GET /api/projects/:id/files/*` returns raw content (≤256KB via `io.LimitReader`). Frontend `FileRow` shows name + size + artifact badge. `FilePreviewView` renders content in `MarkdownOutput` or plain `<pre>`.
- **Artifact parsing:** Agents emit `ARTIFACT_START\nFilename: foo.md\nType: document\n\n<content>\nARTIFACT_END` blocks. `extractAndSaveArtifacts()` in `runner.go` parses these on task completion, writes files to `project.working_dir/<filename>`, and calls `extractAndSaveMemos` to post a briefing entry. `parseArtifactBlocks` is duplicated in `api/project.go` (for `is_artifact` flag in file listings) and `agent/runner.go` (runtime extraction) to avoid circular imports.
- **Health-signal dots:** `ProjectListItem` and `MonitorCard` both load `api.projects.summaries()` (returns `Record<string, ProjectSummary>`) on page load. Dot logic: violet = running/queued/pending > 0, amber = awaiting_approval > 0, red = failed > 0, green = completed > 0 and no others. Same logic in both pages — keep in sync if changing.
- **Left-border color on task/run rows:** `TaskRow` (ProjectsWorkspace) and `RunCard` (MonitorDetailPage) both use `taskStatusVariant()` → `STATUS_BORDER` map: success→emerald-500, warning→amber-500, danger→red-500, info→violet-500, muted/default→slate-600/700. Applied as `border-l-2` (TaskRow) or `border-l-2` (RunCard) with `border-l-{color}` Tailwind class.

---

## Build & Deploy Commands

```bash
# Backend
go build ./...                          # check compilation
go test ./...                           # run all tests
go build -o phoenix ./cmd/phoenix/      # build binary
pkill -f './phoenix'; nohup ./phoenix >> /tmp/phoenix.log 2>&1 &
sleep 1.5 && curl http://localhost:8080/api/agents  # verify up

# Frontend
cd web && npm run build                 # builds web/dist/ (re-embedded on next go build)

# Database inspection
sqlite3 ~/.local/share/phoenix/phoenix.db ".tables"
sqlite3 ~/.local/share/phoenix/phoenix.db ".schema agents"
```

---

## Live Environment

- Crush provider: `4f4119b0` (kind=crush, binary=/opt/homebrew/bin/crush)
- Ollama provider: `83247978` (kind=ollama, model=qwen3.5:latest)
- Sandbox project: `00000000-0000-0000-0000-000000000002`
- Migrations applied: 001–024

---

## Next Steps (see GitHub Issues)

Recently completed (2026-05-31):
- ✓ Per-agent activity log (#10)
- ✓ Full-text task search FTS5 (#11)
- ✓ Cost estimate before running (#5)
- ✓ Database restore endpoint (#8)
- ✓ Model picker dropdown (#18)
- ✓ Task queuing / per-agent concurrency limits (#15)

Recently completed (2026-06-10 — v0.2):
- ✓ Projects workspace redesign — three-pane email-inbox layout (ProjectsWorkspace.tsx)
- ✓ Task compose panel — right-pane form with roster-filtered agent picker and DA toggle
- ✓ File browser — list and preview working directory files in-pane (Files tab)
- ✓ Artifact parsing — agents embed ARTIFACT_START…ARTIFACT_END; runner extracts & saves to working_dir; briefing entry auto-created
- ✓ Visual consistency — health-signal dots on Project/Monitor cards; left-border color on TaskRow/RunCard
- ✓ Monitors visual polish — flat dark card style with status dots; MonitorDetailPage RunCard left-border color

Recently completed (2026-06-04):
- ✓ Project Archive/Restore + Settings → Archived tab
- ✓ Briefing / Memos system (agent-posted + manual pin from task cards)
- ✓ Project/Monitor tags — filter bar, sort (including group-by-tag), tag pills on cards
- ✓ Devil's Advocate / Critic Mode (migration 024) — builtin ephemeral critic, no agent record needed
- ✓ Agents page cleanup — removed template_id field from UI, renamed page from "Agent Templates" to "Agents"

Open backlog — https://github.com/solarisjon/phoenix/issues:
1. **#13** Mobile-friendly layout (sidebar collapses to bottom nav)
2. **#19** Keyboard shortcuts (R=retry, D=dismiss, J/K navigate)
3. **#12** Multi-user authentication (Phase 7 — large)
4. **#14** claudecode smoke test (BLOCKED: needs claude auth)
5. **#7**  Copilot adapter (BLOCKED: needs copilot login)

---

## Gotchas

- **Route ordering in chi:** static routes BEFORE parameterised routes or chi matches wrong handler. `/inbox/dismiss-all` must be before `/inbox/:taskId`.
- **Migration location:** `internal/store/sqlite/migrations/` ONLY (not repo root `migrations/`)
- **Registry caching:** `registry.Get()` caches; `GetWithOverride()` bypasses; call `registry.Invalidate(id)` after provider update
- **Server restart:** NOT hot-reloaded. Kill + rebuild + restart after any Go change
- **pi stdin:** prompt delivered via stdin since 2026-05-30; `buildArgs()` no longer appends prompt arg
- **crush --yolo:** flag does NOT exist in crush run; permissions via `~/.config/crush/crush.json` allowed_tools
- **Ollama thinking:** `message.thinking` field in NDJSON chunks — skip by default, not part of `message.content`
- **taskSelectCols const:** in `task.go` — must stay in sync with schema column count for scan to work. `source` column added after `timeout_at`, before `created_at`.
- **can_hire_agents vs can_spawn_agents:** independent flags; spawn = task for existing agent; hire = propose new agent via drafts
- **ProjectAutonomousView is deleted:** do not reference it. Monitor detail view (`MonitorDetailPage`) is the replacement.
- **isAutonomous flag is gone:** `ProjectDetailPage` always uses `ProjectHumanView`. Monitors are detected by `project.kind === 'monitor'`, not by agent heartbeat_interval.
- **project.kind filter:** `GET /api/projects?kind=monitor` returns only monitors. Without the param, returns all active (backwards compatible for scheduler, stats, etc.).
- **project.status filter:** `GET /api/projects?status=archived` returns archived projects. Default is `status=active`. Scheduler and `List()` (no status filter) return all regardless of status.
- **Settings → Archived tab:** `ArchivedProjectsTab` component in `SettingsPage.tsx`; fetches `listArchived()` for both `'project'` and `'monitor'` kinds; offers Restore and Delete buttons.
- **AssembleRequest signature:** takes `(agent, task, globalGuardrails string)` — third arg added 2026-05-30. All callers must pass it.
- **critic_mode resolution:** task-level `critic_mode` takes precedence over project-level. `"inherit"` on a task means use the project setting. `"none"` disables. `"builtin"` runs `executeBuiltinCritic()` in runner using same provider as original agent — no DB agent record. `"agent:<id>"` uses a registered agent via `launchAgentCritic()`. Critic tasks have `is_critic_review=true` to prevent recursive critic loops.
- **template_id is vestigial:** stored in DB (migration 020), never acted on. UI no longer shows it. Do not build features on top of it without implementing real inheritance first.
- **critic_agent_id is deprecated:** migration 024 copies any existing values into `critic_mode` as `"agent:<id>"`. API still accepts it for backwards compat via `resolveCriticMode()` in `project.go`.
- **taskSelectCols / projectSelectCols:** must stay in sync with schema. `critic_mode` added to both after migration 024.
- **ProjectsWorkspace route:** both `/projects` and `/projects/:id` render `ProjectsWorkspace`. The component reads `useParams().id` and selects the matching project — if no ID, nothing is selected. App.tsx registers both routes pointing to the same component.
- **File content route ordering:** `GET /api/projects/:id/files` must be registered BEFORE `GET /api/projects/:id/files/*` in chi to avoid wildcard eating the bare list call.
- **Artifact MIME detection:** `project.go` `getProjectFileContent` infers `content_type` from extension. Unknown extensions → `text/plain`. Binary files (images, etc.) are not guarded — callers should check `content_type` before rendering.
- **summaries endpoint:** `GET /api/projects/summaries` is a static route and MUST be registered before `GET /api/projects/:id` in chi. It aggregates task counts per project into a `map[string]ProjectSummary` used for health-signal dots.
