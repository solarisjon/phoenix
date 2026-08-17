// Package openaiwire holds the OpenAI chat-completions wire format shared by
// every adapter that speaks it: the hosted `llm` adapter, the native
// `llamacpp` adapter (llama-server's /v1/chat/completions), and anything
// OpenAI-compatible added later (vLLM, LM Studio, …).
//
// It deliberately contains no HTTP client, cost logic, or config — only the
// request/response structs and the SSE stream reader — so adapters can differ
// in auth, pricing, endpoints and extensions while sharing one tested parser.
package openaiwire

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/solarisjon/phoenix/internal/provider"
)

// ---- Request ----

// StreamOptions asks the server to include a usage object in the stream.
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// ChatMessage is one turn in the messages array.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ResponseFormat constrains the model's output. Type is "text", "json_object"
// or "json_schema"; JSONSchema is required for the latter.
type ResponseFormat struct {
	Type       string          `json:"type"`
	JSONSchema *JSONSchemaSpec `json:"json_schema,omitempty"`
}

// JSONSchemaSpec wraps a JSON Schema for response_format=json_schema.
// llama-server and OpenAI both accept this shape; llama-server compiles it to
// a GBNF grammar so the output is guaranteed to validate.
type JSONSchemaSpec struct {
	Name   string          `json:"name"`
	Schema json.RawMessage `json:"schema"`
	Strict bool            `json:"strict,omitempty"`
}

// ChatRequest is the POST body. Fields tagged omitempty are only sent when set
// so hosted defaults are untouched. System / StopSequences exist for the
// Anthropic flavour of the `llm` adapter, which reuses this struct.
type ChatRequest struct {
	Model         string          `json:"model"`
	System        json.RawMessage `json:"system,omitempty"`
	Messages      []ChatMessage   `json:"messages"`
	Stream        bool            `json:"stream"`
	StreamOptions *StreamOptions  `json:"stream_options,omitempty"`
	MaxTokens     int             `json:"max_tokens,omitempty"`
	Temperature   *float64        `json:"temperature,omitempty"`
	// Stop is the OpenAI-style stop list; StopSequences is the Anthropic name.
	// Only one is populated depending on flavour.
	Stop           []string        `json:"stop,omitempty"`
	StopSequences  []string        `json:"stop_sequences,omitempty"`
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`

	// CachePrompt is a llama-server extension: reuse the KV cache for the
	// common prefix of consecutive requests. Ignored by other servers.
	CachePrompt *bool `json:"cache_prompt,omitempty"`
}

// ---- Non-streaming response ----

// Completion is the parsed non-streaming response.
type Completion struct {
	Choices []struct {
		Message struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"` // llama-server --reasoning-format deepseek
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens             int `json:"prompt_tokens"`
		CompletionTokens         int `json:"completion_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	} `json:"usage"`
}

// ParseCompletion decodes a non-streaming chat completion body.
func ParseCompletion(raw []byte) (Completion, error) {
	var c Completion
	if err := json.Unmarshal(raw, &c); err != nil {
		return Completion{}, fmt.Errorf("parse completion: %w", err)
	}
	return c, nil
}

// Text returns the first choice's content ("" if none).
func (c Completion) Text() string {
	if len(c.Choices) == 0 {
		return ""
	}
	return c.Choices[0].Message.Content
}

// Reasoning returns the first choice's reasoning_content ("" if none).
func (c Completion) Reasoning() string {
	if len(c.Choices) == 0 {
		return ""
	}
	return c.Choices[0].Message.ReasoningContent
}

// ---- Streaming ----

// streamDelta is one SSE data payload.
type streamDelta struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// Usage is the token accounting collected from a stream.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	FinishReason     string // last non-empty finish_reason seen ("stop", "length", …)
}

// SSEOptions tunes ReadSSE.
type SSEOptions struct {
	// IncludeReasoning forwards reasoning_content deltas as normal content.
	// Off by default — chain-of-thought should not land in task output.
	IncludeReasoning bool
	// Finish builds the terminal Done chunk from the collected usage. Adapters
	// use it to attach cost. If nil, a zero-cost Done chunk is emitted.
	Finish func(u Usage) provider.StreamChunk
}

// ReadSSE consumes an OpenAI-style SSE body ("data: {…}" lines terminated by
// "data: [DONE]"), forwarding content deltas to ch and finishing with exactly
// one Done chunk (or an Error chunk on read failure / context cancellation).
// It never closes ch — the caller owns the channel.
func ReadSSE(ctx context.Context, body io.Reader, ch chan<- provider.StreamChunk, opts SSEOptions) {
	scanner := bufio.NewScanner(body)
	// Large single deltas (e.g. a whole reasoning block) can exceed the
	// default 64 KB token limit.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var u Usage
	finish := func() provider.StreamChunk {
		if opts.Finish != nil {
			return opts.Finish(u)
		}
		return provider.StreamChunk{Done: true, TokensIn: u.PromptTokens, TokensOut: u.CompletionTokens}
	}

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			ch <- provider.StreamChunk{Error: ctx.Err(), Done: true}
			return
		default:
		}

		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			ch <- finish()
			return
		}

		var delta streamDelta
		if err := json.Unmarshal([]byte(data), &delta); err != nil {
			continue // skip malformed chunks
		}

		if len(delta.Choices) > 0 {
			d := delta.Choices[0]
			if d.Delta.Content != "" {
				ch <- provider.StreamChunk{Content: d.Delta.Content}
			} else if opts.IncludeReasoning && d.Delta.ReasoningContent != "" {
				ch <- provider.StreamChunk{Content: d.Delta.ReasoningContent}
			}
			if d.FinishReason != "" {
				u.FinishReason = d.FinishReason
			}
		}
		// Capture usage when the provider includes it (requires stream_options.include_usage).
		if delta.Usage != nil {
			u.PromptTokens = delta.Usage.PromptTokens
			u.CompletionTokens = delta.Usage.CompletionTokens
		}
	}

	if err := scanner.Err(); err != nil {
		ch <- provider.StreamChunk{Error: fmt.Errorf("stream read: %w", err), Done: true}
		return
	}
	ch <- finish()
}

// Truncate shortens s to at most n bytes for error messages.
func Truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
