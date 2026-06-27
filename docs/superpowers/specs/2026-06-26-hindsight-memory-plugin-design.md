# Hindsight Persistent Memory Plugin — Design Spec

**Date:** 2026-06-26  
**Status:** Approved  
**Author:** Jon Bowman (via brainstorming session)

---

## Overview

Add persistent, cross-task agent memory to Phoenix by integrating with [Hindsight](https://github.com/vectorize-io/hindsight), a biomimetic agent memory system. Memory is delivered as a new plugin type (`memory`, kind `hindsight`). It is seeded as a core plugin but **disabled by default** — no behaviour changes until the user explicitly enables and configures it.

---

## Goals

- Agents that have been working on tasks over time build up relevant memories automatically.
- Recalled memories are injected into the system prompt before each task runs.
- Task outputs are stored into memory after successful completion.
- Users can clear an agent's memory from the agent edit form.
- Failure of the memory system never blocks or fails a task.

---

## Non-Goals

- Per-project memory scoping (per-agent banks cover the primary use case).
- A memory browser UI (a clear button is sufficient for v1).
- Modifying agent output or behaviour based on memory outside the system prompt.
- Supporting memory backends other than Hindsight in v1.

---

## Architecture

### Memory Bank Strategy

Each agent gets its own Hindsight **bank** keyed by its Phoenix agent UUID. This gives each agent a persistent, cross-project identity — the agent accumulates knowledge from every task it completes, regardless of which project the task belonged to.

```
bank_id = agent.ID   // e.g. "4f4119b0-..."
```

### New Package: `internal/plugin/memory/`

```
internal/plugin/memory/
  memory.go          // MemoryClient interface
  hindsight/
    hindsight.go     // HTTP implementation + ConfigSchema
```

**`MemoryClient` interface:**

```go
type MemoryClient interface {
    Recall(ctx context.Context, agentID, query string) (string, error)
    Retain(ctx context.Context, agentID, content string) error
    ClearBank(ctx context.Context, agentID string) error
    Ping(ctx context.Context) error
}
```

**Hindsight HTTP implementation** calls:
- `POST <base_url>/retain` — body: `{"bank_id": agentID, "content": content}`
- `POST <base_url>/recall` — body: `{"bank_id": agentID, "query": query}`, returns recalled text
- `DELETE <base_url>/banks/<agentID>` — clears all memories for the agent
- `GET <base_url>/health` — used for connection test

### Plugin Registration

`SeedCorePlugins` in `internal/plugin/manager.go` inserts a new row on startup if absent:

```go
{
    ID:      "00000000-0000-0000-0000-000000000010",  // stable UUID
    Name:    "Hindsight Memory",
    Type:    model.PluginTypeMemory,    // "memory"
    Kind:    "hindsight",
    Config:  `{"base_url":"http://localhost:8888","api_key":""}`,
    Enabled: false,
    IsCore:  true,
}
```

No new DB migration is required — the existing `plugins` table handles arbitrary types.

### Model Changes

Add `PluginTypeMemory model.PluginType = "memory"` to `internal/model/model.go`.

### Plugin Manager (`internal/plugin/manager.go`)

Add:

```go
// MemoryClient returns the active Hindsight client, or nil if the plugin is
// disabled or not configured.
func (m *Manager) MemoryClient() memory.MemoryClient
```

The method reads the enabled `memory/hindsight` plugin from the DB, constructs a client from its config, and caches it. The cache is invalidated when `enablePlugin` / `disablePlugin` / `updatePlugin` are called.

### Runner Integration (`internal/agent/runner.go`)

The `Runner` struct gains an optional field:

```go
memoryClient memory.MemoryClient  // nil = disabled
```

Set from `manager.MemoryClient()` at Runner construction time in `main.go`.

Two hooks are added inside `executeTask`:

**1. Pre-task recall** (before `AssembleRequest`):

```go
if r.memoryClient != nil {
    query := t.Title + " " + t.Description
    memories, err := r.memoryClient.Recall(ctx, a.ID, query)
    if err != nil {
        log.Printf("[memory] recall failed for agent %s: %v", a.ID, err)
    } else if memories != "" {
        req = agent.InjectMemories(req, memories)
    }
}
```

**2. Post-task retain** (after successful completion, fire-and-forget):

```go
if r.memoryClient != nil && task.Status == model.TaskStatusCompleted {
    go func() {
        content := t.Title + "\n\n" + outputText
        if err := r.memoryClient.Retain(bgCtx, a.ID, content); err != nil {
            log.Printf("[memory] retain failed for agent %s: %v", a.ID, err)
        }
    }()
}
```

### Prompt Injection (`internal/agent/prompt.go`)

New helper:

```go
// InjectMemories adds a ## Persistent Memory section to the system prompt
// when recalled memories are non-empty.
func InjectMemories(req provider.TaskRequest, memories string) provider.TaskRequest {
    if memories == "" {
        return req
    }
    section := "\n## Persistent Memory\n" +
        "The following memories from your prior work are relevant to this task:\n" +
        memories + "\n"
    req.SystemPrompt = req.SystemPrompt + section
    return req
}
```

The section is appended after the fully-assembled system prompt (including global guardrails). Memory is informational context, not an instruction, so its position after guardrails is intentional — guardrails continue to take precedence over all behaviour.

---

## API Routes

One new route registered in `server.go` before `/:id`:

```
DELETE /api/agents/:id/memory    → clearAgentMemory handler
```

Handler calls `r.memoryClient.ClearBank(ctx, agentID)`. Returns 204 on success, 503 if memory plugin is not enabled, 500 on Hindsight error.

---

## Plugin Config Schema

`hindsight.go` implements `ConfigSchema()` returning:

```json
[
  {"key": "base_url", "label": "Hindsight URL", "type": "text",     "required": true,  "placeholder": "http://localhost:8888"},
  {"key": "api_key",  "label": "API Key",       "type": "password", "required": false, "placeholder": "Optional"}
]
```

This drives the existing plugin config editor in the Plugins UI — no frontend changes needed for configuration.

---

## Frontend Changes

### Plugins Page

No structural changes. The Hindsight plugin appears automatically in the core plugins list. The existing config editor renders the two fields from the schema. The existing "Test" button calls `POST /api/plugins/:id/test` → `manager.TestPlugin` → `client.Ping()`.

### Agent Edit Form (`AgentsPage.tsx`)

A **Memory** section is conditionally rendered at the bottom of the agent edit form. It is only shown when the Hindsight plugin is enabled (determined by a new `GET /api/plugins?type=memory` call on page load).

Content:
- Label: "This agent has persistent memory via Hindsight"
- Button: "Clear Memory" — calls `DELETE /api/agents/:id/memory`, shows a confirmation dialog first, toasts success/failure

---

## Error Handling

| Scenario | Behaviour |
|---|---|
| Hindsight server unreachable at recall | Log error, skip memory injection, task proceeds normally |
| Hindsight server unreachable at retain | Log error, silently drop (task already completed) |
| Plugin disabled mid-task | Memory client captured at task start; no effect on running task |
| Clear bank fails | Toast error in UI, 500 returned from API |
| Empty recall result | `## Persistent Memory` section omitted from prompt |
| Invalid `base_url` on save | Validated as URL format in `ValidateConfig`; 400 returned |
| Connection test fails | `TestPlugin` returns error; UI shows failure toast |

---

## File Summary

| File | Change |
|---|---|
| `internal/model/model.go` | Add `PluginTypeMemory` constant |
| `internal/plugin/memory/memory.go` | New — `MemoryClient` interface |
| `internal/plugin/memory/hindsight/hindsight.go` | New — HTTP client + `ConfigSchema` + `ValidateConfig` |
| `internal/plugin/manager.go` | Add `MemoryClient()` accessor, seed hindsight core plugin, `TestPlugin` case |
| `internal/agent/runner.go` | Add `memoryClient` field, recall hook, retain hook |
| `internal/agent/prompt.go` | Add `InjectMemories` helper |
| `internal/api/server.go` | Register `DELETE /api/agents/:id/memory` |
| `internal/api/agent.go` | Add `clearAgentMemory` handler |
| `cmd/phoenix/main.go` | Pass `manager.MemoryClient()` to `agent.New()` |
| `web/src/pages/AgentsPage.tsx` | Add Memory section with Clear button |

---

## Out of Scope (Future)

- Per-project memory isolation
- Memory browser / inspection UI
- Multiple simultaneous memory backends
- Reflect operation (Hindsight's deeper synthesis — could be a future "Reflect" button on agent detail)
