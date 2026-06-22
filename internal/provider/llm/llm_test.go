package llm

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/solarisjon/phoenix/internal/provider"
)

// mockServer starts an httptest server that returns the given response
// for /chat/completions requests.
func mockServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newAdapter(t *testing.T, endpoint string) *Adapter {
	t.Helper()
	cfg := map[string]interface{}{
		"endpoint":             endpoint,
		"model":                "test-model",
		"cost_per_input_token":  0.000001,
		"cost_per_output_token": 0.000002,
	}
	data, _ := json.Marshal(cfg)
	a, err := New(string(data))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a
}

// ---- Execute ----

func TestExecute_Success(t *testing.T) {
	srv := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		// Verify auth and content-type headers
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("missing Content-Type header")
		}

		// Use a plain map to avoid coupling to the internal struct definition.
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]interface{}{"content": "Hello from LLM"}},
			},
			"usage": map[string]interface{}{
				"prompt_tokens":     10,
				"completion_tokens": 5,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	a := newAdapter(t, srv.URL)
	req := provider.TaskRequest{
		SystemPrompt: "You are a helpful assistant.",
		Prompt:       "Say hello.",
	}

	got, err := a.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got.Output != "Hello from LLM" {
		t.Errorf("Output = %q, want %q", got.Output, "Hello from LLM")
	}
	if got.TokensIn != 10 || got.TokensOut != 5 {
		t.Errorf("Tokens = %d/%d, want 10/5", got.TokensIn, got.TokensOut)
	}
	expectedCost := 10*0.000001 + 5*0.000002
	if math.Abs(got.CostUSD-expectedCost) > 1e-10 {
		t.Errorf("CostUSD = %v, want %v", got.CostUSD, expectedCost)
	}
}

func TestExecute_HTTPError(t *testing.T) {
	srv := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"rate limited"}`, http.StatusTooManyRequests)
	})

	a := newAdapter(t, srv.URL)
	_, err := a.Execute(context.Background(), provider.TaskRequest{
		SystemPrompt: "sys", Prompt: "prompt",
	})
	if err == nil {
		t.Fatal("expected error for HTTP 429")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("expected 429 in error, got: %v", err)
	}
}

func TestExecute_WithAuthHeader(t *testing.T) {
	srv := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		resp := chatCompletion{}
		resp.Choices = append(resp.Choices, struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}{})
		resp.Choices[0].Message.Content = "ok"
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	cfg := map[string]interface{}{
		"endpoint":   srv.URL,
		"model":      "test-model",
		"auth_header": "Bearer test-key",
	}
	data, _ := json.Marshal(cfg)
	a, err := New(string(data))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = a.Execute(context.Background(), provider.TaskRequest{
		SystemPrompt: "sys", Prompt: "prompt",
	})
	if err != nil {
		t.Fatalf("Execute with auth: %v", err)
	}
}

func TestExecute_ContextCancellation(t *testing.T) {
	srv := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		// Block until context cancelled - don't write anything
		<-r.Context().Done()
	})

	a := newAdapter(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := a.Execute(ctx, provider.TaskRequest{
		SystemPrompt: "sys", Prompt: "prompt",
	})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// ---- StreamExecute ----

func TestStreamExecute_Success(t *testing.T) {
	srv := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher := w.(http.Flusher)

		chunks := []string{"Hello", " world", "!"}
		for _, c := range chunks {
			delta := streamDelta{}
			delta.Choices = append(delta.Choices, struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			}{Delta: struct {
				Content string `json:"content"`
			}{Content: c}})

			data, _ := json.Marshal(delta)
			w.Write([]byte("data: " + string(data) + "\n\n"))
			flusher.Flush()
		}
		w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	})

	a := newAdapter(t, srv.URL)
	ch, err := a.StreamExecute(context.Background(), provider.TaskRequest{
		SystemPrompt: "sys", Prompt: "prompt",
	})
	if err != nil {
		t.Fatalf("StreamExecute: %v", err)
	}

	var assembled strings.Builder
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("stream error: %v", chunk.Error)
		}
		assembled.WriteString(chunk.Content)
	}

	if assembled.String() != "Hello world!" {
		t.Errorf("assembled = %q, want %q", assembled.String(), "Hello world!")
	}
}

func TestStreamExecute_ErrorStatus(t *testing.T) {
	srv := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	})

	a := newAdapter(t, srv.URL)
	_, err := a.StreamExecute(context.Background(), provider.TaskRequest{
		SystemPrompt: "sys", Prompt: "prompt",
	})
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
}

func TestStreamExecute_CapturesUsage(t *testing.T) {
	srv := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		// Verify stream_options.include_usage is requested.
		var body chatRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		if body.StreamOptions == nil || !body.StreamOptions.IncludeUsage {
			t.Error("expected stream_options.include_usage = true")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		// Content chunk.
		delta := streamDelta{}
		delta.Choices = append(delta.Choices, struct {
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
		}{Delta: struct {
			Content string `json:"content"`
		}{Content: "answer"}})
		data, _ := json.Marshal(delta)
		w.Write([]byte("data: " + string(data) + "\n\n"))
		flusher.Flush()

		// Usage chunk (sent before [DONE] by OpenAI-compatible APIs).
		// Use a plain map to avoid coupling to the internal struct definition.
		usagePayload := map[string]interface{}{
			"usage": map[string]interface{}{
				"prompt_tokens":     20,
				"completion_tokens": 8,
			},
		}
		udata, _ := json.Marshal(usagePayload)
		w.Write([]byte("data: " + string(udata) + "\n\n"))
		flusher.Flush()

		w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	})

	a := newAdapter(t, srv.URL)
	ch, err := a.StreamExecute(context.Background(), provider.TaskRequest{
		SystemPrompt: "sys", Prompt: "prompt",
	})
	if err != nil {
		t.Fatalf("StreamExecute: %v", err)
	}

	var doneChunk provider.StreamChunk
	var assembled strings.Builder
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("stream error: %v", chunk.Error)
		}
		assembled.WriteString(chunk.Content)
		if chunk.Done {
			doneChunk = chunk
		}
	}

	if assembled.String() != "answer" {
		t.Errorf("assembled = %q, want %q", assembled.String(), "answer")
	}
	if doneChunk.TokensIn != 20 || doneChunk.TokensOut != 8 {
		t.Errorf("tokens = %d/%d, want 20/8", doneChunk.TokensIn, doneChunk.TokensOut)
	}
	expectedCost := 20*0.000001 + 8*0.000002
	if math.Abs(doneChunk.CostUSD-expectedCost) > 1e-10 {
		t.Errorf("CostUSD = %v, want %v", doneChunk.CostUSD, expectedCost)
	}
}

// ---- EstimateCost ----

func TestEstimateCost(t *testing.T) {
	a := newAdapter(t, "http://unused")
	req := provider.TaskRequest{
		SystemPrompt: strings.Repeat("a", 400), // ~100 tokens
		Prompt:       strings.Repeat("b", 400), // ~100 tokens
	}
	est := a.EstimateCost(req)
	if est.EstimatedCostUSD <= 0 {
		t.Error("expected positive cost estimate")
	}
}

// ---- Anthropic wire format ----

func newAnthropicAdapter(t *testing.T, endpoint string, useCache bool) *Adapter {
	t.Helper()
	cfg := map[string]interface{}{
		"endpoint":              endpoint,
		"model":                 "claude-opus-4-5",
		"api_flavour":           "anthropic",
		"use_prompt_cache":      useCache,
		"max_tokens":            1024,
		"cost_per_input_token":  0.000001,
		"cost_per_output_token": 0.000002,
	}
	data, _ := json.Marshal(cfg)
	a, err := New(string(data))
	if err != nil {
		t.Fatalf("New (anthropic): %v", err)
	}
	return a
}

func TestBuildRequestBody_OpenAI(t *testing.T) {
	a := newAdapter(t, "http://unused")
	req := provider.TaskRequest{
		SystemPrompt: "You are helpful.",
		Prompt:       "Hello",
	}
	cr := a.buildRequestBody(req, true)

	// System prompt must be in messages[0] with role "system".
	if len(cr.Messages) == 0 || cr.Messages[0].Role != "system" {
		t.Fatalf("expected messages[0].role = system, got %+v", cr.Messages)
	}
	if cr.Messages[0].Content != "You are helpful." {
		t.Errorf("messages[0].content = %q, want %q", cr.Messages[0].Content, "You are helpful.")
	}
	// No top-level system field for OpenAI.
	if cr.System != nil {
		t.Errorf("expected nil System field for OpenAI, got %s", cr.System)
	}
	// stream_options must be present when streaming.
	if cr.StreamOptions == nil || !cr.StreamOptions.IncludeUsage {
		t.Error("expected stream_options.include_usage = true for OpenAI streaming")
	}
	// max_tokens should not be set for OpenAI.
	if cr.MaxTokens != 0 {
		t.Errorf("expected MaxTokens = 0 for OpenAI, got %d", cr.MaxTokens)
	}
}

func TestBuildRequestBody_Anthropic_NoCache(t *testing.T) {
	a := newAnthropicAdapter(t, "http://unused", false)
	req := provider.TaskRequest{
		SystemPrompt: "You are helpful.",
		Prompt:       "Hello",
	}
	cr := a.buildRequestBody(req, true)

	// System field must be a JSON string.
	var systemStr string
	if err := json.Unmarshal(cr.System, &systemStr); err != nil {
		t.Fatalf("System should be a JSON string, got %s: %v", cr.System, err)
	}
	if systemStr != "You are helpful." {
		t.Errorf("System = %q, want %q", systemStr, "You are helpful.")
	}

	// No system role in messages.
	for _, m := range cr.Messages {
		if m.Role == "system" {
			t.Error("Anthropic messages must not contain a system role entry")
		}
	}

	// max_tokens must be set.
	if cr.MaxTokens == 0 {
		t.Error("expected MaxTokens > 0 for Anthropic")
	}

	// stream_options must be absent for Anthropic.
	if cr.StreamOptions != nil {
		t.Error("stream_options must be nil for Anthropic (causes 400)")
	}
}

func TestBuildRequestBody_Anthropic_WithCache(t *testing.T) {
	a := newAnthropicAdapter(t, "http://unused", true)
	req := provider.TaskRequest{
		SystemPrompt: "You are helpful.",
		Prompt:       "Hello",
	}
	cr := a.buildRequestBody(req, false)

	// System field must be a JSON array of content blocks.
	var blocks []contentBlock
	if err := json.Unmarshal(cr.System, &blocks); err != nil {
		t.Fatalf("System should be []contentBlock, got %s: %v", cr.System, err)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(blocks))
	}
	if blocks[0].Type != "text" {
		t.Errorf("block type = %q, want \"text\"", blocks[0].Type)
	}
	if blocks[0].Text != "You are helpful." {
		t.Errorf("block text = %q, want %q", blocks[0].Text, "You are helpful.")
	}
	if blocks[0].CacheControl == nil || blocks[0].CacheControl.Type != "ephemeral" {
		t.Errorf("expected cache_control.type = ephemeral, got %+v", blocks[0].CacheControl)
	}
}

func TestCalcCostWithCache_CacheHit(t *testing.T) {
	a, _ := New(`{"endpoint":"http://x","cost_per_input_token":0.001,"cost_per_output_token":0.002,"cost_per_cache_read_token":0.0001}`)
	// 0 normal input, 10 output, 0 cache write, 500 cache read
	cost := a.calcCostWithCache(0, 10, 0, 500)
	expected := 0*0.001 + 500*0.0001 + 10*0.002
	if abs(cost-expected) > 1e-10 {
		t.Errorf("cost = %v, want %v", cost, expected)
	}
}

func TestCalcCostWithCache_CacheWrite(t *testing.T) {
	a, _ := New(`{"endpoint":"http://x","cost_per_input_token":0.001,"cost_per_output_token":0.002,"cost_per_cache_write_token":0.00125}`)
	// 0 normal input, 5 output, 1000 cache write, 0 cache read
	cost := a.calcCostWithCache(0, 5, 1000, 0)
	expected := 1000*0.00125 + 5*0.002
	if abs(cost-expected) > 1e-10 {
		t.Errorf("cost = %v, want %v", cost, expected)
	}
}

func TestCalcCostWithCache_DefaultRates(t *testing.T) {
	// No explicit cache rates — should default to 1.25x and 0.10x of input rate.
	a, _ := New(`{"endpoint":"http://x","cost_per_input_token":0.001,"cost_per_output_token":0.002}`)
	cost := a.calcCostWithCache(100, 50, 200, 300)
	expectedWrite := 0.001 * 1.25
	expectedRead := 0.001 * 0.10
	expected := 100*0.001 + 200*expectedWrite + 300*expectedRead + 50*0.002
	if abs(cost-expected) > 1e-10 {
		t.Errorf("cost = %v, want %v", cost, expected)
	}
}

func TestExecute_Anthropic_CacheUsage(t *testing.T) {
	srv := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		// Verify Anthropic format: no system in messages, top-level system field present.
		var body chatRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if body.System == nil {
			t.Error("expected top-level system field for Anthropic")
		}
		for _, m := range body.Messages {
			if m.Role == "system" {
				t.Error("system role must not appear in Anthropic messages")
			}
		}

		// Return an Anthropic-style response with cache_read tokens.
		resp := chatCompletion{}
		resp.Choices = append(resp.Choices, struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}{Message: struct {
			Content string `json:"content"`
		}{Content: "cached answer"}})
		resp.Usage.PromptTokens = 10
		resp.Usage.CompletionTokens = 5
		resp.Usage.CacheReadInputTokens = 500

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	a := newAnthropicAdapter(t, srv.URL, false)
	got, err := a.Execute(context.Background(), provider.TaskRequest{
		SystemPrompt: "sys", Prompt: "prompt",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got.Output != "cached answer" {
		t.Errorf("Output = %q, want %q", got.Output, "cached answer")
	}
	// TokensIn should include cache_read tokens.
	if got.TokensIn != 510 { // 10 + 500
		t.Errorf("TokensIn = %d, want 510", got.TokensIn)
	}
	// Cost: 10 normal input + 500 cache read (at 0.10x rate) + 5 output.
	expectedCost := 10*0.000001 + 500*(0.000001*0.10) + 5*0.000002
	if abs(got.CostUSD-expectedCost) > 1e-10 {
		t.Errorf("CostUSD = %v, want %v", got.CostUSD, expectedCost)
	}
}

func TestStreamExecute_AnthropicFormat(t *testing.T) {
	srv := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher := w.(http.Flusher)

		sendEvent := func(v interface{}) {
			data, _ := json.Marshal(v)
			w.Write([]byte("data: " + string(data) + "\n\n"))
			flusher.Flush()
		}

		// message_start: input usage including cache tokens.
		sendEvent(map[string]interface{}{
			"type": "message_start",
			"message": map[string]interface{}{
				"usage": map[string]interface{}{
					"input_tokens":                   50,
					"cache_creation_input_tokens":    200,
					"cache_read_input_tokens":        0,
				},
			},
		})

		// content_block_delta: text chunks.
		for _, chunk := range []string{"Hello", " Anthropic", "!"} {
			sendEvent(map[string]interface{}{
				"type": "content_block_delta",
				"delta": map[string]interface{}{
					"type": "text_delta",
					"text": chunk,
				},
			})
		}

		// message_delta: output token count.
		sendEvent(map[string]interface{}{
			"type":  "message_delta",
			"usage": map[string]interface{}{"output_tokens": 15},
		})

		// message_stop: terminal event.
		sendEvent(map[string]interface{}{"type": "message_stop"})
	})

	a := newAnthropicAdapter(t, srv.URL, true)
	ch, err := a.StreamExecute(context.Background(), provider.TaskRequest{
		SystemPrompt: "sys", Prompt: "prompt",
	})
	if err != nil {
		t.Fatalf("StreamExecute: %v", err)
	}

	var assembled strings.Builder
	var doneChunk provider.StreamChunk
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("stream error: %v", chunk.Error)
		}
		assembled.WriteString(chunk.Content)
		if chunk.Done {
			doneChunk = chunk
		}
	}

	if assembled.String() != "Hello Anthropic!" {
		t.Errorf("assembled = %q, want %q", assembled.String(), "Hello Anthropic!")
	}
	// TokensIn = 50 + 200 (cache write) + 0 (cache read) = 250
	if doneChunk.TokensIn != 250 {
		t.Errorf("TokensIn = %d, want 250", doneChunk.TokensIn)
	}
	if doneChunk.TokensOut != 15 {
		t.Errorf("TokensOut = %d, want 15", doneChunk.TokensOut)
	}
	// Cost: 50 normal input + 200 cache write (1.25x) + 15 output
	expectedCost := 50*0.000001 + 200*(0.000001*1.25) + 15*0.000002
	if abs(doneChunk.CostUSD-expectedCost) > 1e-10 {
		t.Errorf("CostUSD = %v, want %v", doneChunk.CostUSD, expectedCost)
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// ---- Config validation ----

func TestNew_MissingEndpoint(t *testing.T) {
	_, err := New(`{"model":"gpt-4o"}`)
	if err == nil {
		t.Fatal("expected error for missing endpoint")
	}
}

func TestNew_InvalidJSON(t *testing.T) {
	_, err := New(`not json`)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestNew_DefaultModel(t *testing.T) {
	a, err := New(`{"endpoint":"http://test.local"}`)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a.cfg.Model != "gpt-4o" {
		t.Errorf("Model = %q, want gpt-4o", a.cfg.Model)
	}
}
