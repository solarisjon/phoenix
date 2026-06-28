# Phoenix Plugin Specification v1

This document is written for AI coding agents and developers. Every field, constraint, and API call is specified exactly — no ambiguity.

---

## Plugin Types

| Type | Kind values | Created by |
|---|---|---|
| `notifier` | `telegram`, `webhook` | Core (compiled in) |
| `theme` | `custom` | Community (via API/UI) |

---

## Theme Plugin

### Required fields

| Field | Type | Constraints | Example |
|---|---|---|---|
| `name` | string | 1–64 chars, unique | `"Solarized Dark"` |
| `type` | const | must be `"theme"` | `"theme"` |
| `kind` | const | must be `"custom"` | `"custom"` |
| `config` | JSON string | must contain `kind`, `preview`, `vars` | see below |

### Config object schema

| Field | Type | Constraints | Example |
|---|---|---|---|
| `config.kind` | `"dark"` \| `"light"` | required | `"dark"` |
| `config.preview` | `[string, string, string]` | exactly 3 valid CSS colors | `["#002b36","#268bd2","#073642"]` |
| `config.vars` | `object` | all 15 keys required, values are CSS colors | see below |

### CSS Variable Contract (all 15 required)

| Key | Purpose | Example value |
|---|---|---|
| `ph-bg` | Page background | `#002b36` |
| `ph-surface` | Elevated surfaces (sidebar, dropdowns) | `#073642` |
| `ph-card` | Card background | `#0a4050` |
| `ph-card-border` | Card border | `#1a5060` |
| `ph-input` | Input field background | `#0d4555` |
| `ph-hover` | Hover state background | `#1a5060` |
| `ph-text` | Primary text | `#839496` |
| `ph-text-muted` | Secondary text | `#657b83` |
| `ph-text-faint` | Tertiary/disabled text | `#586e75` |
| `ph-accent` | Primary accent color (buttons, links) | `#268bd2` |
| `ph-accent-light` | Lighter accent variant | `#5aafee` |
| `ph-accent-bg` | Accent background tint (badges, highlights) | `rgba(38,139,210,0.12)` |
| `ph-accent-text` | Text on accent-colored backgrounds | `#fdf6e3` |
| `ph-border` | Default border color | `#1a5060` |
| `ph-border-mid` | Stronger border (dividers) | `#2a6070` |

### Complete valid example

```json
{
  "name": "Solarized Dark",
  "type": "theme",
  "kind": "custom",
  "config": "{\"kind\":\"dark\",\"preview\":[\"#002b36\",\"#268bd2\",\"#073642\"],\"vars\":{\"ph-bg\":\"#002b36\",\"ph-surface\":\"#073642\",\"ph-card\":\"#0a4050\",\"ph-card-border\":\"#1a5060\",\"ph-input\":\"#0d4555\",\"ph-hover\":\"#1a5060\",\"ph-text\":\"#839496\",\"ph-text-muted\":\"#657b83\",\"ph-text-faint\":\"#586e75\",\"ph-accent\":\"#268bd2\",\"ph-accent-light\":\"#5aafee\",\"ph-accent-bg\":\"rgba(38,139,210,0.12)\",\"ph-accent-text\":\"#fdf6e3\",\"ph-border\":\"#1a5060\",\"ph-border-mid\":\"#2a6070\"}}"
}
```

### API call to create

```bash
curl -X POST http://localhost:8080/api/plugins \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "Solarized Dark",
    "type": "theme",
    "kind": "custom",
    "config": "{\"kind\":\"dark\",\"preview\":[\"#002b36\",\"#268bd2\",\"#073642\"],\"vars\":{\"ph-bg\":\"#002b36\",\"ph-surface\":\"#073642\",\"ph-card\":\"#0a4050\",\"ph-card-border\":\"#1a5060\",\"ph-input\":\"#0d4555\",\"ph-hover\":\"#1a5060\",\"ph-text\":\"#839496\",\"ph-text-muted\":\"#657b83\",\"ph-text-faint\":\"#586e75\",\"ph-accent\":\"#268bd2\",\"ph-accent-light\":\"#5aafee\",\"ph-accent-bg\":\"rgba(38,139,210,0.12)\",\"ph-accent-text\":\"#fdf6e3\",\"ph-border\":\"#1a5060\",\"ph-border-mid\":\"#2a6070\"}}"
  }'
```

### Validation rules

- All 15 CSS variable keys must be present in `config.vars`
- Values must be valid CSS color values (hex `#RRGGBB`, `rgb()`, `rgba()`)
- `config.preview` should correspond to `ph-bg`, `ph-accent`, `ph-surface` (in that order)
- `config.kind` must be `"dark"` or `"light"`
- `config` field is a **JSON string** (stringified JSON), not a nested object

### Common mistakes

- Passing `config` as a nested object instead of a JSON string
- Missing one or more of the 15 CSS variable keys
- Using `background` instead of `ph-bg` (all keys use the `ph-` prefix)
- Forgetting to set `type: "theme"` (defaults will not work)

---

## Notifier Plugin (Core)

Notifier plugins are compiled into Phoenix and cannot be created via the API. They are configured through the Settings UI or API.

### Telegram config schema

| Field | Type | Required | Supports `${ENV_VAR}` | Example |
|---|---|---|---|---|
| `bot_token` | string | yes | yes | `"${TELEGRAM_BOT_TOKEN}"` |
| `chat_id` | string | yes | no | `"-1001234567890"` |
| `parse_mode` | string | no | no | `"Markdown"` (default) or `"HTML"` |

### Webhook config schema

| Field | Type | Required | Supports `${ENV_VAR}` | Example |
|---|---|---|---|---|
| `url` | string | yes | yes | `"https://hooks.slack.com/services/..."` |
| `auth_header` | string | no | yes | `"Authorization: Bearer ${TOKEN}"` |
| `secret` | string | no | yes | `"${WEBHOOK_SECRET}"` — enables HMAC signing |
| `timeout_seconds` | integer | no | no | `10` (default) |

**HMAC signing:** When `secret` is set, each POST includes an `X-Phoenix-Signature` header containing `HMAC-SHA256(secret, body)` in hex. Verify this on your server to authenticate that the request came from Phoenix.

### Configure a notifier via API

```bash
# Update Telegram config
curl -X PUT http://localhost:8080/api/plugins/core-telegram \
  -H 'Content-Type: application/json' \
  -d '{
    "enabled": true,
    "config": "{\"bot_token\":\"${TELEGRAM_BOT_TOKEN}\",\"chat_id\":\"-1001234567890\",\"parse_mode\":\"Markdown\"}"
  }'

# Enable core plugins master switch
curl -X PUT http://localhost:8080/api/admin/settings \
  -H 'Content-Type: application/json' \
  -d '{"core_plugins_enabled": true, "community_plugins_enabled": false, "global_guardrails_enabled": false, "global_guardrails": ""}'
```

### Create a notification rule

```bash
curl -X POST http://localhost:8080/api/plugins/core-telegram/rules \
  -H 'Content-Type: application/json' \
  -d '{
    "event_type": "task.failed",
    "project_id": null,
    "enabled": true
  }'
```

### Valid event types for notification rules

| Event type | When it fires |
|---|---|
| `task.completed` | Task finishes successfully |
| `task.failed` | Task errors or times out |
| `task.needs_approval` | Task enters awaiting-approval status |
| `task.guardrail_triggered` | A guardrail fires during execution |

---

## API Reference

### Plugin CRUD

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/plugins` | List all. Filter: `?type=notifier\|theme` |
| `POST` | `/api/plugins` | Create community plugin |
| `GET` | `/api/plugins/:id` | Get details |
| `PUT` | `/api/plugins/:id` | Update config |
| `DELETE` | `/api/plugins/:id` | Delete (blocked for core) |
| `POST` | `/api/plugins/:id/enable` | Enable |
| `POST` | `/api/plugins/:id/disable` | Disable |
| `POST` | `/api/plugins/:id/test` | Send test notification |
| `GET` | `/api/plugins/:id/schema` | Get config JSON Schema |

### Notification Rules

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/plugins/:id/rules` | List rules for a plugin |
| `POST` | `/api/plugins/:id/rules` | Create rule |
| `PUT` | `/api/plugins/:id/rules/:rid` | Update rule |
| `DELETE` | `/api/plugins/:id/rules/:rid` | Delete rule |

### Themes

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/themes` | List community themes |

### System Settings (master switches)

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/admin/settings` | Read settings including `core_plugins_enabled`, `community_plugins_enabled` |
| `PUT` | `/api/admin/settings` | Update settings |

---

## Enable/Disable Hierarchy

Three levels, checked top-down:

```
--no-plugins CLI flag → overrides everything (session only, not persisted)
  └─ core_plugins_enabled (system_settings) → controls all core plugins
  └─ community_plugins_enabled (system_settings) → controls all community plugins
       └─ per-plugin enabled field → individual toggle
```

All default to OFF. Per-plugin states are preserved when a master switch is toggled.

---

## Core Plugin IDs

| ID | Name | Kind |
|---|---|---|
| `core-telegram` | Telegram | `telegram` |
| `core-webhook` | Webhook | `webhook` |

These are seeded at startup and cannot be deleted.
