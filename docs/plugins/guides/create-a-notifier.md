# Configure a Webhook Notifier

The Webhook notifier sends a JSON POST to any HTTP endpoint when events occur. Use it for Slack, Discord, PagerDuty, or any custom integration.

## 1. Enable in Phoenix

1. Open **Plugins** in the sidebar
2. Enable **Core Plugins** (top toggle)
3. Click **Configure** on the Webhook card
4. Set:
   - **Webhook URL**: your endpoint (e.g. `https://hooks.slack.com/services/...`)
   - **Auth Header** (optional): e.g. `Authorization: Bearer ${WEBHOOK_TOKEN}`
   - **Timeout**: `10` seconds (default)
5. Click **Save Configuration**
6. Enable the Webhook toggle

## 2. Add rules

Same as Telegram — pick which events trigger a notification.

## 3. What gets sent

The webhook receives a JSON POST with this structure:

```json
{
  "event": "task.failed",
  "timestamp": "2026-06-22T14:30:00Z",
  "task": { "id": "...", "title": "...", "status": "task.failed" },
  "agent": { "name": "..." },
  "project": { "name": "..." },
  "message": "Rendered template text"
}
```

## Slack example

For Slack incoming webhooks, the URL is all you need — no auth header required. Slack will display the `message` field as a notification.

## Custom services

Point the webhook at any HTTP endpoint that accepts JSON POSTs. Your service can transform the payload and forward to any destination.
