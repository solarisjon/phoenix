# Phoenix — Active Development Context

**Last updated:** 2026-05-30 (end of day)  
**Purpose:** Quick-load context for a coding agent resuming work on this project. Read this file first, then the GitHub Issues at https://github.com/solarisjon/phoenix/issues, then proceed.

---

## What Phoenix Is

A self-hosted AI agent orchestration platform. Single Go binary, SQLite, React frontend embedded via `embed.FS`. Users configure LLM or coding-agent providers, create agents with persona/instructions/guardrails, assign agents to projects, and run tasks. Full design in `docs/superpowers/specs/2026-05-28-phoenix-design.md`.

**Running instance:** `http://localhost:8080`  
**Database:** `~/.local/share/phoenix/phoenix.db`  
**Binary:** `/Users/jbowman/src/phoenix/phoenix`  
**GitHub:** `https://github.com/solarisjon/phoenix`  
**Issues:** tracked at https://github.com/solarisjon/phoenix/issues

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
    scheduler.go                       # heartbeat ticker; scans every 60s; skips if agent busy
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
    ProjectDetailPage.tsx              # always human view; no isAutonomous detection
    ProjectsPage.tsx                   # lists kind=project only; edit form has kind toggle
    MonitorsPage.tsx                   # NEW: lists kind=monitor; create monitor form
    MonitorDetailPage.tsx              # NEW: run log, countdown, run-now button
    AgentsPage.tsx, ProvidersPage.tsx
    SettingsPage.tsx                   # System tab: Global Guardrails section + DB backup
    TeamsPage.tsx, TeamDetailPage.tsx
```

---

## Data Model — Current State (migrations 001–011)

### agents
```sql
id, name, persona, instructions, guardrails, provider_id,
model_override TEXT DEFAULT '',
can_spawn_agents INTEGER DEFAULT 0,
can_hire_agents INTEGER DEFAULT 0,
heartbeat_interval INTEGER,
created_by, status, created_at
```

### tasks
```sql
id, project_id, agent_id, parent_task_id, follow_up_of,
title, description, status, input, output,
cost_usd REAL DEFAULT 0,
dismissed INTEGER DEFAULT 0,
runner_pid INTEGER DEFAULT 0,
timeout_at DATETIME,
source TEXT DEFAULT '',              -- free-text provenance (migration 010)
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
owner, status, created_at
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

GET/POST           /api/projects                  # GET: ?kind=project|monitor filter
GET/PUT/DELETE     /api/projects/:id              # PUT: kind field accepted
GET/POST           /api/projects/:id/agents
DELETE             /api/projects/:id/agents/:agentId
POST               /api/projects/:id/teams

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

- **Dismiss vs delete:** `dismissed=1`, preserved for audit; all list queries filter `AND dismissed = 0`
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
- **Bulk inbox dismiss:** `POST /api/inbox/dismiss-all?filter=failed|awaiting|all` marks matching tasks dismissed. Static route registered before `/:taskId`.
- **Inbox badge:** counts failed + awaiting_approval tasks AND pending agent_drafts
- **Model override:** patched into provider config JSON at runtime via `GetWithOverride()`, not cached
- **Team export api_key:** always exported as empty string — never included in bundle
- **Orphaned tasks on restart:** `ResetOrphanedTasks()` marks queued/running → failed; kills subprocesses via `runner_pid`

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
- Migrations applied: 001–011

---

## Next Steps (see GitHub Issues)

Recently completed (2026-05-31):
- ✓ Per-agent activity log (#10)
- ✓ Full-text task search FTS5 (#11)
- ✓ Cost estimate before running (#5)
- ✓ Database restore endpoint (#8)
- ✓ Model picker dropdown (#18)
- ✓ Task queuing / per-agent concurrency limits (#15)

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
- **project.kind filter:** `GET /api/projects?kind=monitor` returns only monitors. Without the param, returns all (backwards compatible for scheduler, stats, etc.).
- **AssembleRequest signature:** takes `(agent, task, globalGuardrails string)` — third arg added 2026-05-30. All callers must pass it.
