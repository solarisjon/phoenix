# Cost Insights — Design Spec

**Date:** 2026-06-26  
**Status:** Approved  
**Feature:** New "Cost Insights" page — provider/agent/project cost breakdown, model pricing registry, and deterministic recommendations engine.

---

## Problem

LLM costs are increasing. Phoenix users have no single place to understand which agents, providers, or projects are driving spend, whether their model choices are appropriate for their workload, or what cheaper alternatives exist. This feature surfaces that information with zero AI dependency — all analysis is deterministic.

---

## Goals

- Show actual historical spend from completed tasks, broken down by agent, provider, and project.
- Show projected monthly cost per agent based on token usage rate and model pricing.
- Surface pricing data from OpenRouter's public API (covers most major models) with a built-in fallback table and per-provider user overrides.
- Generate deterministic recommendations flagging expensive or misconfigured setups.
- Provide a flexible date range picker for the historical view.

---

## Architecture

Three layers:

1. **Pricing Registry** (`internal/pricing/`) — model pricing data with three tiers: built-in seed table (lowest priority) → OpenRouter API cache → user override per provider (highest priority). Lookup checks override first, then OpenRouter cache, then falls back to built-in table.
2. **Cost Insights API** (`GET /api/stats/costs/insights`) — aggregates `tasks` table, joins with pricing registry, runs recommendations engine, returns JSON.
3. **Frontend** (`CostInsightsPage.tsx`) — new sidebar page with summary bar, three-tab breakdown table, and recommendations panel.

---

## Backend

### `internal/pricing/` package

**`registry.go`**

```go
type ModelPrice struct {
    InputPerMToken  float64  // USD per 1M input tokens
    OutputPerMToken float64  // USD per 1M output tokens
}

type Registry struct { ... }

func (r *Registry) GetPrice(modelName string) (ModelPrice, bool)
func (r *Registry) Refresh(ctx context.Context) error  // fetches OpenRouter, merges
func (r *Registry) SetOverride(providerID string, p ModelPrice)
func (r *Registry) GetOverride(providerID string) (ModelPrice, bool)
```

Built-in seed table covers ~30 models including:
- `gpt-4o`, `gpt-4o-mini`, `gpt-4-turbo`, `gpt-3.5-turbo`
- `claude-3-5-sonnet-20241022`, `claude-3-haiku-20240307`, `claude-3-opus-20240229`
- `llama-3.1-8b-instruct`, `llama-3.1-70b-instruct`, `llama-3.1-405b-instruct`
- `mistral-7b-instruct`, `mixtral-8x7b-instruct`
- `gemini-1.5-pro`, `gemini-1.5-flash`

OpenRouter fetch: `GET https://openrouter.ai/api/v1/models` — response includes `pricing.prompt` and `pricing.completion` (USD per token, multiply by 1M for per-million). Refresh runs on startup and every 24h via a background goroutine started in `main.go`.

**`overrides.go`**

User overrides stored in `system_settings` under key `pricing_overrides` as a JSON map:
```json
{ "<provider_id>": { "input_per_mtoken": 3.0, "output_per_mtoken": 15.0 } }
```

Loaded at startup, updated via `PUT /api/providers/:id/pricing`.

### New API endpoint

`GET /api/stats/costs/insights?from=YYYY-MM-DD&to=YYYY-MM-DD`

Registered in `api/server.go` under the existing `/api/stats/` group. Implemented in `api/stats.go`.

**Response shape:**

```json
{
  "period": { "from": "2026-06-01", "to": "2026-06-26" },
  "summary": {
    "total_actual_usd": 42.17,
    "projected_monthly_usd": 58.40,
    "task_count": 312
  },
  "by_agent": [
    {
      "id": "<agent_id>",
      "name": "Researcher",
      "model": "gpt-4o",
      "provider_name": "OpenAI",
      "actual_cost_usd": 18.20,
      "tokens_in": 1200000,
      "tokens_out": 340000,
      "task_count": 47,
      "cost_per_task": 0.387,
      "projected_monthly_usd": 24.10
    }
  ],
  "by_provider": [ ... ],
  "by_project": [ ... ],
  "recommendations": [
    {
      "severity": "warning",
      "kind": "expensive_model_swap",
      "title": "Agent \"Researcher\" costs $18.20/mo on gpt-4o",
      "detail": "Switching to claude-3-haiku would reduce cost by ~72% ($5.10/mo estimated).",
      "agent_id": "<agent_id>",
      "provider_id": "<provider_id>"
    }
  ]
}
```

**Aggregation query** — joins `tasks` with `agents` and `projects`, filters by `completed_at` in range and `cost_usd > 0`, groups by agent/provider/project.

**Projected monthly cost** — `(avg_tokens_in_per_task × input_price + avg_tokens_out_per_task × output_price) × tasks_per_month`. `tasks_per_month` = `task_count / period_days × 30`.

### Recommendations engine

Deterministic rules, evaluated in `api/stats.go` after aggregation:

| Rule | Condition | Severity |
|------|-----------|----------|
| `expensive_model_swap` | Agent projected monthly > $5 AND a known cheaper model exists that saves >50% | warning |
| `overkill_monitor` | Monitor uses a top-tier model (gpt-4o, claude-3-5-sonnet) AND runs <5 tasks/month | info |
| `no_concurrency_cap` | Agent actual spend > $1 in period AND `max_concurrent = 0` | info |

Cheaper model suggestions use a hardcoded tier map:
- Tier 1 (expensive): gpt-4o, claude-3-5-sonnet, claude-3-opus, gpt-4-turbo
- Tier 2 (mid): gpt-4o-mini, claude-3-haiku, mistral-7b, llama-3.1-70b
- Tier 3 (cheap): llama-3.1-8b, gemini-1.5-flash, gpt-3.5-turbo

Suggestion: if agent is on Tier 1, recommend the cheapest Tier 2 model available in the same provider family.

### Provider pricing override endpoint

`PUT /api/providers/:id/pricing`  
Body: `{ "input_per_mtoken": 3.0, "output_per_mtoken": 15.0 }`  
Saves to `system_settings` under `pricing_overrides`. Registered before `/:id` catch-all in `server.go`.

No new DB migration needed — uses existing `system_settings` key/value table.

---

## Frontend

### Sidebar

New entry added to `Sidebar.tsx` between the existing Stats and Settings entries:
- Label: **Cost Insights**
- Icon: `BanknotesIcon` (Heroicons outline)
- Route: `/cost-insights`

### `CostInsightsPage.tsx`

Single file in `web/src/pages/`. Fetches `getCostInsights(from, to)` on mount and on date range change.

**Layout:**

```
┌──────────────────────────────────────────────────────┐
│  Cost Insights                [From: ____] [To: ____] │
├──────────────────────────────────────────────────────┤
│  [ Total Spend: $42.17 ] [ Proj./mo: $58.40 ] [ 312 Tasks ] │
├──────────────────────────────────────────────────────┤
│  [By Agent]  [By Provider]  [By Project]             │
│  ┌────────────────────────────────────────────────┐  │
│  │ Ranked table (sorted by actual spend desc)     │  │
│  │ Cols: Name | Model | Provider | Spend | $/task │  │
│  │        | Proj./mo | Tasks                      │  │
│  └────────────────────────────────────────────────┘  │
├──────────────────────────────────────────────────────┤
│  Recommendations                                      │
│  ⚠ [warning] Agent "Researcher" costs $18/mo …      │
│    → claude-haiku saves ~72%   [View Agent →]        │
│  ℹ [info] Monitor "Daily Digest" uses gpt-4o but … │
└──────────────────────────────────────────────────────┘
```

**Sub-components (all in `CostInsightsPage.tsx`):**

- `CostSummaryBar` — three `StatCard`-style boxes: Total Actual Spend, Projected Monthly, Task Count.
- `CostBreakdownTable` — shared component for all three tabs. Props: `rows: BreakdownRow[]`, `tab: 'agent'|'provider'|'project'`. Agent tab shows Model and Provider columns; Provider tab shows Model column; Project tab hides both.
- `RecommendationsPanel` — amber-bordered section below the table. Each item: severity badge (amber `warning` / slate `info`), title, detail text, optional deep-link button (`View Agent →` or `View Provider →`).
- `DateRangePicker` — two `<input type="date">` fields, default range = last 30 days. On change, re-fetches insights.

### Provider edit form

In `ProvidersPage.tsx` (or wherever the provider edit form lives), add a collapsible "Pricing Override" section:
- Two number inputs: "Input cost ($/M tokens)" and "Output cost ($/M tokens)"
- Help text: "Leave blank to use built-in pricing. This affects cost projections in Cost Insights."
- On save, calls `updateProviderPricing(id, input, output)`.

### `lib/api.ts` additions

```typescript
export interface BreakdownRow {
  id: string;
  name: string;
  model?: string;
  provider_name?: string;
  actual_cost_usd: number;
  tokens_in: number;
  tokens_out: number;
  task_count: number;
  cost_per_task: number;
  projected_monthly_usd: number;
}

export interface Recommendation {
  severity: 'warning' | 'info';
  kind: string;
  title: string;
  detail: string;
  agent_id?: string;
  provider_id?: string;
}

export interface CostInsights {
  period: { from: string; to: string };
  summary: { total_actual_usd: number; projected_monthly_usd: number; task_count: number };
  by_agent: BreakdownRow[];
  by_provider: BreakdownRow[];
  by_project: BreakdownRow[];
  recommendations: Recommendation[];
}

// In api object:
getCostInsights: (from: string, to: string) => Promise<CostInsights>
updateProviderPricing: (id: string, inputPerMToken: number, outputPerMToken: number) => Promise<void>
```

---

## What Is Not In Scope

- AI-generated narrative recommendations (no LLM calls in this feature)
- Cost alerts / notifications (future: trigger a notification rule when projected monthly exceeds a threshold)
- Export to CSV (future)
- Per-task cost drill-down (tasks already have cost shown in task detail)

---

## Open Questions / Future Extensions

- **Cost alert rules:** a future notification rule type `cost.threshold_exceeded` could reuse the projections computed here.
- **Ollama / local models:** local models have $0 API cost but non-zero compute cost. Pricing overrides can be used to assign a cost-per-token if the user wants to track GPU/server cost.
