// Package provider defines the core Provider interface and shared types used
// by all provider adapters (LLM endpoints, coding agents, etc.).
package provider

import "context"

// Message is a single turn in a conversation history.
type Message struct {
	Role    string `json:"role"`    // "system", "user", "assistant"
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
	Content string // Partial text content.
	Done    bool   // True on the final chunk.
	Error   error  // Non-nil if the stream encountered an error.
}

// CostEstimate is a best-effort cost prediction before execution.
type CostEstimate struct {
	EstimatedCostUSD float64
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
