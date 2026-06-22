# Phoenix Plugins

Plugins extend Phoenix with optional features like notifications and custom themes. There are two tiers:

- **Core plugins** — shipped with Phoenix, maintained by the project. Currently: Telegram and Webhook notifiers.
- **Community plugins** — created by you through the Settings UI. Currently: custom color themes.

## Quick start

1. Open **Settings → Plugins** in the Phoenix UI
2. Enable **Core Plugins** or **Community Plugins** (both off by default)
3. Configure a notifier or create a custom theme

## Plugin types

| Type | What it does | Who creates them |
|---|---|---|
| **Notifier** | Sends push notifications when tasks complete, fail, or need approval | Core (Telegram, Webhook) |
| **Theme** | Custom color scheme for the Phoenix UI | Community (you) |

## Safety

- Both plugin categories are **disabled by default** — opt-in only
- Three-level enable/disable: `--no-plugins` flag → master switches → per-plugin toggle
- Community plugins are config-only — no executable code
- Start Phoenix with `--no-plugins` to disable all plugins for troubleshooting

## Documentation

- **[PLUGIN-SPEC.md](./PLUGIN-SPEC.md)** — Machine-readable specification (for AI agents and developers)
- **[guides/telegram-setup.md](./guides/telegram-setup.md)** — Set up Telegram notifications
- **[guides/create-a-theme.md](./guides/create-a-theme.md)** — Create a custom theme
- **[guides/create-a-notifier.md](./guides/create-a-notifier.md)** — Configure a webhook notifier
- **[examples/](./examples/)** — Copy-pasteable JSON examples
