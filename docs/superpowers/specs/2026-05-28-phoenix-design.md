# Phoenix — Design Specification

**Date:** 2026-05-28
**Status:** Draft
**Scope:** Vertical slice — core loop (Phase 1)

---

## Overview

Phoenix is a self-hosted agent orchestration platform. Users configure AI agents with unique personas, instructions, and guardrails, then assign them to projects where they execute tasks. Agents can be backed by LLM providers or coding agent tools, communicate via direct calls, todo queues, and broadcasts, and operate on manual invocation or scheduled heartbeats.

The UI is a polished daily-driver dashboard showing agent status cards, project workspaces, an inbox for human-in-the-loop approvals, and cost tracking graphs.

## Technology Stack

- **Backend:** Go, single binary
- **Frontend:** React + shadcn/ui + Tailwind CSS, embedded via `embed.FS`
- **Database:** SQLite (abstracted behind repository interfaces for future Postgres swap)
- **Real-time:** WebSocket for live agent status, inbox updates, streaming output
- **Deployment:** Single binary, download and run

## Configuration & Data Paths

Config and data directories follow platform conventions:

**Config** (settings, provider credentials):
1. If `XDG_CONFIG_HOME` is set → `$XDG_CONFIG_HOME/phoenix/`
2. Linux/macOS default → `~/.config/phoenix/`
3. Windows → `%APPDATA%\phoenix\`

**Data** (SQLite database, logs):
1. If `XDG_DATA_HOME` is set → `$XDG_DATA_HOME/phoenix/`
2. Linux/macOS default → `~/.local/share/phoenix/`
3. Windows → `%LOCALAPPDATA%\phoenix\`

A small internal `paths` package resolves these once at startup. No hardcoded paths.

Config file: `config.yaml` (port, default provider settings, etc.)

---

## Data Model

### User
| Field    | Type   | Notes                              |
|----------|--------|------------------------------------|
| id       | UUID   | Primary key                        |
| name     | string |                                    |
| email    | string | Nullable (optional for single user)|
| settings | JSON   | User preferences                   |

Single user for now. The FK exists on other entities so multi-user is a data model change, not a rewrite.

### Provider
| Field      | Type   | Notes                                              |
|------------|--------|----------------------------------------------------|
| id         | UUID   | Primary key                                        |
| name       | string | Display name (e.g., "LLM Proxy", "Pi")             |
| type       | enum   | `llm` or `coding_agent`                            |
| config     | JSON   | Endpoint URL, auth headers, model, cost-per-token  |
| created_by | UUID   | FK → User                                          |
| created_at | datetime |                                                  |

LLM provider config includes: `endpoint`, `auth_header`, `model`, `cost_per_input_token`, `cost_per_output_token`.

Coding agent provider config includes: `binary_path`, `args_template`, `working_directory`.

### Agent
| Field              | Type     | Notes                                        |
|--------------------|----------|----------------------------------------------|
| id                 | UUID     | Primary key                                  |
| name               | string   | Display name (e.g., "Senior Ops Manager")    |
| persona            | text     | High-level personality/role description       |
| instructions       | text     | Detailed operational instructions             |
| guardrails         | text     | Constraints, boundaries, escalation rules     |
| provider_id        | UUID     | FK → Provider                                |
| heartbeat_interval | integer  | Nullable — seconds between heartbeat ticks    |
| created_by         | UUID     | FK → User                                    |
| status             | enum     | `active`, `paused`, `disabled`               |
| created_at         | datetime |                                              |

### Project
| Field       | Type     | Notes                     |
|-------------|----------|---------------------------|
| id          | UUID     | Primary key               |
| name        | string   |                           |
| description | text     |                           |
| owner       | UUID     | FK → User                 |
| status      | enum     | `active`, `archived`      |
| created_at  | datetime |                           |

### ProjectAgent (join table)
| Field      | Type | Notes        |
|------------|------|--------------|
| project_id | UUID | FK → Project |
| agent_id   | UUID | FK → Agent   |

### Task
| Field          | Type     | Notes                                                  |
|----------------|----------|--------------------------------------------------------|
| id             | UUID     | Primary key                                            |
| project_id     | UUID     | FK → Project                                           |
| agent_id       | UUID     | FK → Agent                                             |
| parent_task_id | UUID     | Nullable — links delegation chains                     |
| title          | string   |                                                        |
| description    | text     |                                                        |
| status         | enum     | `pending`, `queued`, `running`, `completed`, `failed`, `awaiting_approval` |
| input          | JSON     | Task input/context                                     |
| output         | JSON     | Agent's response/artifacts                             |
| cost_usd       | decimal  | Cost consumed by this task                             |
| created_at     | datetime |                                                        |
| started_at     | datetime | Nullable                                               |
| completed_at   | datetime | Nullable                                               |

### TodoItem (agent todo queue)
| Field           | Type     | Notes                              |
|-----------------|----------|------------------------------------|
| id              | UUID     | Primary key                        |
| target_agent_id | UUID     | FK → Agent (who receives it)       |
| source_agent_id | UUID     | Nullable — could be from user      |
| project_id      | UUID     | FK → Project                       |
| title           | string   |                                    |
| payload         | JSON     | Task details/context               |
| status          | enum     | `pending`, `picked_up`, `done`     |
| created_at      | datetime |                                    |

### Broadcast
| Field           | Type     | Notes                    |
|-----------------|----------|--------------------------|
| id              | UUID     | Primary key              |
| project_id      | UUID     | FK → Project             |
| source_agent_id | UUID     | FK → Agent               |
| message         | text     |                          |
| created_at      | datetime |                          |

### BroadcastSubscription
| Field      | Type | Notes        |
|------------|------|--------------|
| project_id | UUID | FK → Project |
| agent_id   | UUID | FK → Agent   |

---

## Provider Architecture

Two categories behind a common interface:

### Provider Interface (Go)

```go
type Message struct {
    Role    string // "user", "assistant", "system"
    Content string
}

type TaskRequest struct {
    Prompt       string
    Context      []Message  // conversation history
    SystemPrompt string     // persona + instructions + guardrails combined
    Config       map[string]interface{}
}

type TaskResponse struct {
    Output      string
    TokensIn    int
    TokensOut   int
    CostUSD     float64
    Metadata    map[string]interface{}
}

type StreamChunk struct {
    Content string
    Done    bool
    Error   error
}

type CostEstimate struct {
    EstimatedCostUSD float64
}

type Provider interface {
    Execute(ctx context.Context, request TaskRequest) (TaskResponse, error)
    StreamExecute(ctx context.Context, request TaskRequest) (<-chan StreamChunk, error)
    EstimateCost(request TaskRequest) (CostEstimate, error)
}
```

### LLM Providers
- Send prompt to a configured HTTP endpoint
- Custom endpoint config (URL, auth headers, model name)
- LLM Proxy is an instance of this — user configures the endpoint
- Additional providers (OpenAI, Anthropic, Ollama) added as presets later

### Coding Agent Providers
- Spawn local subprocess (pi, opencode, crush, copilot CLI)
- Send task via stdin or CLI args
- Capture output from stdout/files
- Each coding agent gets its own adapter implementing the Provider interface

---

## Agent Runtime

### Lifecycle
- Agents run as goroutines managed by an `AgentRunner` service
- Heartbeat agents get a goroutine with a ticker on their configured interval
- Manual invocations spawn a goroutine on demand
- All goroutines tracked and cancellable via context

### Communication Patterns

**1. Direct Call**
Agent A calls Agent B synchronously within the same task chain. A `parent_task_id` links them. Agent A blocks until Agent B completes and returns output. Functions like a synchronous function call.

**2. Todo Queue**
Agent A (or a user) pushes a TodoItem onto Agent B's queue. Agent B picks it up on its next heartbeat tick or when manually triggered. Fully async — Agent A does not wait.

**3. Broadcast**
Agent A publishes a message to a project. All agents subscribed to that project receive it as a TodoItem on their queue. Used for context changes that affect multiple agents.

### Human-in-the-Loop
- Agent sets task status to `awaiting_approval` and pauses
- Task appears in user's inbox with the agent's output/proposal
- User can: **approve** (task resumes), **reject** (task marked failed), or **revise** (user provides feedback, agent retries)

---

## API Design

RESTful JSON API with WebSocket for real-time updates.

### Routes
- `GET/POST /api/projects` — list/create projects
- `GET/PUT/DELETE /api/projects/:id` — project CRUD
- `POST /api/projects/:id/agents` — assign agent to project
- `GET/POST /api/agents` — list/create agents
- `GET/PUT/DELETE /api/agents/:id` — agent CRUD
- `GET/POST /api/tasks` — list/create tasks
- `GET/PUT /api/tasks/:id` — task detail, update (approve/reject/revise)
- `GET/POST /api/providers` — list/create providers
- `GET/PUT/DELETE /api/providers/:id` — provider CRUD
- `GET /api/inbox` — pending approval tasks, filterable by project/agent
- `POST /api/inbox/:taskId/approve` — approve task
- `POST /api/inbox/:taskId/reject` — reject task
- `POST /api/inbox/:taskId/revise` — revise with feedback
- `GET /api/stats/costs` — cost aggregations (by agent, project, time range)
- `WS /api/ws` — WebSocket for real-time updates

---

## Frontend Views

### 1. Dashboard
Overview of all active projects, agent activity summary, cost graphs (line chart over time, bar charts by agent/project), pending inbox count badge.

### 2. Inbox
List of tasks awaiting approval. Filterable by project and agent. Each item shows: agent name, task title, output preview. Actions: approve, reject, revise (with feedback text).

### 3. Project View
Project detail with: assigned agents list, task list with status indicators, task timeline/history, project cost summary.

### 4. Agent Cards
Each agent displays: name, persona summary, status (idle/running/paused), current task with live streaming output, cost consumed, heartbeat schedule if configured.

### 5. Agent Configuration
Create/edit agent: persona, instructions, guardrails, provider selection, heartbeat interval.

### 6. Provider Management
Configure LLM endpoints (URL, auth, model, cost-per-token) and coding agent paths (binary, args).

### 7. Task Detail
Full task view: input, output, delegation chain (parent/child tasks visualized), cost, timestamps.

---

## Cost Tracking

- Each task records `cost_usd`, updated as the task runs
- LLM providers: cost = (input tokens × cost_per_input_token) + (output tokens × cost_per_output_token), configurable per provider
- Coding agents: cost recorded as `0` unless the agent reports it
- Aggregation computed via queries: per agent, per project, by date range
- Dashboard graphs: cost over time (line), cost by agent (bar), cost by project (bar)

---

## Project Structure

```
phoenix/
├── cmd/phoenix/          # main.go — single entry point
├── internal/
│   ├── api/              # HTTP handlers, routes, WebSocket
│   ├── agent/            # Agent runner, lifecycle, communication
│   ├── scheduler/        # Heartbeat ticker management
│   ├── provider/         # Provider interface + adapters (llm, coding agents)
│   ├── project/          # Project & task service logic
│   ├── inbox/            # Inbox/approval service
│   ├── broadcast/        # Broadcast pub/sub logic
│   ├── store/            # Data access layer (repository interfaces)
│   │   └── sqlite/       # SQLite implementations
│   ├── queue/            # Agent todo queue logic
│   ├── paths/            # Platform-aware config/data path resolution
│   └── model/            # Shared domain types
├── migrations/           # SQL migration files
├── web/                  # React frontend source
│   ├── src/
│   ├── package.json
│   └── dist/             # Built frontend (embedded into binary)
├── docs/
├── go.mod
├── go.sum
├── Makefile              # build, dev, migrate commands
└── README.md
```

---

## Phase 1 Scope (Vertical Slice)

The initial build delivers one complete path end-to-end:

1. **Create a provider** — configure a custom LLM endpoint (LLM Proxy)
2. **Create an agent** — name, persona, instructions, guardrails, select provider
3. **Create a project** — name, description
4. **Assign agent to project**
5. **Create a task** — assign to agent within project
6. **Agent executes task** — calls LLM provider, streams output
7. **View agent card** — see live status, streaming output
8. **Approval flow** — task pauses for approval, appears in inbox, user approves/rejects/revises
9. **Cost tracking** — task records cost, visible on agent card and project view

### Not in Phase 1
- Heartbeat/scheduled agents
- Agent-to-agent communication (direct call, todo queue, broadcast)
- Coding agent providers
- Multi-user auth
- Cost graphs/charts (just raw numbers)

These are designed for but not built until subsequent phases.

---

## Future Phases (Outline)

**Phase 2:** Heartbeat scheduling, agent todo queues
**Phase 3:** Agent-to-agent direct calls, delegation chains
**Phase 4:** Broadcast communication
**Phase 5:** Coding agent providers (pi, opencode, etc.)
**Phase 6:** Cost graphs and analytics dashboard
**Phase 7:** Multi-user authentication
