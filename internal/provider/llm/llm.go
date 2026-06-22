// Package llm provides a Provider adapter for OpenAI-compatible LLM HTTP endpoints.
// This covers custom endpoints (e.g. LLM Proxy) and any service that speaks
// the OpenAI chat completions API, as well as the Anthropic Messages API
// (set api_flavour: "anthropic" in config).
package llm

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

// Config holds the configuration for a custom LLM endpoint.
type Config struct {
	Endpoint           string            `json:"endpoint"`             // Base URL, e.g. "http://llm.local/v1"
	AuthHeader         string            `json:"auth_header"`          // e.g. "Bearer sk-..."
	Model              string            `json:"model"`                // e.g. "gpt-4o"
	CostPerInputToken  float64           `json:"cost_per_input_token"`  // USD per token
	CostPerOutputToken float64           `json:"cost_per_output_token"` // USD per token
	ExtraHeaders       map[string]string `json:"extra_headers"`        // Optional additional headers
	TimeoutSeconds     int               `json:"timeout_seconds"`      // 0 = default (60s)

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
}

// Adapter implements provider.Provider for an OpenAI-compatible HTTP endpoint.
type Adapter struct {
	cfg    Config
	client *http.Client
}

// New creates a new LLM Adapter from a JSON config blob.
func New(configJSON string) (*Adapter, error) {
	var cfg Config
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return nil, fmt.Errorf("parse llm config: %w", err)
	}
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("llm config: endpoint is required")
	}
	if cfg.Model == "" {
		cfg.Model = "gpt-4o"
	}
	timeout := 60 * time.Second
	if cfg.TimeoutSeconds > 0 {
		timeout = time.Duration(cfg.TimeoutSeconds) * time.Second
	}
	return &Adapter{
		cfg:    cfg,
		client: &http.Client{Timeout: timeout},
	}, nil
}

// isAnthropic reports whether the adapter is configured for the Anthropic Messages API.
func (a *Adapter) isAnthropic() bool {
	return a.cfg.ApiFlavour == "anthropic"
}

// Execute sends a chat completion request and returns the full response.
func (a *Adapter) Execute(ctx context.Context, req provider.TaskRequest) (provider.TaskResponse, error) {
	body := a.buildRequestBody(req, false)

	resp, err := a.doRequest(ctx, body)
	if err != nil {
		return provider.TaskResponse{}, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return provider.TaskResponse{}, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return provider.TaskResponse{}, fmt.Errorf("llm api error %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}

	var completion chatCompletion
	if err := json.Unmarshal(raw, &completion); err != nil {
		return provider.TaskResponse{}, fmt.Errorf("parse completion: %w", err)
	}

	output := ""
	if len(completion.Choices) > 0 {
		output = completion.Choices[0].Message.Content
	}

	tokensIn := completion.Usage.PromptTokens
	tokensOut := completion.Usage.CompletionTokens
	cacheWrite := completion.Usage.CacheCreationInputTokens
	cacheRead := completion.Usage.CacheReadInputTokens
	cost := a.calcCostWithCache(tokensIn, tokensOut, cacheWrite, cacheRead)
	totalIn := tokensIn + cacheWrite + cacheRead

	return provider.TaskResponse{
		Output:    output,
		TokensIn:  totalIn,
		TokensOut: tokensOut,
		CostUSD:   cost,
	}, nil
}

// StreamExecute sends a streaming chat completion request and returns a channel
// of chunks. The channel is closed after the final chunk (Done=true).
func (a *Adapter) StreamExecute(ctx context.Context, req provider.TaskRequest) (<-chan provider.StreamChunk, error) {
	body := a.buildRequestBody(req, true)

	// Use a longer-lived client for streaming (no timeout on reads).
	streamClient := &http.Client{}
	httpReq, err := a.buildHTTPRequest(ctx, body)
	if err != nil {
		return nil, err
	}

	resp, err := streamClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("stream request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("llm stream error %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}

	ch := make(chan provider.StreamChunk, 32)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		if a.isAnthropic() {
			a.readAnthropicSSEStream(ctx, resp.Body, ch)
		} else {
			a.readSSEStream(ctx, resp.Body, ch)
		}
	}()

	return ch, nil
}

// EstimateCost returns a rough cost estimate based on approximate token counts.
func (a *Adapter) EstimateCost(req provider.TaskRequest) provider.CostEstimate {
	// Rough estimate: 1 token ≈ 4 characters.
	chars := len(req.SystemPrompt) + len(req.Prompt)
	for _, m := range req.Context {
		chars += len(m.Content)
	}
	estimatedIn := chars / 4
	estimatedOut := 512 // conservative output estimate
	return provider.CostEstimate{
		EstimatedCostUSD: a.calcCostWithCache(estimatedIn, estimatedOut, 0, 0),
	}
}

// ---- Internal helpers ----

func (a *Adapter) buildRequestBody(req provider.TaskRequest, stream bool) chatRequest {
	cr := chatRequest{
		Model:  a.cfg.Model,
		Stream: stream,
	}

	if a.isAnthropic() {
		// Anthropic Messages API: system is a separate top-level field.
		// Messages contain only the conversation turns (no system role entry).
		var systemRaw json.RawMessage
		if a.cfg.UsePromptCache {
			// Content-block array with cache breakpoint on the system prompt.
			blocks := []contentBlock{
				{
					Type:         "text",
					Text:         req.SystemPrompt,
					CacheControl: &cacheControl{Type: "ephemeral"},
				},
			}
			systemRaw, _ = json.Marshal(blocks)
		} else {
			// Plain string form.
			systemRaw, _ = json.Marshal(req.SystemPrompt)
		}
		cr.System = systemRaw

		// Anthropic requires max_tokens.
		cr.MaxTokens = a.cfg.MaxTokens
		if cr.MaxTokens == 0 {
			cr.MaxTokens = 8192
		}

		// Build messages: context turns + user prompt (no system entry).
		messages := make([]chatMessage, 0, len(req.Context)+1)
		for _, m := range req.Context {
			messages = append(messages, chatMessage{Role: m.Role, Content: m.Content})
		}
		messages = append(messages, chatMessage{Role: "user", Content: req.Prompt})
		cr.Messages = messages

		// Do NOT set StreamOptions — Anthropic always includes usage and does not
		// support the OpenAI stream_options extension (returns 400 if present).
	} else {
		// OpenAI format: system prompt is messages[0] with role "system".
		messages := make([]chatMessage, 0, len(req.Context)+2)
		messages = append(messages, chatMessage{Role: "system", Content: req.SystemPrompt})
		for _, m := range req.Context {
			messages = append(messages, chatMessage{Role: m.Role, Content: m.Content})
		}
		messages = append(messages, chatMessage{Role: "user", Content: req.Prompt})
		cr.Messages = messages

		if stream {
			cr.StreamOptions = &streamOptions{IncludeUsage: true}
		}
	}

	return cr
}

func (a *Adapter) buildHTTPRequest(ctx context.Context, body chatRequest) (*http.Request, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	endpoint := strings.TrimRight(a.cfg.Endpoint, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("build http request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if a.cfg.AuthHeader != "" {
		req.Header.Set("Authorization", a.cfg.AuthHeader)
	}
	for k, v := range a.cfg.ExtraHeaders {
		req.Header.Set(k, v)
	}

	return req, nil
}

func (a *Adapter) doRequest(ctx context.Context, body chatRequest) (*http.Response, error) {
	req, err := a.buildHTTPRequest(ctx, body)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llm request: %w", err)
	}
	return resp, nil
}

// readSSEStream handles OpenAI-style SSE: chunks are streamDelta with choices[].delta.content,
// terminated by "data: [DONE]".
func (a *Adapter) readSSEStream(ctx context.Context, body io.Reader, ch chan<- provider.StreamChunk) {
	scanner := bufio.NewScanner(body)
	var tokensIn, tokensOut int
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			ch <- provider.StreamChunk{Error: ctx.Err(), Done: true}
			return
		default:
		}

		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			cost := a.calcCostWithCache(tokensIn, tokensOut, 0, 0)
			ch <- provider.StreamChunk{Done: true, TokensIn: tokensIn, TokensOut: tokensOut, CostUSD: cost}
			return
		}

		var delta streamDelta
		if err := json.Unmarshal([]byte(data), &delta); err != nil {
			continue // skip malformed chunks
		}

		content := ""
		if len(delta.Choices) > 0 {
			content = delta.Choices[0].Delta.Content
		}
		if content != "" {
			ch <- provider.StreamChunk{Content: content}
		}
		// Capture usage when the provider includes it (requires stream_options.include_usage).
		if delta.Usage != nil {
			tokensIn = delta.Usage.PromptTokens
			tokensOut = delta.Usage.CompletionTokens
		}
	}

	if err := scanner.Err(); err != nil {
		ch <- provider.StreamChunk{Error: fmt.Errorf("stream read: %w", err), Done: true}
		return
	}
	cost := a.calcCostWithCache(tokensIn, tokensOut, 0, 0)
	ch <- provider.StreamChunk{Done: true, TokensIn: tokensIn, TokensOut: tokensOut, CostUSD: cost}
}

// readAnthropicSSEStream handles Anthropic Messages API SSE format.
// Events: content_block_delta (text), message_delta (usage), message_stop (terminal).
func (a *Adapter) readAnthropicSSEStream(ctx context.Context, body io.Reader, ch chan<- provider.StreamChunk) {
	scanner := bufio.NewScanner(body)
	var tokensIn, tokensOut, cacheWrite, cacheRead int

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			ch <- provider.StreamChunk{Error: ctx.Err(), Done: true}
			return
		default:
		}

		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		var ev anthropicEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue // skip malformed
		}

		switch ev.Type {
		case "content_block_delta":
			if ev.Delta.Type == "text_delta" && ev.Delta.Text != "" {
				ch <- provider.StreamChunk{Content: ev.Delta.Text}
			}
		case "message_delta":
			// Usage is reported in the message_delta event.
			if ev.Usage != nil {
				tokensOut = ev.Usage.OutputTokens
			}
		case "message_start":
			// Initial usage (input tokens) is in message_start.
			if ev.Message != nil && ev.Message.Usage != nil {
				tokensIn = ev.Message.Usage.InputTokens
				cacheWrite = ev.Message.Usage.CacheCreationInputTokens
				cacheRead = ev.Message.Usage.CacheReadInputTokens
			}
		case "message_stop":
			totalIn := tokensIn + cacheWrite + cacheRead
			cost := a.calcCostWithCache(tokensIn, tokensOut, cacheWrite, cacheRead)
			ch <- provider.StreamChunk{Done: true, TokensIn: totalIn, TokensOut: tokensOut, CostUSD: cost}
			return
		}
	}

	if err := scanner.Err(); err != nil {
		ch <- provider.StreamChunk{Error: fmt.Errorf("stream read: %w", err), Done: true}
		return
	}
	totalIn := tokensIn + cacheWrite + cacheRead
	cost := a.calcCostWithCache(tokensIn, tokensOut, cacheWrite, cacheRead)
	ch <- provider.StreamChunk{Done: true, TokensIn: totalIn, TokensOut: tokensOut, CostUSD: cost}
}

// ListModels queries the OpenAI-compatible /v1/models endpoint.
// The endpoint is derived from cfg.Endpoint by stripping the path and
// appending /v1/models, so it works for both /v1/chat/completions and
// bare base-URL styles.
// Implements provider.ModelLister.
func (a *Adapter) ListModels(ctx context.Context) ([]string, error) {
	// Derive base: strip known path suffixes to get the server root.
	base := a.cfg.Endpoint
	for _, suffix := range []string{"/v1/chat/completions", "/chat/completions", "/v1"} {
		if strings.HasSuffix(base, suffix) {
			base = base[:len(base)-len(suffix)]
			break
		}
	}
	base = strings.TrimRight(base, "/")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("llm: list models request: %w", err)
	}
	if a.cfg.AuthHeader != "" {
		req.Header.Set("Authorization", a.cfg.AuthHeader)
	}
	for k, v := range a.cfg.ExtraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llm: list models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("llm: list models: server returned %d", resp.StatusCode)
	}

	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("llm: decode models: %w", err)
	}

	ids := make([]string, 0, len(body.Data))
	for _, m := range body.Data {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	return ids, nil
}

// calcCostWithCache computes the total cost accounting for cache write and read tokens.
// cacheWrite tokens are billed at CostPerCacheWriteToken (default: 1.25x input rate).
// cacheRead tokens are billed at CostPerCacheReadToken (default: 0.10x input rate).
// tokensIn are the non-cached input tokens billed at the normal input rate.
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

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// ---- OpenAI wire types ----

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type chatRequest struct {
	Model         string          `json:"model"`
	System        json.RawMessage `json:"system,omitempty"`
	Messages      []chatMessage   `json:"messages"`
	Stream        bool            `json:"stream"`
	StreamOptions *streamOptions  `json:"stream_options,omitempty"`
	MaxTokens     int             `json:"max_tokens,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletion struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens                int `json:"prompt_tokens"`
		CompletionTokens            int `json:"completion_tokens"`
		CacheCreationInputTokens    int `json:"cache_creation_input_tokens"`
		CacheReadInputTokens        int `json:"cache_read_input_tokens"`
	} `json:"usage"`
}

type streamDelta struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// ---- Anthropic wire types ----

type contentBlock struct {
	Type         string        `json:"type"`
	Text         string        `json:"text"`
	CacheControl *cacheControl `json:"cache_control,omitempty"`
}

type cacheControl struct {
	Type string `json:"type"` // "ephemeral"
}

// anthropicEvent covers all Anthropic SSE event shapes we care about.
type anthropicEvent struct {
	Type  string `json:"type"`
	Delta struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta"`
	// message_start carries the initial usage (input tokens).
	Message *struct {
		Usage *struct {
			InputTokens                 int `json:"input_tokens"`
			OutputTokens                int `json:"output_tokens"`
			CacheCreationInputTokens    int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens        int `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
	// message_delta carries output token count.
	Usage *struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}
