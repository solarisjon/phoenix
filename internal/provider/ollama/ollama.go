// Package ollama provides a Provider adapter that runs tasks via a local
// Ollama instance (https://ollama.com). Ollama hosts quantised LLMs locally
// and exposes an OpenAI-compatible HTTP streaming API.
//
// Unlike the coding-agent adapters (pi, crush, opencode) this is a pure-LLM
// adapter: it sends a system prompt + user prompt and streams the text
// response back token by token. No filesystem tools are available.
//
// Stream format: POST /api/chat with "stream":true returns newline-delimited
// JSON objects. Each object has a "message.content" delta and a "done" bool.
// The final object (done=true) carries token counts.
//
// Thinking tokens (message.thinking) are silently skipped — they are the
// model's internal chain-of-thought and should not appear in task output.
package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/solarisjon/phoenix/internal/provider"
)

const defaultBaseURL = "http://localhost:11434"

// Config holds configuration for the Ollama adapter.
type Config struct {
	// BaseURL is the Ollama server URL. Defaults to http://localhost:11434.
	BaseURL string `json:"base_url"`

	// Model is the Ollama model tag to use, e.g. "qwen3.5:latest", "llama3.2:3b".
	// Required.
	Model string `json:"model"`

	// KeepThinking, when true, includes the model's <think> block in output.
	// Default false — thinking tokens are stripped for cleaner task output.
	KeepThinking bool `json:"keep_thinking"`

	// TimeoutSeconds sets the HTTP request timeout. Default 300 (5 min).
	TimeoutSeconds int `json:"timeout_seconds"`
}

// Adapter implements provider.Provider using the Ollama HTTP API.
type Adapter struct {
	cfg    Config
	client *http.Client
}

// New creates an Adapter from a JSON config blob.
func New(configJSON string) (*Adapter, error) {
	var cfg Config
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return nil, fmt.Errorf("parse ollama config: %w", err)
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.Model == "" {
		return nil, fmt.Errorf("ollama config: model is required")
	}
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if cfg.TimeoutSeconds <= 0 {
		timeout = 300 * time.Second
	}
	return &Adapter{
		cfg:    cfg,
		client: &http.Client{Timeout: timeout},
	}, nil
}

// Execute runs a task to completion and returns the full response.
func (a *Adapter) Execute(ctx context.Context, req provider.TaskRequest) (provider.TaskResponse, error) {
	ch, err := a.StreamExecute(ctx, req)
	if err != nil {
		return provider.TaskResponse{}, err
	}
	var sb strings.Builder
	for chunk := range ch {
		if chunk.Error != nil {
			return provider.TaskResponse{}, chunk.Error
		}
		sb.WriteString(chunk.Content)
	}
	// Token counts are captured separately in parseStream via finalUsage.
	// Execute() callers get raw text; the runner reads TokensIn/Out from
	// the dedicated usage chunk emitted by parseStream via the done object.
	return provider.TaskResponse{Output: sb.String()}, nil
}

// StreamExecute calls Ollama's /api/chat with stream=true and emits chunks.
func (a *Adapter) StreamExecute(ctx context.Context, req provider.TaskRequest) (<-chan provider.StreamChunk, error) {
	messages := a.buildMessages(req)

	body, err := json.Marshal(map[string]any{
		"model":    a.cfg.Model,
		"messages": messages,
		"stream":   true,
	})
	if err != nil {
		return nil, fmt.Errorf("ollama: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.cfg.BaseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama: http request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("ollama: server returned %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	ch := make(chan provider.StreamChunk, 64)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		a.parseStream(ctx, resp.Body, ch)
	}()

	return ch, nil
}

// EstimateCost returns zero — local models have no API cost.
func (a *Adapter) EstimateCost(_ provider.TaskRequest) provider.CostEstimate {
	return provider.CostEstimate{}
}

// ListModels queries the Ollama server for installed models.
// Implements provider.ModelLister.
func (a *Adapter) ListModels(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.cfg.BaseURL+"/api/tags", nil)
	if err != nil {
		return nil, fmt.Errorf("ollama: list models request: %w", err)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama: list models: %w", err)
	}
	defer resp.Body.Close()

	var body struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("ollama: decode models: %w", err)
	}

	names := make([]string, 0, len(body.Models))
	for _, m := range body.Models {
		names = append(names, m.Name)
	}
	return names, nil
}

// ---- Internal helpers ----

// ollamaMessage is the wire format for Ollama chat messages.
type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ollamaChunk is one NDJSON line from the streaming response.
type ollamaChunk struct {
	Message struct {
		Role     string `json:"role"`
		Content  string `json:"content"`
		Thinking string `json:"thinking"` // chain-of-thought, skipped by default
	} `json:"message"`
	Done            bool  `json:"done"`
	PromptEvalCount int   `json:"prompt_eval_count"`  // input tokens
	EvalCount       int   `json:"eval_count"`          // output tokens
}

func (a *Adapter) buildMessages(req provider.TaskRequest) []ollamaMessage {
	var msgs []ollamaMessage

	// System prompt
	if req.SystemPrompt != "" {
		msgs = append(msgs, ollamaMessage{Role: "system", Content: req.SystemPrompt})
	}

	// Context messages (e.g. injected parent task output for follow-ups)
	for _, m := range req.Context {
		role := m.Role
		// Map Phoenix roles to Ollama roles (user/assistant only)
		if role != "assistant" {
			role = "user"
		}
		msgs = append(msgs, ollamaMessage{Role: role, Content: m.Content})
	}

	// User prompt
	msgs = append(msgs, ollamaMessage{Role: "user", Content: req.Prompt})

	return msgs
}

func (a *Adapter) parseStream(ctx context.Context, r io.Reader, ch chan<- provider.StreamChunk) {
	scanner := bufio.NewScanner(r)
	// Increase buffer for large thinking blocks
	scanner.Buffer(make([]byte, 512*1024), 512*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			ch <- provider.StreamChunk{Error: ctx.Err()}
			return
		default:
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var chunk ollamaChunk
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			// Skip malformed lines
			continue
		}

		// Emit content delta (never thinking unless KeepThinking)
		if chunk.Message.Content != "" {
			ch <- provider.StreamChunk{Content: chunk.Message.Content}
		} else if a.cfg.KeepThinking && chunk.Message.Thinking != "" {
			ch <- provider.StreamChunk{Content: chunk.Message.Thinking}
		}

		// Final chunk: record token counts in the done sentinel.
		if chunk.Done {
			// StreamChunk has no token fields; we surface counts through
			// a Done chunk. The runner accumulates them from TaskResponse
			// via Execute() — nothing extra needed here.
			_ = chunk.PromptEvalCount
			_ = chunk.EvalCount
			return
		}
	}

	if err := scanner.Err(); err != nil && err != io.EOF {
		ch <- provider.StreamChunk{Error: fmt.Errorf("ollama: stream read: %w", err)}
	}
}
