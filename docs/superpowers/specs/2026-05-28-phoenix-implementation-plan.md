# Phoenix — Phase 1 Implementation Plan

**Date:** 2026-05-28
**Spec:** `docs/superpowers/specs/2026-05-28-phoenix-design.md`
**Goal:** Vertical slice — one complete path from provider config to task execution and approval

---

## Implementation Order

Each step builds on the previous. Every step produces working, testable code.

---

### Step 1: Project Scaffolding & Build Pipeline

**What:** Initialize the Go module, React app, directory structure, Makefile, and build pipeline so that `make build` produces a single binary serving a hello-world React page.

**Files:**
- `go.mod`, `go.sum`
- `cmd/phoenix/main.go` — HTTP server, embeds frontend, serves on `:8080`
- `internal/paths/paths.go` — platform-aware config/data directory resolution (XDG, Windows)
- `internal/paths/paths_test.go`
- `web/` — React app scaffolded with Vite + TypeScript + Tailwind + shadcn/ui
- `web/src/App.tsx` — hello world placeholder
- `Makefile` — targets: `dev`, `build`, `clean`
- `.gitignore`

**Done when:** `make build` produces `./phoenix` binary. Running it serves the React app at `localhost:8080`. `make dev` runs Go with hot reload + Vite dev server with proxy.

---

### Step 2: Database Layer & Migrations

**What:** Set up SQLite, migration runner, and repository interfaces for all Phase 1 entities (User, Provider, Agent, Project, ProjectAgent, Task).

**Files:**
- `internal/store/store.go` — repository interfaces (`UserRepo`, `ProviderRepo`, `AgentRepo`, `ProjectRepo`, `TaskRepo`)
- `internal/model/model.go` — domain types (User, Provider, Agent, Project, Task, enums)
- `internal/store/sqlite/sqlite.go` — SQLite connection setup, migration runner
- `internal/store/sqlite/user.go` — UserRepo implementation
- `internal/store/sqlite/provider.go` — ProviderRepo implementation
- `internal/store/sqlite/agent.go` — AgentRepo implementation
- `internal/store/sqlite/project.go` — ProjectRepo implementation (includes ProjectAgent join)
- `internal/store/sqlite/task.go` — TaskRepo implementation
- `migrations/001_initial.sql` — all Phase 1 tables
- `internal/store/sqlite/*_test.go` — tests for each repo

**Done when:** All repos have working CRUD operations with tests passing. Migration runs automatically on startup. A default user is seeded on first run.

---

### Step 3: Provider Interface & Custom LLM Adapter

**What:** Implement the Provider interface and the custom LLM adapter that calls a user-configured HTTP endpoint (e.g., LLM Proxy).

**Files:**
- `internal/provider/provider.go` — Provider interface, TaskRequest, TaskResponse, StreamChunk, Message types
- `internal/provider/llm/llm.go` — Custom LLM adapter: Execute, StreamExecute, EstimateCost
- `internal/provider/llm/llm_test.go` — tests with HTTP test server mocking an LLM endpoint
- `internal/provider/registry.go` — provider registry: loads provider configs from DB, instantiates adapters

**Done when:** Custom LLM adapter can send a prompt to a configurable endpoint, parse the response, extract token counts, and calculate cost. Streaming works via SSE/chunked response parsing. Tests pass with a mock HTTP server.

---

### Step 4: Agent Runner (Core Execution)

**What:** Build the agent runner that takes a task, assembles the prompt (persona + instructions + guardrails + task input), calls the provider, and writes output back to the task.

**Files:**
- `internal/agent/runner.go` — AgentRunner service: RunTask method, goroutine management, context cancellation
- `internal/agent/prompt.go` — prompt assembly: combines agent persona, instructions, guardrails, and task description into a system prompt + user prompt
- `internal/agent/runner_test.go` — tests with mock provider

**Done when:** Given an agent config and a task, the runner assembles the prompt, calls the provider, streams output, updates task status (running → completed/failed), and records cost. Runs in a goroutine that's tracked and cancellable.

---

### Step 5: REST API — CRUD Endpoints

**What:** Build the HTTP API layer for all Phase 1 entities: providers, agents, projects, project-agent assignments, and tasks.

**Files:**
- `internal/api/server.go` — HTTP server setup, router, middleware (CORS, JSON content-type, logging)
- `internal/api/provider.go` — `GET/POST /api/providers`, `GET/PUT/DELETE /api/providers/:id`
- `internal/api/agent.go` — `GET/POST /api/agents`, `GET/PUT/DELETE /api/agents/:id`
- `internal/api/project.go` — `GET/POST /api/projects`, `GET/PUT/DELETE /api/projects/:id`, `POST /api/projects/:id/agents`
- `internal/api/task.go` — `GET/POST /api/tasks`, `GET/PUT /api/tasks/:id`
- `internal/api/errors.go` — consistent error response format
- `internal/api/*_test.go` — HTTP handler tests

**Done when:** All CRUD endpoints return correct JSON. Input validation returns 400 with helpful messages. Tests cover happy path and error cases. API is wired into `main.go`.

---

### Step 6: Task Execution & Inbox API

**What:** Wire task creation to the agent runner so creating a task triggers execution. Add inbox endpoints for the approval flow.

**Files:**
- `internal/api/task.go` — update POST to trigger agent runner async
- `internal/inbox/inbox.go` — inbox service: list pending approvals, approve, reject, revise
- `internal/api/inbox.go` — `GET /api/inbox`, `POST /api/inbox/:taskId/approve`, `POST /api/inbox/:taskId/reject`, `POST /api/inbox/:taskId/revise`
- `internal/agent/runner.go` — update to handle `awaiting_approval` status, pause/resume on approval, retry on revise with user feedback
- `internal/api/inbox_test.go`

**Done when:** Creating a task triggers the agent. Agent can set status to `awaiting_approval`. Inbox API lists pending tasks. Approving resumes the agent. Rejecting marks it failed. Revising sends feedback and the agent retries. All tested.

---

### Step 7: WebSocket Real-Time Updates

**What:** Add WebSocket endpoint for real-time push of agent status changes, task updates, and streaming output.

**Files:**
- `internal/api/ws.go` — WebSocket upgrade handler, client connection management, broadcast hub
- `internal/api/events.go` — event types: `task.status_changed`, `task.output_stream`, `agent.status_changed`, `inbox.new_item`
- `internal/agent/runner.go` — update to publish events during execution (status changes, streaming chunks)

**Done when:** A WebSocket client at `/api/ws` receives real-time events when tasks change status, agents stream output, or new inbox items appear. Multiple browser tabs work correctly.

---

### Step 8: Cost Tracking API

**What:** Add cost aggregation endpoint.

**Files:**
- `internal/api/stats.go` — `GET /api/stats/costs` with query params for grouping (by agent, by project) and date range filtering
- `internal/api/stats_test.go`

**Done when:** Cost endpoint returns aggregated cost data grouped by agent or project, filterable by date range. Tests pass.

---

### Step 9: Frontend — Layout & Navigation

**What:** Build the app shell: sidebar navigation, top bar with inbox badge, routing, and theme.

**Files:**
- `web/src/App.tsx` — router setup (React Router)
- `web/src/components/layout/Sidebar.tsx` — navigation: Dashboard, Inbox, Projects, Agents, Providers
- `web/src/components/layout/TopBar.tsx` — inbox count badge, app title
- `web/src/components/layout/AppLayout.tsx` — shell combining sidebar + top bar + content area
- `web/src/lib/api.ts` — API client (fetch wrapper with typed responses)
- `web/src/lib/ws.ts` — WebSocket client, auto-reconnect, event dispatcher
- `web/src/pages/` — placeholder pages for each route

**Done when:** App has a polished shell with working navigation. API client and WebSocket client are wired up. Pages are placeholders ready to be filled in.

---

### Step 10: Frontend — Provider Management

**What:** Build the provider configuration UI.

**Files:**
- `web/src/pages/ProvidersPage.tsx` — list providers, add new button
- `web/src/components/providers/ProviderForm.tsx` — create/edit form: name, type, endpoint, auth, model, cost-per-token fields
- `web/src/components/providers/ProviderCard.tsx` — display a configured provider

**Done when:** User can create, view, edit, and delete LLM providers through the UI. Form validates required fields. Looks polished.

---

### Step 11: Frontend — Agent Configuration

**What:** Build the agent creation and configuration UI.

**Files:**
- `web/src/pages/AgentsPage.tsx` — list agents with status indicators
- `web/src/components/agents/AgentForm.tsx` — create/edit: name, persona, instructions, guardrails, provider dropdown, status toggle
- `web/src/components/agents/AgentCard.tsx` — agent card: name, persona summary, status, cost consumed

**Done when:** User can create, view, edit agents. Agent cards show status and cost. Provider dropdown populated from configured providers. Looks polished.

---

### Step 12: Frontend — Project View & Task Management

**What:** Build the project workspace with agent assignment and task creation.

**Files:**
- `web/src/pages/ProjectsPage.tsx` — list projects
- `web/src/pages/ProjectDetailPage.tsx` — project detail: assigned agents, task list, cost summary
- `web/src/components/projects/ProjectForm.tsx` — create/edit project
- `web/src/components/projects/AgentAssignment.tsx` — assign/remove agents to project
- `web/src/components/tasks/TaskForm.tsx` — create task: title, description, assign to agent
- `web/src/components/tasks/TaskCard.tsx` — task card: title, status badge, agent, cost, timestamps
- `web/src/components/tasks/TaskDetail.tsx` — full task view: input, output, status timeline

**Done when:** User can create projects, assign agents, create tasks, and see task status. Task output displays when complete. Status badges update in real-time via WebSocket.

---

### Step 13: Frontend — Agent Live Status & Streaming Output

**What:** Add live streaming output to agent cards and task detail views.

**Files:**
- `web/src/components/agents/AgentCard.tsx` — update to show current task, streaming output
- `web/src/components/tasks/TaskDetail.tsx` — update with live streaming output panel
- `web/src/hooks/useAgentStream.ts` — hook that subscribes to WebSocket events for a specific agent/task

**Done when:** When an agent is running, its card shows live streaming text output. Task detail page shows the same stream. Output appears character-by-character or chunk-by-chunk.

---

### Step 14: Frontend — Inbox & Approval Flow

**What:** Build the inbox UI for reviewing and acting on pending approvals.

**Files:**
- `web/src/pages/InboxPage.tsx` — list of pending approval tasks, filters by project/agent
- `web/src/components/inbox/InboxItem.tsx` — card: agent name, task title, output preview, approve/reject/revise buttons
- `web/src/components/inbox/ReviseDialog.tsx` — modal for providing feedback when revising
- `web/src/components/layout/TopBar.tsx` — update inbox badge with real-time count via WebSocket

**Done when:** Inbox shows all pending approvals. User can approve (task resumes), reject (task fails), or revise (modal for feedback, agent retries). Badge count updates in real-time. Looks polished.

---

### Step 15: Frontend — Dashboard

**What:** Build the dashboard overview page.

**Files:**
- `web/src/pages/DashboardPage.tsx` — active projects summary, agent activity, pending inbox count, cost totals
- `web/src/components/dashboard/ProjectSummaryCard.tsx` — project name, task counts by status, cost
- `web/src/components/dashboard/AgentActivityFeed.tsx` — recent agent activity (task completions, approvals needed)
- `web/src/components/dashboard/CostSummary.tsx` — total cost, per-project breakdown (numbers only for Phase 1, charts in Phase 6)

**Done when:** Dashboard gives a clear at-a-glance view of everything happening in Phoenix. Updates in real-time. Looks like something you'd want to open every day.

---

### Step 16: End-to-End Testing & Polish

**What:** Test the full vertical slice end-to-end and fix any rough edges.

**Tasks:**
- Manual walkthrough: create provider → create agent → create project → assign agent → create task → watch it run → approve in inbox
- Fix any UI/UX rough edges found during walkthrough
- Add loading states, empty states, error states to all pages
- Ensure WebSocket reconnection works after disconnect
- Test with an actual LLM endpoint (LLM Proxy if available, or OpenAI-compatible)
- Write a README.md with setup instructions
- Update `make build` to produce a production-ready binary

**Done when:** A new user can download the binary, run it, and complete the full loop without confusion. README documents the process. All happy and error paths work cleanly.

---

## Summary

| Step | What                              | Depends On |
|------|-----------------------------------|------------|
| 1    | Project scaffolding & build       | —          |
| 2    | Database layer & migrations       | 1          |
| 3    | Provider interface & LLM adapter  | 2          |
| 4    | Agent runner (core execution)     | 2, 3       |
| 5    | REST API — CRUD endpoints         | 2          |
| 6    | Task execution & inbox API        | 4, 5       |
| 7    | WebSocket real-time updates       | 5, 6       |
| 8    | Cost tracking API                 | 5          |
| 9    | Frontend — layout & navigation    | 1          |
| 10   | Frontend — provider management    | 5, 9       |
| 11   | Frontend — agent configuration    | 5, 9       |
| 12   | Frontend — project & tasks        | 5, 6, 9    |
| 13   | Frontend — live streaming         | 7, 12      |
| 14   | Frontend — inbox & approvals      | 6, 7, 9    |
| 15   | Frontend — dashboard              | 8, 9       |
| 16   | E2E testing & polish              | All        |
