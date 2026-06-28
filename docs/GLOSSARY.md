# Phoenix — Domain Glossary

This document defines the core concepts in Phoenix and their relationships.
It is the authoritative reference for product, design, and engineering decisions.
When in doubt about what something is or how it relates to something else, start here.

> **Audience:** Phoenix is aimed at non-technical users. Every term here should be
> explainable to someone who has never written code. Implementation details live
> elsewhere — this document captures *what things are*, not *how they work*.

---

## Guiding Principles

- **Providers are infrastructure.** Users should never need to think about them after setup.
- **Agents are personas, not processes.** An agent is a defined role, not a running thing.
- **The Inbox is the only required interaction point.** If it's empty, the user doesn't need to open Phoenix.
- **Tasks are universal.** Whether a human, a schedule, or another agent creates work — it always becomes a Task.
- **Guardrails are the safety layer.** They are how the human stays in control.

---

## Terms

### Provider

> A back-end AI service connection — an API key, endpoint, and model configuration.
> Providers are **infrastructure**. Non-technical users should never need to think about
> them after initial setup. They are selected by an admin and then hidden behind Agents.

- Examples: Anthropic API, Ollama local endpoint, Claude Code CLI
- Two kinds under the hood: `llm` (API-based) and `coding_agent` (subprocess-based) — implementation detail only
- When exporting Agents or Teams, a Provider reference becomes a *requirement declaration*
  ("requires: an LLM provider"), not a hard link

---

### Agent Template

> A reusable, named AI persona. An Agent Template defines *how* an agent behaves —
> its instructions, personality, guardrails, and the Provider it uses. Agent Templates
> are shared platform resources: crafted once, used across many Projects and Monitors.

- Has: name, instructions/behaviour description, guardrails, provider
- `instructions` and `persona` are unified into a single user-facing **Behaviour** field
- Advanced settings (spawning subtasks, hiring agents) are admin-only, hidden from normal users
- Agent Templates are **exportable and importable** — shareable with other Phoenix users or the community
- When exported, the Provider reference becomes a capability requirement, not a hard dependency

---

### Agent Instance

> An ephemeral execution of an Agent Template. When a Task runs, an Agent Instance
> is created from the Template for that Task. The Instance lives only for the duration
> of the Task and then ceases to exist. No state persists between Instances unless
> explicitly written into the Task description or output.

- Think of it like Docker: Agent Template = image, Agent Instance = container
- Users never interact with Instances directly — they select Templates
- This allows the same Agent Template to run many Tasks concurrently

---

### Team

> A named group of Agent Templates with a collective identity and purpose.
> Agents on a Team can coordinate — passing work to each other, sharing context,
> and broadcasting messages within the Team. A Team is a **deployable unit**:
> it can be crafted, exported, and shared with others as a ready-to-use capability.

- Has: name, description/mission, member Agent Templates, designated roles (e.g. coordinator, critic)
- Intra-team coordination uses the Queue and Broadcast primitives
- Exportable as a bundle: Team + its Agent Templates + their behaviours
- When assigned to a Project, the Team's agents are available for Tasks in that Project

---

### Critic

> A specialised Agent whose role is to **challenge and advise** on the output of other
> Agents. A Critic reviews completed work, surfaces concerns, gaps, and risks, and
> presents its findings alongside the original output for the human to consider.
>
> A Critic **advises only — it never blocks or automatically re-runs work.**
> The human always decides what to do with a Critic's findings.

- A Critic is not a special type — it is an Agent Template whose instructions make it adversarial
- Can be a member of a Team, automatically reviewing output from other team members
- Can be optionally enabled on a Project ("require a second opinion on all tasks")
- Output is always visible to the user alongside the original Task output

---

### Guardrail

> A rule attached to an Agent Template that constrains its behaviour.
> Guardrails are the primary safety mechanism in Phoenix — they define
> what an agent is and is not allowed to do.

**Two types:**

| Type | Behaviour | Example |
|------|-----------|---------|
| **Soft** | Guides the agent's decisions | "Prefer concise responses" |
| **Hard** | Pauses the Task and creates an Inbox item — work cannot continue until the human responds | "Always ask before deleting data" |

**Two levels:**

| Level | Scope |
|-------|-------|
| **Agent-level** | Apply to every Task that Agent runs, in every Project and Monitor |
| **System-level** | Platform-wide rules set by admin that override all Agent-level guardrails |

- Hard guardrails are the mechanism by which Tasks enter `awaiting_approval` status
- Examples of hard guardrail triggers: deleting data, sending external communications, spending above a threshold, making production changes

---

### Project

> A human-driven workspace that groups related Tasks toward a bigger goal.
> A Project has no fixed end date — it is done when the user says it is done.
> Think of a Project as a **todo list** with an AI collaborator: you set the objective,
> and the AI can help you figure out what to do next.

- Has: name, **objective** (plain-English goal statement), optional working directory, assigned Team or individual Agents
- The **Objective** is the high-level statement of intent — e.g. "Build out articles and resources to help managers grow"
- Tasks in a Project are created by humans, by Agents spawning subtasks, or via the **✦ Suggest** button
- Tasks are grouped by status: Needs Attention → Running → Failed → Completed
- Completed Tasks (including inbox-dismissed ones) are always visible in the Project's history
- A Project can optionally have a Critic enabled — reviewing Task outputs before the human sees them
- Archived when complete — history and outputs are preserved

---

### Monitor

> A schedule-driven autonomous runner. A Monitor has a standing instruction and
> fires automatically on a user-defined cadence. Each time it fires, it spins up
> an Agent Instance, executes the instruction, and produces a Report.
>
> Monitors are **self-contained** — they do not interact with Projects.
> Each execution is independent; no state carries between runs unless
> explicitly included in the Monitor's instruction.

- Has: name, description/instruction, schedule interval, assigned Agent Template, working directory (optional)
- The **schedule belongs to the Monitor**, not to the Agent
- Each execution produces a Task (same concept as in Projects) and a Report
- Reports are stored in the Monitor's run history and carry a health signal: all clear / needs attention / failed
- A Monitor adds work to an Agent's Queue when it fires; the Agent processes it immediately

---

### Task

> The universal unit of work in Phoenix. Whether created by a human in a Project,
> fired by a Monitor on a schedule, spawned by an Agent as a subtask, or added
> via a Queue — it is always a Task when it runs. A Task is executed by one
> Agent Instance, has a clear start, a clear end, and produces an output.

**Task lifecycle:**
```
Queue → Running → Completed
               → Failed        → Inbox (intervention needed)
               → Awaiting      → Inbox (human decision required)
                 Approval        → human approves → continues
                                 → human rejects  → cancelled
```

- Tasks can have **subtasks**: an Agent can break work down and spawn child Tasks
  (requires `can_spawn_agents` permission on the Agent Template)
- Task output is always human-readable prose or markdown — not raw data blobs
- Tasks have a timeout — if a Task runs too long it is killed and sent to the Inbox
- Cost (tokens, USD) is tracked per Task and rolled up to Project/Monitor level

---

### Task Template

> A saved prompt scaffold — a title and description pre-filled for a common task.
> Templates make repetitive work faster: click once to pre-fill the compose panel,
> tweak if needed, and run.

- Can be global (available in every project) or scoped to a specific project or agent
- Created from the task compose panel or via Settings → Task Templates
- Applied from the **Templates** button in the compose panel

---

### Queue

> An Agent's waiting room. Work items arrive from multiple sources and are
> processed in priority order (higher priority first; ties broken by arrival time).
> The Queue only builds up when the Agent is already at maximum concurrency.
> The human can inspect the Queue and intervene — cancelling items before they become Tasks.

**Sources that write to an Agent's Queue:**
- A human (directly queuing work for an agent)
- A Monitor (firing on its schedule)
- Another Agent (spawning a subtask or delegating work)

**Queue intervention:**
- See all waiting items and their source
- Cancel an item before it executes
- **Bump** an item to move it to the front (higher priority runs first)
- See how long items have been waiting

---

### Inbox

> The human's action-required list. The Inbox contains items that are **blocking
> something** — a Task paused waiting for approval, an agent that needs clarification,
> a failure that needs intervention. The Inbox is the user's **only required
> interaction point** with Phoenix.
>
> If the Inbox is empty, no human action is needed today.

**Inbox item types:**
- ✋ Task awaiting human approval (triggered by a hard Guardrail)
- 🤔 Agent needs clarification — stuck and asking a question
- ⚠️ Task failed — needs human decision on how to proceed
- 🎯 Critic flagged something requiring a decision
- 💀 Task timed out or hung

**Inbox actions:** approve / reject / respond / dismiss

---

### Feed

> A real-time activity stream showing everything happening across Phoenix.
> The Feed requires **no action** — it is purely for awareness, visibility, and audit.
> If the Inbox is empty, the Feed is how the user knows everything is healthy.

**Feed shows:**
- Task started / completed / failed
- Monitor fired and produced a Report
- Agent spawned a subtask
- Guardrail triggered
- Cost incurred

---

### Report

> The structured output of a Monitor run. Every time a Monitor fires and its Task
> completes, a Report is produced and stored in the Monitor's run history.
> A Report is always human-readable and carries a **health signal**:
> all clear / needs attention / failed.

- Report ≠ raw Task output: Reports are surfaced prominently and always readable
- A Monitor's run history is a chronological list of Reports
- A "needs attention" Report may create an Inbox item if the Monitor is configured to do so

---

## Relationships at a glance

```
Provider ◄─────────────── used by ───────────── Agent Template
                                                      │
                                          ┌───────────┴───────────┐
                                       Template               Template
                                          │                       │
                                        Team ──── exports as ──► Bundle
                                          │
                              ┌───────────┴────────────┐
                              │                        │
                           Project                  Monitor
                        (human-driven)           (schedule-driven)
                              │                        │
                        creates Tasks           fires on cadence
                              │                        │
                              └───────────┬────────────┘
                                          │
                                        Queue
                                          │
                                        Task ──── output ──► Report (Monitor)
                                          │
                              ┌───────────┴───────────┐
                           subtask               Guardrail fires
                              │                        │
                           child Task              Inbox item
                                                       │
                                                  human decides
```

---

### Plugin

> A unit of extensibility that adds capabilities to Phoenix without changing the core binary.
> Plugins are configured in Settings → Plugins and can be individually enabled or disabled.

**Two categories:**

| Category | Description | Examples |
|---|---|---|
| **Core** | Ship built-in with Phoenix | Telegram notifier, Webhook notifier |
| **Community** | Created by users via the Themes editor | Custom colour themes |

- Each plugin has a `type` (notifier or theme) and a `kind` (telegram, webhook, custom, etc.)
- Plugins are stored in the `plugins` table and managed via `/api/plugins`
- Master switches in Settings control all core and all community plugins globally

---

### Notifier

> A Plugin that sends an external notification when a Task event occurs.
> Notifiers connect Phoenix to external communication channels so users know when
> something needs their attention even when Phoenix is not open.

- Configured with Notification Rules that specify which events trigger which notifier
- **Telegram notifier:** sends messages via a Telegram bot; chat ID can be auto-discovered
- **Webhook notifier:** sends a JSON POST to any URL; supports `Authorization` headers with `${ENV_VAR}` secrets
- Events that can trigger notifiers: task completed, task failed, task awaiting approval, etc.

---

### Theme

> A custom colour scheme for the Phoenix UI. Themes replace the built-in colour palette
> with a user-defined set of CSS variables covering backgrounds, text, accents, and borders.

- Five built-in themes: Dark, Midnight, Forest, Ember, Light
- Custom themes created via Settings → Plugins → Themes → + Create Theme
- The theme editor shows grouped colour pickers with a live preview that updates in real-time
- Custom themes appear in the sidebar theme picker under a "Custom" section
- Themes are stored as community plugins (`type=theme`) and apply instantly on save

---

### Memory Plugin

> An optional plugin type that gives an Agent persistent memory across Tasks.
> Without a memory plugin, every Task starts fresh — the Agent has no recollection
> of prior runs. With a memory plugin, the Agent can recall past outputs,
> decisions, and learned context in subsequent Tasks.

- Currently: **Hindsight** — a built-in memory plugin that summarises completed task output and stores it for future recall
- Configured in Settings → Plugins → Memory
- Memory is per-agent; enabling the plugin does not share memory between agents

---

## What is NOT in scope (yet)

- **Workflow / Pipeline** — ordered or conditional chains of Tasks (task dependency chains are on the roadmap)
- **Trigger** — event-based firing (webhook, file change, external event) beyond schedules
- **Integration / Channel** — deep two-way connections to external systems (GitHub Issues plugin is in progress; Slack, Jira are not)
- **Multi-user** — roles, permissions, team access control

---

*Last updated: 2026-06-27 (v0.7 in progress)*
*Agreed with: Jon Bowman*
