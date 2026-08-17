package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/solarisjon/phoenix/internal/provider"
)

func TestNew_Defaults(t *testing.T) {
	a, err := New(`{"model":"llama3.2:3b"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.cfg.BaseURL != defaultBaseURL {
		t.Errorf("BaseURL = %q, want %q", a.cfg.BaseURL, defaultBaseURL)
	}
	if a.cfg.Model != "llama3.2:3b" {
		t.Errorf("Model = %q, want llama3.2:3b", a.cfg.Model)
	}
}

func TestNew_MissingModel(t *testing.T) {
	_, err := New(`{"base_url":"http://localhost:11434"}`)
	if err == nil {
		t.Fatal("expected error for missing model, got nil")
	}
	if !strings.Contains(err.Error(), "model is required") {
		t.Errorf("error = %q, want 'model is required'", err.Error())
	}
}

func TestNew_InvalidJSON(t *testing.T) {
	_, err := New(`not json`)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestNew_CustomBaseURL_TrailingSlash(t *testing.T) {
	a, err := New(`{"model":"m","base_url":"http://myhost:11434/"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.HasSuffix(a.cfg.BaseURL, "/") {
		t.Errorf("BaseURL should have trailing slash stripped, got %q", a.cfg.BaseURL)
	}
}

func TestBuildMessages_SystemPrompt(t *testing.T) {
	a, _ := New(`{"model":"m"}`)
	req := provider.TaskRequest{
		SystemPrompt: "You are helpful.",
		Prompt:       "Hello",
	}
	msgs := a.buildMessages(req)
	if len(msgs) != 2 {
		t.Fatalf("want 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "system" || msgs[0].Content != "You are helpful." {
		t.Errorf("unexpected system message: %+v", msgs[0])
	}
	if msgs[1].Role != "user" || msgs[1].Content != "Hello" {
		t.Errorf("unexpected user message: %+v", msgs[1])
	}
}

func TestBuildMessages_NoSystemPrompt(t *testing.T) {
	a, _ := New(`{"model":"m"}`)
	msgs := a.buildMessages(provider.TaskRequest{Prompt: "Hi"})
	if len(msgs) != 1 {
		t.Fatalf("want 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != "user" {
		t.Errorf("role = %q, want user", msgs[0].Role)
	}
}

func TestBuildMessages_Context(t *testing.T) {
	a, _ := New(`{"model":"m"}`)
	req := provider.TaskRequest{
		Prompt: "Follow up",
		Context: []provider.Message{
			{Role: "user", Content: "Previous output here"},
		},
	}
	msgs := a.buildMessages(req)
	// context message + final user prompt
	if len(msgs) != 2 {
		t.Fatalf("want 2 messages, got %d", len(msgs))
	}
	if msgs[0].Content != "Previous output here" {
		t.Errorf("context content = %q", msgs[0].Content)
	}
	if msgs[1].Content != "Follow up" {
		t.Errorf("final prompt = %q", msgs[1].Content)
	}
}

// ---- HTTP-level tests using a mock server ----

func newMockServer(t *testing.T, chunks []ollamaChunk) (*httptest.Server, *Adapter) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		enc := json.NewEncoder(w)
		for _, c := range chunks {
			if err := enc.Encode(c); err != nil {
				t.Errorf("encode chunk: %v", err)
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
	a, err := New(`{"model":"test"}`)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.cfg.BaseURL = srv.URL
	return srv, a
}

func TestStreamExecute_BasicContent(t *testing.T) {
	chunks := []ollamaChunk{
		{Message: struct {
			Role     string `json:"role"`
			Content  string `json:"content"`
			Thinking string `json:"thinking"`
		}{Role: "assistant", Content: "Hello"}, Done: false},
		{Message: struct {
			Role     string `json:"role"`
			Content  string `json:"content"`
			Thinking string `json:"thinking"`
		}{Role: "assistant", Content: " world"}, Done: false},
		{Done: true, PromptEvalCount: 5, EvalCount: 10},
	}
	srv, a := newMockServer(t, chunks)
	defer srv.Close()

	ch, err := a.StreamExecute(context.Background(), provider.TaskRequest{Prompt: "Hi"})
	if err != nil {
		t.Fatalf("StreamExecute: %v", err)
	}

	var got strings.Builder
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("stream error: %v", chunk.Error)
		}
		got.WriteString(chunk.Content)
	}
	if got.String() != "Hello world" {
		t.Errorf("content = %q, want %q", got.String(), "Hello world")
	}
}

func TestStreamExecute_ThinkingSkippedByDefault(t *testing.T) {
	chunks := []ollamaChunk{
		{Message: struct {
			Role     string `json:"role"`
			Content  string `json:"content"`
			Thinking string `json:"thinking"`
		}{Thinking: "internal cot"}, Done: false},
		{Message: struct {
			Role     string `json:"role"`
			Content  string `json:"content"`
			Thinking string `json:"thinking"`
		}{Content: "answer"}, Done: false},
		{Done: true},
	}
	srv, a := newMockServer(t, chunks)
	defer srv.Close()

	ch, _ := a.StreamExecute(context.Background(), provider.TaskRequest{Prompt: "q"})
	var got strings.Builder
	for chunk := range ch {
		got.WriteString(chunk.Content)
	}
	if got.String() != "answer" {
		t.Errorf("thinking leaked into output: %q", got.String())
	}
}

func TestStreamExecute_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model not found", http.StatusNotFound)
	}))
	defer srv.Close()

	a, _ := New(`{"model":"gone"}`)
	a.cfg.BaseURL = srv.URL

	_, err := a.StreamExecute(context.Background(), provider.TaskRequest{Prompt: "hi"})
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error = %q, want 404", err.Error())
	}
}

func TestEstimateCost(t *testing.T) {
	a, _ := New(`{"model":"m"}`)
	est := a.EstimateCost(provider.TaskRequest{Prompt: "anything"})
	if est.EstimatedCostUSD != 0 {
		t.Errorf("cost = %v, want 0 for local model", est.EstimatedCostUSD)
	}
}

// ---- Generation options (local-models phase 0.1) ----

func TestBuildOptions_Defaults(t *testing.T) {
	a, _ := New(`{"model":"qwen3:8b"}`)
	opts := a.buildOptions(provider.TaskRequest{Prompt: "hi"})
	if opts["num_predict"] != defaultNumPredict {
		t.Errorf("num_predict = %v, want default %d", opts["num_predict"], defaultNumPredict)
	}
	for _, k := range []string{"num_ctx", "temperature", "stop"} {
		if _, ok := opts[k]; ok {
			t.Errorf("expected %s absent by default, got %v", k, opts[k])
		}
	}
}

func TestBuildOptions_FromConfigAndRequest(t *testing.T) {
	a, err := New(`{"model":"qwen3:8b","num_ctx":16384,"num_predict":2048,"temperature":0.3}`)
	if err != nil {
		t.Fatal(err)
	}
	opts := a.buildOptions(provider.TaskRequest{Prompt: "hi"})
	if opts["num_ctx"] != 16384 || opts["num_predict"] != 2048 || opts["temperature"] != 0.3 {
		t.Errorf("config options not applied: %v", opts)
	}
	temp := 0.9
	opts = a.buildOptions(provider.TaskRequest{Prompt: "hi", MaxOutputTokens: 256, Temperature: &temp, StopSequences: []string{"END"}})
	if opts["num_predict"] != 256 || opts["temperature"] != 0.9 {
		t.Errorf("request values must override config: %v", opts)
	}
	if stop, ok := opts["stop"].([]string); !ok || len(stop) != 1 || stop[0] != "END" {
		t.Errorf("stop = %v, want [END]", opts["stop"])
	}
}

func TestStreamExecute_SendsOptionsOnWire(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"ok"},"done":true,"prompt_eval_count":1,"eval_count":1}` + "\n"))
	}))
	defer srv.Close()
	a, err := New(`{"model":"m","base_url":"` + srv.URL + `","num_ctx":8192}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Execute(context.Background(), provider.TaskRequest{Prompt: "hi"}); err != nil {
		t.Fatal(err)
	}
	opts, ok := got["options"].(map[string]any)
	if !ok {
		t.Fatalf("options object missing from request body: %v", got)
	}
	if opts["num_ctx"] != float64(8192) || opts["num_predict"] != float64(defaultNumPredict) {
		t.Errorf("options on wire = %v", opts)
	}
}

func TestNew_DefaultTimeout(t *testing.T) {
	a, _ := New(`{"model":"m"}`)
	if a.client.Timeout != 900*time.Second {
		t.Errorf("default timeout = %v, want 900s", a.client.Timeout)
	}
}

func TestStreamExecute_SendsFormatSchema(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"{\"ok\":true}"},"done":true}` + "\n"))
	}))
	defer srv.Close()
	a, _ := New(`{"model":"m","base_url":"` + srv.URL + `"}`)
	schema := json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean"}}}`)
	if _, err := a.Execute(context.Background(), provider.TaskRequest{Prompt: "hi", ResponseSchema: schema}); err != nil {
		t.Fatal(err)
	}
	f, ok := got["format"].(map[string]any)
	if !ok || f["type"] != "object" {
		t.Errorf("format = %v", got["format"])
	}
	got = nil
	_, _ = a.Execute(context.Background(), provider.TaskRequest{Prompt: "hi"})
	if _, ok := got["format"]; ok {
		t.Errorf("format must be absent without a schema")
	}
}
