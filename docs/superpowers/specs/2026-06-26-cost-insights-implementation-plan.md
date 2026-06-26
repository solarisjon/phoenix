# Cost Insights — Implementation Plan

**Date:** 2026-06-26  
**Spec:** `docs/superpowers/specs/2026-06-26-cost-insights-design.md`  
**Goal:** New "Cost Insights" sidebar page — actual spend breakdown by agent/provider/project, model pricing registry, and deterministic recommendations engine.

---

## Implementation Order

Each step builds on the previous. Every step produces working, compilable code.

---

### Step 1: Pricing Registry (`internal/pricing/`)

**What:** Create the `internal/pricing` package with a `Registry` that holds model pricing data. Seed it with a built-in table of ~30 common models. Add a `Refresh()` method that fetches from OpenRouter's public API and merges results. Add override support (load/save from `system_settings`).

**Files:**
- `internal/pricing/registry.go` — `ModelPrice` struct, `Registry` struct with built-in seed map, `New()`, `GetPrice(modelName string)`, `Refresh(ctx)`, `SetOverride(providerID string, p ModelPrice)`, `GetOverride(providerID string)`
- `internal/pricing/overrides.go` — `LoadOverrides(repo SystemSettingsRepo)`, `SaveOverrides(repo SystemSettingsRepo)` — marshals/unmarshals `pricing_overrides` key from `system_settings`
- `internal/pricing/tiers.go` — `Tier1Models`, `Tier2Models`, `Tier3Models` string slices; `SuggestCheaperModel(currentModel string) (suggested string, savingPct float64, ok bool)` — returns a Tier 2 suggestion if current model is Tier 1
- `internal/pricing/registry_test.go` — tests for `GetPrice` (built-in lookup, OpenRouter merge, override precedence), `SuggestCheaperModel`

**Built-in seed table (copy-paste from spec):** gpt-4o ($5/$15), gpt-4o-mini ($0.15/$0.60), gpt-4-turbo ($10/$30), gpt-3.5-turbo ($0.50/$1.50), claude-3-5-sonnet ($3/$15), claude-3-haiku ($0.25/$1.25), claude-3-opus ($15/$75), llama-3.1-8b ($0.05/$0.08), llama-3.1-70b ($0.35/$0.40), llama-3.1-405b ($2.70/$2.70), mistral-7b ($0.07/$0.07), mixtral-8x7b ($0.45/$0.45), gemini-1.5-pro ($3.50/$10.50), gemini-1.5-flash ($0.35/$1.05). All prices USD per 1M tokens.

**Done when:** `go test ./internal/pricing/...` passes. `Registry.GetPrice("gpt-4o")` returns correct built-in price. Override wins over built-in. OpenRouter merge test uses a mock HTTP server.

---

### Step 2: Wire Registry into main.go

**What:** Instantiate the `Registry` in `main.go`, call `Refresh()` on startup, and start a 24h background ticker to refresh periodically. Pass the registry to the stats handler (Step 3).

**Files:**
- `cmd/phoenix/main.go` — add `pricing.New()`, call `reg.Refresh(ctx)` (log warning on error, don't fatal), start goroutine with 24h ticker calling `reg.Refresh(ctx)`

**Done when:** Server starts and logs "pricing registry refreshed" (or a warning if OpenRouter unreachable). No test needed for wiring — covered by unit tests in Step 1.

---

### Step 3: Cost Insights API endpoint

**What:** Add `GET /api/stats/costs/insights?from=YYYY-MM-DD&to=YYYY-MM-DD` to the stats handler. Aggregate actual spend from the `tasks` table grouped by agent, provider, and project. Compute projected monthly cost. Run the three recommendation rules.

**Files:**
- `internal/store/store.go` — add `CostBreakdownRow` struct and `CostInsightsQuery(from, to time.Time) ([]CostBreakdownRow, error)` to `TaskRepo` interface
- `internal/store/sqlite/task.go` — implement `CostInsightsQuery`: SQL that joins `tasks → agents → providers` and `tasks → projects`, filters `completed_at BETWEEN ? AND ? AND cost_usd > 0`, groups by `agent_id`, `provider_id`, `project_id` in three separate queries, returns results
- `internal/api/stats.go` — add `getCostInsights` handler:
  1. Parse `from`/`to` query params (default: last 30 days)
  2. Call `taskRepo.CostInsightsQuery(from, to)`
  3. For each agent row: look up `registry.GetPrice(model)`, compute `projected_monthly_usd`
  4. Run recommendations engine (inline in handler, ~60 lines): evaluate three rules against agent rows
  5. Build and JSON-encode response matching spec shape
- `internal/api/server.go` — register `GET /api/stats/costs/insights` before existing `/api/stats/costs` route

**SQL for agent aggregation (example):**
```sql
SELECT
    a.id, a.name, a.model_override,
    p.name AS provider_name, p.config AS provider_config,
    SUM(t.cost_usd) AS actual_cost_usd,
    SUM(t.tokens_in) AS tokens_in,
    SUM(t.tokens_out) AS tokens_out,
    COUNT(t.id) AS task_count
FROM tasks t
JOIN agents a ON t.agent_id = a.id
JOIN providers p ON a.provider_id = p.id
WHERE t.completed_at BETWEEN ? AND ?
  AND t.cost_usd > 0
  AND t.dismissed = 0
GROUP BY a.id
ORDER BY actual_cost_usd DESC
```

Provider and project aggregations follow the same pattern.

**Done when:** `curl "http://localhost:8080/api/stats/costs/insights?from=2026-01-01&to=2026-06-26"` returns valid JSON with `by_agent`, `by_provider`, `by_project`, `recommendations` arrays (may be empty if no tasks have cost data).

---

### Step 4: Provider pricing override endpoint

**What:** Add `PUT /api/providers/:id/pricing` to save per-provider token price overrides into `system_settings`.

**Files:**
- `internal/api/provider.go` — add `updateProviderPricing` handler: decode `{input_per_mtoken, output_per_mtoken}`, call `registry.SetOverride(id, price)`, call `pricing.SaveOverrides(settingsRepo)` to persist
- `internal/api/server.go` — register `PUT /api/providers/{id}/pricing` **before** the existing `PUT /api/providers/{id}` route

**Done when:** `curl -X PUT /api/providers/<id>/pricing -d '{"input_per_mtoken":3.0,"output_per_mtoken":15.0}'` succeeds and a subsequent insights call uses the override price.

---

### Step 5: Frontend — API client additions

**What:** Add TypeScript types and API client methods to `lib/api.ts`.

**Files:**
- `web/src/lib/api.ts` — add `BreakdownRow`, `Recommendation`, `CostInsightsSummary`, `CostInsights` interfaces; add `getCostInsights(from, to)` and `updateProviderPricing(id, input, output)` to the api object

**Done when:** TypeScript compiles with no errors (`cd web && npx tsc --noEmit`).

---

### Step 6: Frontend — CostInsightsPage

**What:** Build the full page with summary bar, date range picker, three-tab breakdown table, and recommendations panel.

**Files:**
- `web/src/pages/CostInsightsPage.tsx` — contains all sub-components inline:
  - `DateRangePicker` — two `<input type="date">` controlled inputs; default `from` = today−30d, `to` = today; on change triggers re-fetch
  - `CostSummaryBar` — three stat cards: Total Spend, Projected/mo, Task Count; formatted with `formatCost` from `lib/utils.ts`
  - `CostBreakdownTable` — ranked table, prop `tab: 'agent'|'provider'|'project'` controls which columns render; columns: Name, Model (agent/provider tabs only), Provider (agent tab only), Actual Spend, $/Task, Proj./mo, Tasks; sorted descending by actual spend; empty state: "No cost data for this period"
  - `RecommendationsPanel` — conditionally rendered only if `recommendations.length > 0`; amber left-border card per item; severity badge (amber pill for `warning`, slate pill for `info`); deep-link button navigates to `/agents?id=<agent_id>` or `/providers`
  - Page root: fetches on mount + on date change; loading skeleton; error state

**Done when:** Page renders without errors. With real data, three tabs show ranked rows. Recommendations section appears when rules fire. Empty state shows gracefully when no cost data exists.

---

### Step 7: Frontend — Sidebar + Router + Provider form

**What:** Wire the new page into the app's navigation and add the pricing override inputs to the provider edit form.

**Files:**
- `web/src/components/layout/Sidebar.tsx` — add Cost Insights nav entry with `BanknotesIcon`, between Stats and Settings entries, route `/cost-insights`
- `web/src/App.tsx` — add `<Route path="/cost-insights" element={<CostInsightsPage />} />`
- `web/src/pages/ProvidersPage.tsx` — find the provider edit form/modal; add collapsible "Pricing Override" section with two number inputs (`Input cost $/M tokens`, `Output cost $/M tokens`); on save call `api.updateProviderPricing(id, input, output)`; prefill from provider config if override already set (fetch from insights or store in component state)

**Done when:** Clicking "Cost Insights" in sidebar navigates to the page. Provider edit form shows Pricing Override section. Saving a provider pricing override persists across page reloads (verify via `system_settings` in SQLite).

---

### Step 8: Build, test, and verify

**What:** Full build and smoke test.

```bash
# Backend
go build ./...
go test ./...

# Frontend
cd web && npm run build

# Rebuild binary and restart
go build -o phoenix ./cmd/phoenix/
pkill -f './phoenix'; nohup ./phoenix >> /tmp/phoenix.log 2>&1 &
sleep 1.5 && curl "http://localhost:8080/api/stats/costs/insights?from=2026-01-01&to=2026-06-26"
```

**Checklist:**
- [ ] `go test ./...` passes (including new `internal/pricing` tests)
- [ ] `cd web && npx tsc --noEmit` passes
- [ ] `npm run build` succeeds
- [ ] `/cost-insights` page loads in browser
- [ ] Summary bar shows correct totals
- [ ] Switching tabs updates the table
- [ ] Date range change re-fetches data
- [ ] Provider edit form shows Pricing Override section
- [ ] Saving a pricing override reflects in the next insights fetch

---

## File Change Summary

| File | Change |
|------|--------|
| `internal/pricing/registry.go` | New |
| `internal/pricing/overrides.go` | New |
| `internal/pricing/tiers.go` | New |
| `internal/pricing/registry_test.go` | New |
| `internal/store/store.go` | Add `CostInsightsQuery` to `TaskRepo` interface |
| `internal/store/sqlite/task.go` | Implement `CostInsightsQuery` |
| `internal/api/stats.go` | Add `getCostInsights` handler |
| `internal/api/provider.go` | Add `updateProviderPricing` handler |
| `internal/api/server.go` | Register two new routes |
| `cmd/phoenix/main.go` | Wire pricing registry, refresh goroutine |
| `web/src/lib/api.ts` | Add types + two API methods |
| `web/src/pages/CostInsightsPage.tsx` | New page |
| `web/src/components/layout/Sidebar.tsx` | Add nav entry |
| `web/src/App.tsx` | Add route |
| `web/src/pages/ProvidersPage.tsx` | Add pricing override section to edit form |

No DB migrations required.
