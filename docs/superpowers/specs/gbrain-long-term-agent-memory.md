# GBrain and the Long-Term Agent Memory Problem: A Deep Technical Guide

> **Last updated:** June 2026  
> **Source:** Primary research from [garrytan/gbrain](https://github.com/garrytan/gbrain) — 24.1k stars, MIT license

---

## The Problem: Agents Are Amnesiac by Design

Every AI agent deployed today faces the same fundamental constraint: the context window. Whatever doesn't fit inside the current context doesn't exist. An agent finishing a task today starts the next one with zero institutional memory. It re-reads the same docs, asks the same clarifying questions, makes the same mistakes.

This is not a model capability problem. Claude Opus and GPT-5 are remarkable reasoning engines. The problem is the absence of a durable, structured memory layer between the agent and the world it operates in.

For a coding agent, the impact is clear: it forgets the architectural decisions from two weeks ago, doesn't know about the incident that caused a refactor, and has no sense of context across a long-running project. For an executive agent or personal assistant, the gap is even worse — it can't synthesize across hundreds of past interactions to answer a simple question like "what's still open between me and Alice?"

This is the problem gbrain is designed to solve.

---

## What Is GBrain?

**GBrain** is an open-source, self-hostable personal and team knowledge brain built by **Garry Tan**, President and CEO of Y Combinator. The repository lives at [github.com/garrytan/gbrain](https://github.com/garrytan/gbrain) (24.1k stars, 3.5k forks as of mid-2026).

Tan built it to run his own AI agents. By his account, his production brain holds 146,646 pages, 24,585 people, and 5,339 companies, with 66 cron jobs running autonomously. His agent ingests meetings, emails, tweets, and voice calls while he sleeps.

The core pitch is simple: **Search gives you raw pages. GBrain gives you the answer.**

That distinction — retrieved chunks vs. synthesized answer — is the architectural heart of the project. Most personal-knowledge tools give you a ranked list of pages to read yourself. GBrain reads them and writes the answer, with citations and an explicit note about what the brain doesn't know yet.

### What problem does it solve?

GBrain solves three distinct memory problems simultaneously:

1. **The retrieval problem:** Finding the right information across large, heterogeneous knowledge bases (people, companies, meetings, emails, notes, ideas).
2. **The synthesis problem:** Converting retrieved chunks into a coherent, cited answer rather than a list of documents to skim.
3. **The staleness problem:** Identifying when knowledge is missing, contradictory, or outdated — the "gap analysis" that tells an agent what to go verify.

---

## Architecture: How GBrain Works Under the Hood

GBrain's architecture is best understood as four layers stacked on top of each other.

### Layer 1: Storage — Git + Postgres (Two Engines, One Contract)

The fundamental data model is Markdown files in a git repository. Your knowledge lives as `.md` files — a format that is human-readable, versionable, diff-able, and LLM-native.

GBrain then syncs that repository into a Postgres-compatible database for retrieval:

- **PGLite** (Postgres 17 via WASM): Zero-config, no Docker, runs in-process. Handles personal brains up to ~50K pages. Spins up in 2 seconds with `gbrain init --pglite`.
- **Postgres + pgvector** (Supabase or self-hosted): For shared, large, or multi-machine deployments. Required for team deployments beyond single-user scale.

The contract between these two engines is defined by the `BrainEngine` interface (`src/core/engine.ts`), which specifies ~47 operations both implementations must satisfy. This means the CLI and MCP server are generated from a single source and work identically against either backend.

**Key implication:** Deleting a file from the git repo becomes a soft-delete in the database. Your knowledge is auditable and reversible.

### Layer 2: The Four-Strategy Retrieval Pipeline

Most RAG systems use vector search alone, which fails on a significant class of real queries. GBrain layers four strategies:

```
intent classify
       │
       ▼
hybrid search:
   ├── Vector (HNSW on pgvector)  — semantic similarity
   ├── BM25 keyword (tsvector)    — lexical match
   ├── Source-aware re-rank       — content quality boosting
   └── RRF fusion → top 30 candidates
       │
       ▼
graph augment (typed-edge traversal)
       │
       ▼
ZeroEntropy reranker (zerank-2 cross-encoder)
       │
       ▼
token-budget enforcement + deduplication
       │
       ▼
results
```

**Why each layer exists:**

- **Vector alone** fails on factual relationship queries. "Companies in my portfolio" returns essays about portfolios, not company pages.
- **BM25 alone** is brittle to synonyms. "Who works on retrieval?" misses pages that say "search ranking."
- **Graph traversal** catches causal chains that embeddings can't encode: `bob ── invested_in ──> company ── dated ──> Q1` answers "what did Bob invest in this quarter?" without any semantic similarity.
- **Reciprocal Rank Fusion** merges rankings without globally overweighting either strategy.
- **The reranker** (ZeroEntropy `zerank-2`) reshuffles 60% of top-1 results by reading query + candidate jointly with full attention.

**The benchmark result:** On BrainBench (240-page Opus-generated corpus), the full stack achieves **P@5 = 49.1%, R@5 = 97.9%** — compared to ~18% P@5 for vector-only RAG or ripgrep-BM25 alone. The +31 P@5 points come primarily from the knowledge graph.

### Layer 3: The Self-Wiring Knowledge Graph

This is gbrain's most distinctive technical feature. On every page write, gbrain extracts entity references and writes typed graph edges — with **zero LLM calls**.

The extraction uses three regexes:
- Standard markdown links: `[Garry Tan](wiki/people/garry-tan)`
- Obsidian wikilinks: `[[wiki/people/garry-tan|Garry Tan]]`
- Typed-link blockquotes for explicit edge typing

Typed edges include: `attended`, `works_at`, `invested_in`, `founded`, `advises`, `mentions`.

The SQL batch write (`addLinksBatch`) uses `INSERT ... SELECT FROM jsonb_to_recordset(...) JOIN pages ON CONFLICT DO NOTHING`, making it concurrent-safe and fast. On a 17K-page brain, full graph extraction completes in seconds.

This zero-LLM-cost graph construction is what makes the architecture economically viable at scale. The graph grows as a side-effect of writing pages — it doesn't require a dedicated enrichment pass.

### Layer 4: The Synthesis Layer (`gbrain think`)

GBrain exposes two query modes that serve different purposes:

```bash
# Raw retrieval: fast, no LLM cost, returns ranked pages
gbrain search "who's working on AI agents at portfolio companies?"

# Synthesis: retrieval + LLM composition, returns an actual answer
gbrain think "who's working on AI agents at portfolio companies?"
```

The synthesis layer runs the full retrieval pipeline, then passes the results to an LLM to compose a single coherent answer with:
- Inline citations to source pages
- An explicit "gap analysis" noting what the brain doesn't know
- Contradiction detection when two sources conflict

The gap analysis is the differentiator. Knowing what your brain doesn't know is as valuable as knowing what it does.

### The Dream Cycle

GBrain ships with a cron-driven background enrichment loop (the "dream cycle") that runs while you sleep:

- Deduplicates person and company pages
- Fixes broken citations
- Scores page salience
- Finds contradictions between pages
- Preps task lists for the next day

This is what makes the knowledge base self-improving over time rather than a static dump that decays.

---

## Schema Packs: Giving Your Brain a Shape

Most knowledge tools force a fixed layout. GBrain ships with bundled schema packs and lets you define your own:

| Pack | Description |
|---|---|
| `gbrain-base-v2` | Default. 15-type taxonomy: `person`, `company`, `media`, `tweet`, `analysis`, `concept`, `deal`, `email`, `slack`, `project`, `note`, and more |
| `gbrain-base` | Legacy 24-type layout, kept for back-compat |
| `gbrain-recommended` | Extends `gbrain-base` with 13 additional directories |
| Custom | Define via `gbrain schema detect` + `gbrain schema suggest` |

The active schema pack threads through every read and write path. Switch packs and the brain re-interprets itself. The cache key includes the pack name and version, so cross-pack contamination is structurally impossible.

---

## Practical Implementation Guide

### Prerequisites

- Bun (not npm — gbrain uses Bun as its runtime)
- An embedding provider API key (OpenAI, ZeroEntropy, Voyage, or Ollama for local)

### Option A: Local Brain (Zero Server, Zero Docker)

This is the recommended starting point for a developer integrating gbrain with a coding agent.

```bash
# 1. Install gbrain globally
bun install -g github:garrytan/gbrain

# 2. Initialize a local PGLite brain (2 seconds, no Docker)
#    Set your embedding provider key first:
export OPENAI_API_KEY=sk-...
gbrain init --pglite

# 3. Verify health
gbrain doctor

# 4. Connect to Claude Code (one command)
claude mcp add gbrain -- gbrain serve

# Or connect to Codex:
codex mcp add gbrain -- gbrain serve

# 5. Import existing notes
gbrain import ~/notes/      # imports .md files
gbrain import ~/obsidian/   # Obsidian vaults work natively
```

### Option B: Agent-Driven Install (Full Stack)

Paste this into Claude Code, Codex, Cursor, or any agent that can read files over HTTPS:

```
Retrieve and follow the instructions at:
https://raw.githubusercontent.com/garrytan/gbrain/master/INSTALL_FOR_AGENTS.md
```

The agent installs gbrain, creates the brain, asks for API keys, loads 43 skills, configures the dream cycle, and verifies the install end-to-end. Takes ~30 minutes.

### Option C: Remote Brain (Team or Multi-Machine)

```bash
# Serve gbrain over HTTP with OAuth 2.1
gbrain serve --http

# Connect a laptop coding agent to a remote brain
gbrain connect https://your-host/mcp --token gbrain_xxx --install               # Claude Code
gbrain connect https://your-host/mcp --token gbrain_xxx --agent codex --install # Codex
```

### Ingesting Data

```bash
# Capture a thought
gbrain capture "the architectural decision we made about the queue"

# Capture a file
gbrain capture --file ./notes/today.md

# From stdin
echo "from a pipe" | gbrain capture --stdin

# Webhook ingestion (for Zapier, Apple Shortcuts, IFTTT)
curl -X POST https://your-brain/ingest \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: text/markdown" \
  -d "# A thought from an automation"
```

### Querying

```bash
# Fast search (no LLM cost)
gbrain search "what architectural decisions did we make in March?"

# Full synthesis (costs an LLM call, returns an answer)
gbrain think "what architectural decisions did we make in March?"

# With per-stage attribution (useful for debugging retrieval)
gbrain search "query here" --explain

# Multi-hop graph query
gbrain graph-query "who has Alice worked with at companies in our portfolio?"
```

### MCP Tool Surface

When gbrain is connected via MCP, your agent gets 30+ tools including:

- `search` / `query` — retrieval (cheap-hybrid vs. full-control)
- `put_page` / `get_page` — read/write pages
- `graph_query` — multi-hop graph traversal
- `synthesize` — the think layer as a tool call
- `schema_apply_mutations` — agent-authored schema evolution
- `capture` — capture a thought or document

### Core Integration Pattern

The brain-first protocol (borrowed from gbrain's own `AGENTS.md`):

```markdown
# BRAIN-FIRST PROTOCOL
Before any external search or API call:
1. gbrain search/query the local brain
2. If found and fresh: use it
3. If stale or missing: fetch externally, then gbrain capture the result
4. Never write a duplicate: check create_safety hint before put_page
```

This pattern is what converts gbrain from a retrieval tool into a learning system. Every external fetch that returns useful information is captured back into the brain, growing the corpus organically.

---

## Configuration: `gbrain.yml`

The `gbrain.yml` file at the repo root controls search behavior:

```yaml
search:
  mode: balanced          # conservative | balanced | tokenmax
  reranker:
    enabled: true         # ZeroEntropy zerank-2
  source_boosts:
    "originals/": 1.5
    "your-openclaw/chat/": 0.6
    "archive/": 0.5

link_resolution:
  global_basename: false  # set true for Obsidian bare [[wikilinks]]

embedding:
  provider: zeroentropy   # openai | zeroentropy | voyage | ollama
  model: zerank-2
```

Search modes bundle cost/quality knobs:
- `conservative`: no reranker, no expansion, lowest cost
- `balanced`: reranker on, expansion off (default)
- `tokenmax`: reranker + query expansion, highest recall

---

## Strengths and Limitations

### Strengths

**The synthesis layer is genuinely differentiating.** Returning a cited answer with gap analysis is a qualitatively different capability than ranked document retrieval. For agents that need to reason across large, heterogeneous knowledge bases, this is the key capability.

**Zero-LLM-cost graph construction scales.** The auto-link mechanism means the graph grows without a dedicated enrichment budget. At 100K pages this matters enormously.

**The benchmark results are honest and reproducible.** The BrainBench harness is published in a sibling repo. The methodology document is detailed and the numbers are reproducible. This is uncommon in this space.

**Local-first with a clear path to team deployment.** PGLite for personal use, Postgres for teams, same API contract for both. The OAuth 2.1 + multi-user RBAC for team deployments is production-grade.

**It's Garry Tan's production system.** He runs 146K pages and 66 cron jobs on it. That means bug reports are real-world bugs, not theoretical ones.

**Active development.** The changelog is dense: multi-user RBAC, schema packs, named-thing retrieval improvements, reranker integration, batch retry hardening for Supabase pooler blips — all shipped in v0.41.x.

### Limitations

**The install surface is complex.** Bun, pgvector, an embedding provider key, optional reranker API, optional Postgres — the happy path is smooth, but debugging a broken install requires comfort with the whole stack.

**Markdown-only ingestion.** The core `gbrain import` path handles `.md` files. Other formats (PDF, DOCX, audio transcripts) require skillpack recipes or the community-contributed `AnyFile2Gbrain-skill` converter. This is a real operational friction point.

**PGLite is single-writer.** If you run `gbrain serve` (for MCP connections) and `gbrain sync` concurrently on a PGLite brain, they contend for the write lock. The documentation recommends stopping the server before large syncs. This is a constraint you need to plan around.

**No managed cloud option.** GBrain is fully self-hosted. If you want zero ops, you need to either deploy to Railway/Render/Fly.io yourself or use Supabase as the Postgres backend. There is no gbrain.io SaaS equivalent.

**Skillpack ecosystem is early.** The 43 bundled skills are useful, but extending gbrain's ingestion to new sources (Linear, GitHub, Notion) requires writing skillpack recipes. The contract is documented but the ecosystem is still forming.

**Windows support is uneven.** Several issues in the changelog specifically call out Windows bugs (DNS resolution failures, schema migration hangs). If you're developing on Windows, expect rougher edges.

---

## Alternatives to GBrain

The long-term agent memory space has several strong alternatives. Here is an honest comparison.

### Mem0

**What it is:** A managed memory-as-a-service API focused on user-level and session-level personalization. Backed by Y Combinator (notably, gbrain's creator is YC's CEO — these are not affiliated projects).

**How it works:** Mem0 extracts structured "memories" from conversation history using an LLM, stores them as key-value facts, and retrieves them via semantic search. It uses a single-pass hierarchical distillation algorithm. Integration is three lines of Python:

```python
from mem0 import MemoryClient
client = MemoryClient(api_key=os.getenv("MEM0_API_KEY"))
client.add(messages, user_id="user123")
results = client.search("dietary restrictions?", user_id="user123")
```

**Compared to gbrain:** Mem0 is substantially easier to integrate — it's an API call, not an infrastructure deployment. It's optimized for user personalization in chat applications (customer support, healthcare assistants, tutoring). It does not have a knowledge graph or a synthesis layer. The memory model is fact-extraction from conversations, not general knowledge management across arbitrary document types. SOC 2 Type I compliant; HIPAA available. 90,000+ developers according to their website.

**When to use Mem0:** When you need persistent user context across chat sessions and want a managed service. When your memory needs are user-scoped preferences and facts, not large-scale document retrieval.

**When gbrain is better:** When you have a large, heterogeneous knowledge base (notes, meetings, emails, code), when you need synthesis and gap analysis, when you want local-first and full data control, when your agents need multi-hop reasoning across relationships.

### Zep

**What it is:** Enterprise-grade agent memory infrastructure built around a temporal context graph engine. Positioned at the "Context Lake" layer — millions of context graphs served at sub-200ms p95 latency.

**How it works:** Zep constructs knowledge graphs from any input source (chat history, business data, structured JSON). Graphs are typed (person, organization, topic, product) with temporal validity — when new information contradicts the graph, Zep invalidates the old fact rather than accumulating conflicts. Every fact traces to the source episode that produced it. It also surfaces "Observations" — patterns detected from recurring co-occurrences in the graph (e.g., "Jane upgrades within two weeks of each product launch").

```python
response = client.thread.add_messages(
    thread_id=thread_id,
    messages=[Message(name="Jane", role="user", content="...")],
    return_context=True,
)
user_context = client.thread.get_user_context(thread_id=thread_id)
```

**Compared to gbrain:** Zep and gbrain share the knowledge graph approach but differ in target audience and deployment model. Zep is a managed service with BYOC (Bring Your Own Cloud) options, SOC 2 Type II, HIPAA BAA, and attribute-based access control designed for enterprise multi-tenant deployments. Its benchmark numbers are strong: 94.7% accuracy on LoCoMo, 90.2% on LongMemEval, 155ms retrieval latency. Zep is purpose-built for agent memory in deployed applications; gbrain is purpose-built for personal and team knowledge management.

Zep's temporal validity model (automatic fact invalidation) is more sophisticated than gbrain's contradiction detection (flagging contradictions for human review). Zep's enterprise governance features (retention policies, audit logs, ABAC) have no gbrain equivalent.

**When to use Zep:** Enterprise applications with strict compliance requirements. Multi-tenant platforms where users need isolated memory graphs. Production agents where you need provenance on every fact. When you want managed infrastructure with SLAs.

**When gbrain is better:** When you want full data ownership. When your memory is personal knowledge (notes, meetings, ideas) rather than user interaction data. When you need the synthesis / "give me the answer" layer. When cost control is paramount and you don't want per-call API pricing.

### Letta (formerly MemGPT)

**What it is:** A platform for stateful agents with a tiered memory architecture inspired by operating system memory hierarchy. Originally the MemGPT research project, now commercialized as Letta (23.5k GitHub stars, Apache 2.0).

**How it works:** Letta models agent memory as three tiers:
- **In-context memory:** Core memory blocks (persona, human, notes) that always live in the context window
- **Recall storage:** Searchable database of past interactions
- **Archival storage:** Unlimited long-term storage that the agent can search explicitly

Agents self-manage memory across tiers: when in-context memory fills up, the agent writes to recall or archival storage. The agent can also explicitly recall from storage during reasoning. This gives agents explicit, LLM-visible control over their own memory.

```python
agent_state = client.agents.create(
    model="openai/gpt-5.2",
    memory_blocks=[
        {"label": "human", "value": "Name: Alice. Role: product manager."},
        {"label": "persona", "value": "I am a technical writing assistant."},
    ],
    tools=["web_search", "fetch_webpage"]
)
```

**Compared to gbrain:** Letta and gbrain solve different problems. Letta gives agents self-managed tiered memory within a single agent runtime — the agent decides what to remember and what to let go. GBrain is an external knowledge layer that many agents can all connect to via MCP. Letta's model is agent-centric; gbrain's model is knowledge-centric.

Letta is a better fit when you're building an agent that needs to reason explicitly about its own memory within a single session. GBrain is a better fit when you have a large, pre-existing knowledge corpus that you want to make queryable by agents.

**When to use Letta:** Single-agent deployments where the agent needs to manage its own long-term context. Research environments where memory self-management behavior is part of what you're building. When you want the agent to be the memory manager, not an external system.

**When gbrain is better:** When you have an existing knowledge corpus to query. When multiple agents need to share a common knowledge base. When you need synthesis across heterogeneous documents rather than memory within a conversation thread.

### LangChain Memory Modules

**What it is:** The memory abstraction layer in LangChain, offering several memory types: `ConversationBufferMemory`, `ConversationSummaryMemory`, `VectorStoreRetrieverMemory`, `EntityMemory`, and more.

**How it works:** LangChain memory modules store and retrieve conversation context in a variety of ways. `ConversationBufferMemory` keeps the full message history. `ConversationSummaryMemory` compresses history using an LLM. `VectorStoreRetrieverMemory` stores any text in a vector store and retrieves relevant chunks. `EntityMemory` extracts named entities and maintains a summary per entity.

```python
from langchain.memory import ConversationSummaryBufferMemory
memory = ConversationSummaryBufferMemory(llm=llm, max_token_limit=2000)
conversation = ConversationChain(llm=llm, memory=memory, verbose=True)
```

**Compared to gbrain:** LangChain memory modules are glue code within an LLM pipeline, not a standalone knowledge system. They solve the in-session context management problem. They do not have knowledge graphs, synthesis layers, schema packs, or multi-agent sharing. They're useful and well-understood, but they're not in the same category as gbrain — they're a component rather than a system.

**When to use LangChain memory:** When you're already in the LangChain ecosystem and need a quick in-session memory solution. For prototyping. When your memory needs are simple conversation context management.

**When gbrain is better:** When you need persistent memory across sessions, multi-agent memory sharing, large-scale document retrieval, or knowledge graph traversal.

### Comparison Table

| | **GBrain** | **Mem0** | **Zep** | **Letta** | **LangChain Memory** |
|---|---|---|---|---|---|
| **Storage model** | Markdown + Postgres/PGLite | Managed cloud | Temporal context graph | Tiered (in-context + DB) | Pluggable (buffer, vector, etc.) |
| **Knowledge graph** | Yes (auto-wired, zero LLM) | No | Yes (temporal) | No | No |
| **Synthesis layer** | Yes (`gbrain think`) | No | No | No | No |
| **Managed SaaS** | No (self-hosted) | Yes | Yes (+ BYOC) | Yes (+ OSS) | N/A (library) |
| **Multi-agent sharing** | Yes (MCP) | Yes (user-scoped) | Yes | Limited | No |
| **Local-first** | Yes | No | BYOC only | Yes (OSS) | Yes |
| **Compliance** | Self-managed | SOC 2 T1, HIPAA | SOC 2 T2, HIPAA BAA | Varies | N/A |
| **Setup complexity** | Medium-High | Low | Low-Medium | Medium | Low |
| **Scale ceiling** | ~100K pages (PGLite), unlimited (Postgres) | API-governed | Millions of graphs | DB-governed | Context-window |
| **Primary use case** | Personal + team knowledge management | User personalization | Enterprise agent memory | Stateful single agents | In-session context |
| **License** | MIT | Proprietary | Proprietary | Apache 2.0 (OSS server) | MIT |

---

## Who Should Use GBrain?

**GBrain is a strong fit if:**
- You are building or operating AI agents that need to reason across a large, pre-existing knowledge base (notes, meetings, documents, people, companies)
- You want full data ownership and a local-first architecture
- You need the synthesis layer — actual answers with citations, not ranked document lists
- You are comfortable operating a Bun + Postgres stack
- You want to use MCP to connect multiple agents (Claude Code, Codex, Cursor, Windsurf) to a shared brain

**GBrain is not the right fit if:**
- You need managed infrastructure with an SLA and enterprise compliance certifications → use Zep
- You need a three-line SDK integration for user personalization in a chat product → use Mem0
- You want an agent that self-manages its own memory tiers → use Letta
- You're already deep in the LangChain ecosystem and need something quick → use LangChain memory modules

---

## Conclusion

GBrain occupies a specific and non-trivial position in the agent memory landscape. It is the only open-source project that combines: a self-wiring knowledge graph (at zero LLM cost per write), hybrid retrieval with a proven +31 P@5 lift over vector-only RAG, a synthesis layer that delivers actual answers with gap analysis, and a local-first architecture that scales to team deployments.

The tradeoff is operational complexity. GBrain is not a three-line SDK. It is a full knowledge infrastructure system. For teams that have existing knowledge bases worth querying — years of notes, meetings, people profiles, project history — that complexity is worth paying. For simpler use cases, Mem0 or Zep's managed offerings will get you further faster.

The key insight behind gbrain's design, and behind any effective long-term agent memory system, is this: **retrieval is not the goal, reasoning is.** The goal is an agent that wakes up knowing what happened yesterday, what commitments are still open, what knowledge is missing, and what to do next. That requires synthesis, gap analysis, and a self-wiring knowledge graph — not just a faster vector search.

---

*Sources: [garrytan/gbrain](https://github.com/garrytan/gbrain) (README, DESIGN.md, docs/architecture/RETRIEVAL.md), [mem0.ai](https://mem0.ai), [getzep.com](https://www.getzep.com), [letta-ai/letta](https://github.com/letta-ai/letta), [huytieu/COG-second-brain](https://github.com/huytieu/COG-second-brain).*
