# Phoenix — Active Development Context

**Last updated:** 2026-06-27 (v0.7 in progress)  
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
      033_plugins.sql                  # plugins + notification_rules tables, master switch settings
      034_project_objective.sql        # projects.objective TEXT DEFAULT ''
      035_context_summarisation.sql    # tasks.summary_cache for follow-up chain summarisation
      036_plugin_memory_type.sql       # rebuild plugins table to allow type='memory'
      037_obsidian_vaults.sql          # obsidian_vaults table + obsidian_root/auto_write settings
      038_memo_artifact_path.sql       # memos.artifact_path for .md inline viewing
      039_project_paused_status.sql    # projects.status adds 'paused' value for monitor pause/resume
      040_agents_projects_fts.sql      # FTS5 tables for agents and projects (was tasks-only)
      041_provider_health.sql          # providers: health_status, health_latency_ms, health_error, health_checked_at
      042_task_templates.sql           # task_templates table (id, name, description, title, body, project_id, agent_id)
      043_task_priority.sql            # tasks.priority INTEGER DEFAULT 0 + idx_tasks_priority (priority DESC, created_at ASC)
    agent.go, task.go, project.go, team.go, agent_draft.go, stats.go, admin.go, sqlite.go
    task_template.go                   # TaskTemplateRepo: List/Get/Create/Delete
    system_settings.go                 # SystemSettingsRepo: Get/Save global guardrails + Obsidian settings
    plugin.go, notification_rule.go    # PluginRepo + NotificationRuleRepo
    obsidian.go                        # ObsidianVaultRepo: CRUD for vault configurations
  api/
    server.go                          # router — ALL routes registered here
    agent.go                           # CRUD + generate + spawnTask + import/export + clearMemory
    agent_draft.go                     # CRUD + approve + reject + dismiss
    task.go                            # CRUD + retry + bumpTask + cancel + forceReset + dismiss + undismiss + followup + quick + listRunning + listAttention + estimate + search
    task_template.go                   # listTaskTemplates + createTaskTemplate + deleteTaskTemplate
    inbox.go                           # approve + reject + revise + dismissAll
    settings.go                        # GET/PUT /admin/settings + generate-guardrails + restore + sysinfo + reset
    memo.go                            # CRUD + getMemoFileContent (serves .md artifacts inline)
    obsidian.go                        # vault CRUD + discover + generateContext + write (manual trigger)
    provider.go                        # CRUD + models + pricing + resync + test + health (healthProvider)
    project.go, team.go, stats.go, admin.go
    plugin.go                          # CRUD + enable/disable + test + schema + chats (Telegram) + rules + /api/themes
    hub.go / ws.go / events.go         # WebSocket
  healthcheck/
    checker.go                         # background provider health-check goroutine; pings each provider on interval
  provider/
    check.go                           # CheckCodingAgentBinary: verify binary exists on PATH before spawning
  agent/
    runner.go                          # goroutine lifecycle, task execution, PID tracking, timeout, Obsidian auto-write
    prompt.go                          # system prompt assembly; guardrails; hiring; InjectObsidianVaults()
    artifacts.go                       # ParseArtifactBlocks: file|url|jira|confluence|html|obsidian types
    runner_extract.go                  # extractAndSaveMemos, extractAndSaveArtifacts, maybeAutoWriteObsidian
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
    ui/theme-picker.tsx                # theme switcher; fetches /api/themes, injects community theme CSS
    edit-retry-modal.tsx               # pre-fills failed task title+description for edit-before-retry; creates follow-up task
  pages/
    DashboardPage.tsx, InboxPage.tsx, TasksPage.tsx
    BriefingPage.tsx                   # memos list; MdFileViewer renders .md artifact_path inline
    PluginsPage.tsx                    # plugin management: notifiers (Telegram/Webhook), custom themes, memory plugins
    ProjectsWorkspace.tsx              # THREE-PANE workspace: project list | task view | task detail/compose
                                       # replaces old ProjectsPage.tsx + ProjectDetailPage.tsx
                                       # includes: ProjectListItem, MiddlePane (status-grouped sections,
                                       #   inline objective editor, AI suggest card), TaskRow, TaskDetailView,
                                       #   TaskComposeView (task templates picker), FileRow, FilePreviewView, AgentPickerModal
    MonitorsPage.tsx                   # lists kind=monitor; flat dark cards with health-signal dots; pause button
    MonitorDetailPage.tsx              # run log, countdown, run-now; RunCard with left-border status color
    AgentActivityPage.tsx              # per-agent task history / activity log
    AgentsPage.tsx, ProvidersPage.tsx  # ProvidersPage shows health dot + Test button per provider
    SettingsPage.tsx                   # System tab: Global Guardrails + DB backup + configurable server URL; Obsidian tab: vault manager + auto-write toggle
    TeamsPage.tsx, TeamDetailPage.tsx
```

---

## Data Model — Current State (migrations 001–043)

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
last_error TEXT DEFAULT '',          -- last error message (migration 025)
is_critic_review INTEGER DEFAULT 0,  -- critic tasks flagged to prevent critic loops (migration 019)
reviewed_task_id TEXT,               -- FK to original task this critic reviewed (migration 019)
critic_mode TEXT DEFAULT 'inherit',  -- inherit|none|builtin|agent:<id> (migration 024)
prompt_hash TEXT DEFAULT '',         -- dedup key: SHA256 of (project_id+agent_id+prompt) (migration 027)
summary_cache TEXT DEFAULT '',       -- cached chain summary for follow-up context (migration 035)
priority INTEGER NOT NULL DEFAULT 0, -- scheduler ordering: higher runs first; ties → created_at ASC (migration 043)
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
objective TEXT NOT NULL DEFAULT '',  -- migration 034: plain-English goal statement
working_dir TEXT DEFAULT '',
kind TEXT DEFAULT 'project' CHECK(kind IN ('project','monitor')),  -- migration 011
schedule_interval INTEGER,           -- seconds between monitor runs (migration 015); nil=no schedule
schedule_kind TEXT NOT NULL DEFAULT 'interval',  -- migration 026: 'interval' | 'daily'
schedule_times TEXT NOT NULL DEFAULT '[]',       -- migration 026: JSON array of "HH:MM" local times (daily only)
schedule_catch_up INTEGER NOT NULL DEFAULT 0,    -- migration 026: daily only; run a missed time once at next opportunity same day
critic_agent_id TEXT,                -- deprecated; use critic_mode (migration 019)
critic_mode TEXT DEFAULT 'none',     -- none|builtin|agent:<id> (migration 024)
owner,
status TEXT DEFAULT 'active' CHECK(status IN ('active','archived','paused')), -- migration 039 adds 'paused'
created_at,
tags TEXT NOT NULL DEFAULT '[]'      -- migration 023: JSON array of tag strings
```

### task_templates (migration 042)
```sql
id, name, description TEXT DEFAULT '', title, body TEXT DEFAULT '',
project_id TEXT REFERENCES projects(id) ON DELETE CASCADE,  -- NULL = global
agent_id   TEXT REFERENCES agents(id)   ON DELETE SET NULL, -- NULL = any agent
created_at
```

### providers (added columns, migrations 041)
```sql
-- existing: id, name, type, kind, config, created_at
health_status     TEXT NOT NULL DEFAULT 'unknown',  -- 'unknown'|'healthy'|'degraded'|'unreachable'
health_latency_ms INTEGER,
health_error      TEXT NOT NULL DEFAULT '',
health_checked_at DATETIME
```

### plugins (migrations 033, 036)
```sql
id, name, type TEXT,   -- 'notifier' | 'theme' | 'memory'
kind TEXT,             -- 'telegram' | 'webhook' | 'custom' (themes) | 'hindsight' (memory)
config TEXT,           -- JSON blob (provider-specific)
enabled INTEGER DEFAULT 1,
created_at
```

### notification_rules (migration 033)
```sql
id, plugin_id, event TEXT,  -- task.completed | task.failed | task.awaiting_approval | etc.
enabled INTEGER DEFAULT 1,
created_at
```

### memos (migration 022, 038)
```sql
id, project_id, project_name, task_id, agent_id, agent_name,
title, body,
artifact_path TEXT DEFAULT '',  -- migration 038: absolute path to a .md file artifact, if any
priority TEXT DEFAULT 'normal' CHECK(priority IN ('normal','high')),
status TEXT DEFAULT 'unread' CHECK(status IN ('unread','read','flagged','archived')),
created_at
```

### obsidian_vaults (migration 037)
```sql
id, name, path, context TEXT DEFAULT '',
enabled INTEGER DEFAULT 1,
sort_order INTEGER DEFAULT 0,
created_at
```

### system_settings (migration 009)
```sql
key TEXT PRIMARY KEY, value TEXT, updated_at DATETIME
-- rows: global_guardrails_enabled ('0'/'1'), global_guardrails (text),
--       core_plugins_enabled ('0'/'1'), community_plugins_enabled ('0'/'1'),
--       obsidian_root (path string), obsidian_auto_write ('0'/'1')  -- migration 037
```

### teams + team_agents + project_agents (from migration 005)

---

## API Routes (complete)

```
GET/POST           /api/providers
GET/PUT/DELETE     /api/providers/:id
GET                /api/providers/:id/models

GET/POST           /api/providers
GET/PUT/DELETE     /api/providers/:id
GET                /api/providers/:id/models
PUT                /api/providers/:id/pricing
POST               /api/providers/:id/resync      # clear cached adapter (hot-reload config)
POST               /api/providers/:id/test        # on-demand health check
GET                /api/providers/:id/health      # latest cached health status

GET/POST           /api/agents
POST               /api/agents/generate
POST               /api/agents/import             # import agent JSON bundle
POST               /api/agents/spawn              # body: source field optional
GET                /api/agents/:id
PUT/DELETE         /api/agents/:id
GET                /api/agents/:id/tasks          # per-agent task history
GET                /api/agents/:id/export         # export agent JSON
DELETE             /api/agents/:id/memory         # clear agent memory (Hindsight plugin)

GET/POST           /api/agent-drafts
PUT                /api/agent-drafts/:id
POST               /api/agent-drafts/:id/approve
POST               /api/agent-drafts/:id/reject
POST               /api/agent-drafts/:id/dismiss

GET/POST           /api/projects                  # GET: ?kind=project|monitor&status=active|archived|paused
GET/PUT/DELETE     /api/projects/:id
POST               /api/projects/:id/archive      # sets status=archived; blocks if tasks running
POST               /api/projects/:id/restore      # sets status=active
POST               /api/projects/:id/pause        # sets status=paused (monitors only)
GET                /api/projects/summaries        # task health summary keyed by project ID
POST               /api/projects/generate-description
GET/POST           /api/projects/:id/agents
DELETE             /api/projects/:id/agents/:agentId
GET                /api/projects/:id/agents       # list assigned agents
POST               /api/projects/:id/teams
GET                /api/projects/:id/files        # list files in working_dir (depth ≤3, no hidden)
GET                /api/projects/:id/files/*      # get file content (≤256KB)
GET                /api/projects/:id/history      # all completed tasks regardless of dismissed state
GET                /api/projects/:id/summary      # single project summary (used internally)
GET                /api/projects/:id/spend        # per-project cost breakdown
POST               /api/projects/:id/suggest      # AI next-action suggestions

GET/POST           /api/task-templates            # GET: ?project_id= (null=global+project)
DELETE             /api/task-templates/:id

GET/POST           /api/tasks                     # ?project_id= filter
POST               /api/tasks/quick               # sandbox project
GET                /api/tasks/running
GET                /api/tasks/attention
GET                /api/tasks/search              # FTS5 across tasks
POST               /api/tasks/estimate            # pre-run token/cost estimate
POST               /api/tasks/generate-description
GET/PUT/DELETE     /api/tasks/:id
POST               /api/tasks/:id/retry
POST               /api/tasks/:id/bump            # increase priority (higher = runs sooner)
POST               /api/tasks/:id/cancel          # cancel queued task
POST               /api/tasks/:id/force-reset     # force-fail running/stuck task
POST               /api/tasks/:id/dismiss
POST               /api/tasks/:id/undismiss
POST               /api/tasks/:id/followup
POST               /api/tasks/:id/obsidian-write  # manually save task to Obsidian vault

GET                /api/inbox
POST               /api/inbox/dismiss-all         # ?filter=failed|awaiting|completed|all
POST               /api/inbox/:taskId/approve
POST               /api/inbox/:taskId/reject
POST               /api/inbox/:taskId/revise

GET/POST           /api/teams
POST               /api/teams/generate-description
GET/PUT/DELETE     /api/teams/:id
GET/POST           /api/teams/:id/agents
DELETE             /api/teams/:id/agents/:agentId
POST               /api/teams/:id/broadcast
GET                /api/teams/:id/export
POST               /api/import/team

GET                /api/memos                     # ?status=unread|read|flagged|archived
POST               /api/memos
GET                /api/memos/count               # unread+flagged count (sidebar badge)
GET                /api/memos/file-content        # ?path=<abs_path> — serve .md artifact inline
PUT                /api/memos/:id/status
DELETE             /api/memos/:id

GET/POST           /api/obsidian/vaults           # requireObsidianEnabled middleware
GET/PUT/DELETE     /api/obsidian/vaults/:id
GET                /api/obsidian/discover         # scan obsidian_root for vault directories
POST               /api/obsidian/generate-context # AI-generate vault context description

GET/POST           /api/plugins
GET/PUT/DELETE     /api/plugins/:id
POST               /api/plugins/:id/enable
POST               /api/plugins/:id/disable
POST               /api/plugins/:id/test
GET                /api/plugins/:id/schema
GET                /api/plugins/:id/chats         # Telegram: discover chat IDs via getUpdates
GET/POST           /api/plugins/:id/rules
PUT/DELETE         /api/plugins/:id/rules/:rid
GET                /api/themes                    # enabled community themes

GET                /api/search                    # global FTS across tasks, agents, projects
GET                /api/stats/costs
GET                /api/stats/costs/insights      # trend analysis + anomaly flags

GET                /api/fs/stat                   # ?path= — stat a filesystem path
POST               /api/fs/mkdir                  # {path} — create directory

GET                /api/admin/backup              # stream VACUUM INTO snapshot
POST               /api/admin/restore             # restore from uploaded snapshot
GET                /api/admin/sysinfo             # OS/Go/disk/memory stats
GET/PUT            /api/admin/settings            # global guardrails + plugin master switches + server URL
POST               /api/admin/settings/generate-guardrails
POST               /api/admin/reset               # DANGER: factory reset (wipe all data)

WS                 /api/ws
```

**Route ordering:** static routes MUST be before `{id}` params in chi. `/tasks/quick`, `/tasks/running`, `/tasks/attention`, `/tasks/search`, `/tasks/estimate`, `/agents/generate`, `/agents/spawn`, `/agents/import`, `/inbox/dismiss-all`, `/projects/summaries`, `/projects/generate-description`, `/memos/count`, `/memos/file-content` all registered before `/:id` params.

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
- Migrations applied: 001–043

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

Recently completed (2026-06-22 — v0.4):
- ✓ Plugin system — core/community architecture, Telegram + Webhook notifiers, notification rules engine
- ✓ Custom themes — visual editor (grouped colour pickers, live preview), community themes in sidebar picker, instant apply
- ✓ Dashboard: active task cards clickable — opens live-streaming detail modal with WS output feed
- ✓ Project view redesign — projects.objective field (migration 034), inline-editable in header
- ✓ Status-grouped task sections (Needs Attention → Running → Failed → Completed) with collapsible completed
- ✓ Full project history — GET /api/projects/:id/history returns completed tasks regardless of dismissed state; dismiss only affects Inbox, not project
- ✓ AI next-action suggestion — POST /api/projects/:id/suggest; inline suggestion card with one-click Run

Recently completed (2026-06-26 — v0.5):
- ✓ Obsidian vault integration — vault CRUD, auto-write post-task, vault routing in system prompts, Settings → Obsidian tab, TaskThreadCard "Save to Obsidian" button (#70)
- ✓ Briefing: clickable link to view .md artifact files inline — MdFileViewer in BriefingPage, artifact_path on memos, GET /api/memos/file-content endpoint (#71)

Recently completed (2026-06-27 — v0.6):
- ✓ Themes: 15 built-in themes (8 dark, 7 light); DB-persisted selection; theme-aware status badges (#63, #59, #45, #56-58)
- ✓ Webhook HMAC-SHA256 signing — X-Phoenix-Signature header on each POST (#50)
- ✓ Telegram /status command — reply with recent task statuses without opening UI (#44)
- ✓ FTS5 extended to agents and projects (was tasks-only) — GET /api/search global; GET /api/tasks/search per-tasks (#48)
- ✓ Monitor pause/resume — POST /api/projects/:id/pause sets status=paused; resume via restore (#45)
- ✓ Agent assignment guard — scheduler skips monitors whose agent already has a running/queued task (#43)
- ✓ Dedup scheduler — prompt_hash dedup prevents re-queuing same prompt in same project (#41)
- ✓ Configurable server URL — stored in system_settings, injected into spawn/hire agent prompts (#40)

In progress (v0.7):
- ◑ Provider health checks (#62) — background checker in internal/healthcheck/, GET /api/providers/:id/health, POST /api/providers/:id/test, status dot on provider cards
- ◑ Task templates (#47) — migration 042, GET/POST/DELETE /api/task-templates, template picker in compose panel, EditRetryModal for retry-with-edit
- ◑ Task priority / bump (#61) — migration 043, tasks.priority, POST /api/tasks/:id/bump, scheduler orders by priority DESC, created_at ASC
- ◑ Retry with edit (#46) — EditRetryModal component (web/src/components/edit-retry-modal.tsx); creates follow-up task with edited prompt

Open backlog — https://github.com/solarisjon/phoenix/issues:
1. **#13** Mobile-friendly layout (sidebar collapses to bottom nav)
2. **#19** Keyboard shortcuts (R=retry, D=dismiss, J/K navigate)
3. **#12** Multi-user authentication (Phase 7 — large)
4. **#14** claudecode smoke test (BLOCKED: needs claude auth)
5. **#7**  Copilot adapter (BLOCKED: needs copilot login)
6. **#51** Discord notifier plugin
7. **#52** Email (SMTP) notifier plugin
8. **#53** GitHub Issues plugin — bidirectional
9. **#66** Task dependency chains

---

## Gotchas

- **Route ordering in chi:** static routes BEFORE parameterised routes or chi matches wrong handler. `/inbox/dismiss-all` must be before `/inbox/:taskId`. `/memos/count` and `/memos/file-content` must be before `/memos/:id`.
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
- **Obsidian auto-write:** `maybeAutoWriteObsidian()` fires in a goroutine after every non-critic task completion when `obsidian_auto_write=1` and at least one enabled vault exists. Uses the task's provider to generate the note. Errors logged but never surface to the user.
- **Obsidian artifact type:** agents use `Type: obsidian` in ARTIFACT blocks (not `Type: file`) to declare a vault write. `ParsedArtifact.Vault` carries the vault name. `extractAndSaveArtifacts()` in `runner_extract.go` handles directory creation for obsidian type.
- **artifact_path on memos:** only set for `Type: file` artifacts whose path ends in `.md`. Other file types and all non-file artifact types leave `artifact_path` empty. The `GET /api/memos/file-content` endpoint only serves absolute paths with `.md` extension — rejects all others with 400.
- **plugins.type 'memory':** migration 036 rebuilds the plugins table to allow the 'memory' type (SQLite cannot ALTER CHECK constraints). Safe to run on existing DB — data is copied.
- **taskSelectCols priority column:** `priority` was added to `taskSelectCols` const in `task.go` (migration 043). If you add a new task column, add it to `taskSelectCols` AND update `scanTask` + `scanTasks` — mismatched column count causes a scan panic at runtime.
- **SetPriority in test fakes:** any struct implementing `store.TaskRepo` (test fakes like `memTaskRepo`, `fakeTaskRepo`) must implement `SetPriority(ctx, taskID, priority) error`. Add a no-op stub if the test doesn't need real priority.
- **task_templates project_id NULL semantics:** `project_id = NULL` means global (available everywhere). `project_id = X` means scoped to that project only. `GET /api/task-templates?project_id=X` returns global + project-specific templates. No `project_id` param returns only global.
- **Provider health checker:** `internal/healthcheck/checker.go` starts in a goroutine via `healthchecker.Start(sigCtx)` in `main.go`. It calls `GET /api/providers` internally and pings each provider. For coding_agent providers it calls `CheckCodingAgentBinary()` from `internal/provider/check.go` (binary PATH check only, no subprocess). For llm/ollama it makes an HTTP request. Results written via `providerRepo.UpdateHealth()`.
- **Monitor pause status:** `projects.status` now has three values: `active`, `archived`, `paused`. Paused monitors appear in the monitors list but fire no scheduler ticks. The scheduler `shouldRun()` checks `status == active`. `POST /api/projects/:id/pause` sets status=paused; `POST /api/projects/:id/restore` sets it back to active.
- **Retry-with-edit is a follow-up:** `EditRetryModal` does NOT call `/retry` — it calls `/api/tasks` (create) with `follow_up_of: task.id`. This keeps the original task intact and chains the re-run as a follow-up. The UI shows it in the same thread.
- **Configurable server URL:** stored in system_settings as key `server_url`. Empty = use `http://localhost:8080`. Injected into agent spawn/hire instructions by `prompt.go`. Agents use this URL to call back to the API — set it when running Phoenix behind a reverse proxy or non-default port.
