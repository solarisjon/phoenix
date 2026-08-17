// Package provider defines the core Provider interface and shared types used
// by all provider adapters (LLM endpoints, coding agents, etc.).
package provider

import (
	"context"
	"encoding/json"
	"net"
	"net/url"
	"strings"
)

// Message is a single turn in a conversation history.
type Message struct {
	Role    string `json:"role"` // "system", "user", "assistant"
	Content string `json:"content"`
}

// TaskRequest is the input sent to a provider for execution.
type TaskRequest struct {
	// SystemPrompt is the fully assembled agent system prompt
	// (persona + instructions + guardrails).
	SystemPrompt string

	// Prompt is the user-facing task description / instruction.
	Prompt string

	// Context holds prior conversation turns for multi-turn tasks.
	Context []Message

	// WorkingDir is an optional filesystem path for coding agents to use as
	// the working directory when spawning their subprocess. Empty = default
	// (adapter's own default, usually the process working directory).
	WorkingDir string

	// ---- Generation controls (all optional; adapters ignore what they can't honour) ----

	// MaxOutputTokens caps the number of tokens the model may generate
	// (OpenAI max_tokens, Anthropic max_tokens, Ollama num_predict, llama.cpp
	// n_predict). 0 = adapter/config default. Important for local models,
	// whose servers often default to an unbounded output length.
	MaxOutputTokens int

	// Temperature overrides the sampling temperature. nil = adapter/model default.
	Temperature *float64

	// StopSequences are strings that terminate generation when emitted.
	StopSequences []string

	// ResponseSchema, when set, is a JSON Schema the output must satisfy.
	// Adapters whose backend supports constrained decoding (llama.cpp
	// json_schema/grammar, Ollama format, OpenAI response_format) enforce it;
	// others ignore it and callers must parse tolerantly.
	ResponseSchema json.RawMessage
}

// TaskResponse is the result returned by a provider after execution.
type TaskResponse struct {
	Output    string  // Text output from the model/agent.
	TokensIn  int     // Input tokens consumed (0 if unavailable).
	TokensOut int     // Output tokens produced (0 if unavailable).
	CostUSD   float64 // Calculated cost (0 if unavailable).
}

// StreamChunk is a single chunk of streaming output.
type StreamChunk struct {
	Content   string  // Partial text content.
	Done      bool    // True on the final chunk.
	Error     error   // Non-nil if the stream encountered an error.
	PID       int     // OS process ID of the subprocess (sent once, on stream start). 0 if not applicable.
	TokensIn  int     // Input tokens consumed (non-zero only on the final Done chunk, when available).
	TokensOut int     // Output tokens produced (non-zero only on the final Done chunk, when available).
	CostUSD   float64 // Actual cost in USD (non-zero only on the final Done chunk, when the provider reports it directly).
}

// CostEstimate is a best-effort cost prediction before execution.
type CostEstimate struct {
	EstimatedCostUSD float64
}

// ModelLister is an optional interface that providers can implement to
// return the list of available models. Callers should type-assert before use.
type ModelLister interface {
	ListModels(ctx context.Context) ([]string, error)
}

// Pinger is an optional interface for providers that support a lightweight
// connectivity check without running a full inference request. When a provider
// implements Pinger the health checker calls Ping instead of Execute, avoiding
// expensive model warm-up or long thinking-model response times.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Capabilities describes what the backend behind an adapter can do, so the
// runner can adapt prompt assembly and parsing (prompt budgeting, structured
// output, concurrency) to the model actually in use. Zero values mean
// "unknown / not supported" and preserve today's behaviour.
type Capabilities struct {
	// ContextWindow is the usable prompt+output token budget per request
	// (per-slot n_ctx for llama.cpp). 0 = unknown.
	ContextWindow int
	// MaxOutputTokens is a hard output cap if the backend enforces one. 0 = unknown.
	MaxOutputTokens int
	// SupportsJSONSchema: response_format=json_schema / grammar-constrained output.
	SupportsJSONSchema bool
	// SupportsJSONMode: response_format=json_object only.
	SupportsJSONMode bool
	// SupportsTools: native tool calling (informational for now).
	SupportsTools bool
	// Local: self-hosted — no per-token cost, latency dominated by hardware.
	Local bool
	// Reasoning: a thinking model whose chain-of-thought must be budgeted/stripped.
	Reasoning bool
	// ExactTokenCount: TokenCounter (if implemented) is exact rather than heuristic.
	ExactTokenCount bool
	// Slots is the number of concurrent requests the backend can serve
	// (llama-server --parallel). 0 = unknown/unbounded.
	Slots int
	// Model is the backend's own name for the loaded model, if it reports one.
	Model string
}

// Capable is an optional interface for adapters that can describe their
// backend. Callers should type-assert before use.
type Capable interface {
	Capabilities(ctx context.Context) Capabilities
}

// TokenCounter is an optional interface for adapters that can count tokens
// for the model behind them (exactly, via a server tokenizer endpoint, or
// heuristically). Callers should type-assert before use and fall back to
// HeuristicTokenCount otherwise.
type TokenCounter interface {
	CountTokens(ctx context.Context, text string) (int, error)
}

// SlotLimiter is an optional interface for adapters whose backend can only
// serve a bounded number of concurrent requests. The runner uses it to keep
// excess tasks queued in Phoenix rather than blocking inside the server.
type SlotLimiter interface {
	MaxConcurrent() int
}

// HeuristicTokenCount is the shared fallback estimate used when an adapter
// does not implement TokenCounter: roughly 4 characters per token, plus a
// small per-call overhead. Deliberately conservative (rounds up).
func HeuristicTokenCount(s string) int {
	if s == "" {
		return 0
	}
	return (len(s) + 3) / 4
}

// Provider is the common interface implemented by all provider adapters.
type Provider interface {
	// Execute runs a task to completion and returns the full response.
	Execute(ctx context.Context, req TaskRequest) (TaskResponse, error)

	// StreamExecute runs a task and streams output chunks over the returned channel.
	// The channel is closed when the stream ends (Done=true or Error set).
	StreamExecute(ctx context.Context, req TaskRequest) (<-chan StreamChunk, error)

	// EstimateCost returns a best-effort cost prediction for the given request.
	// Returns zero if cost estimation is not supported.
	EstimateCost(req TaskRequest) CostEstimate
}

// IsLocalEndpoint reports whether a URL (or bare host[:port]) points at a
// machine-local or private-network address — loopback, *.local, *.localhost,
// or RFC 1918 / link-local ranges. Adapters use it to pick friendlier
// defaults for self-hosted model servers (longer timeouts, zero cost).
func IsLocalEndpoint(rawURL string) bool {
	host := rawURL
	if u, err := url.Parse(rawURL); err == nil && u.Host != "" {
		host = u.Hostname()
	} else if h, _, err := net.SplitHostPort(rawURL); err == nil {
		host = h
	}
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()
	}
	return false
}
