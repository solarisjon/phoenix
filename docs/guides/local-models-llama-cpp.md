# Run Phoenix on Local Models with llama.cpp

Use `llama-server` (from [llama.cpp](https://github.com/ggml-org/llama.cpp)) as a Phoenix provider so agents run entirely on your own machine — no API keys, no per-token cost, no data leaving the box.

Phoenix has a **native llama.cpp provider** (`🦙 llama.cpp` in the provider type dropdown) that reads the server's context window and slot count from `/props`, counts tokens exactly via `/tokenize`, keeps the KV cache warm with `cache_prompt`, and can ask llama-server for grammar-constrained JSON. This guide covers setup and the settings that matter most for smaller models. The remaining roadmap (prompt budgeting, compact prompt profile, helper-model routing, model eval harness) is in [`docs/superpowers/specs/2026-08-17-local-models-design.md`](../superpowers/specs/2026-08-17-local-models-design.md).

> **Ollama users:** Phoenix also has a native `ollama` adapter. Most of the model-choice and prompt-size advice below applies equally; the provider config differs — see [Ollama notes](#ollama-instead-of-llama-cpp) at the end.

---

## 1. Install llama.cpp

```bash
brew install llama.cpp        # macOS (Apple Silicon: Metal acceleration is built in)
llama-server --version
```

Linux/Windows: grab a release from the llama.cpp GitHub releases page or build from source with `cmake`. Make sure you build with GPU support (`-DGGML_CUDA=ON`, `-DGGML_VULKAN=ON`, etc.) if you have one.

---

## 2. Pick a model

Phoenix asks a model to do two different things: **follow instructions** (the agent's persona and task) and **emit protocol markers** (`MEMO_START…MEMO_END`, `HEALTH_SIGNAL:`, `ARTIFACT_START…`, `NEXT_ACTION:`) that Phoenix parses. Small models are fine at the first and unreliable at the second unless you help them (see §5). Instruction-tuned models from the last year or so handle both acceptably at 7B+.

Rule of thumb for **unified-memory Macs**: model file size (GGUF at Q4_K_M) + KV cache must fit comfortably in RAM alongside Phoenix and your other apps.

| Machine RAM | Comfortable size | Example models (Q4_K_M) | Approx. file |
|---|---|---|---|
| 16 GB | 7–8B | `Qwen3-8B`, `Llama-3.1-8B-Instruct`, `Qwen2.5-Coder-7B-Instruct` | 4.5–5 GB |
| 24 GB | 8–14B | `Qwen3-14B`, `Gemma-3-12B-it`, `Phi-4` (14B) | 8–9 GB |
| 32 GB | 14–24B | `Mistral-Small-3.x-24B`, `Qwen3-30B-A3B` (MoE — fast) | 14–18 GB |
| 64 GB+ | 27–70B | `Gemma-3-27B`, `Llama-3.3-70B` (Q4 ≈ 40 GB) | 16–40 GB |

Recommendations for Phoenix workloads specifically:

- **General agent / monitor work:** `Qwen3-8B` or `Qwen3-14B`. Strong instruction following, good at emitting exact markers. They are *thinking* models — see the reasoning flags in §3.
- **Structured/JSON output (orchestrator, summariser):** `Qwen2.5-7B-Instruct` / `Qwen3` with grammar (§5).
- **Fast helper for summarisation and extraction:** `Qwen3-4B`, `Llama-3.2-3B-Instruct`, `Gemma-3-4B-it`. Cheap to keep resident alongside a bigger model.
- **Coding-flavoured projects (via a coding-agent provider such as opencode/pi pointing at llama-server):** `Qwen2.5-Coder-14B-Instruct`.

Quantisation: `Q4_K_M` is the sweet spot. `Q5_K_M`/`Q6_K` are noticeably better at following formats if you have RAM to spare; `Q3`/`IQ2` and below degrade instruction following sharply and are not recommended for Phoenix.

Download directly from Hugging Face with `-hf` (cached under `~/Library/Caches/llama.cpp` on macOS):

```bash
llama-server -hf Qwen/Qwen3-8B-GGUF:Q4_K_M --port 8081
```

---

## 3. Start `llama-server`

> ⚠️ **Port clash.** Both Phoenix and `llama-server` default to port **8080**. Always give llama-server a different port (`--port 8081` in these examples).

A good starting command for a 24 GB Mac:

```bash
llama-server \
  -hf Qwen/Qwen3-8B-GGUF:Q4_K_M \
  --port 8081 \
  --ctx-size 16384 \
  --parallel 2 \
  --n-gpu-layers 99 \
  --flash-attn on \
  --cache-reuse 256 \
  --jinja \
  --reasoning-format deepseek \
  --reasoning-budget 2048 \
  --metrics
```

What each flag does and why it matters for Phoenix:

| Flag | Why |
|---|---|
| `--ctx-size 16384` | **Total** context (prompt + output) shared across slots. Phoenix system prompts start ~350 tokens and grow with skills, memories, and follow-up history; task outputs can be several thousand tokens. 8k is a floor, 16k is comfortable, 32k if RAM allows. Phoenix will not (yet) truncate to fit — see §4. |
| `--parallel 2` | Number of concurrent request slots. Each slot gets `ctx-size / parallel` tokens of context. Match this to how many agents you expect to run at once (`max_concurrent` per agent). If unsure, use `1` and let Phoenix queue. |
| `--n-gpu-layers 99` | Offload everything to Metal/CUDA. |
| `--flash-attn on` | Lower KV-cache memory, faster long prompts. |
| `--cache-reuse 256` | Reuse KV cache for shared prompt prefixes. Phoenix sends the same system prompt for every task of an agent, so this makes the second-and-later runs much faster. |
| `--jinja` | Use the model's own chat template (needed for correct system-prompt handling and tool-call formats on newer models). |
| `--reasoning-format deepseek` | For thinking models (Qwen3, DeepSeek-R1 distils, etc.): strips `<think>…</think>` out of `content` and puts it in `reasoning_content`, so Phoenix's task output stays clean. Without this, the think block lands in the task output. |
| `--reasoning-budget 2048` | Caps how long a thinking model may ruminate before answering — keeps monitor runs from taking minutes. Use `--reasoning off` to disable thinking entirely for speed. |
| `--metrics` | Prometheus `/metrics` endpoint (tokens/sec, KV usage). Handy when tuning. |

Check it is up:

```bash
curl -s http://localhost:8081/health
```

```bash
curl -s http://localhost:8081/props | jq '.default_generation_settings.n_ctx, .model_path'
```

`/props` reports the **per-slot** context size (`n_ctx`), which is the number Phoenix ultimately has to fit within.

### Serving more than one model (router mode)

Recent llama.cpp builds can serve several models from one process and load them on demand — useful for the "big model plans, small model summarises" pattern that Phoenix's orchestrator and summariser can exploit:

```bash
llama-server --port 8081 --models-dir ~/models --models-max 2 --models-autoload
```

Each model then appears under `/v1/models` and is selected per request by the `model` field — exactly what a Phoenix provider's `model` (or per-agent `model_override`) sets. Phoenix's llama.cpp provider understands router mode: it reads the *per-model* context window and slot count from `/props?model=…` once the model is loaded, and never forces a model to load from a background health check (the first task does that). Create one Phoenix provider per model you want agents to use (e.g. "llama 0.6B" for cheap monitors, "llama 14B" for real work).

---

## 4. Add the provider in Phoenix

**Providers → Add Provider → type 🦙 llama.cpp (llama-server, local)**.

| Field | What to put |
|---|---|
| llama-server URL | `http://localhost:8081` (no `/v1`) |
| Model | pick from the dropdown (populated from `/v1/models`); optional for single-model servers, required in router mode |
| API key | only if you started llama-server with `--api-key` |
| Context window override | leave blank — Phoenix probes the per-slot `n_ctx` from `/props` |
| Max output tokens | blank = 4096. Keeps a looping model from filling the context |
| Max concurrent | blank = the server's slot count (`--parallel`). Extra tasks queue **in Phoenix** rather than inside llama-server, so their timeout doesn't tick while waiting |
| Timeout | blank = 900 s |
| Reuse KV cache | on (default) — repeat runs of the same agent skip re-processing the system prompt |
| Show thinking tokens | off unless you want `<think>` blocks in task output (needs `--reasoning-format deepseek` on the server) |

Equivalent JSON config (what gets stored):

```json
{
  "kind": "llamacpp",
  "base_url": "http://localhost:8081",
  "model": "Qwen3-8B-GGUF:Q4_K_M",
  "max_output_tokens": 4096,
  "cache_prompt": true
}
```

Click **Test** on the provider card (hits `/health` — instant, no inference). Then create an agent on it and run a quick task (⌘K) such as *"Reply with the single word OK."* to confirm streaming works end to end. Costs show as $0, which is correct.

**Using the generic LLM endpoint instead.** You can still add llama-server as an *LLM Endpoint (OpenAI-compatible)* with `endpoint: http://localhost:8081/v1` — that path works, but Phoenix then knows nothing about the server's context window or slots. Prefer the native type.

### Populate the model pool (optional but recommended)

If you use the orchestrator / dynamic model selection, add entries to the provider's **Allowed Models** with a sensible `capability_tier`:

- 3–4B → `fast`
- 7–14B → `standard`
- 24B+ or a strong reasoning model → `powerful` / `planning`

Phoenix's `SelectModelForDomain` picks the *cheapest* model that meets the tier a task needs; with local models all costs are 0, so tiers are what actually drive selection.

---

## 5. Getting reliable output from small models

Until the roadmap items land, these settings and habits make the biggest difference:

**Keep prompts lean.**
- Put concise agent behaviour text in *Behaviour*; small models lose the thread past ~1–2k tokens of instructions.
- Avoid enabling every optional injection at once (spawn + hire + Obsidian + skills + memories + ReAct loop). Each adds several hundred tokens and another output protocol the model has to remember.
- On long follow-up threads, start a new task rather than replying in-thread; Phoenix summarises old turns after ~8k characters, but the two most recent turns are always sent verbatim.
- Skills: prefer one focused skill per project over stacking several.

**Prefer explicit formats.** Small models comply much better with *"Respond in exactly this format"* than with *"you MAY optionally…"*. When writing task descriptions for monitors, restate the expected `HEALTH_SIGNAL:` line at the end of the description. Phoenix's parsers tolerate the common paraphrases (`**Health signal** — needs_attention`, `health_signal: …`, `Memo Start`), but the marker must still begin its own line.

**Turn thinking off (or budget it) for monitors.** Cron-style monitor checks rarely need extended reasoning; `--reasoning off` or a small `--reasoning-budget` keeps runs quick and reduces the chance the model spends its context on thoughts and truncates the answer.

**Set `max_tokens`.** llama-server's default `n_predict` is unlimited; a model that loops will run until the slot's context is exhausted. `4096` is plenty for most Phoenix tasks. (Since Phase 0 the `llm` provider form has *Max output tokens* and *Temperature* fields and sends them on the wire.)

**Watch the slot context.** If tasks fail with a 400 mentioning context size, or outputs stop mid-sentence, the prompt + output exceeded `n_ctx / parallel`. Raise `--ctx-size`, lower `--parallel`, or trim the agent's prompt. (Automatic prompt budgeting against the probed context window is Phase 2 of the roadmap.)

**Structured JSON.** The native provider passes a JSON schema to llama-server (`response_format: json_schema`, compiled to a GBNF grammar) whenever Phoenix sets one on a request. Wiring the orchestrator plan, agent generation and health classifier to actually send schemas is Phase 4 of the roadmap.

---

## 6. Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| Provider health dot red, "connection refused" | llama-server not running / wrong port | `curl localhost:8081/health`; check `--port` |
| Task fails after ~60 s with a timeout | Adapter default timeout | Set `timeout_seconds` (§4) |
| Output contains `<think>…</think>` | Thinking model without reasoning-format flag | Add `--reasoning-format deepseek` (or `--reasoning off`) |
| Output cut off / ends abruptly | Hit `max_tokens` or slot context | Raise `max_tokens`; raise `--ctx-size` or lower `--parallel` |
| First task very slow, later ones fast | Model load + no cache reuse | Expected on cold start; add `--cache-reuse 256`; keep server running |
| Two agents run but one waits | `--parallel 1` | Raise `--parallel` (each slot needs its own KV memory) |
| Monitor shows no health signal / memo not extracted | Model didn't emit exact marker | Restate the format in the task description; use a ≥7B Q4_K_M+ model; check the raw output in the task detail |
| `HTTP 400 … tokens` from llama-server | Prompt alone exceeds slot context | Trim prompt / raise ctx-size |
| Machine swapping, everything slow | Model + KV cache > free RAM | Smaller quant, lower `--ctx-size`, or fewer `--parallel` slots |

---

## Ollama instead of llama.cpp

Ollama wraps llama.cpp and Phoenix has a dedicated adapter for it (`kind: ollama`). Config:

```json
{ "base_url": "http://localhost:11434", "model": "qwen3:8b", "num_ctx": 16384, "num_predict": 4096 }
```

Differences to be aware of:

- Ollama's **default context window is small** (historically 2048–4096 tokens) regardless of the model's capability. Set **Context window (num_ctx)** on the Phoenix Ollama provider (8192–16384 recommended); Phoenix sends it per request. Alternatively set it globally with `OLLAMA_CONTEXT_LENGTH=16384` before `ollama serve`. Phoenix also sends `num_predict` (default 4096) so a looping model can't fill the context.
- Thinking tokens are stripped by default (`keep_thinking: false`).
- The `llm` adapter *also* works against Ollama's OpenAI-compatible endpoint (`http://localhost:11434/v1`) if you prefer one code path.

---

## What's coming

See the design spec for the full plan. Next: **prompt budgeting** against the probed context window (with a visible "prompt trimmed" note), a **compact prompt profile** for small models, a **helper-model** setting so summaries/classification run on a small fast model while agents use a bigger one (llama-server router mode fits this perfectly), schemas on every JSON-producing call, and an **evaluation harness** that scores how well a given local model handles Phoenix's protocols before you trust it with a monitor.
