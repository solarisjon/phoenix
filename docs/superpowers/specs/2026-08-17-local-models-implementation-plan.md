# Local Models (llama.cpp) — Implementation Plan

**Date:** 2026-08-17  
**Spec:** `docs/superpowers/specs/2026-08-17-local-models-design.md`  
**Guide:** `docs/guides/local-models-llama-cpp.md`  
**Goal:** Make Phoenix reliable on small/local models served by `llama-server` (and, with less native depth, Ollama): never overflow context, get structured output right or repaired, route small jobs to small models, and let users measure a model's fitness before trusting it.

Each phase is independently shippable and leaves `go build ./... && go test ./...` green. Phases 0–2 are the minimum to call llama.cpp "supported"; 3–6 are what makes it *good*. Ordering is by value-per-risk: the earliest phases are provider-agnostic fixes that also help hosted models.

Migration numbers: Phase 0 used **055** (`health_reason`); Phase 2's task-meta migration is **056**.

---

## Phase 0 — Quick wins (½ day) — ✅ DONE 2026-08-17

Cheap fixes that remove the sharpest edges. All behind existing config; nothing changes for users who don't set the new fields.

> Shipped: #105 (0.1), #106 (0.2), #101 (0.3). One small schema change was taken after all — migration `055_task_health_reason.sql` — so `HEALTH_REASON` could be stored and shown rather than parsed and thrown away. Phase 2's migration is therefore **056**. Short-form `NEXT:`/`DONE:` markers were deliberately NOT enabled in 0.2 (too prose-like to accept before the compact profile that introduces them); they land with 2.4. Shared marker primitives live in `internal/agent/markers.go`.

### Step 0.1: Generation controls on the wire

**What:** Add `MaxOutputTokens`, `Temperature *float64`, `StopSequences` to `provider.TaskRequest`. Send them where the backend accepts them.

**Files:**
- `internal/provider/provider.go` — extend `TaskRequest`
- `internal/provider/llm/llm.go` — OpenAI flavour: `max_tokens` (from req, else `cfg.MaxTokens`, else omit), `temperature`, `stop`; Anthropic flavour: `temperature`, `stop_sequences`. Bump default timeout when `endpoint` host is loopback/`.local`/RFC1918 to 900 s (log at startup) — hosted default stays 60 s.
- `internal/provider/ollama/ollama.go` — new config fields `num_ctx`, `num_predict` (default 4096), `temperature`; send as `options{}`. Default timeout → 900 s.
- `internal/provider/llm/llm_test.go`, `ollama_test.go` — request-body assertions.

**Done when:** tests prove the fields are on the wire when set and absent when not; existing tests unchanged.

### Step 0.2: Tolerant marker parsers

**What:** Make every marker parser trim, ignore case on the marker, and tolerate Markdown decoration; parse `HEALTH_REASON`; line-anchor `TASK_COMPLETE`/`NEXT_ACTION` and take the last occurrence.

**Files:**
- `internal/agent/runner_extract.go` — `parseMemoBlocks`, `deriveHealthSignal` (+ return reason), `parseNextAction`, `parseTaskComplete`, `extractGuardrailTrigger`; new shared helper `markerLine(line, marker string) (rest string, ok bool)` that strips `*`, `` ` ``, `#`, `>` and whitespace before matching case-insensitively.
- `internal/agent/artifacts.go` — same helper for `ARTIFACT_START/END` and header keys.
- `internal/agent/orchestrator.go` — move `extractPlanJSON` to `internal/agent/jsonextract.go` as `ExtractJSONObject(s string) (json.RawMessage, error)`; use from all assist endpoints in Phase 4.
- Tests: table cases for `**MEMO_START**`, fenced blocks, `memo_start`, indented `Title:`, `Priority: HIGH`, `HEALTH_SIGNAL: Needs_Attention`, quoted `TASK_COMPLETE:` mid-text (must NOT fire), last-of-two `NEXT_ACTION` blocks.

**Done when:** all new table tests pass; the keyword-scan fallback in `deriveHealthSignal` is retained but demoted (returns `needs_attention` only when ≥ 2 keywords hit, and sets reason `"inferred from keywords"`) — fully removed in Phase 4.

### Step 0.3: Skill size guard + ordering fix

**What:** Warn (log + task note) when a single skill or total injected skills exceed 3k tokens (heuristic); reorder `buildTaskRequest` so global guardrails are the last system section and skill-mode sections come after behaviour.

**Files:** `internal/agent/prompt.go` (`InjectSkills` returns trimmed-note), `internal/agent/runner.go` (`buildTaskRequest` ordering), `prompt_test.go` snapshot update.

**Done when:** snapshot test shows `## Platform-Wide Guardrails` last; oversized-skill test logs the warning.

---

## Phase 1 — `llamacpp` provider (1–2 days) — ✅ DONE 2026-08-17 (#102)

> Shipped: `internal/provider/openaiwire/`, `internal/provider/llamacpp/`, `Capable`/`TokenCounter`/`SlotLimiter` + `Capabilities` in `provider.go`, `TaskRequest.ResponseSchema` (llamacpp honours it now; other adapters in Phase 4), runner provider-slot gate (`startIfCapacity`/`canStartLocked`/`drainProviderPeers`), Ollama `max_concurrent`+`Capable`, healthcheck capability refresh, 🦙 Providers form. Verified end-to-end (create → test → models → task) against a fake llama-server; adapter unit tests cover props/tokenize/schema/reasoning/usage/errors. Deviation: Ollama `max_concurrent` defaults to 0 (unchanged) not 1. Also verified against a **real** llama-server b10423 in router mode (Qwen3-0.6B Q8_0 and Qwen2.5-14B-Instruct Q4_K_M): test/models/quick task/monitor tasks all completed with parsed `HEALTH_SIGNAL`+`HEALTH_REASON`. Real-model finding folded in: the 14B paraphrased our bold "**Health signal** —" heading instead of emitting `HEALTH_SIGNAL:`, so (a) `markerLine` now accepts spaces/hyphens for underscores and `:`/`—`/`–`/`-` separators, and (b) the monitor prompt no longer offers a paraphrasable heading. Router mode: per-model `/props?model=` used only when `/models` reports the model loaded.

### Step 1.1: Share the OpenAI wire code

**What:** Extract from `llm.go` into `internal/provider/openaiwire/`: `ChatRequest`, `ChatMessage`, `ReadSSE(ctx, body, ch)` (the streaming loop incl. `stream_options` usage handling), `ParseCompletion`. `llm.Adapter` becomes a thin user of it. Pure refactor.

**Files:** `internal/provider/openaiwire/{wire.go,sse.go,wire_test.go}`, `internal/provider/llm/llm.go` (shrinks), tests unchanged.

**Done when:** `go test ./internal/provider/...` green; `llm_test.go` untouched.

### Step 1.2: The adapter

**What:** `internal/provider/llamacpp/llamacpp.go` implementing `Provider`, `ModelLister` (`/v1/models`), `Pinger` (`/health`), plus new `Capable`, `TokenCounter`, `SlotLimiter` (add these interfaces + `Capabilities` struct to `provider.go`). Probe `/props` lazily and cache 60 s; `/tokenize` memoised by SHA-256 of text (LRU 512 entries). Send `cache_prompt`, `n_predict`, `temperature`, `stop`, `response_format` (json_schema) when `TaskRequest.ResponseSchema` set. Handle `reasoning_content` (drop unless `keep_thinking`). Streaming via `openaiwire.ReadSSE`.

**Files:** `internal/provider/llamacpp/{llamacpp.go,llamacpp_test.go}` (httptest server faking `/props`, `/tokenize`, `/health`, `/v1/models`, `/v1/chat/completions` NDJSON/SSE), `internal/provider/registry/registry.go` (`kind=llamacpp`), `internal/healthcheck/checker.go` (use `Pinger`; refresh capabilities), `internal/api/provider.go` (health/test/models already generic — verify).

**Done when:** adapter tests cover: capabilities from `/props`; exact token count; schema passthrough; reasoning stripped; usage tokens on final chunk; 400 from server surfaces as task error with body excerpt.

### Step 1.3: Provider-level concurrency gate

**What:** Runner holds `map[providerID]*semaphore` sized from `SlotLimiter.MaxConcurrent()` (Ollama: new config `max_concurrent`, default 1; hosted: unlimited). Tasks that can't get a slot stay `queued` (already the state machine for per-agent limits — reuse `tryStartLocked` path).

**Files:** `internal/agent/runner.go` (`tryStartLocked`, release on finish/fail), `runner_concurrency_test.go`.

**Done when:** test: 2 tasks, provider with 1 slot → second stays queued until first completes; timeout clock for the second starts only when it actually starts.

### Step 1.4: UI + registry + docs touch

**What:** Providers page: `llamacpp` kind in the add/edit form (base URL, model dropdown from `/models`, timeout, keep_thinking, cache_prompt, context override, max output tokens). `CONTEXT.md` provider table row. Guide §4 gains the native config block.

**Files:** `web/src/pages/ProvidersPage.tsx`, `web/src/lib/api.ts` (kind union), `docs/superpowers/specs/CONTEXT.md`, `docs/guides/local-models-llama-cpp.md`.

**Done when:** create a llamacpp provider in the UI against a running llama-server, Test passes, run a quick task end-to-end.

---

## Phase 2 — Model profiles & prompt budgeting (2–3 days)

### Step 2.1: `ModelEntry` extensions + resolution

**What:** Add `ContextWindow`, `MaxOutputTokens`, `PromptProfile`, `Reasoning`, `Compatibility` to `model.ModelEntry` (JSON-additive, no migration). New `agent.ResolveModelProfile(prov provider.Provider, providerRec *model.Provider, modelID string) ModelProfile` implementing the order: entry → `Capable` probe → defaults.

**Files:** `internal/model/model.go`, `internal/agent/profile.go` (+ test), `web/src/lib/api.ts` types.

**Done when:** unit tests for each resolution branch.

### Step 2.2: Section-based prompt assembly

**What:** Refactor `AssembleRequest` + the `Inject*` functions to build `[]PromptSection` with `Priority` and `Shrink`; a `Compose(sections, profile) TaskRequest` renders them. **Byte-identical output for `standard` profile** — guarded by golden files.

**Files:** `internal/agent/prompt.go` (sections), `internal/agent/prompt_sections.go` (types, `Compose`), `internal/agent/testdata/prompt_*.golden`, `prompt_test.go` (golden compare; regenerate with `-update`).

**Done when:** all existing prompt/runner tests pass unmodified except for the golden harness itself.

### Step 2.3: Budget fitting

**What:** `internal/agent/budget.go`: `Fit(sections, budget Budget, count TokenCounterFunc) (fitted []PromptSection, trims []Trim, err error)` per spec §5.4. Wire into `buildTaskRequest` after all injections; `ContextWindow==0` ⇒ no-op. Force chain summarisation when over budget even if project opt-out. Store `prompt_tokens`, `prompt_trims` on the task; emit `task.prompt_trimmed` WS event.

**Files:** `internal/agent/budget.go` (+ table tests: fits / trims skills / drops obsidian / mandatory-only-too-big → error), `internal/agent/runner.go`, migration `056_task_prompt_meta.sql` (`prompt_tokens`, `prompt_trims`, `repair_attempts` — `health_reason` already landed in 055), `internal/store/sqlite/task.go` (`taskSelectCols`, scanners, `SetPromptMeta`), `internal/store/store.go`, test fakes (`memTaskRepo`, `fakeTaskRepo`) get the new method, `internal/api/events.go`.

**Done when:** integration test with a fake `Capable`+`TokenCounter` provider (ctx 4096) and a 20k-token skill: task runs, skill shrunk to description, `prompt_trims` populated, prompt fits.

### Step 2.4: `compact` profile text

**What:** Compact variants of memo/artifact, monitor, react, spawn/hire, orchestrator sections per spec §5.5; parsers (already tolerant from 0.2) accept `NEXT:`/`DONE:` in addition to the long forms. Auto-suggest `compact` when ctx ≤ 16k or tier `fast`.

**Files:** `internal/agent/prompt.go` / `prompt_compact.go`, golden files for compact, `runner_extract.go` (accept short forms), UI: profile selector on model pool rows (`ProvidersPage.tsx`).

**Done when:** golden tests for both profiles; a monitor run under `compact` against llama-server produces a parsed signal.

### Step 2.5: Task detail + compose meter

**What:** Task detail shows "Prompt trimmed" panel from `prompt_trims`; `POST /api/tasks/estimate` becomes a dry-run of `buildTaskRequest` returning `prompt_tokens`, `context_window`, `fits`, `trims`, `local`; compose panel shows `3.1k / 8k` meter.

**Files:** `internal/api/task_critic.go` (`estimateTask` → calls a new `agent.DryRunRequest`), `internal/agent/runner.go` (export dry-run), `web/src/pages/ProjectsWorkspace.tsx` (TaskComposeView meter, TaskDetailView panel), `web/src/lib/api.ts`.

**Done when:** estimate returns `local:true, cost 0, fits:false` for an oversized prompt on a 4k model; UI shows amber/red.

---

## Phase 3 — Utility model routing (1 day)

### Step 3.1: Setting + resolver

**What:** `system_settings` keys `utility_provider_id`, `utility_model`; `agent.ResolveUtilityProvider(ctx, registry, providerRepo, settings) (provider.Provider, providerID, model string)` with the fallback chain (setting → cheapest `fast` in any pool → first LLM provider). Settings → System UI picker.

**Files:** `internal/model/model.go` (`SystemSettings`), `internal/store/sqlite/system_settings.go`, `internal/api/settings.go`, `internal/agent/utility.go` (+ test), `web/src/pages/SettingsPage.tsx`.

### Step 3.2: Switch consumers

**What:** One shared `s.assistProvider(ctx, requestedID)` in `internal/api/` used by all eight assist endpoints; runner summariser (`runner.go:596`), Obsidian vault pick + note generation, health classifier (Phase 4) use the resolver. Orchestrator: honour `SelectModelForDomain` result for existing agents (`orchestrator.go:576`), call `SelectOrchestrationModel` when the orchestrator agent has no override.

**Files:** `internal/api/{agent,task_critic,project,settings,obsidian,team}.go`, `internal/agent/{runner,runner_extract,orchestrator}.go`, tests.

**Done when:** with a utility model configured, summariser and assist calls hit it (assert via fake registry); with none configured behaviour is unchanged.

---

## Phase 4 — Structured output & repair (1–2 days)

### Step 4.1: `ResponseSchema` end-to-end

**What:** Add `ResponseSchema json.RawMessage` to `TaskRequest` (if not already in Phase 0/1); llamacpp → `response_format json_schema`; ollama → `format`; llm OpenAI flavour → `response_format`; Anthropic flavour → ignore (or tool-forced later). Define schemas as Go vars next to their parsers: `orchestrator.PlanSchema`, `api.AgentGenSchema`, `api.DescriptionSchema`, `agent.HealthSchema`, `agent.VaultPickSchema`.

**Files:** adapters + tests, `internal/agent/schemas.go`, `internal/api/schemas.go`.

### Step 4.2: Health classifier

**What:** When a monitor output lacks a valid `HEALTH_SIGNAL`, call the utility model with `HealthSchema` over the output; store `health_signal` + `health_reason`; remove the keyword scan entirely.

**Files:** `internal/agent/runner_extract.go` (`deriveHealthSignal` → `classifyHealth(ctx, out)`), tests with fake provider.

### Step 4.3: One-shot repair

**What:** `agent.RepairStructured(ctx, prov, prevOutput, parseErr, schema, instructions) (string, error)` used by orchestrator plan parsing, agent generation, health classifier. Increment `tasks.repair_attempts`; UI notice on task detail.

**Files:** `internal/agent/repair.go` (+ test), call sites, `web/src/pages/ProjectsWorkspace.tsx`.

**Done when:** test: first output `Sure! Here is the plan: {…broken` → repair returns valid plan → task decomposes; second failure → same "left as completed" path as today with a clear `last_error`.

---

## Phase 5 — Model evaluation harness (2 days)

### Step 5.1: Suite + runner

**What:** `internal/agent/eval/` with the eight cases from spec §5.8, each building prompts via the real `prompt.go` and scoring with the real parsers. `Run(ctx, prov, opts) Report`. CLI `phoenix eval-model --provider <id> [--model <m>] [--profile compact]`.

**Files:** `internal/agent/eval/{suite.go,cases.go,report.go,eval_test.go}`, `cmd/phoenix/main.go` (subcommand), fixtures in `internal/agent/eval/testdata/`.

### Step 5.2: API + UI

**What:** `POST /api/providers/:id/eval` (async; progress via WS `provider.eval_progress`; result stored in `ModelEntry.Compatibility`); Providers page badge + "Run eval" + suggested profile/tier apply button. On non-local providers show a token estimate and confirm.

**Files:** `internal/api/provider.go`, `internal/api/events.go`, `web/src/pages/ProvidersPage.tsx`, `web/src/lib/api.ts`.

**Done when:** running eval on a local Qwen3-8B produces a report in < 5 min; suggested profile is `compact` for ctx ≤ 16k or low marker scores.

---

## Phase 6 — Docs, cleanup, hardening (½ day)

- `CONTEXT.md`: provider table (`llamacpp`), new columns, new interfaces, gotchas (port 8080 clash; `n_ctx` is per-slot; `/tokenize` memo; `standard` golden files must be regenerated deliberately).
- Guide: replace "What's coming" with the native config, eval instructions, and helper-model setup.
- Repurpose `agents.max_tokens_per_run` (migration 030, currently dead) as the per-agent `MaxOutputTokens` override — wire into `buildTaskRequest`; document. (Alternative: drop it — decide at the time.)
- README: "Runs fully local with llama.cpp" section linking the guide.
- Reconcile the two tier systems: `pricing.ModelTier()` (1/2/3 by name prefix) vs `model.ModelCapabilityTier` — make `pricing` derive from `ModelEntry` when present.

---

## GitHub issues (filed 2026-08-17)

| Issue | Phase | Scope |
|---|---|---|
| [#100](https://github.com/solarisjon/phoenix/issues/100) | Epic | First-class local model support (llama.cpp / small models) |
| [#105](https://github.com/solarisjon/phoenix/issues/105) | 0.1 | max_tokens/temperature/num_ctx on llm + ollama; local timeouts |
| [#106](https://github.com/solarisjon/phoenix/issues/106) | 0.2 | Tolerant marker parsers; HEALTH_REASON; line-anchored TASK_COMPLETE (follow-up to #72) |
| [#101](https://github.com/solarisjon/phoenix/issues/101) | 0.3 | Skill size warning; global guardrails last |
| [#102](https://github.com/solarisjon/phoenix/issues/102) | 1 | `llamacpp` provider kind + provider-level concurrency gate |
| [#107](https://github.com/solarisjon/phoenix/issues/107) | 2 | Model profiles, section-based assembly, budget fitting, `compact` profile, estimate dry-run + meter |
| [#103](https://github.com/solarisjon/phoenix/issues/103) | 3 | Helper (utility) model routing; orchestrator model-selection fixes (follow-up to #34) |
| [#108](https://github.com/solarisjon/phoenix/issues/108) | 4 | ResponseSchema through adapters; health classifier; one-shot repair |
| [#109](https://github.com/solarisjon/phoenix/issues/109) | 5 | Model evaluation harness + compatibility badge |
| [#104](https://github.com/solarisjon/phoenix/issues/104) | 6 | Docs/cleanup; repurpose `max_tokens_per_run` (#38); reconcile tier systems |

---

## Decisions (agreed 2026-08-17)

1. **Token heuristic for non-exact providers:** chars/4 with a 15 % safety margin. Revisit only if it visibly misfires; llama.cpp uses exact `/tokenize` counts anyway.
2. **`compact` profile default:** *auto* — compact when resolved context window ≤ 16k or tier `fast`; user can pin per model.
3. **Repair call target:** same provider as the original call (it saw the prompt). The utility model is used only for fresh calls (health classifier, summariser, assist endpoints).
4. **`max_tokens_per_run`:** repurpose as the per-agent `MaxOutputTokens` override (Phase 6), not drop.

**Next step:** Phase 0 (#105, #106, #101) — ~½ day, no schema changes, makes llama-server usable through the existing `llm` provider immediately.
