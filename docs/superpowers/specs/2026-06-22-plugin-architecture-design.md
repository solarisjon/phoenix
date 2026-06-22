# Plugin Architecture Design

**Date:** 2026-06-22
**Status:** Draft
**Scope:** Plugin framework + Telegram/Webhook notifiers + Community themes

---

## Overview

Phoenix gains a plugin system with two tiers:

- **Core plugins** — compiled-in Go code, shipped with the binary, can't be deleted. Examples: Telegram notifier, Webhook notifier.
- **Community plugins** — configuration-driven, created entirely through the Settings UI. No executable code. Examples: custom color themes.

The system provides a low barrier to entry for users and is documented so that an AI coding agent can read the spec and produce a correct plugin on the first attempt.

---

## Goals

1. Let users extend Phoenix without modifying core code or recompiling.
2. Ship Telegram and Webhook as core notification plugins.
3. Let users create custom themes through a UI form.
4. Provide documentation that is both human-friendly and machine-readable.
5. Establish a plugin architecture that scales to future extension types.

## Non-goals

- Executable community plugins (no subprocess, no WASM, no shared libraries).
- Refactoring existing providers into the plugin system (future work).
- Plugin marketplace or remote installation.

---

## Plugin Enable/Disable Hierarchy

Plugins can be controlled at three levels, evaluated top-down. If any level above is off, everything below it is effectively disabled — but individual settings are preserved in the DB so state is restored when the higher-level switch is turned back on.

```
Level 1: --no-plugins CLI flag (overrides everything for this session)
  └─ Level 2: "Enable Core Plugins" master switch (system_settings)
  │    └─ Level 3: Per core plugin enabled/disabled (plugins.enabled)
  └─ Level 2: "Enable Community Plugins" master switch (system_settings)
       └─ Level 3: Per community plugin enabled/disabled (plugins.enabled)
```

### Level 1: Startup safety valve

```bash
./phoenix --no-plugins       # all plugin dispatch disabled for this session
```

The `--no-plugins` flag is a runtime override. It does not modify the database — master switches and per-plugin states remain unchanged. The plugin manager skips all event dispatch and the Settings UI shows a banner: "Plugins are disabled via --no-plugins startup flag." This is the escape hatch when a misconfigured plugin causes startup issues or performance problems.

### Level 2: Master switches

Two independent toggles stored in `system_settings`:

| Key | Default | Effect |
|---|---|---|
| `core_plugins_enabled` | `0` (off) | When off, all core plugins (Telegram, Webhook) are disabled regardless of per-plugin state |
| `community_plugins_enabled` | `0` (off) | When off, all community plugins (custom themes, future types) are disabled regardless of per-plugin state |

**Both default to off.** Plugins are opt-in. A fresh Phoenix installation has no active plugin dispatch until the user explicitly enables a category and configures a plugin.

These are displayed as prominent toggles at the top of the Settings → Plugins page, above the Notifiers/Themes tabs.

### Level 3: Per-plugin toggle

The `enabled` column on the `plugins` table. Each plugin has its own enable/disable toggle on its card in the Settings UI. This is the existing spec behavior — the granular control within a category.

### Resolution logic in PluginManager

```go
func (m *PluginManager) isPluginActive(p *Plugin) bool {
    if m.noPluginsFlag {
        return false
    }
    if p.IsCore && !m.corePluginsEnabled {
        return false
    }
    if !p.IsCore && !m.communityPluginsEnabled {
        return false
    }
    return p.Enabled
}
```

This check runs before every notification dispatch and before injecting community themes into the `/api/themes` response.

---

## Data Model

### `plugins` table

```sql
CREATE TABLE plugins (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    type        TEXT NOT NULL,       -- 'notifier' | 'theme'
    kind        TEXT NOT NULL,       -- e.g. 'telegram', 'webhook', 'custom'
    is_core     BOOLEAN DEFAULT 0,   -- true = shipped with Phoenix, can't delete
    enabled     BOOLEAN DEFAULT 1,
    config      TEXT DEFAULT '{}',   -- JSON blob, schema depends on type+kind
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

The `type` field discriminates which plugin subsystem handles this record. The `kind` field identifies the specific implementation within that type. The `config` field is an opaque JSON blob interpreted by the handler for that type+kind — the same pattern used by the existing `providers` table.

Core plugins have `is_core = true` and are seeded at startup if they don't already exist. They can be enabled/disabled and configured but not deleted.

### `notification_rules` table

```sql
CREATE TABLE notification_rules (
    id          TEXT PRIMARY KEY,
    plugin_id   TEXT NOT NULL REFERENCES plugins(id) ON DELETE CASCADE,
    event_type  TEXT NOT NULL,       -- 'task.completed' | 'task.failed' | 'task.needs_approval' | 'task.guardrail_triggered'
    project_id  TEXT,                -- NULL = all projects
    enabled     BOOLEAN DEFAULT 1,
    template    TEXT,                -- Go text/template override for the message body (NULL = use default)
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

Decouples "which notifier" from "which events trigger it." A user can configure "Telegram for failed tasks on Project X" and "Webhook for everything" independently.

### Notifiable event types (v1)

Notification event types are a distinct namespace from task statuses. The plugin manager maps task status changes to these event types.

| Event type | Trigger | Mapped from |
|---|---|---|
| `task.completed` | Task finishes successfully | `task.status_changed` where status = `completed` |
| `task.failed` | Task errors or times out | `task.status_changed` where status = `failed` |
| `task.needs_approval` | Task enters awaiting-approval status | `task.status_changed` where status = `awaiting_approval` |
| `task.guardrail_triggered` | A guardrail fires during task execution | `task.status_changed` where status = `awaiting_approval` AND `guardrail_reason` is non-empty |

### Theme config schema

For plugins with `type = "theme"`, the `config` JSON blob has this structure:

```json
{
  "kind": "dark",
  "preview": ["#1e1f28", "#bd93f9", "#282a36"],
  "vars": {
    "ph-bg": "#1e1f28",
    "ph-surface": "#282a36",
    "ph-card": "#2d2f3d",
    "ph-card-border": "#3a3d52",
    "ph-input": "#353849",
    "ph-hover": "#3a3d52",
    "ph-text": "#f8f8f2",
    "ph-text-muted": "#a0a4b8",
    "ph-text-faint": "#6c7086",
    "ph-accent": "#bd93f9",
    "ph-accent-light": "#d4bbff",
    "ph-accent-bg": "rgba(189,147,249,0.12)",
    "ph-accent-text": "#ffffff",
    "ph-border": "#3a3d52",
    "ph-border-mid": "#4a4d62"
  }
}
```

All 15 CSS variable keys are required. Values must be valid CSS color values (hex, rgb, rgba).

### Telegram notifier config schema

```json
{
  "bot_token": "${TELEGRAM_BOT_TOKEN}",
  "chat_id": "-1001234567890",
  "parse_mode": "Markdown"
}
```

- `bot_token` (string, required): Telegram Bot API token. Supports `${ENV_VAR}` expansion.
- `chat_id` (string, required): Numeric chat ID or `@channel_name`.
- `parse_mode` (string, optional): `"Markdown"` (default) or `"HTML"`.

### Webhook notifier config schema

```json
{
  "url": "https://hooks.slack.com/services/...",
  "auth_header": "Authorization: Bearer ${WEBHOOK_TOKEN}",
  "timeout_seconds": 10
}
```

- `url` (string, required): The endpoint to POST to.
- `auth_header` (string, optional): HTTP header for authentication. Supports `${ENV_VAR}` expansion.
- `timeout_seconds` (integer, optional): Request timeout, default 10.

---

## Plugin Manager

### Location

`internal/plugin/manager.go`

### Initialization

Created in `main.go` at startup:

```go
noPlugins := flag.Bool("no-plugins", false, "disable all plugin dispatch for this session")
flag.Parse()

pluginManager := plugin.NewManager(pluginRepo, notificationRuleRepo, plugin.ManagerOpts{
    NoPlugins: *noPlugins,
})
pluginManager.SeedCorePlugins(ctx)   // ensure Telegram, Webhook exist in DB
pluginManager.LoadAll(ctx)           // load enabled plugins + master switch state

// Wire plugin manager to receive hub events.
// The hub broadcasts to WebSocket clients via channels.
// The plugin manager registers as an internal listener via
// hub.OnEvent(pluginManager.HandleEvent) — a callback the hub
// invokes synchronously before broadcasting to WS clients.
hub.OnEvent(pluginManager.HandleEvent)
```

The `--no-plugins` flag is passed via `ManagerOpts`. When set, `HandleEvent` returns immediately without dispatching, and `/api/themes` excludes community themes. The flag state is exposed via `GET /api/admin/settings` so the frontend can show the warning banner.

The hub gains a single new method: `OnEvent(fn func(Event))`. This registers an internal callback invoked during `Broadcast()` before the event is fanned out to WebSocket clients. The plugin manager's `HandleEvent` spawns goroutines for notification dispatch, so it returns immediately and never blocks the broadcast path.

### Responsibilities

1. **Lifecycle management** — loads enabled plugins from DB at startup, seeds core plugins on first run.
2. **Event dispatch** — receives events from the WebSocket hub, filters against notification rules, dispatches to notifiers.
3. **CRUD coordination** — provides methods for the API layer to create/update/delete/enable/disable plugins and rules.
4. **Config validation** — delegates to the notifier's `ValidateConfig()` before saving.

### Notifier interface

```go
// Notifier is the interface that core notification plugins implement.
type Notifier interface {
    // Send delivers a notification. The message is pre-rendered
    // from the template — the notifier just delivers it.
    Send(ctx context.Context, cfg json.RawMessage, msg NotifyMessage) error

    // ValidateConfig checks that a config blob is well-formed
    // before saving to DB. Returns a user-friendly error.
    ValidateConfig(cfg json.RawMessage) error

    // ConfigSchema returns a JSON Schema describing the config
    // fields. The Settings UI renders this as a dynamic form.
    ConfigSchema() JSONSchema
}
```

Core notifiers are registered in a package-level map:

```go
var coreNotifiers = map[string]Notifier{
    "telegram": &TelegramNotifier{},
    "webhook":  &WebhookNotifier{},
}
```

### NotifyMessage type

```go
type NotifyMessage struct {
    EventType   string    // "task.failed"
    Title       string    // "Task Failed: Daily Report"
    Body        string    // Rendered template text
    TaskID      string
    TaskTitle   string
    AgentName   string
    ProjectName string
    Error       string    // empty for non-failure events
    Timestamp   time.Time
}
```

### Event dispatch flow

```
1. Hub broadcasts event (e.g. task.status_changed)
2. PluginManager.HandleEvent(event) is called
3. Map the event to a notifiable event type:
   - status → "failed"    → "task.failed"
   - status → "completed" → "task.completed"
   - status → "awaiting_approval" → "task.needs_approval"
   - guardrail_reason set  → "task.guardrail_triggered"
4. Query notification_rules WHERE event_type = X AND enabled = true
5. For each matching rule:
   a. Check project_id filter (NULL matches all)
   b. Load the plugin record to get config
   c. Render the message template (rule.template or default)
   d. Look up the Notifier by plugin.kind in coreNotifiers
   e. Spawn a goroutine: notifier.Send(ctx, config, message)
   f. Log success or failure (never block)
```

### Default message templates

Each event type has a built-in default template. The `template` field in `notification_rules` overrides it when set.

```
task.failed:
  🔴 Task Failed: {{.TaskTitle}}
  Agent: {{.AgentName}}
  Project: {{.ProjectName}}
  Error: {{.Error}}

task.completed:
  ✅ Task Completed: {{.TaskTitle}}
  Agent: {{.AgentName}}
  Project: {{.ProjectName}}

task.needs_approval:
  ⏳ Approval Needed: {{.TaskTitle}}
  Agent: {{.AgentName}}
  Project: {{.ProjectName}}

task.guardrail_triggered:
  ⚠️ Guardrail Triggered: {{.TaskTitle}}
  Agent: {{.AgentName}}
  Project: {{.ProjectName}}
  Reason: {{.Error}}
```

Templates use Go `text/template` syntax with the `NotifyMessage` struct as the data context.

---

## Core Plugin Implementations

### Telegram Notifier

**Location:** `internal/plugin/notifiers/telegram/telegram.go`

**Implementation:** A single HTTP POST to `https://api.telegram.org/bot<token>/sendMessage`. No SDK — just `net/http` with a JSON body containing `chat_id`, `text`, and `parse_mode`.

The `bot_token` is expanded via the existing `provider.ExpandEnv()` utility before use.

**Error handling:** HTTP errors and Telegram API errors are logged but never block the task pipeline. Rate limiting (HTTP 429) is handled by logging a warning — no retry logic in v1.

**Test endpoint:** `POST /api/plugins/:id/test` sends a test message: "Phoenix plugin test — Telegram is configured correctly."

### Webhook Notifier

**Location:** `internal/plugin/notifiers/webhook/webhook.go`

**Implementation:** HTTP POST with a JSON body to the configured URL.

**Payload format:**

```json
{
  "event": "task.failed",
  "timestamp": "2026-06-22T14:30:00Z",
  "task": {
    "id": "...",
    "title": "...",
    "status": "failed"
  },
  "agent": {
    "id": "...",
    "name": "..."
  },
  "project": {
    "id": "...",
    "name": "..."
  },
  "message": "Rendered template text here"
}
```

The webhook notifier serves as a generic integration point. Users can point it at any HTTP endpoint — a Slack incoming webhook, a Discord webhook proxy, a PagerDuty integration, or a custom service — without any changes to Phoenix.

**Error handling:** Same as Telegram — log and move on. Configurable timeout (default 10s) prevents a hung endpoint from consuming resources.

---

## API Routes

### Plugin CRUD

| Method | Path | Description |
|---|---|---|
| GET | `/api/plugins` | List all plugins. Filter with `?type=notifier\|theme` |
| POST | `/api/plugins` | Create a community plugin |
| GET | `/api/plugins/:id` | Get plugin details |
| PUT | `/api/plugins/:id` | Update plugin config |
| DELETE | `/api/plugins/:id` | Delete plugin (blocked for `is_core`) |
| POST | `/api/plugins/:id/enable` | Enable a plugin |
| POST | `/api/plugins/:id/disable` | Disable a plugin |
| POST | `/api/plugins/:id/test` | Test a notifier (sends a test message) |

### Notification Rules

| Method | Path | Description |
|---|---|---|
| GET | `/api/plugins/:id/rules` | List rules for a notifier plugin |
| POST | `/api/plugins/:id/rules` | Create a notification rule |
| PUT | `/api/plugins/:id/rules/:rid` | Update a rule |
| DELETE | `/api/plugins/:id/rules/:rid` | Delete a rule |

### Themes

| Method | Path | Description |
|---|---|---|
| GET | `/api/themes` | List all themes (built-in + community) |

The `/api/themes` endpoint merges the hardcoded built-in themes with theme plugins from the DB. The frontend calls this instead of reading the static `THEMES` array.

### Request/Response formats

**Create a theme plugin:**

```
POST /api/plugins
Content-Type: application/json

{
  "name": "Solarized Dark",
  "type": "theme",
  "kind": "custom",
  "config": {
    "kind": "dark",
    "preview": ["#002b36", "#268bd2", "#073642"],
    "vars": {
      "ph-bg": "#002b36",
      "ph-surface": "#073642",
      "ph-card": "#0a4050",
      "ph-card-border": "#1a5060",
      "ph-input": "#0d4555",
      "ph-hover": "#1a5060",
      "ph-text": "#839496",
      "ph-text-muted": "#657b83",
      "ph-text-faint": "#586e75",
      "ph-accent": "#268bd2",
      "ph-accent-light": "#5aafee",
      "ph-accent-bg": "rgba(38,139,210,0.12)",
      "ph-accent-text": "#fdf6e3",
      "ph-border": "#1a5060",
      "ph-border-mid": "#2a6070"
    }
  }
}
```

**Create a notification rule:**

```
POST /api/plugins/:id/rules
Content-Type: application/json

{
  "event_type": "task.failed",
  "project_id": null,
  "enabled": true,
  "template": null
}
```

---

## Frontend Integration

### Theme injection

Community themes are injected at runtime as `<style>` blocks in `<head>`.

In `web/src/lib/theme.ts`, a new function:

```typescript
function injectCommunityThemes(themes: CommunityTheme[]) {
  const existing = document.getElementById('phoenix-community-themes');
  if (existing) existing.remove();

  const style = document.createElement('style');
  style.id = 'phoenix-community-themes';
  style.textContent = themes.map(t =>
    `[data-theme="plugin-${t.id}"] {\n` +
    Object.entries(t.vars).map(([k, v]) => `  --${k}: ${v};`).join('\n') +
    '\n}'
  ).join('\n\n');
  document.head.appendChild(style);
}
```

Community theme IDs are prefixed with `plugin-` to prevent collisions with built-in theme IDs.

### Theme picker changes

1. The static `THEMES` array remains as a fallback (works before the API responds).
2. On mount, the picker fetches `GET /api/themes` and merges community themes.
3. Community themes display a "Custom" badge.
4. Selecting a community theme calls `setTheme('plugin-<id>')`.

### Settings → Plugins page

A new route at `/settings/plugins` (lazy-loaded) with master switches and two tabs:

**Master switches (top of page, above tabs):**
- "Enable Core Plugins" toggle — controls all `is_core=true` plugins globally
- "Enable Community Plugins" toggle — controls all `is_core=false` plugins globally
- Both default to OFF on fresh install
- When `--no-plugins` flag is active, a yellow warning banner appears above the switches: "All plugins are disabled via --no-plugins startup flag. Restart Phoenix without this flag to enable plugins."
- When a master switch is off, the plugins in that category are visually dimmed and their per-plugin toggles are non-interactive (greyed out with a tooltip: "Enable core/community plugins above to configure individual plugins")

**Notifiers tab:**
- Lists all notifier plugins (core ones always present with "Core" badge)
- Each card: enable/disable toggle, Configure button, Test button
- Configure opens a form rendered from `ConfigSchema()` JSON Schema
- Notification rules section with add/edit/delete per notifier
- Rules show event type, project filter (dropdown of all projects + "All"), and enabled toggle

**Themes tab:**
- "+ Create Custom Theme" button opens the color form
- Form fields: name (text), kind (dark/light dropdown), 15 color pickers for CSS variables
- Live preview swatch (3-dot preview from first 3 colors)
- Edit/delete for existing community themes
- Built-in themes are NOT shown here (they're not plugins)

### No new WebSocket events

Plugin operations are user-initiated from Settings. Notification dispatch is server-side. No new WebSocket event types needed.

### No sidebar badge

Plugins are a settings concern, not an inbox concern. The existing Settings gear icon is sufficient.

---

## Documentation

### File structure

```
docs/plugins/
├── README.md                    # "What are plugins?" — 2 minute overview
├── PLUGIN-SPEC.md               # Machine-readable contract (agent-optimized)
├── guides/
│   ├── create-a-theme.md        # Step-by-step with screenshots
│   ├── create-a-notifier.md     # Webhook notifier walkthrough
│   └── telegram-setup.md        # Telegram bot creation + Phoenix config
└── examples/
    ├── theme-solarized.json     # Complete working theme example
    ├── theme-nord.json          # Another complete theme
    └── webhook-slack.json       # Webhook config targeting Slack
```

### PLUGIN-SPEC.md — agent-readable contract

Written so an AI coding agent can read it and produce a valid plugin without ambiguity:

- Every field has a type, constraint, and example value
- Complete working JSON examples for each plugin type
- Exact API calls (full curl commands) for create/update/delete
- Validation rules stated explicitly
- "Common mistakes" section that preempts typical agent errors
- CSS variable contract with purpose descriptions for each of the 15 vars

### Guide docs — human-friendly

Conversational, step-by-step walkthroughs. The theme guide covers the Settings UI flow in under 30 seconds of reading. The Telegram guide walks through BotFather setup, getting a chat ID, and configuring Phoenix.

### Example files

Complete, valid, copy-pasteable JSON files. An agent or human can take `theme-solarized.json`, change the colors, and POST it to the API with zero modification to structure.

---

## Migration

A single SQL migration file: `033_plugins.sql`

```sql
CREATE TABLE plugins (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    type        TEXT NOT NULL CHECK(type IN ('notifier', 'theme')),
    kind        TEXT NOT NULL,
    is_core     BOOLEAN NOT NULL DEFAULT 0,
    enabled     BOOLEAN NOT NULL DEFAULT 1,
    config      TEXT NOT NULL DEFAULT '{}',
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE notification_rules (
    id          TEXT PRIMARY KEY,
    plugin_id   TEXT NOT NULL REFERENCES plugins(id) ON DELETE CASCADE,
    event_type  TEXT NOT NULL CHECK(event_type IN (
        'task.completed', 'task.failed',
        'task.needs_approval', 'task.guardrail_triggered'
    )),
    project_id  TEXT,
    enabled     BOOLEAN NOT NULL DEFAULT 1,
    template    TEXT,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_plugins_type ON plugins(type);
CREATE INDEX idx_plugins_enabled ON plugins(enabled);
CREATE INDEX idx_notification_rules_plugin_id ON notification_rules(plugin_id);
CREATE INDEX idx_notification_rules_event_type ON notification_rules(event_type);

-- Master switches for plugin categories (both off by default)
INSERT OR IGNORE INTO system_settings (key, value, updated_at)
VALUES ('core_plugins_enabled', '0', CURRENT_TIMESTAMP);
INSERT OR IGNORE INTO system_settings (key, value, updated_at)
VALUES ('community_plugins_enabled', '0', CURRENT_TIMESTAMP);
```

---

## Package Structure

```
internal/plugin/
├── manager.go              # PluginManager — lifecycle, event dispatch, CRUD
├── types.go                # Plugin, NotificationRule, NotifyMessage, JSONSchema types
├── store.go                # PluginRepo, NotificationRuleRepo interfaces
└── notifiers/
    ├── notifier.go         # Notifier interface + coreNotifiers registry
    ├── telegram/
    │   └── telegram.go     # TelegramNotifier implementation
    └── webhook/
        └── webhook.go      # WebhookNotifier implementation

internal/store/sqlite/
├── plugin.go               # PluginRepo SQLite implementation
└── notification_rule.go    # NotificationRuleRepo SQLite implementation

internal/api/
├── plugin.go               # HTTP handlers for /api/plugins/*
└── theme.go                # HTTP handler for /api/themes

web/src/
├── lib/theme.ts            # Modified: community theme injection + API fetch
├── components/
│   ├── plugins/
│   │   ├── PluginsPage.tsx      # Main settings page with tabs
│   │   ├── NotifiersTab.tsx     # Notifier list + config forms
│   │   ├── NotifierConfigForm.tsx # Dynamic form from JSON Schema
│   │   ├── NotificationRules.tsx  # Rules CRUD for a notifier
│   │   ├── ThemesTab.tsx        # Theme list + create button
│   │   └── ThemeForm.tsx        # Color picker form for themes
│   └── ui/
│       └── theme-picker.tsx     # Modified: merge community themes
└── lib/
    └── api.ts                   # Modified: add plugin + theme API methods
```

---

## What This Does NOT Change

- **Providers** stay as-is — same registry, same `buildProvider()` switch, same adapters.
- **Built-in themes** stay as compiled frontend code in `index.css` and the `THEMES` array.
- **The WebSocket hub** gains an `OnEvent()` callback hook but its existing broadcast behavior is unchanged.
- **The task runner** is not modified — the plugin manager observes events passively.
- **Existing Settings pages** (Providers, Agents, System) are unchanged.

---

## Future Extensions (out of scope for v1)

- **More notifier kinds:** Slack (native), Discord, email, desktop (OS notifications).
- **More plugin types:** Custom output processors, CI/CD triggers, external integrations.
- **Provider plugins:** Refactor the provider registry to use the plugin system.
- **Theme import/export:** JSON export from the UI, import via file upload or API.
- **Plugin marketplace:** Browse and install community plugins from a registry.
- **Subprocess plugins:** For community plugins that need executable logic.
