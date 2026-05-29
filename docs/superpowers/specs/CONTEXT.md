# Phoenix — Active Development Context

**Last updated:** 2026-05-29  
**Purpose:** Quick-load context for a coding agent resuming work on this project. Read this file first, then `jons-todo-list`, then proceed.

---

## What Phoenix Is

A self-hosted AI agent orchestration platform. Single Go binary, SQLite, React frontend embedded via `embed.FS`. Users configure LLM or coding-agent providers, create agents with persona/instructions/guardrails, assign agents to projects, and run tasks. Full design in `docs/superpowers/specs/2026-05-28-phoenix-design.md`.

**Running instance:** `http://localhost:8080`  
**Database:** `~/.local/share/phoenix/phoenix.db`  
**Binary:** `/Users/jbowman/src/phoenix/phoenix`  
**Server process:** killed/rebuilt/restarted frequently during dev — always `pkill -f './phoenix'` then rebuild before testing

---

## Tech Stack

- **Backend:** Go, `github.com/solarisjon/phoenix`
- **Router:** `github.com/go-chi/chi/v5`
- **DB:** SQLite via `modernc.org/sqlite`, migrations embedded in `internal/store/sqlite/migrations/`
- **Frontend:** React + TypeScript + Vite + Tailwind CSS, built into `web/dist/`, embedded via `embed.FS` in `internal/frontend/`
- **Real-time:** WebSocket hub in `internal/api/hub.go`, events in `internal/api/events.go`

---

## Repository Layout (actual, not design-doc)

```
cmd/phoenix/main.go                    # entry point
internal/
  model/model.go                       # ALL domain types (single file)
  store/store.go                       # repository interfaces
  store/sqlite/                        # SQLite impls + embedded migrations
    migrations/001_initial.sql
    migrations/002_agent_model_override.sql   # model_override, dismissed
    migrations/003_agent_spawn.sql            # can_spawn_agents
  api/
    server.go                          # router, all routes registered here
    agent.go                           # CRUD + generate + spawnTask
    task.go                            # CRUD + retry + dismiss + listRunning + listAttention
    inbox.go                           # approve + reject + revise
    provider.go
    project.go
    stats.go
    hub.go / ws.go / events.go         # WebSocket
  agent/
    runner.go                          # goroutine lifecycle, task execution
    prompt.go                          # system prompt assembly (injects spawn instructions)
  provider/
    provider.go                        # Provider interface + shared types
    envexpand.go                       # ${ENV_VAR} expansion in configs
    llm/llm.go                         # HTTP LLM adapter (SSE streaming)
    opencode/opencode.go               # opencode CLI adapter
    pi/pi.go                           # pi CLI adapter (--mode json)
    claudecode/claudecode.go           # claude CLI adapter (--output-format stream-json)
    registry/registry.go               # builds Provider from DB record, GetWithOverride()
web/src/
  lib/api.ts                           # typed API client
  lib/ws.ts                            # WebSocket client
  lib/utils.ts                         # taskStatusVariant, parseOutput, formatCost, timeAgo
  pages/
    DashboardPage.tsx
    InboxPage.tsx
    ProjectDetailPage.tsx
    ProjectsPage.tsx
    AgentsPage.tsx
    ProvidersPage.tsx
```

---

## Data Model — Current State (post all migrations)

### Agent (adds beyond design doc)
- `model_override TEXT DEFAULT ''` — overrides provider's model at execution time
- `can_spawn_agents INTEGER DEFAULT 0` — if true, system prompt includes spawn API instructions

### Task (adds beyond design doc)
- `dismissed INTEGER DEFAULT 0` — soft-hide from inbox without deletion

### Provider config shape for coding agents
JSON blob with `kind` field dispatching to adapter:
```json
{ "kind": "opencode|pi|claudecode", "binary_path": "...", "model": "", "working_dir": "", "dangerously_skip_permissions": false, "extra_args": [] }
```
**Critical:** Old records (before 2026-05-29) had wrong shape (`args_template`, `working_directory`) — already fixed in DB.

---

## API Routes (complete, as of last commit)

```
GET/POST   /api/providers
GET/PUT/DELETE /api/providers/:id

GET/POST   /api/agents
POST       /api/agents/generate          # AI-generate persona/instructions/guardrails
POST       /api/agents/spawn             # agent spawns task for another agent
GET/PUT/DELETE /api/agents/:id

GET/POST   /api/projects
GET/PUT/DELETE /api/projects/:id
POST/GET   /api/projects/:id/agents
DELETE     /api/projects/:id/agents/:agentId

GET/POST   /api/tasks
GET        /api/tasks/running            # all running+queued, cross-project
GET        /api/tasks/attention          # all failed+awaiting_approval, cross-project (dismissed excluded)
GET/PUT/DELETE /api/tasks/:id
POST       /api/tasks/:id/retry         # reset failed task and rerun
POST       /api/tasks/:id/dismiss       # soft-hide from inbox

GET        /api/inbox                   # awaiting_approval only (legacy, use /tasks/attention)
POST       /api/inbox/:taskId/approve
POST       /api/inbox/:taskId/reject
POST       /api/inbox/:taskId/revise

GET        /api/stats/costs

WS         /api/ws
```

---

## Provider Kinds & Adapters

| Kind | Binary | Key flags | Stream format |
|------|--------|-----------|---------------|
| `llm` | HTTP endpoint | SSE | `data: {"choices":[...]}` |
| `opencode` | `opencode` | `run --format json` | NDJSON `{"type":"text","part":{"text":"..."}}` |
| `pi` | `pi` | `--print --mode json` | NDJSON `{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"..."}}` |
| `claudecode` | `claude` | `--print --output-format stream-json --verbose` | NDJSON `{"type":"assistant","message":{"content":[{"type":"text","text":"..."}]}}` |

Registry dispatches on `kind` field. `GetWithOverride(ctx, providerID, modelOverride)` patches `"model"` in config JSON before building — used by runner when agent has `model_override` set.

---

## Agent System Prompt

Assembled in `internal/agent/prompt.go`:
1. `## Persona` — agent.Persona
2. `## Instructions` — agent.Instructions  
3. `## Guardrails` — agent.Guardrails
4. `## Agent Spawning` — injected only if `can_spawn_agents=true`, includes `POST /api/agents/spawn` JSON template with `source_agent_id` and `project_id` pre-filled

---

## Frontend Pages

| Page | Key features |
|------|-------------|
| Dashboard | Stat cards (clickable: Running→live panel, Attention→/inbox), recent activity list (clickable→detail modal), running tasks card grid with live stream |
| Inbox | Shows `failed` + `awaiting_approval` tasks grouped by state; Retry, Approve/Revise/Reject, Edit, Dismiss, Details per card |
| Project Detail | Agent roster, task list with Retry on failed cards, live stream on running |
| Agents | CRUD, model_override field, can_spawn_agents checkbox, "✦ Generate with AI" button |
| Providers | LLM + coding agent (opencode/pi/claudecode) with per-kind fields |

---

## Build & Deploy Commands

```bash
# Backend
cd /Users/jbowman/src/phoenix
go build ./...                          # check compilation
go test ./...                           # run all tests
go build -o phoenix ./cmd/phoenix/      # build binary
pkill -f './phoenix'; nohup ./phoenix > /tmp/phoenix.log 2>&1 &  # restart

# Frontend
cd web && npm run build                 # build into web/dist/ (re-embedded on next go build)

# Database inspection
sqlite3 ~/.local/share/phoenix/phoenix.db ".tables"
sqlite3 ~/.local/share/phoenix/phoenix.db "SELECT id, name, type FROM providers;"
```

---

## TODO LIST — Priority Order

See `jons-todo-list` for the full annotated list. Below is the prioritised next-steps view:

### High priority (user-visible gaps)
1. **Project working directory** — when creating/editing a project, allow specifying a folder on disk; pass it as `working_dir` override to coding agents for that project's tasks
2. **Project deletion** — currently only archive is available; add DELETE with cascade or block if active tasks exist
3. **Heartbeat scheduling** — honour `agent.heartbeat_interval`; `internal/scheduler/` package exists but is empty; create a ticker-per-agent that fires a synthetic task on the interval
4. **Cost graphs** — dashboard currently shows just totals; add line chart (cost over time) and bar charts (by agent, by project) — Phase 6 in the design doc
5. **Model picker dropdown** — instead of free-text model field, fetch available models from the adapter (pi: `pi --list-models`, claude: list from API, opencode: from config)

### Medium priority (operational)
6. **README** — professional project documentation with setup, configuration, and usage
7. **Database backup/restore** — CLI flag or API endpoint to dump/restore the SQLite file
8. **Agent teams** — group agents into a named team, assign the whole team to a project at once
9. **Import/export agents** — JSON export/import of agent configs (and teams)

### Lower priority / design decisions needed
10. **UI theming** — colour scheme customisation
11. **Multi-user auth** — Phase 7 in design doc, single-user now

### Coding agent adapter TODOs
- **pi adapter:** existing DB records have `no_session: false` — should default `true`; update existing PI provider config in DB or add fallback in adapter
- **claudecode adapter:** needs smoke test when claude auth is configured

---

## Key Patterns to Follow

### Adding a new backend feature
1. Add field to `internal/model/model.go`
2. Write migration in `internal/store/sqlite/migrations/NNN_name.sql`
3. Update `internal/store/sqlite/*.go` (SELECT, INSERT, UPDATE, scan functions — all query strings must stay in sync)
4. Update store interface in `internal/store/store.go` if new methods needed
5. Update API handler in `internal/api/*.go`
6. Register route in `internal/api/server.go` (static routes before `{id}` params)
7. Update mock in `internal/agent/runner_test.go` if store interface changed
8. `go test ./...` — must be green before building frontend

### Adding a new coding agent adapter
1. Create `internal/provider/<name>/<name>.go` implementing `provider.Provider`
2. Add `case "<name>":` dispatch in `internal/provider/registry/registry.go`
3. Test stream parsing with NDJSON fixtures
4. Add `kind` option to `CodingAgentFields` in `web/src/pages/ProvidersPage.tsx`

### Frontend patterns
- All API calls go through `web/src/lib/api.ts` typed client
- WebSocket events subscribed via `phoenixWS.on(handler)` returns unsubscribe fn — call in `useEffect` cleanup
- `parseOutput(task.output)` extracts `text` field from task output JSON blob
- Always `npm run build` after frontend changes, then rebuild the Go binary to re-embed

---

## Gotchas Learned

- **Route ordering in chi:** static routes (`/tasks/running`, `/tasks/attention`, `/agents/generate`, `/agents/spawn`) MUST be registered before parameterised routes (`/tasks/{id}`, `/agents/{id}`) or chi routes to the param handler
- **Migration file location:** files go in `internal/store/sqlite/migrations/` (embedded via `//go:embed migrations`), NOT `migrations/` at the repo root (that directory exists but is unused)
- **Task `update` API scope:** `PUT /api/tasks/:id` was previously restricted to `pending` status only — now allows any non-running/non-queued task to have title/description edited
- **Registry caching:** `registry.Get()` caches provider instances by ID. `GetWithOverride()` bypasses cache (model override makes the instance task-specific). Call `registry.Invalidate(id)` after provider update/delete
- **Server restart:** the running binary is NOT hot-reloaded. After any Go change, kill the old process and restart. Check `ps aux | grep phoenix` and `lsof -p <pid>` to confirm which binary is running
- **Coding agent provider configs:** the `kind` field is required for dispatch. Old records created before 2026-05-29 may have wrong shape — fix with `sqlite3 ... "UPDATE providers SET config = '...' WHERE id = '...'"` 
