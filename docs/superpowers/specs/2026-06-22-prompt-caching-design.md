# Prompt Caching Support — Design Spec

**Date:** 2026-06-22  
**Issue:** [#37](https://github.com/solarisjon/phoenix/issues/37)  
**Status:** Approved

---

## Problem

The system prompt (persona + instructions + guardrails) is identical across every task run for a given agent. Re-sending it on every call and paying full input token price is unnecessary. Anthropic supports cache breakpoints that reduce re-use cost by ~90%. OpenAI caches automatically for prompts ≥1024 tokens with no API changes.

---

## Scope

- **In scope:** Anthropic prompt caching via `cache_control` content blocks in the `llm` adapter; correct cost calculation for cache reads/writes; frontend config fields.
- **Out of scope:** Caching in coding-agent adapters (opencode, pi, claude, crush — they handle it internally or don't support it); a new Anthropic-specific adapter package; OpenAI wire changes (not needed).

---

## Approach

Extend the existing `llm` adapter (Approach A) — dual wire format in one file, gated by two new config fields. No new packages.

---

## Design

### 1. Config (`internal/provider/llm/llm.go` — `Config` struct)

Two new fields added to `Config`:

```go
// ApiFlavour selects the wire format. "openai" (default) uses the standard
// OpenAI chat completions format. "anthropic" uses the Anthropic Messages API
// format, which has a separate top-level "system" field and requires "max_tokens".
ApiFlavour string `json:"api_flavour"`

// UsePromptCache adds an Anthropic cache_control breakpoint to the system
// prompt content block. Only effective when ApiFlavour == "anthropic".
// OpenAI caches automatically — no wire change needed.
UsePromptCache bool `json:"use_prompt_cache"`

// MaxTokens is the maximum number of output tokens. Required by the Anthropic
// API; ignored by OpenAI. Defaults to 8192 if zero.
MaxTokens int `json:"max_tokens"`

// CostPerCacheWriteToken is the USD cost per token when the cache is written
// (first call). Defaults to CostPerInputToken * 1.25 if zero.
CostPerCacheWriteToken float64 `json:"cost_per_cache_write_token"`

// CostPerCacheReadToken is the USD cost per token on a cache hit.
// Defaults to CostPerInputToken * 0.1 if zero.
CostPerCacheReadToken float64 `json:"cost_per_cache_read_token"`
```

---

### 2. Wire format (`buildRequestBody`)

**OpenAI mode** (`api_flavour` omitted or `"openai"`): unchanged. System prompt is `messages[0]` with `role: "system"`.

**Anthropic mode** (`api_flavour: "anthropic"`):
- System prompt is moved to a top-level `system` field.
- The `messages` array contains only the conversation turns (context + user prompt). No `role: "system"` entry.
- Anthropic requires `max_tokens` at the top level.

**Anthropic + caching** (`api_flavour: "anthropic"`, `use_prompt_cache: true`):
- `system` field is an **array of content blocks** instead of a plain string:
  ```json
  "system": [
    {
      "type": "text",
      "text": "<full system prompt>",
      "cache_control": {"type": "ephemeral"}
    }
  ]
  ```
- The cache breakpoint is placed on the last (and only) block of the static prefix. This is correct per Anthropic's docs — the system prompt is the stable prefix most worth caching.

**Struct changes:**

`chatRequest` gains optional top-level fields used only in Anthropic mode:

```go
type chatRequest struct {
    Model         string           `json:"model"`
    System        json.RawMessage  `json:"system,omitempty"`      // string or []contentBlock
    Messages      []chatMessage    `json:"messages"`
    Stream        bool             `json:"stream"`
    StreamOptions *streamOptions   `json:"stream_options,omitempty"`
    MaxTokens     int              `json:"max_tokens,omitempty"`
}

type contentBlock struct {
    Type         string        `json:"type"`
    Text         string        `json:"text"`
    CacheControl *cacheControl `json:"cache_control,omitempty"`
}

type cacheControl struct {
    Type string `json:"type"` // "ephemeral"
}
```

`System` is `json.RawMessage` so it can hold either a JSON string (no-cache Anthropic) or a JSON array (cache Anthropic) without a custom marshaller.

---

### 3. Usage / cost tracking

Anthropic's response includes additional usage fields:

```json
"usage": {
  "input_tokens": 100,
  "output_tokens": 50,
  "cache_creation_input_tokens": 1000,
  "cache_read_input_tokens": 0
}
```

The existing `chatCompletion` and `streamDelta` usage structs are extended with:

```go
CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
CacheReadInputTokens     int `json:"cache_read_input_tokens"`
```

**Cost calculation** (`calcCost` extended to `calcCostWithCache`):

```
cost = (input_tokens * cost_per_input_token)
     + (cache_creation_input_tokens * cost_per_cache_write_token)
     + (cache_read_input_tokens * cost_per_cache_read_token)
     + (output_tokens * cost_per_output_token)
```

Defaults if config fields are zero:
- `cost_per_cache_write_token` → `cost_per_input_token * 1.25`
- `cost_per_cache_read_token` → `cost_per_input_token * 0.10`

`TokensIn` reported in `TaskResponse` / `StreamChunk` = `input_tokens + cache_creation_input_tokens + cache_read_input_tokens` (total tokens consumed from the context window, for display purposes).

---

### 4. Frontend (`web/src/pages/SettingsPage.tsx`)

The LLM provider config form gains three new fields rendered when `type == "llm"`:

| Field | Type | Label |
|---|---|---|
| `api_flavour` | Select (`openai` / `anthropic`) | API Flavour |
| `use_prompt_cache` | Checkbox | Enable prompt caching |
| `max_tokens` | Number input | Max output tokens |

`use_prompt_cache` and `max_tokens` are greyed out (disabled) when `api_flavour` is `"openai"` (or unset), since OpenAI caches automatically and the field has no effect.

Optionally (stretch): show `cost_per_cache_write_token` and `cost_per_cache_read_token` fields when Anthropic + caching is enabled, pre-populated with the formula-derived defaults. This gives power users control without requiring it.

---

## Error handling

- If `api_flavour` is set to an unrecognised value, the adapter logs a warning and falls back to `"openai"` mode.
- Anthropic API errors (e.g. cache not available for a model) are surfaced as task errors via the existing error path — no special handling needed.
- `stream_options` (`include_usage`) is OpenAI-specific and must be omitted in Anthropic mode (Anthropic always includes usage in the final stream event).

---

## Testing

- Unit tests in `llm_test.go`:
  - `buildRequestBody` with `api_flavour: "openai"` → messages[0] is system role, no `system` field
  - `buildRequestBody` with `api_flavour: "anthropic"`, `use_prompt_cache: false` → `system` is a JSON string, no cache_control, messages has no system entry
  - `buildRequestBody` with `api_flavour: "anthropic"`, `use_prompt_cache: true` → `system` is a content-block array with `cache_control`
  - `calcCostWithCache` with cache hits → correct cheaper rate applied
  - `calcCostWithCache` with cache writes → correct 1.25x rate applied
- No integration test against live Anthropic API (requires API key, not in CI scope).

---

## Files changed

| File | Change |
|---|---|
| `internal/provider/llm/llm.go` | Config fields, wire format branching, new struct types, cost calculation |
| `internal/provider/llm/llm_test.go` | Unit tests for new behaviour |
| `web/src/pages/SettingsPage.tsx` | API flavour dropdown, caching toggle, max_tokens field |

No migrations. No new packages. No changes to `provider.go`, `model.go`, or other adapters.
