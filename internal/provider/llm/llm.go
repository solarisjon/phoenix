// Package llm provides a Provider adapter for OpenAI-compatible LLM HTTP endpoints.
// This covers custom endpoints (e.g. LLM Proxy) and any service that speaks
// the OpenAI chat completions API.
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
	Endpoint           string            `json:"endpoint"`            // Base URL, e.g. "http://llm.local/v1"
	AuthHeader         string            `json:"auth_header"`         // e.g. "Bearer sk-..."
	Model              string            `json:"model"`               // e.g. "gpt-4o"
	CostPerInputToken  float64           `json:"cost_per_input_token"`  // USD per token
	CostPerOutputToken float64           `json:"cost_per_output_token"` // USD per token
	ExtraHeaders       map[string]string `json:"extra_headers"`       // Optional additional headers
	TimeoutSeconds     int               `json:"timeout_seconds"`     // 0 = default (60s)
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
	cost := a.calcCost(tokensIn, tokensOut)

	return provider.TaskResponse{
		Output:    output,
		TokensIn:  tokensIn,
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
		a.readSSEStream(ctx, resp.Body, ch)
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
		EstimatedCostUSD: a.calcCost(estimatedIn, estimatedOut),
	}
}

// ---- Internal helpers ----

func (a *Adapter) buildRequestBody(req provider.TaskRequest, stream bool) chatRequest {
	messages := []chatMessage{
		{Role: "system", Content: req.SystemPrompt},
	}
	for _, m := range req.Context {
		messages = append(messages, chatMessage{Role: m.Role, Content: m.Content})
	}
	messages = append(messages, chatMessage{Role: "user", Content: req.Prompt})

	return chatRequest{
		Model:    a.cfg.Model,
		Messages: messages,
		Stream:   stream,
	}
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

func (a *Adapter) readSSEStream(ctx context.Context, body io.Reader, ch chan<- provider.StreamChunk) {
	scanner := bufio.NewScanner(body)
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
			ch <- provider.StreamChunk{Done: true}
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
	}

	if err := scanner.Err(); err != nil {
		ch <- provider.StreamChunk{Error: fmt.Errorf("stream read: %w", err), Done: true}
		return
	}
	ch <- provider.StreamChunk{Done: true}
}

func (a *Adapter) calcCost(tokensIn, tokensOut int) float64 {
	return float64(tokensIn)*a.cfg.CostPerInputToken +
		float64(tokensOut)*a.cfg.CostPerOutputToken
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// ---- OpenAI wire types ----

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
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
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

type streamDelta struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}
