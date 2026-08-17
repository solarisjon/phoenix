package llamacpp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/solarisjon/phoenix/internal/provider"
)

// fakeServer imitates the llama-server endpoints the adapter uses. Handlers
// record the last chat request body so tests can assert wire fields.
type fakeServer struct {
	srv          *httptest.Server
	lastChat     map[string]any
	propsCalls   int32
	tokenizeCall int32
	nCtx         int
	slots        int
	modelPath    string
	chatStatus   int
	chatBody     string // non-streaming body
	sseBody      string // streaming body
	healthStatus int
}

func newFake(t *testing.T) *fakeServer {
	t.Helper()
	f := &fakeServer{nCtx: 8192, slots: 2, modelPath: "/models/Qwen3-8B-Q4_K_M.gguf", chatStatus: 200, healthStatus: 200}
	f.chatBody = `{"choices":[{"message":{"content":"pong","reasoning_content":"hmm"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":1}}`
	f.sseBody = "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"thinking\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"po\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"ng\"},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":2}}\n\n" +
		"data: [DONE]\n\n"
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(f.healthStatus)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/props", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&f.propsCalls, 1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"default_generation_settings": map[string]any{"n_ctx": f.nCtx},
			"total_slots":                 f.slots,
			"model_path":                  f.modelPath,
		})
	})
	mux.HandleFunc("/tokenize", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&f.tokenizeCall, 1)
		var in struct {
			Content string `json:"content"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		// one token per word
		n := len(strings.Fields(in.Content))
		toks := make([]int, n)
		_ = json.NewEncoder(w).Encode(map[string]any{"tokens": toks})
	})
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"Qwen3-8B-Q4_K_M"},{"id":"gemma-3-4b"}]}`))
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.lastChat = body
		if f.chatStatus != 200 {
			w.WriteHeader(f.chatStatus)
			_, _ = w.Write([]byte(`{"error":{"message":"the request exceeds the available context size"}}`))
			return
		}
		if stream, _ := body["stream"].(bool); stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(f.sseBody))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(f.chatBody))
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func newAdapter(t *testing.T, f *fakeServer, extra string) *Adapter {
	t.Helper()
	cfg := `{"base_url":"` + f.srv.URL + `","model":"Qwen3-8B-Q4_K_M"` + extra + `}`
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestNew_Defaults(t *testing.T) {
	a, err := New(`{}`)
	if err != nil {
		t.Fatal(err)
	}
	if a.cfg.BaseURL != "http://localhost:8081" {
		t.Errorf("default base url = %q", a.cfg.BaseURL)
	}
	if a.client.Timeout != defaultTimeout {
		t.Errorf("timeout = %v", a.client.Timeout)
	}
	a, _ = New(`{"base_url":"http://x:1/v1/"}`)
	if a.cfg.BaseURL != "http://x:1" {
		t.Errorf("/v1 suffix not stripped: %q", a.cfg.BaseURL)
	}
}

func TestExecute_StripsReasoningAndReportsUsage(t *testing.T) {
	f := newFake(t)
	a := newAdapter(t, f, "")
	resp, err := a.Execute(context.Background(), provider.TaskRequest{SystemPrompt: "sys", Prompt: "ping"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Output != "pong" || resp.TokensIn != 7 || resp.TokensOut != 1 || resp.CostUSD != 0 {
		t.Errorf("resp = %+v", resp)
	}
	// keep_thinking prepends the reasoning
	a2 := newAdapter(t, f, `,"keep_thinking":true`)
	resp, _ = a2.Execute(context.Background(), provider.TaskRequest{Prompt: "ping"})
	if !strings.HasPrefix(resp.Output, "hmm") {
		t.Errorf("keep_thinking output = %q", resp.Output)
	}
}

func TestExecute_WireFields(t *testing.T) {
	f := newFake(t)
	a := newAdapter(t, f, `,"temperature":0.4`)
	temp := 0.1
	_, err := a.Execute(context.Background(), provider.TaskRequest{
		SystemPrompt:    "sys",
		Prompt:          "ping",
		Context:         []provider.Message{{Role: "assistant", Content: "earlier"}},
		MaxOutputTokens: 321,
		Temperature:     &temp,
		StopSequences:   []string{"END"},
		ResponseSchema:  json.RawMessage(`{"type":"object","properties":{"a":{"type":"integer"}}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	b := f.lastChat
	if b["model"] != "Qwen3-8B-Q4_K_M" || b["stream"] != false {
		t.Errorf("model/stream: %v", b)
	}
	if b["max_tokens"] != float64(321) || b["temperature"] != 0.1 || b["cache_prompt"] != true {
		t.Errorf("controls: max_tokens=%v temperature=%v cache_prompt=%v", b["max_tokens"], b["temperature"], b["cache_prompt"])
	}
	if stop, _ := b["stop"].([]any); len(stop) != 1 || stop[0] != "END" {
		t.Errorf("stop = %v", b["stop"])
	}
	rf, _ := b["response_format"].(map[string]any)
	if rf == nil || rf["type"] != "json_schema" {
		t.Fatalf("response_format = %v", b["response_format"])
	}
	js, _ := rf["json_schema"].(map[string]any)
	if js["strict"] != true || js["schema"].(map[string]any)["type"] != "object" {
		t.Errorf("json_schema = %v", js)
	}
	msgs, _ := b["messages"].([]any)
	if len(msgs) != 3 || msgs[0].(map[string]any)["role"] != "system" || msgs[1].(map[string]any)["role"] != "assistant" || msgs[2].(map[string]any)["role"] != "user" {
		t.Errorf("messages = %v", msgs)
	}

	// Defaults: max_tokens 4096, config temperature, no response_format.
	_, _ = a.Execute(context.Background(), provider.TaskRequest{Prompt: "ping"})
	b = f.lastChat
	if b["max_tokens"] != float64(4096) || b["temperature"] != 0.4 {
		t.Errorf("defaults: %v", b)
	}
	if _, ok := b["response_format"]; ok {
		t.Errorf("response_format must be absent when no schema")
	}
	// -1 → unlimited → omit
	a3 := newAdapter(t, f, `,"max_output_tokens":-1,"cache_prompt":false`)
	_, _ = a3.Execute(context.Background(), provider.TaskRequest{Prompt: "ping"})
	if _, ok := f.lastChat["max_tokens"]; ok {
		t.Errorf("max_tokens should be omitted for -1")
	}
	if f.lastChat["cache_prompt"] != false {
		t.Errorf("cache_prompt=false not honoured")
	}
}

func TestExecute_ServerErrorSurfacesBody(t *testing.T) {
	f := newFake(t)
	f.chatStatus = 400
	a := newAdapter(t, f, "")
	_, err := a.Execute(context.Background(), provider.TaskRequest{Prompt: "ping"})
	if err == nil || !strings.Contains(err.Error(), "400") || !strings.Contains(err.Error(), "context size") {
		t.Errorf("err = %v", err)
	}
	_, err = a.StreamExecute(context.Background(), provider.TaskRequest{Prompt: "ping"})
	if err == nil || !strings.Contains(err.Error(), "400") {
		t.Errorf("stream err = %v", err)
	}
}

func TestStreamExecute(t *testing.T) {
	f := newFake(t)
	a := newAdapter(t, f, "")
	ch, err := a.StreamExecute(context.Background(), provider.TaskRequest{Prompt: "ping"})
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	var done provider.StreamChunk
	for c := range ch {
		if c.Done {
			done = c
		} else {
			sb.WriteString(c.Content)
		}
	}
	if sb.String() != "pong" {
		t.Errorf("streamed text = %q (reasoning must be stripped)", sb.String())
	}
	if done.TokensIn != 7 || done.TokensOut != 2 || done.CostUSD != 0 {
		t.Errorf("done = %+v", done)
	}
	if f.lastChat["stream"] != true {
		t.Errorf("stream flag not set")
	}
	if so, _ := f.lastChat["stream_options"].(map[string]any); so == nil || so["include_usage"] != true {
		t.Errorf("stream_options.include_usage missing: %v", f.lastChat["stream_options"])
	}
}

func TestPing(t *testing.T) {
	f := newFake(t)
	a := newAdapter(t, f, "")
	if err := a.Ping(context.Background()); err != nil {
		t.Errorf("ping: %v", err)
	}
	f.healthStatus = 503
	if err := a.Ping(context.Background()); err == nil {
		t.Errorf("expected error on 503")
	}
}

func TestListModels(t *testing.T) {
	f := newFake(t)
	a := newAdapter(t, f, "")
	ms, err := a.ListModels(context.Background())
	if err != nil || len(ms) != 2 || ms[0] != "Qwen3-8B-Q4_K_M" {
		t.Errorf("models = %v, err = %v", ms, err)
	}
}

func TestCapabilities_FromPropsCachedAndOverridable(t *testing.T) {
	f := newFake(t)
	a := newAdapter(t, f, "")
	caps := a.Capabilities(context.Background())
	if caps.ContextWindow != 8192 || caps.Slots != 2 || !caps.Local || !caps.SupportsJSONSchema || !caps.ExactTokenCount {
		t.Errorf("caps = %+v", caps)
	}
	if !caps.Reasoning {
		t.Errorf("Qwen3 should be inferred as a reasoning model")
	}
	if caps.Model != "Qwen3-8B-Q4_K_M" {
		t.Errorf("model = %q", caps.Model)
	}
	_ = a.Capabilities(context.Background())
	if atomic.LoadInt32(&f.propsCalls) != 1 {
		t.Errorf("props should be cached, calls = %d", f.propsCalls)
	}
	a.InvalidateProps()
	_ = a.Capabilities(context.Background())
	if atomic.LoadInt32(&f.propsCalls) != 2 {
		t.Errorf("InvalidateProps should force a re-probe")
	}
	if a.MaxConcurrent() != 2 {
		t.Errorf("MaxConcurrent = %d", a.MaxConcurrent())
	}

	// Config overrides win over probe.
	b := newAdapter(t, f, `,"context_window":4096,"max_concurrent":1,"reasoning":false,"max_output_tokens":1024`)
	caps = b.Capabilities(context.Background())
	if caps.ContextWindow != 4096 || caps.Slots != 1 || caps.Reasoning || caps.MaxOutputTokens != 1024 {
		t.Errorf("override caps = %+v", caps)
	}
	if b.MaxConcurrent() != 1 {
		t.Errorf("MaxConcurrent override = %d", b.MaxConcurrent())
	}
}

func TestCapabilities_ServerDown(t *testing.T) {
	a, _ := New(`{"base_url":"http://127.0.0.1:1","model":"llama-3.2-3b","context_window":2048}`)
	caps := a.Capabilities(context.Background())
	if caps.ContextWindow != 2048 || caps.Slots != 0 || caps.Reasoning || !caps.Local {
		t.Errorf("caps with dead server = %+v", caps)
	}
	if a.MaxConcurrent() != 0 {
		t.Errorf("MaxConcurrent with dead server should be 0 (unknown)")
	}
}

func TestCountTokens_ExactAndMemoised(t *testing.T) {
	f := newFake(t)
	a := newAdapter(t, f, "")
	n, err := a.CountTokens(context.Background(), "one two three four")
	if err != nil || n != 4 {
		t.Errorf("n=%d err=%v", n, err)
	}
	n, _ = a.CountTokens(context.Background(), "one two three four")
	if n != 4 || atomic.LoadInt32(&f.tokenizeCall) != 1 {
		t.Errorf("memoisation failed: calls = %d", f.tokenizeCall)
	}
	if n, _ := a.CountTokens(context.Background(), ""); n != 0 {
		t.Errorf("empty text should be 0 tokens")
	}
	// Dead server → heuristic + error.
	dead, _ := New(`{"base_url":"http://127.0.0.1:1"}`)
	n, err = dead.CountTokens(context.Background(), strings.Repeat("a", 40))
	if err == nil || n != 10 {
		t.Errorf("fallback n=%d err=%v", n, err)
	}
}

func TestLooksLikeReasoningModel(t *testing.T) {
	yes := []string{"Qwen3-8B", "DeepSeek-R1-Distill-Llama-8B", "gpt-oss-20b", "Magistral-Small", "QwQ-32B"}
	no := []string{"Llama-3.1-8B-Instruct", "gemma-3-12b-it", "Mistral-Small-24B", "Qwen2.5-Coder-7B"}
	for _, m := range yes {
		if !looksLikeReasoningModel(m) {
			t.Errorf("%s should be reasoning", m)
		}
	}
	for _, m := range no {
		if looksLikeReasoningModel(m) {
			t.Errorf("%s should not be reasoning", m)
		}
	}
}

// TestCapabilities_RouterMode covers llama-server --models-dir: root /props is
// role=router with no model; per-model props come from /props?model=… but only
// when /models says the model is loaded (never force a load from a probe).
func TestCapabilities_RouterMode(t *testing.T) {
	loaded := "unloaded"
	var perModelCalls int32
	mux := http.NewServeMux()
	mux.HandleFunc("/props", func(w http.ResponseWriter, r *http.Request) {
		if m := r.URL.Query().Get("model"); m != "" {
			atomic.AddInt32(&perModelCalls, 1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"default_generation_settings": map[string]any{"n_ctx": 8192},
				"total_slots":                 4,
				"model_alias":                 m,
				"chat_template_caps":          map[string]any{"supports_tools": true},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"role": "router", "model_alias": "llama-server", "model_path": "none",
			"default_generation_settings": map[string]any{"n_ctx": 0}})
	})
	mux.HandleFunc("/models", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{
			{"id": "Qwen/Qwen3-0.6B-GGUF:Q8_0", "status": map[string]any{"value": loaded}},
			{"id": "other", "status": map[string]any{"value": "loaded"}},
		}})
	})
	mux.HandleFunc("/tokenize", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in["model"] != "Qwen/Qwen3-0.6B-GGUF:Q8_0" {
			http.Error(w, "model required in router mode", 400)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"tokens": []int{1, 2}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	a, _ := New(`{"base_url":"` + srv.URL + `","model":"Qwen/Qwen3-0.6B-GGUF:Q8_0"}`)
	caps := a.Capabilities(context.Background())
	if caps.ContextWindow != 0 || caps.Slots != 0 || atomic.LoadInt32(&perModelCalls) != 0 {
		t.Errorf("unloaded model must not be probed/loaded: caps=%+v perModelCalls=%d", caps, perModelCalls)
	}
	if !caps.Reasoning || caps.Model != "Qwen/Qwen3-0.6B-GGUF:Q8_0" {
		t.Errorf("model/reasoning from config: %+v", caps)
	}

	loaded = "loaded"
	a.InvalidateProps()
	caps = a.Capabilities(context.Background())
	if caps.ContextWindow != 8192 || caps.Slots != 4 || !caps.SupportsTools {
		t.Errorf("loaded model props not resolved: %+v", caps)
	}
	if n, err := a.CountTokens(context.Background(), "a b"); err != nil || n != 2 {
		t.Errorf("tokenize with model: n=%d err=%v", n, err)
	}
}
