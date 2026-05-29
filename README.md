# Phoenix

A self-hosted AI agent orchestration platform. Configure agents with personas, assign them to projects, and let them run tasks — backed by LLM endpoints or local coding agent tools like pi, opencode, or Claude Code.

Single binary. No cloud dependency. SQLite database.

---

## What it does

- **Agents** — give each AI agent a name, persona, instructions, and guardrails. Back them with an LLM endpoint or a local coding tool.
- **Projects** — create project workspaces with an optional working directory. Assign one or more agents to each project.
- **Tasks** — create tasks for agents within a project. Tasks run immediately, stream output live, and complete automatically.
- **Inbox** — failed tasks and tasks awaiting human approval surface here. Retry, edit, dismiss, or approve from one place.
- **Heartbeats** — agents with a heartbeat interval automatically receive a scheduled check-in task for each project they're assigned to.
- **Agent spawning** — agents can delegate work to other agents via the spawn API.
- **Cost tracking** — LLM token costs are tracked per task, per agent, per project.

---

## Quick start

### 1. Prerequisites

- Go 1.21+
- Node.js 18+ (for the frontend build)
- At least one of:
  - An LLM-compatible HTTP endpoint (OpenAI, Anthropic, LM Studio, LLM Proxy, etc.)
  - A local coding agent: [`pi`](https://github.com/earendil-works/pi), [`opencode`](https://opencode.ai), or [`claude`](https://www.anthropic.com/claude-code)

### 2. Build

```bash
git clone https://github.com/solarisjon/phoenix
cd phoenix

# Build frontend then embed into Go binary
cd web && npm install && npm run build && cd ..
go build -o phoenix ./cmd/phoenix/
```

Or with Make:

```bash
make build
```

### 3. Run

```bash
./phoenix
# Phoenix listening on http://localhost:8080
```

Open [http://localhost:8080](http://localhost:8080).

Data is stored at `~/.local/share/phoenix/phoenix.db` (Linux/macOS) or `%LOCALAPPDATA%\phoenix\phoenix.db` (Windows). The database is created automatically on first run.

Set `PHOENIX_PORT` to change the port:

```bash
PHOENIX_PORT=9000 ./phoenix
```

---

## First-time setup

### Step 1 — Add a provider

Go to **Providers** and click **+ Add Provider**.

**LLM endpoint** (e.g. LM Studio, LLM Proxy, OpenAI-compatible):

| Field | Example |
|---|---|
| Name | LLM Proxy |
| Type | LLM |
| Endpoint | `http://localhost:8080/v1/chat/completions` |
| Auth Header | `Authorization: Bearer sk-...` |
| Model | `claude-sonnet-4-5` |
| Cost per 1M input tokens | `3.00` |
| Cost per 1M output tokens | `15.00` |

**Coding agent** (pi, opencode, or Claude Code):

| Field | pi | opencode | Claude Code |
|---|---|---|---|
| Name | PI | Opencode | Claude Code |
| Type | Coding Agent | Coding Agent | Coding Agent |
| Kind | pi | opencode | claudecode |
| Binary Path | `/usr/local/bin/pi` | `/usr/local/bin/opencode` | `/usr/local/bin/claude` |

Environment variables in config fields are expanded at runtime — use `${ANTHROPIC_API_KEY}` rather than pasting secrets into the database.

### Step 2 — Create an agent

Go to **Agents** and click **+ New Agent**.

Fill in:
- **Name** — how the agent appears in the UI
- **Persona** — who the agent is (role, seniority, personality)
- **Instructions** — what the agent does and how
- **Guardrails** — what the agent must never do
- **Provider** — which LLM or coding tool powers this agent
- **Model Override** — optional, overrides the provider's default model for this specific agent

Click **✦ Generate with AI** to have an LLM draft the persona, instructions, and guardrails from a plain-English description.

Optional:
- **Heartbeat Interval** — how often (in seconds) to automatically trigger a check-in task for each project the agent is assigned to. Leave blank for manual-only.
- **Allow agent to spawn tasks for other agents** — enables the agent to delegate work via `POST /api/agents/spawn`.

### Step 3 — Create a project

Go to **Projects** and click **+ New Project**.

- **Name** and **Description** — what the project is for
- **Working Directory** — optional filesystem path passed to coding agents as their working directory (e.g. `/Users/you/my-repo`). Leave blank to use the coding agent's default.

### Step 4 — Assign agents to the project

Open the project → click **+ Assign Agent** → pick from your agents.

### Step 5 — Create a task

Inside the project, click **+ New Task**:
- **Title** — brief task name
- **Description** — full instructions for the agent
- **Agent** — which assigned agent handles this task

The task starts immediately. Watch output stream live in the task card.

---

## UI overview

| Page | What it shows |
|---|---|
| **Dashboard** | Active stats, live running tasks panel, recent activity. Click *Tasks Running* to expand the live panel; click *Needs Attention* to go to the inbox. |
| **Inbox** | Failed tasks + tasks awaiting approval. Retry, dismiss, edit, approve, or reject. |
| **Projects** | All projects. Click a project to open its workspace. |
| **Project detail** | Assigned agents, task list with live streaming, cost summary. |
| **Agents** | All agents with status and heartbeat indicator. Create, edit, delete. |
| **Providers** | All configured LLM endpoints and coding agent tools. |

---

## Provider types

### LLM providers

Calls any OpenAI-compatible chat completions endpoint. Streams response via SSE. Calculates cost from token counts and configured per-token rates.

Config fields:
```json
{
  "endpoint": "https://api.openai.com/v1/chat/completions",
  "auth_header": "Authorization: Bearer ${OPENAI_API_KEY}",
  "model": "gpt-4o",
  "cost_per_input_token": 0.0000025,
  "cost_per_output_token": 0.00001
}
```

### Coding agent providers

Spawns a local subprocess and streams its output. The agent's system prompt (persona + instructions + guardrails) and the task description are passed as arguments. Output is streamed in real time.

Supported kinds:

| Kind | Binary | Notes |
|---|---|---|
| `pi` | `pi` | Runs `pi --print --mode json`. Streams `text_delta` events. |
| `opencode` | `opencode` | Runs `opencode run --format json`. Streams JSON text parts. |
| `claudecode` | `claude` | Runs `claude --print --output-format stream-json --verbose`. |

The project's **Working Directory** overrides the provider-level `working_dir` when set, so you can have one coding agent provider serve multiple projects in different directories.

---

## Agent system prompt

Each agent's system prompt is assembled from:

1. **Persona** — who the agent is
2. **Instructions** — operational detail
3. **Guardrails** — constraints and escalation rules
4. **Spawn instructions** — injected automatically if *Allow agent to spawn tasks* is enabled

---

## Heartbeat scheduling

If an agent has a **Heartbeat Interval** set:

- Phoenix automatically creates a `Heartbeat — YYYY-MM-DD HH:MM` task for that agent in every project it's assigned to, on the configured interval.
- If the agent already has a running or queued task in a project, the heartbeat is skipped for that cycle.
- Agent/project assignments are re-scanned every 60 seconds, so new assignments take effect within a minute.
- Setting the interval to blank (null) stops future heartbeats.

---

## Agent spawning

An agent with **Allow agent to spawn tasks** enabled receives spawn instructions in its system prompt. It can create tasks for other agents by calling:

```http
POST /api/agents/spawn
{
  "source_agent_id": "<this agent's ID>",
  "target_agent_id": "<target agent's ID>",
  "project_id": "<project ID>",
  "title": "Task title",
  "description": "What the target agent should do"
}
```

The source agent's ID and project ID are injected into its system prompt automatically — the agent only needs to know the target agent's ID.

---

## API reference

All endpoints are under `/api/`.

### Providers
| Method | Path | Description |
|---|---|---|
| `GET` | `/api/providers` | List all providers |
| `POST` | `/api/providers` | Create a provider |
| `GET` | `/api/providers/:id` | Get a provider |
| `PUT` | `/api/providers/:id` | Update a provider |
| `DELETE` | `/api/providers/:id` | Delete a provider |

### Agents
| Method | Path | Description |
|---|---|---|
| `GET` | `/api/agents` | List all agents |
| `POST` | `/api/agents` | Create an agent |
| `GET` | `/api/agents/:id` | Get an agent |
| `PUT` | `/api/agents/:id` | Update an agent |
| `DELETE` | `/api/agents/:id` | Delete an agent |
| `POST` | `/api/agents/generate` | AI-generate persona/instructions/guardrails from a description |
| `POST` | `/api/agents/spawn` | Create a task on behalf of an agent (requires `can_spawn_agents`) |

### Projects
| Method | Path | Description |
|---|---|---|
| `GET` | `/api/projects` | List all projects |
| `POST` | `/api/projects` | Create a project |
| `GET` | `/api/projects/:id` | Get a project |
| `PUT` | `/api/projects/:id` | Update a project |
| `DELETE` | `/api/projects/:id` | Delete a project (blocked if tasks are running) |
| `GET` | `/api/projects/:id/agents` | List agents assigned to a project |
| `POST` | `/api/projects/:id/agents` | Assign an agent to a project |
| `DELETE` | `/api/projects/:id/agents/:agentId` | Remove an agent from a project |

### Tasks
| Method | Path | Description |
|---|---|---|
| `GET` | `/api/tasks?project_id=` | List tasks for a project |
| `POST` | `/api/tasks` | Create and immediately run a task |
| `GET` | `/api/tasks/:id` | Get a task |
| `PUT` | `/api/tasks/:id` | Edit task title/description (non-running tasks only) |
| `DELETE` | `/api/tasks/:id` | Delete a task |
| `POST` | `/api/tasks/:id/retry` | Reset a failed task and re-run it |
| `POST` | `/api/tasks/:id/dismiss` | Soft-hide a task from the inbox |
| `GET` | `/api/tasks/running` | All running + queued tasks across all projects |
| `GET` | `/api/tasks/attention` | All failed + awaiting-approval tasks (undismissed) |

### Inbox / Approval
| Method | Path | Description |
|---|---|---|
| `GET` | `/api/inbox` | Tasks awaiting approval |
| `POST` | `/api/inbox/:taskId/approve` | Approve a task (resumes execution) |
| `POST` | `/api/inbox/:taskId/reject` | Reject a task (marks failed) |
| `POST` | `/api/inbox/:taskId/revise` | Send feedback and re-run |

### Stats
| Method | Path | Description |
|---|---|---|
| `GET` | `/api/stats/costs` | Cost totals by agent and by project |

### Real-time
| | Path | Description |
|---|---|---|
| `WS` | `/api/ws` | WebSocket — receives `task.status_changed`, `task.output_stream`, `agent.status_changed` events |

---

## Data locations

| Platform | Config | Database |
|---|---|---|
| Linux/macOS | `~/.config/phoenix/` | `~/.local/share/phoenix/phoenix.db` |
| macOS (XDG override) | `$XDG_CONFIG_HOME/phoenix/` | `$XDG_DATA_HOME/phoenix/phoenix.db` |
| Windows | `%APPDATA%\phoenix\` | `%LOCALAPPDATA%\phoenix\phoenix.db` |

### Backup

The database is a single SQLite file. Back it up with:

```bash
cp ~/.local/share/phoenix/phoenix.db ~/.local/share/phoenix/phoenix.db.bak
```

Or snapshot it while running (SQLite WAL mode is safe for online copies):

```bash
sqlite3 ~/.local/share/phoenix/phoenix.db ".backup backup.db"
```

---

## Development

```bash
# Run tests
make test
# or
go test ./...

# Frontend dev server (hot reload at :5173, proxies API to :8080)
make dev-web

# Backend with hot reload (requires `air`)
make dev-go

# Full production build
make build
```

Project structure:

```
cmd/phoenix/          — entry point
internal/
  api/                — HTTP handlers, WebSocket hub
  agent/              — task runner, prompt assembly
  scheduler/          — heartbeat ticker management
  provider/           — Provider interface + adapters
    llm/              — OpenAI-compatible HTTP adapter
    opencode/         — opencode CLI adapter
    pi/               — pi CLI adapter
    claudecode/       — Claude Code CLI adapter
    registry/         — builds Provider instances from DB records
  store/              — repository interfaces
    sqlite/           — SQLite implementations + embedded migrations
  model/              — shared domain types
  paths/              — XDG/platform-aware data directory resolution
  frontend/           — embedded React dist
web/                  — React + TypeScript + Vite + Tailwind frontend
docs/                 — design specs and implementation notes
```

---

## Roadmap

- [ ] Agent teams — group agents, assign a whole team to a project at once
- [ ] Cost graphs — spending over time, by agent, by project
- [ ] Import/export agents — share agent configurations as JSON
- [ ] Database backup endpoint — `GET /api/admin/backup` streams the SQLite file
- [ ] UI theming
- [ ] Multi-user authentication

---

## License

MIT
