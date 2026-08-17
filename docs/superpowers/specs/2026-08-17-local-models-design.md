# Local Models (llama.cpp) — Design Spec

**Date:** 2026-08-17  
**Status:** Draft — for review  
**Issue:** [#100](https://github.com/solarisjon/phoenix/issues/100) (epic; child issues per phase listed in the plan)  
**Related:** [`docs/guides/local-models-llama-cpp.md`](../../guides/local-models-llama-cpp.md) (user guide), [`2026-08-17-local-models-implementation-plan.md`](2026-08-17-local-models-implementation-plan.md) (phased plan)  
**Supersedes / touches:** `2026-06-22-prompt-caching-design.md` (llm adapter), `2026-06-26-cost-insights-design.md` (tiers), memory: *Agent architecture direction* (HEALTH_SIGNAL structured output)

---

## 1. Problem

Phoenix was built and tuned against frontier hosted models (Claude, GPT-4-class) and coding-agent CLIs. It *technically* works with local models today — the `ollama` adapter exists and the `llm` adapter speaks OpenAI wire format so `llama-server` can be pointed at — but in practice small models (3–14B, 4–16k usable context, weaker instruction following, no tool use) fail in ways Phoenix neither prevents nor notices:

| Failure | Root cause in code | Evidence |
|---|---|---|
| Prompt silently overflows the model's context; output truncated or nonsense | **No prompt budgeting anywhere.** Skills are injected in full (`InjectSkills`, `prompt.go:421`); one real SKILL.md is 32 KB ≈ 8k tokens. Follow-up chains only summarise when the project opts in and only after 8k *chars*. `Context: nil` is hardcoded so the cost-budget trimming loop has nothing to trim. | `internal/agent/prompt.go:23`, `:256`, `runner.go:588`, `:922` |
| Ollama runs at its default 2–4k context regardless of model | `options.num_ctx` never sent; adapter config has only `base_url/model/keep_thinking/timeout` | `internal/provider/ollama/ollama.go:33-48` |
| Model rambles until slot context is exhausted | `max_tokens` only sent for the Anthropic flavour; `n_predict` defaults to ∞ in llama-server; `temperature` never sent | `internal/provider/llm/llm.go:246-275` |
| Memos / artifacts / health signals not extracted although the model "basically" emitted them | Parsers require exact, case-sensitive, whole-line markers (`MEMO_START`), untrimmed `Title:` prefixes; `HEALTH_REASON:` is documented but never parsed; missing `HEALTH_SIGNAL` falls back to a 20-word keyword scan of the whole output ("error", "issue"…) with a high false-positive rate | `runner_extract.go:199-253`, `:510-540`, `artifacts.go:24-60` |
| Orchestrator plan / agent-generation JSON fails to parse → task "left as completed" | JSON compliance is prompt-only ("respond with a JSON object … no other text"); no `response_format`; **no retry-on-malformed-output anywhere** | `orchestrator.go:67-140`, `:322-326` |
| `TASK_COMPLETE:` fires when the model merely quotes the instruction | `strings.Contains` anywhere in output | `runner_extract.go:729` |
| Task times out at 60 s on cold model load | `llm` adapter default timeout is 60 s | `llm.go:72` |
| Cheap/small model never chosen where it would suffice | Tiers exist (`ModelEntry.CapabilityTier`) but `SelectModelForDomain`'s result is discarded when an existing agent matches; `SelectOrchestrationModel` has no caller; summariser/critic/Obsidian reuse the task's own provider; all eight "assist" endpoints pick *the first LLM provider* with its default model | `orchestrator.go:576`, `:259`, `runner.go:596`, `api/agent.go:216` et al. |
| Estimate endpoint says "unsupported" for local models, and under-counts everyone | Uses only `agent.Behaviour` chars/4; cost 0 ⇒ `supported:false` | `api/task_critic.go:39-99` |
| Global guardrails claim to "override all other instructions" but are not last in the prompt | Steps 5–9 of `buildTaskRequest` append after them; skill mode sections prepend before everything | `runner.go:555-652`, `prompt.go:179` |
| Two agents on a 1-slot server: one "runs" while actually queued in llama-server, burning its timeout | No provider-level concurrency; only per-agent `max_concurrent` | `runner.go:424` |

The net effect: a user with a 24 GB Mac and `llama-server` gets an experience that *looks* like Phoenix is flaky, when the real issue is that Phoenix assumes a 128k-context, format-obedient, retry-tolerant model on the other end.

---

## 2. Goals

1. **Phoenix on a single local `llama-server` is a first-class, documented, tested configuration** — not a happy accident of OpenAI compatibility.
2. **Never overflow the context window.** Phoenix knows each model's usable context and fits the prompt into it deterministically, telling the user what it dropped.
3. **Protocol markers and JSON either come out right or get repaired** — grammar-constrained where the backend supports it, tolerant parsing plus one automatic repair pass where it does not.
4. **The right-sized model does each job.** Big model plans/reasons; small model summarises, classifies health, generates descriptions.
5. **The user can see how well a given model copes with Phoenix** before trusting it with a monitor.

## 3. Non-goals

- Native tool-calling / function-calling loop inside Phoenix's pure-LLM path. (Coding-agent providers keep providing tools; llama-server's tool-call support is noted as a future option.)
- Running the llama-server process itself (lifecycle, model downloads) from Phoenix. Docs cover it; Phoenix stays a client.
- Fine-tuning or training. Prompt/protocol adaptation only.
- Replacing the Ollama adapter. It gets the cheap fixes (`num_ctx`, `num_predict`) and benefits from everything provider-agnostic; llama.cpp is the primary target for the native work.
- Perfect token counting for hosted providers. Heuristics stay for them; exactness is for local backends where it is free (`/tokenize`).

---

## 4. Principles

- **Prefer generic over llama.cpp-specific.** Every improvement lands as (a) a provider-agnostic mechanism in `internal/agent` gated by declared *capabilities*, and (b) a llama.cpp adapter that declares the richest capabilities. Hosted providers get better too.
- **Optional interfaces, not a fatter `Provider`.** Follow the `ModelLister` / `Pinger` pattern: new behaviours are optional interfaces the runner type-asserts.
- **Degrade loudly, not silently.** Anything Phoenix trims, drops, repairs, or guesses is recorded on the task (visible in the task detail) and logged.
- **Deterministic budgeting.** Given the same inputs and a context size, the assembled prompt is the same. No "ask the model to be brief and hope".
- **Small models get a smaller contract.** A `compact` prompt profile that says less, demands fewer simultaneous protocols, and uses stricter phrasing ("respond in exactly this format") replaces the "you MAY optionally…" boilerplate.

---

## 5. Design

### 5.1 Provider capabilities & request options (`internal/provider/provider.go`)

Extend `TaskRequest` with generation controls every adapter may honour:

```go
type TaskRequest struct {
    SystemPrompt string
    Prompt       string
    Context      []Message
    WorkingDir   string

    // New — all optional; adapters ignore what they can't honour.
    MaxOutputTokens int             // n_predict / max_tokens. 0 = adapter default.
    Temperature     *float64        // nil = adapter/model default.
    ResponseSchema  json.RawMessage // JSON Schema; when set, adapter constrains output if it can.
    CachePrompt     bool            // hint: prefix reuse desired (llama.cpp cache_prompt, Anthropic cache_control).
    StopSequences   []string
}
```

New optional interfaces:

```go
// Capabilities lets the runner adapt prompt assembly and parsing to the backend.
type Capabilities struct {
    ContextWindow      int  // usable tokens per request (per-slot for llama.cpp), 0 = unknown
    MaxOutputTokens    int  // hard cap if known
    SupportsJSONSchema bool // response_format json_schema / grammar
    SupportsJSONMode   bool // response_format json_object only
    SupportsTools      bool // native tool calling (informational for now)
    Local              bool // no per-token cost; latency dominated by hardware
    Reasoning          bool // thinking model — budget/strip reasoning
    ExactTokenCount    bool // TokenCounter is exact, not heuristic
}
type Capable interface{ Capabilities(ctx context.Context) Capabilities }

// TokenCounter counts tokens for the model behind the adapter (exact or heuristic).
type TokenCounter interface{ CountTokens(ctx context.Context, text string) (int, error) }

// Concurrency-limited providers report their slot count so the runner can gate.
type SlotLimiter interface{ MaxConcurrent() int }
```

A shared fallback `provider.HeuristicTokenCount(s string) int` (chars/4 today; can be swapped for a real BPE estimator later) is used when the adapter is not a `TokenCounter`.

### 5.2 llama.cpp adapter (`internal/provider/llamacpp/`)

New provider kind `llamacpp` (type `llm`), dispatched in `registry.go` alongside `ollama`. Reuses the OpenAI-format request/stream code from `llm` (factor the SSE reader and `chatRequest` into a small shared package or export them; do **not** copy) and adds llama-server-native calls:

| llama-server endpoint | Used for |
|---|---|
| `POST /v1/chat/completions` | Chat, streaming, `stream_options.include_usage`. Adds `cache_prompt`, `n_predict`/`max_tokens`, `temperature`, `response_format` (`json_schema`), `stop`. Reads `reasoning_content` and discards it unless `keep_thinking`. |
| `GET /props` | `default_generation_settings.n_ctx` (per-slot context), `total_slots`, model path/name, chat template → fills `Capabilities` |
| `POST /tokenize` | Exact token counts (`TokenCounter`) — cached by content hash for the life of the adapter |
| `GET /health` | `Pinger` (already-cheap; no inference) |
| `GET /v1/models` | `ModelLister` (single model, or all in router mode) |
| `GET /slots` | Optional: current busy slots for the health page (`--slots` is enabled by default) |

Config:

```go
type Config struct {
    BaseURL        string  `json:"base_url"`         // http://localhost:8081
    Model          string  `json:"model"`            // as reported by /v1/models; router mode selects by this
    APIKey         string  `json:"api_key"`          // optional (--api-key)
    TimeoutSeconds int     `json:"timeout_seconds"`  // default 900 — local models are slow to start
    KeepThinking   bool    `json:"keep_thinking"`
    ContextWindow  int     `json:"context_window"`   // override; 0 = probe /props
    MaxOutputTokens int    `json:"max_output_tokens"`// default 4096
    Temperature    *float64 `json:"temperature"`
    CachePrompt    bool    `json:"cache_prompt"`     // default true
}
```

Capabilities: `SupportsJSONSchema=true`, `Local=true`, `ExactTokenCount=true`, `ContextWindow` from `/props` (refreshed on `registry.Invalidate` and every health tick), `Reasoning` inferred from `/props` chat template / model name heuristics (`qwen3`, `r1`, `-think`) or config.

`EstimateCost` returns zero cost but **non-zero token estimate** — see 5.6 for how the estimate endpoint stops treating "free" as "unsupported".

**Ollama parity (cheap):** add `num_ctx`, `num_predict`, `temperature`, `format` (json / schema) to the Ollama request `options`; implement `Capable` (context from `/api/show` `model_info.*.context_length`, `SupportsJSONSchema=true` — Ollama accepts a schema in `format`), `TokenCounter` heuristic only.

**`llm` adapter (hosted) parity:** send `max_tokens`, `temperature`, `response_format` when set for the OpenAI flavour; declare `Capabilities{SupportsJSONSchema:true}` for OpenAI-compatible endpoints; declare `ContextWindow` from a config field or the pricing registry's model table (extend `ModelPrice` with `ContextWindow`).

### 5.3 Model profile (`model.ModelEntry` + provider-level defaults)

`ModelEntry` already carries `CapabilityTier`, `ProbedAt`. Extend:

```go
type ModelEntry struct {
    // existing…
    ContextWindow      int    `json:"context_window,omitempty"`
    MaxOutputTokens    int    `json:"max_output_tokens,omitempty"`
    PromptProfile      string `json:"prompt_profile,omitempty"` // "" | "standard" | "compact"
    Reasoning          bool   `json:"reasoning,omitempty"`
    Compatibility      *ModelCompatibility `json:"compatibility,omitempty"` // filled by the eval harness (5.8)
}
```

Resolution order for the values the runner uses: `ModelEntry` for the resolved model → provider `Capabilities()` probe → global defaults (`prompt_profile=standard`, `context_window=0` meaning "unknown, don't trim").

`prompt_profile` is auto-suggested: `compact` when `ContextWindow ≤ 16k` **or** the model is `fast` tier **or** the eval harness scores marker compliance below threshold. The user can pin it per model.

### 5.4 Prompt budgeting (`internal/agent/budget.go`, changes in `prompt.go`, `runner.go`)

Prompt assembly becomes **section-based** rather than string-append. `buildTaskRequest` produces an ordered list of `PromptSection{Key, Role(system|user), Text, Priority, Shrink func(tokens int) string}` and a `Budget` fits them:

```
budget.total      = ContextWindow − MaxOutputTokens − safety(256 + 5%)
mandatory (never dropped; if they alone exceed budget → fail task with a clear error, no LLM call):
  behaviour/persona+instructions, hard guardrails (+GUARDRAIL contract), global guardrails,
  task title/description/input, project objective,
  the ONE protocol section this task type needs (monitor→HEALTH_SIGNAL+memo; orchestration→plan JSON;
  react→NEXT_ACTION/TASK_COMPLETE; else→memo/artifact in compact form)
shrinkable, in drop order (last dropped first):
  1. Obsidian vault section          → dropped entirely
  2. spawn/hire instructions         → dropped if agent has the flag but the task text doesn't mention delegation/hiring; else kept
  3. persistent memories             → head-truncated to N tokens
  4. skills                          → each skill: full → description + first H2 section → description-only → dropped
  5. follow-up chain                 → older turns summarised (existing summariser, now forced not opt-in when over budget), then oldest dropped, most recent turn always kept but head-truncated
```

Everything dropped or shrunk is recorded in `tasks.prompt_trims` (JSON: `[{section, from_tokens, to_tokens, action}]`, new column) and shown in the task detail as *"Prompt trimmed to fit 8k context: skills 8,100→900 tokens, 1 memory dropped."* Also emitted as a `task.prompt_trimmed` WS event so the UI can warn before the run finishes.

Token counting: `TokenCounter` if available (llama.cpp exact, one `/tokenize` call per section, memoised), else heuristic. When `ContextWindow == 0` (unknown) budgeting is disabled and behaviour is exactly today's.

Ordering fix: global guardrails are always the **final** system section; skill mode sections move to just after behaviour instead of prepending before it.

### 5.5 Prompt profiles: `standard` vs `compact`

Same sections, different text. `compact` versions:

- Memo/artifact boilerplate collapses to ~120 tokens and is only injected for non-monitor tasks when the task/skill text suggests durable output (heuristic: words like report/write/save/create/summary, or a skill declares deliverables) — else omitted entirely.
- Monitor contract: exactly two lines demanded at the *end* of output, restated once, no "optionally":
  ```
  Finish with exactly these two lines and nothing after them:
  HEALTH_SIGNAL: <all_clear|needs_attention|failed>
  HEALTH_REASON: <one sentence>
  ```
  and the memo becomes optional-and-short (or is auto-generated from the answer by the utility model — see 5.7).
- ReAct contract: single closing line `NEXT: <what next>` or `DONE: <summary>` (parsers accept both old and new forms).
- Spawn/hire: one-paragraph form; the JSON body examples are dropped and replaced by "call `POST …/api/agents/spawn` with fields …".
- Orchestrator: schema shown once, no prose repetition; `ResponseSchema` sent so the backend enforces it.

Profiles are pure functions in `prompt.go` selected by `PromptProfile`; existing tests pin `standard` output so nothing changes for hosted models unless the user opts in.

### 5.6 Structured output & tolerant parsing

Two layers, both provider-agnostic:

**(a) Constrain when possible.** Any call whose result Phoenix parses as JSON — orchestrator plan, agent generation, task/project/team description generation, guardrails generation, vault pick — sets `TaskRequest.ResponseSchema`. Adapters with `SupportsJSONSchema` pass it through (llama-server: `response_format:{type:"json_schema", schema}`; Ollama: `format: <schema>`; OpenAI-compatible: `response_format`). Others ignore it.

For **monitors** the free-text answer is not constrained (it would wreck the write-up); instead the health signal is obtained by a **second, tiny classification call** with an enum schema `{"signal": "all_clear|needs_attention|failed", "reason": string}` over the answer, using the utility model (5.7). This *replaces* the keyword scan. Existing `HEALTH_SIGNAL:` in the answer is still honoured first (cheapest path); the classifier runs only when it's absent or invalid.

**(b) Parse tolerantly, then repair once.** `runner_extract.go` / `artifacts.go` parsers become:
- line-anchored but `TrimSpace`d and case-insensitive on markers; tolerate Markdown decoration (`**MEMO_START**`, `` `MEMO_START` ``, inside code fences, trailing colon);
- header keys (`Title:`, `Priority:`, `Path:`) matched after trim, case-insensitive; `Priority: High`/`HIGH` accepted;
- `HEALTH_REASON:` parsed and stored (`tasks.health_reason`, new column) and displayed on monitor run cards;
- `TASK_COMPLETE:` / `NEXT_ACTION:` line-anchored (start of line), taking the **last** occurrence;
- JSON extraction shared (`extractJSONObject`) and used by all assist endpoints, not just the orchestrator.

If a *required* structured result still fails to parse (plan JSON, agent generation, health classifier), the runner makes **one** repair call: same provider, minimal system prompt, `"Your previous reply could not be parsed: <error>. Reply with only the <thing>, exactly in this format: …"` with `ResponseSchema` set. Recorded on the task as `repair_attempts=1`. Never more than one.

### 5.7 Utility model routing

New system settings: `utility_provider_id`, `utility_model` ("Helper model — used for summaries, classification, descriptions"). Resolution helper `agent.ResolveUtilityProvider(ctx)`:

1. explicit setting → 2. cheapest `fast`-tier model in any provider pool (existing tier data) → 3. today's behaviour (first LLM provider / task's own provider).

Consumers switched to it: follow-up chain summariser (`runner.go:596`), Obsidian vault pick + note generation, health classifier (5.6), the eight assist endpoints (`generateAgent`, `generateTaskDescription`, `generateProjectDescription`, `suggestProjectNextAction`, `generateGlobalGuardrails`, `generateObsidianVaultContext`, `writeTaskToObsidian`, `generateTeamDescription`) via one shared `s.assistProvider(ctx, requestedProviderID)`. Built-in critic stays on the task's provider by default (a critique benefits from the stronger model) but honours `critic_model` if the user sets it.

Orchestrator fixes folded in: honour `mID/pID` from `SelectModelForDomain` when an existing agent matches (spawn with `ModelOverride`), and actually call `SelectOrchestrationModel` for the planning task when the orchestrator agent has no override.

For local setups this maps neatly onto llama-server **router mode** (`--models-dir`, one process, models loaded on demand): a 14B for agents, a 3–4B utility model for the rest.

### 5.8 Model evaluation harness ("Phoenix compatibility")

A deterministic suite that runs a model through Phoenix's real prompts and scores it. Lives in `internal/agent/eval/` with fixtures in `testdata/`; exposed as `POST /api/providers/:id/eval?model=` (async, streams progress over WS) and a CLI `phoenix eval-model --provider <id> [--model <m>]`.

Cases (each: prompt built with the real `prompt.go` code, expected parse result, timeout):

| Case | Checks |
|---|---|
| `marker_memo` | emits a parseable MEMO block when asked to report a finding |
| `marker_health` | monitor prompt → valid `HEALTH_SIGNAL` line (with and without compact profile) |
| `marker_react` | react prompt → exactly one of NEXT/DONE, correctly placed |
| `json_plan` | orchestrator prompt → schema-valid plan (with and without `ResponseSchema`) |
| `guardrail_stop` | hard guardrail violation → `GUARDRAIL_TRIGGERED` first line |
| `long_prompt_follow` | 6k-token prompt with instruction buried mid-way → instruction followed |
| `format_under_pressure` | 3 protocols enabled at once → still emits the required one |
| `perf` | tokens/s, time-to-first-token, cold vs warm (cache_prompt) |

Output: `ModelCompatibility{Score 0–100, PerCase map, ProbedAt, SuggestedProfile, SuggestedTier}` stored on the `ModelEntry`, shown on the Providers page as a badge (A/B/C/D) with a "why" popover, and used to auto-suggest `prompt_profile` and `capability_tier`. Costs nothing on local models; on hosted models the UI shows an estimate and asks first.

### 5.9 Concurrency & timeouts for local backends

- `SlotLimiter`: llama.cpp adapter reports `total_slots` from `/props`; the runner holds a per-provider semaphore so tasks stay **queued** in Phoenix (visible, not counting against timeout) rather than blocking inside llama-server's queue. Ollama: config field `max_concurrent` (default 1).
- Default timeout for `Local` providers: 900 s (config-overridable). Watchdog unchanged.
- Health checker: `/health` for llama.cpp; also refreshes `Capabilities` (n_ctx can change if the user restarts the server with new flags).

### 5.10 Estimate endpoint & UI

- `POST /api/tasks/estimate` assembles the **real** request (dry-run of `buildTaskRequest`, no execution) and returns `prompt_tokens`, `context_window`, `fits bool`, `trims []`, plus cost (0 for local). `supported` becomes `estimable`; a `local: true` flag lets the UI say "Free · 3.1k / 8k tokens" instead of "unsupported".
- Compose panel shows a context meter (`3.1k / 8k`) that turns amber when trims would occur and red when mandatory sections alone don't fit.
- Providers page: `llamacpp` form (base URL, model dropdown from `/v1/models`, timeout, context override, keep_thinking, cache_prompt); model pool rows gain context window, profile selector, compatibility badge and "Run eval" button.
- Settings → System: Helper model picker.
- Task detail: "Prompt trimmed" and "Output repaired" notices with details.

---

## 6. Data model changes

| Migration | Change |
|---|---|
| `055_task_health_reason.sql` ✅ | `tasks.health_reason TEXT NOT NULL DEFAULT ''` (Phase 0.2, landed) |
| `056_task_prompt_meta.sql` | `tasks.prompt_tokens INTEGER DEFAULT 0`, `tasks.prompt_trims TEXT DEFAULT '[]'`, `tasks.repair_attempts INTEGER DEFAULT 0` (+ `taskSelectCols` / scanners) |
| — (`utility_model`) | `system_settings` rows `utility_provider_id`, `utility_model` (no schema change; documented) |
| — | `providers.allowed_models` JSON gains the new `ModelEntry` fields (no migration; additive JSON) |
| — | Drop dead column `agents.max_tokens_per_run` (migration 030) **or** repurpose it as the per-agent `MaxOutputTokens` override — recommend repurpose (it is exactly that) |

---

## 7. Alternatives considered

- **Extend `llm` with `api_flavour: "llamacpp"` instead of a new package.** Consistent with the Anthropic precedent, but llama.cpp needs three extra endpoints, a probe cache, and exact tokenisation; the flavour switch would make `llm.go` (already 600 lines) the home of everything. Chosen: new package that *reuses* llm's OpenAI wire code via a shared helper — the same split as `ollama`.
- **Truncate by characters at the end of assembly** (simplest). Rejected: cuts mid-section, drops the wrong things (task text is last), invisible to the user.
- **Ask the model to be concise / "you have a small context".** Rejected: not deterministic; small models are exactly the ones that ignore it.
- **Only constrained output (grammar), no tolerant parsing.** Rejected: hosted providers and coding-agent CLIs can't be constrained; tolerant parsing is cheap and helps everyone.
- **A separate "small-model mode" toggle at the system level.** Rejected in favour of per-model profiles — mixed setups (Claude for one agent, Qwen-8B for monitors) are the realistic case.
- **Native tool calling via llama-server `--jinja` tool support.** Deferred: it's a different agent loop (Phoenix has none today); capture in `Capabilities.SupportsTools` and revisit after the above lands.

---

## 8. Risks & mitigations

- **Prompt-profile drift** — two variants of every section double the surface for bugs. Mitigation: table-driven tests over both profiles; eval harness exercises real prompts.
- **Budgeting hides content the user expected the agent to see.** Mitigation: trims are visible on the task and pre-flight in compose; mandatory sections never trimmed.
- **`/tokenize` cost per section on every task.** Mitigation: memoise by hash; sections like behaviour and boilerplate are stable across tasks; heuristic fallback if `/tokenize` is slow (> 200 ms).
- **Repair calls double the cost of a failed parse.** Mitigation: max one; only for *required* structured results; skipped when the first output was empty (different failure).
- **Refactoring `buildTaskRequest` into sections touches every prompt test.** Mitigation: Phase 0 keeps text byte-identical for `standard`; snapshot tests guard it.

---

## 9. Success criteria

- On a 24 GB Apple-silicon Mac with `llama-server` serving Qwen3-8B Q4_K_M at 16k context: the sample monitor + project setup from the guide runs for 24 h with zero context-overflow failures, health signal correctly parsed on ≥ 95 % of runs, and no task stuck "running" while queued behind a busy slot.
- Eval harness produces a score for any `llm`/`ollama`/`llamacpp` provider in < 5 min on local hardware and its suggested profile matches manual judgement on the three reference models (Qwen3-4B, Qwen3-8B, Qwen3-14B).
- Hosted-provider behaviour unchanged with `standard` profile (snapshot tests), plus `max_tokens`/`temperature` now controllable.
- Docs: guide + this spec + plan; `CONTEXT.md` provider table lists `llamacpp`; Providers UI has the form.
