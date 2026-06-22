# Prompt Caching — Implementation Plan

**Spec:** `2026-06-22-prompt-caching-design.md`  
**Issue:** #37  
**Files touched:** `internal/provider/llm/llm.go`, `internal/provider/llm/llm_test.go`, `web/src/pages/SettingsPage.tsx`

---

## Task sequence

### Task 01 — Extend `llm.Config` and wire types

**File:** `internal/provider/llm/llm.go`

Add to `Config` struct (after `TimeoutSeconds`):

```go
ApiFlavour             string  `json:"api_flavour"`               // "openai" (default) or "anthropic"
UsePromptCache         bool    `json:"use_prompt_cache"`          // Anthropic only
MaxTokens              int     `json:"max_tokens"`                // Anthropic requires this; default 8192
CostPerCacheWriteToken float64 `json:"cost_per_cache_write_token"` // default: CostPerInputToken * 1.25
CostPerCacheReadToken  float64 `json:"cost_per_cache_read_token"`  // default: CostPerInputToken * 0.10
```

Add new wire types after the existing `streamOptions` type:

```go
type contentBlock struct {
    Type         string        `json:"type"`                    // always "text"
    Text         string        `json:"text"`
    CacheControl *cacheControl `json:"cache_control,omitempty"`
}

type cacheControl struct {
    Type string `json:"type"` // "ephemeral"
}
```

Extend `chatRequest`:

```go
type chatRequest struct {
    Model         string          `json:"model"`
    System        json.RawMessage `json:"system,omitempty"`      // Anthropic: string or []contentBlock
    Messages      []chatMessage   `json:"messages"`
    Stream        bool            `json:"stream"`
    StreamOptions *streamOptions  `json:"stream_options,omitempty"`
    MaxTokens     int             `json:"max_tokens,omitempty"`
}
```

Extend usage structs with cache fields:

```go
// In chatCompletion:
Usage struct {
    PromptTokens                int `json:"prompt_tokens"`
    CompletionTokens            int `json:"completion_tokens"`
    CacheCreationInputTokens    int `json:"cache_creation_input_tokens"`
    CacheReadInputTokens        int `json:"cache_read_input_tokens"`
} `json:"usage"`

// In streamDelta:
Usage *struct {
    PromptTokens                int `json:"prompt_tokens"`
    CompletionTokens            int `json:"completion_tokens"`
    CacheCreationInputTokens    int `json:"cache_creation_input_tokens"`
    CacheReadInputTokens        int `json:"cache_read_input_tokens"`
} `json:"usage"`
```

Add `"encoding/json"` to imports (already present — verify it's not added twice).

**Acceptance:** `go build ./internal/provider/llm/...` passes with no errors.

---

### Task 02 — Implement `buildRequestBody` branching

**File:** `internal/provider/llm/llm.go`

Replace the current `buildRequestBody` with a version that branches on `api_flavour`:

```
isAnthropic := a.cfg.ApiFlavour == "anthropic"

if isAnthropic:
    build messages WITHOUT the system role entry (context + user only)
    marshal system prompt:
        if UsePromptCache:
            system = json.Marshal([]contentBlock{{Type:"text", Text: req.SystemPrompt, CacheControl: &cacheControl{Type:"ephemeral"}}})
        else:
            system = json.Marshal(req.SystemPrompt)   // plain JSON string
    set cr.System = system
    set cr.MaxTokens = cfg.MaxTokens (default 8192 if zero)
    do NOT set cr.StreamOptions (Anthropic always includes usage)
else (OpenAI):
    prepend {role:"system", content: req.SystemPrompt} to messages
    set cr.StreamOptions when stream=true (existing behaviour)
```

Helper method `(a *Adapter) isAnthropic() bool { return a.cfg.ApiFlavour == "anthropic" }` to avoid repeating the string comparison.

**Acceptance:** Existing `TestExecute_Success` and `TestStreamExecute_*` still pass (OpenAI path unchanged).

---

### Task 03 — Implement `calcCostWithCache`

**File:** `internal/provider/llm/llm.go`

Add a new method alongside the existing `calcCost`:

```go
func (a *Adapter) calcCostWithCache(tokensIn, tokensOut, cacheWrite, cacheRead int) float64 {
    writeRate := a.cfg.CostPerCacheWriteToken
    if writeRate == 0 {
        writeRate = a.cfg.CostPerInputToken * 1.25
    }
    readRate := a.cfg.CostPerCacheReadToken
    if readRate == 0 {
        readRate = a.cfg.CostPerInputToken * 0.10
    }
    return float64(tokensIn)*a.cfg.CostPerInputToken +
        float64(cacheWrite)*writeRate +
        float64(cacheRead)*readRate +
        float64(tokensOut)*a.cfg.CostPerOutputToken
}
```

Keep the existing `calcCost` for the OpenAI path (it delegates to `calcCostWithCache` with zeros, or stays as-is — either is fine).

Update `Execute` and `readSSEStream` to use `calcCostWithCache`, passing the new cache usage fields from the extended usage structs. `TokensIn` in the response = `PromptTokens + CacheCreationInputTokens + CacheReadInputTokens`.

**Acceptance:** `go test ./internal/provider/llm/...` passes.

---

### Task 04 — Suppress `stream_options` for Anthropic in streaming path

**File:** `internal/provider/llm/llm.go`

In `buildRequestBody`, when `isAnthropic()` and `stream=true`: do **not** set `cr.StreamOptions`. Anthropic's SSE stream always includes a final `message_delta` event with usage — `stream_options.include_usage` is an OpenAI extension and will cause a 400 on the Anthropic API.

Verify the existing `readSSEStream` still works for Anthropic: Anthropic sends `data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"..."}}` and a final `data: {"type":"message_delta","usage":{...}}`. 

**Note:** Anthropic's SSE format differs from OpenAI's. The current `readSSEStream` parses OpenAI `choices[].delta.content`. For Anthropic streaming to work properly, the SSE reader needs to handle both formats.

Add Anthropic SSE parsing to `readSSEStream`:

```go
// Anthropic delta shapes:
type anthropicDelta struct {
    Type  string `json:"type"`
    Delta struct {
        Type string `json:"type"`
        Text string `json:"text"`
    } `json:"delta"`
    Usage *struct {
        InputTokens                 int `json:"input_tokens"`
        OutputTokens                int `json:"output_tokens"`
        CacheCreationInputTokens    int `json:"cache_creation_input_tokens"`
        CacheReadInputTokens        int `json:"cache_read_input_tokens"`
    } `json:"usage"`
}
```

When `isAnthropic()`, unmarshal as `anthropicDelta` instead of `streamDelta`. Extract text from `delta.text` when `type == "content_block_delta"`. Extract usage from the `message_delta` event's `usage` field. Anthropic signals end of stream with `data: {"type":"message_stop"}` (not `[DONE]`) — handle this as the terminal event.

**Acceptance:** New unit test `TestStreamExecute_AnthropicFormat` passes (mock server returns Anthropic-style SSE).

---

### Task 05 — Unit tests

**File:** `internal/provider/llm/llm_test.go`

Add the following test cases using the existing `mockServer` + `newAdapter` helper pattern (add an `newAnthropicAdapter` helper that sets `api_flavour: "anthropic"`):

1. **`TestBuildRequestBody_OpenAI`** — verifies system role is in `messages[0]`, no `system` field, `stream_options` present when streaming.

2. **`TestBuildRequestBody_Anthropic_NoCache`** — verifies `system` is a JSON string equal to the system prompt, no cache_control, `messages` has no system entry, `max_tokens` is set.

3. **`TestBuildRequestBody_Anthropic_WithCache`** — verifies `system` is a JSON array with one block containing `cache_control: {type: "ephemeral"}`.

4. **`TestCalcCostWithCache_CacheHit`** — verifies cache_read tokens are billed at 10% rate.

5. **`TestCalcCostWithCache_CacheWrite`** — verifies cache_creation tokens are billed at 125% rate.

6. **`TestCalcCostWithCache_DefaultRates`** — verifies defaults are applied when `CostPerCacheWriteToken` and `CostPerCacheReadToken` are zero.

7. **`TestExecute_Anthropic_CacheUsage`** — mock server returns Anthropic response with `cache_read_input_tokens: 500`; verifies cost is computed at the read rate.

8. **`TestStreamExecute_AnthropicFormat`** — mock server sends Anthropic-style SSE events (`content_block_delta`, `message_delta` with usage, `message_stop`); verifies assembled output, token counts, and cost.

**Acceptance:** `go test ./internal/provider/llm/... -v` — all tests pass, no regressions.

---

### Task 06 — Frontend: API flavour + caching fields in provider form

**File:** `web/src/pages/SettingsPage.tsx`

In the LLM provider config form (the section that renders when `providerType == "llm"`), add three new fields after the existing `model` field:

**API Flavour** — `<select>` with options `openai` (label: "OpenAI / compatible") and `anthropic` (label: "Anthropic Messages API"). Reads/writes `config.api_flavour`. Default is `openai`.

**Prompt caching** — `<input type="checkbox">` that reads/writes `config.use_prompt_cache`. Disabled (greyed) when `api_flavour !== "anthropic"`. Help text: "Adds a cache breakpoint to the system prompt. Reduces repeat-call cost by ~90%. Anthropic only."

**Max output tokens** — `<input type="number">` that reads/writes `config.max_tokens`. Disabled when `api_flavour !== "anthropic"`. Placeholder: `8192`. Help text: "Required by the Anthropic API. Leave blank for default (8192)."

These sit below the existing `model` input and above the cost fields. No new components — use the same `<label>` + `<input>` / `<select>` pattern already in use in that form.

**Acceptance:** 
- Provider form renders without TS errors (`npm run build` clean).
- Toggling `api_flavour` to `openai` disables the caching checkbox and max_tokens field.
- Toggling to `anthropic` enables them.
- Values round-trip correctly: save provider, re-open form, values are restored.

---

### Task 07 — Build verification and commit

```bash
# Backend
go build ./...
go test ./internal/provider/llm/...

# Frontend
cd web && npm run build

# Integration smoke test (manual, optional)
# Configure a provider with api_flavour: "anthropic", use_prompt_cache: true
# Run a task and verify in logs that cache_creation/read tokens appear
```

Commit message:
```
feat: prompt caching support for Anthropic providers (#37)

- Add api_flavour ("openai"/"anthropic") and use_prompt_cache to llm.Config
- Switch system prompt to Anthropic Messages API format when api_flavour=anthropic
- Add cache_control content block when use_prompt_cache=true
- Handle Anthropic SSE format (content_block_delta / message_delta / message_stop)
- Track cache_creation_input_tokens and cache_read_input_tokens in cost calculation
- Default cache rates: write=1.25x input, read=0.10x input (configurable)
- Frontend: API flavour dropdown, caching toggle, max_tokens field in provider form
- Unit tests for all new behaviour paths
```

---

## Execution order

Tasks 01 → 02 → 03 → 04 must be done sequentially (each builds on the previous struct changes). Task 05 (tests) can be written alongside 02–04 as each behaviour is implemented. Task 06 (frontend) is independent of 01–05 and can be done in parallel or last.

## Risk notes

- **Anthropic SSE format** (Task 04) is the most complex change. The existing `readSSEStream` assumes OpenAI format throughout. Take care to keep the OpenAI path unchanged — use `isAnthropic()` guards, not rewrites.
- **`stream_options` must be absent** for Anthropic or the API returns 400. This is easy to miss.
- **`max_tokens` is required** by Anthropic (the API returns 400 if omitted). The default of 8192 must be applied before the request is sent, not left as zero.
