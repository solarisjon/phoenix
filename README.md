# Phoenix

A self-hosted AI agent orchestration platform. Give agents personas, assign them to projects, run tasks — backed by local coding tools or any LLM endpoint.

**Single binary. SQLite. No cloud dependency.**

---

## What it does

| Feature | Description |
|---|---|
| **Agents** | Persona + instructions + guardrails. Backed by an LLM or local coding tool. |
| **Projects** | Workspaces with optional filesystem working directory. |
| **Tasks** | Run immediately, stream output live, track cost. |
| **Follow-up threads** | Chat-style refinement on any completed task — output carried forward as context. |
| **Quick Tasks** | One-off tasks without a project (⌘K from anywhere). |
| **Inbox** | Failed tasks, awaiting-approval tasks, and pending agent hire proposals in one place. |
| **Heartbeats** | Agents with an interval auto-trigger scheduled check-in tasks per project. |
| **Agent spawning** | Agents delegate work to other agents via the API. |
| **Agent hiring** | Agents propose new hires → land in Inbox for human approval before any agent is created. |
| **Teams** | Group agents into named teams; assign a whole team to a project at once. |
| **Cost tracking** | Token costs tracked per task, per agent, per project. Area charts + bar charts on dashboard. |
| **Themes** | 5 built-in themes (Dark, Midnight, Forest, Ember, Light), live switcher. |
| **DB backup** | `GET /api/admin/backup` streams a consistent SQLite snapshot safe during live operation. |

---

## Quick start

### Prerequisites

- Go 1.21+
- Node.js 18+
- At least one provider:
  - An LLM-compatible HTTP endpoint (OpenAI, Anthropic, LM Studio, LLM Proxy…)
  - **Ollama** for local models (qwen3, llama3, mistral, etc.)
  - A local coding agent: [`pi`](https://github.com/earendil-works/pi), [`opencode`](https://opencode.ai), [`claude`](https://www.anthropic.com/claude-code), or [`crush`](https://github.com/charmbracelet/crush)

### Build

```bash
git clone https://github.com/solarisjon/phoenix
cd phoenix
cd web && npm install && npm run build && cd ..
go build -o phoenix ./cmd/phoenix/
```

Or:

```bash
make build
```

### Run

```bash
./phoenix
# → http://localhost:8080
```

Data lives at `~/.local/share/phoenix/phoenix.db`. Created automatically on first run.

```bash
PHOENIX_PORT=9000 ./phoenix   # custom port
```

---

## Providers

### Ollama (local models)

Run models locally with [Ollama](https://ollama.com). No API key needed.

1. Install Ollama and pull a model: `ollama pull qwen3.5:latest`
2. Settings → Providers → Add Provider → **🧠 Ollama (local models)**
3. Set model name (e.g. `qwen3.5:latest`, `llama3.2:3b`, `mistral:7b`)

Thinking tokens (chain-of-thought from qwen3, deepseek-r1) are suppressed by default for clean output. Enable *Show thinking tokens* if you want to see the reasoning.

### LLM endpoints

Any OpenAI-compatible chat completions endpoint:

```json
{
  "endpoint": "https://api.openai.com/v1/chat/completions",
  "auth_header": "Authorization: Bearer ${OPENAI_API_KEY}",
  "model": "gpt-4o",
  "cost_per_input_token": 0.0000025,
  "cost_per_output_token": 0.00001
}
```

Use `${ENV_VAR}` for secrets — they're expanded at runtime, never stored in plaintext.

### Coding agents

Spawn a local subprocess. The agent's system prompt and task description are passed to the tool, output streamed live.

| Kind | Binary | How it runs |
|---|---|---|
| `pi` | `pi` | `pi --print --mode json` via stdin |
| `opencode` | `opencode` | `opencode run --format json` |
| `claudecode` | `claude` | `claude --print --output-format stream-json --verbose` |
| `crush` | `crush` | `crush run --quiet` via stdin; system prompt via `AGENTS.md` |

The project **Working Directory** overrides the provider-level `working_dir`, so one coding agent provider can serve multiple projects in different repos.

---

## Agents

### Creating an agent

Settings → Agents → **+ New Agent**

- **Name** — display name
- **Persona** — who the agent is (role, seniority, style)
- **Instructions** — what the agent does and how
- **Guardrails** — hard constraints and escalation rules
- **Provider** — which LLM or coding tool powers this agent
- **Model Override** — overrides the provider's default model for this agent only

Click **✦ Generate with AI** to draft persona/instructions/guardrails from a plain-English description.

### Heartbeats

Set a **Heartbeat Interval** (minimum 60s) and the agent automatically receives a scheduled check-in task for each project it's assigned to. If the agent already has a running or queued task, the heartbeat is skipped for that cycle.

### Agent spawning

Enable **Allow agent to spawn tasks for other agents**. The agent's system prompt gains instructions to call `POST /api/agents/spawn`, creating tasks for any other agent by ID.

### Agent hiring

Enable **Allow agent to hire new agents 🧑‍💼**. When the agent identifies a capability gap during a task, it can propose a new hire via `POST /api/agent-drafts`. The proposal lands in the **Inbox** for human review — name, persona, instructions, guardrails, and provider are all editable before approval. Approval creates a live agent; rejection dismisses the proposal. No agent is ever created without human sign-off.

---

## Projects

Projects have an optional **Working Directory** passed to coding agents. Two modes are detected automatically:

- **Autonomous mode** — one or more assigned agents have a heartbeat interval. Shows an activity scoreboard, last session summary, and attention panel.
- **Human-driven mode** — no heartbeat agents. Shows task thread cards with inline follow-up reply input.

### Follow-up threads

On any completed or failed task, type a follow-up message to refine the output. The previous output is automatically injected as context. Follow-ups chain indefinitely, forming a conversation thread. Available from every task detail modal across Dashboard, Tasks, Project, and Inbox pages.

---

## Teams

Group agents into named teams. Assign a whole team to a project in one click. Export a team as a bundle (agents + provider templates, no secrets) and import it on another Phoenix instance via the 3-step import wizard.

---

## Quick Tasks

Press **⌘K** (or click the ✦ button) from anywhere to run a one-off task without creating a project first. Quick Tasks run in an internal sandbox project and appear in the Tasks page.

---

## Inbox

Three sections, highest priority first:

1. **Pending Hires** (purple) — agent hire proposals awaiting approval. Edit, approve with provider selection, or reject.
2. **Awaiting Approval** (amber) — tasks where an agent requested human sign-off. Approve, revise with feedback, or reject.
3. **Failed** (red) — tasks that errored. Retry, edit, or dismiss.

The Inbox badge on the sidebar counts all three categories in real time.

---

## API reference

### Providers
| Method | Path | |
|---|---|---|
| GET | `/api/providers` | List all |
| POST | `/api/providers` | Create |
| GET/PUT/DELETE | `/api/providers/:id` | Read / update / delete |

### Agents
| Method | Path | |
|---|---|---|
| GET | `/api/agents` | List all |
| POST | `/api/agents` | Create |
| GET/PUT/DELETE | `/api/agents/:id` | Read / update / delete |
| POST | `/api/agents/generate` | AI-generate persona/instructions/guardrails |
| POST | `/api/agents/spawn` | Create a task on behalf of an agent |

### Agent Drafts (hiring)
| Method | Path | |
|---|---|---|
| GET | `/api/agent-drafts` | List pending hire proposals |
| POST | `/api/agent-drafts` | Submit a hire proposal (agents call this) |
| PUT | `/api/agent-drafts/:id` | Edit a draft |
| POST | `/api/agent-drafts/:id/approve` | Approve → creates live agent |
| POST | `/api/agent-drafts/:id/reject` | Reject |
| POST | `/api/agent-drafts/:id/dismiss` | Dismiss without rejecting |

### Projects
| Method | Path | |
|---|---|---|
| GET | `/api/projects` | List all |
| POST | `/api/projects` | Create |
| GET/PUT/DELETE | `/api/projects/:id` | Read / update / delete |
| GET/POST | `/api/projects/:id/agents` | List / assign agents |
| DELETE | `/api/projects/:id/agents/:agentId` | Remove agent |

### Tasks
| Method | Path | |
|---|---|---|
| GET | `/api/tasks?project_id=` | List project tasks |
| POST | `/api/tasks` | Create + run |
| POST | `/api/tasks/quick` | Create quick task (no project) |
| GET | `/api/tasks/running` | All running + queued (cross-project) |
| GET | `/api/tasks/attention` | All failed + awaiting-approval (cross-project) |
| GET/PUT/DELETE | `/api/tasks/:id` | Read / edit / delete |
| POST | `/api/tasks/:id/retry` | Re-run a failed task |
| POST | `/api/tasks/:id/dismiss` | Soft-hide from inbox |
| POST | `/api/tasks/:id/followup` | Create a follow-up refinement task |

### Inbox / Approval
| Method | Path | |
|---|---|---|
| GET | `/api/inbox` | Tasks awaiting approval |
| POST | `/api/inbox/:taskId/approve` | Approve |
| POST | `/api/inbox/:taskId/reject` | Reject |
| POST | `/api/inbox/:taskId/revise` | Send feedback + re-run |

### Teams
| Method | Path | |
|---|---|---|
| GET | `/api/teams` | List all |
| POST | `/api/teams` | Create |
| GET/PUT/DELETE | `/api/teams/:id` | Read / update / delete |
| GET/POST | `/api/teams/:id/agents` | List / add agents |
| DELETE | `/api/teams/:id/agents/:agentId` | Remove agent |
| POST | `/api/teams/:id/assign/:projectId` | Assign whole team to project |
| GET | `/api/teams/:id/export` | Export team bundle JSON |
| POST | `/api/import/team` | Import a team bundle |

### Stats & Admin
| Method | Path | |
|---|---|---|
| GET | `/api/stats/costs` | Cost totals + charts data |
| GET | `/api/admin/backup` | Stream a consistent SQLite snapshot |

### WebSocket
| | `/api/ws` | Events: `task.status_changed`, `task.output_stream`, `agent.status_changed`, `inbox.new_item`, `agent_draft.created` |

---

## Database & backup

Single SQLite file at `~/.local/share/phoenix/phoenix.db`.

**Download a snapshot while running:**
```
Settings → System → Download Backup
```
or:
```bash
curl -o backup.db http://localhost:8080/api/admin/backup
```

Uses `VACUUM INTO` for a WAL-consolidated snapshot — safe during live operation.

---

## Development

```bash
go test ./...          # run all tests
make test              # same via Make

cd web && npm run dev  # frontend dev server at :5173 (proxies API to :8080)
make build             # full production build
```

### Project structure

```
cmd/phoenix/           entry point
internal/
  api/                 HTTP handlers + WebSocket hub
  agent/               task runner + prompt assembly
  scheduler/           heartbeat ticker management
  provider/            Provider interface + adapters
    llm/               OpenAI-compatible HTTP adapter
    ollama/            Ollama local model adapter
    opencode/          opencode CLI adapter
    pi/                pi CLI adapter (stdin prompt delivery)
    claudecode/        Claude Code CLI adapter
    crush/             crush CLI adapter (AGENTS.md lifecycle)
    registry/          builds Provider instances from DB records
  store/               repository interfaces
    sqlite/            SQLite implementations + embedded migrations
  model/               shared domain types
  paths/               XDG/platform-aware data directory resolution
  frontend/            embedded React dist (web/dist)
web/                   React + TypeScript + Vite + Tailwind
```

### Migrations

SQL files in `internal/store/sqlite/migrations/` are embedded and applied in order at startup. To add a migration, create `NNN_description.sql` — the number must be higher than the current highest.

---

## Roadmap

See [GitHub Issues](https://github.com/solarisjon/phoenix/issues) for the full tracked backlog.

High-level upcoming work:
- Model picker dropdown (list available models per adapter)
- Task cancellation (kill a running task mid-stream)
- Bulk inbox actions (dismiss all failed, clear all)
- Running task count badge in nav
- Cost estimates before running
- Copilot CLI adapter
- Multi-user authentication

---

## License

MIT
