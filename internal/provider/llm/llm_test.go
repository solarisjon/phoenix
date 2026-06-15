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

		resp := chatCompletion{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{{Message: struct {
				Content string `json:"content"`
			}{Content: "Hello from LLM"}}},
			Usage: struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			}{PromptTokens: 10, CompletionTokens: 5},
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
		usageDelta := streamDelta{}
		usageDelta.Usage = &struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		}{PromptTokens: 20, CompletionTokens: 8}
		udata, _ := json.Marshal(usageDelta)
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
