// Package llamacpp provides a native Provider adapter for llama.cpp's
// `llama-server` (https://github.com/ggml-org/llama.cpp/tree/master/tools/server).
//
// llama-server speaks the OpenAI chat-completions format, so a plain `llm`
// provider works against it — but this adapter additionally uses the
// server's own endpoints to give Phoenix what small local models need:
//
//   - GET  /props     → per-slot context window (n_ctx), slot count, model
//     name → provider.Capabilities, so the runner can budget prompts and
//     gate concurrency instead of overflowing silently.
//   - POST /tokenize  → exact token counts (provider.TokenCounter), memoised.
//   - GET  /health    → cheap liveness check (provider.Pinger) — no inference.
//   - GET  /v1/models → provider.ModelLister (one model, or all in router mode).
//   - POST /v1/chat/completions with llama.cpp extensions: cache_prompt (KV
//     prefix reuse — the same agent system prompt is sent every task),
//     n_predict / max_tokens, temperature, stop, response_format json_schema
//     (compiled server-side to a GBNF grammar, so JSON is guaranteed to
//     validate), and reasoning_content stripped unless keep_thinking.
//
// It is a pure-LLM adapter: no filesystem tools. Cost is always zero.
package llamacpp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/solarisjon/phoenix/internal/provider"
	"github.com/solarisjon/phoenix/internal/provider/openaiwire"
)

const (
	defaultBaseURL         = "http://localhost:8081" // NOT 8080 — that's Phoenix
	defaultTimeout         = 900 * time.Second
	defaultMaxOutputTokens = 4096
	propsCacheTTL          = 60 * time.Second
	tokenizeCacheSize      = 512
	tokenizeTimeout        = 5 * time.Second
)

// Config is the JSON config blob for a llamacpp provider (kind: "llamacpp").
type Config struct {
	// BaseURL of llama-server, e.g. http://localhost:8081. Default as above.
	BaseURL string `json:"base_url"`
	// Model as reported by GET /v1/models. In single-model mode llama-server
	// ignores it; in router mode (--models-dir) it selects which model to load.
	Model string `json:"model"`
	// APIKey if llama-server was started with --api-key. Sent as Bearer.
	APIKey string `json:"api_key"`
	// TimeoutSeconds for a whole request. Default 900 (cold model load + slow
	// inference routinely exceed a few minutes).
	TimeoutSeconds int `json:"timeout_seconds"`
	// KeepThinking forwards reasoning_content into the task output. Default false.
	KeepThinking bool `json:"keep_thinking"`
	// ContextWindow overrides the value probed from /props (0 = probe).
	ContextWindow int `json:"context_window"`
	// MaxOutputTokens is the default n_predict. Default 4096; -1 = unlimited.
	// A per-request TaskRequest.MaxOutputTokens takes precedence.
	MaxOutputTokens int `json:"max_output_tokens"`
	// Temperature default. nil = server/model default.
	Temperature *float64 `json:"temperature,omitempty"`
	// CachePrompt enables KV-cache prefix reuse. Default true (nil).
	CachePrompt *bool `json:"cache_prompt,omitempty"`
	// Reasoning marks the model as a thinking model. nil = infer from the
	// model name (qwen3, r1, gpt-oss, …).
	Reasoning *bool `json:"reasoning,omitempty"`
	// MaxConcurrent overrides the slot count probed from /props (0 = probe).
	MaxConcurrent int `json:"max_concurrent"`
}

// Adapter implements provider.Provider (+ ModelLister, Pinger, Capable,
// TokenCounter, SlotLimiter) against a llama-server instance.
type Adapter struct {
	cfg    Config
	client *http.Client

	propsMu      sync.Mutex
	props        *serverProps
	propsFetched time.Time

	tokMu    sync.Mutex
	tokCache map[[32]byte]int
	tokOrder [][32]byte // insertion order for cheap eviction
}

// New creates an Adapter from a JSON config blob.
func New(configJSON string) (*Adapter, error) {
	var cfg Config
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return nil, fmt.Errorf("parse llamacpp config: %w", err)
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	// Accept an /v1 suffix pasted from OpenAI-style docs.
	cfg.BaseURL = strings.TrimSuffix(cfg.BaseURL, "/v1")
	timeout := defaultTimeout
	if cfg.TimeoutSeconds > 0 {
		timeout = time.Duration(cfg.TimeoutSeconds) * time.Second
	}
	return &Adapter{
		cfg:      cfg,
		client:   &http.Client{Timeout: timeout},
		tokCache: make(map[[32]byte]int),
	}, nil
}

// ---- provider.Provider ----

// Execute runs a task to completion.
func (a *Adapter) Execute(ctx context.Context, req provider.TaskRequest) (provider.TaskResponse, error) {
	body := a.buildRequest(req, false)
	resp, err := a.post(ctx, "/v1/chat/completions", body, a.client)
	if err != nil {
		return provider.TaskResponse{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return provider.TaskResponse{}, fmt.Errorf("llamacpp: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return provider.TaskResponse{}, fmt.Errorf("llamacpp: server returned %d: %s", resp.StatusCode, openaiwire.Truncate(strings.TrimSpace(string(raw)), 300))
	}
	c, err := openaiwire.ParseCompletion(raw)
	if err != nil {
		return provider.TaskResponse{}, fmt.Errorf("llamacpp: %w", err)
	}
	out := c.Text()
	if a.cfg.KeepThinking && c.Reasoning() != "" {
		out = c.Reasoning() + "\n\n" + out
	}
	return provider.TaskResponse{
		Output:    out,
		TokensIn:  c.Usage.PromptTokens,
		TokensOut: c.Usage.CompletionTokens,
	}, nil
}

// StreamExecute streams the response as it is generated.
func (a *Adapter) StreamExecute(ctx context.Context, req provider.TaskRequest) (<-chan provider.StreamChunk, error) {
	body := a.buildRequest(req, true)
	// Streaming responses can legitimately run for the whole task timeout;
	// don't cap them with the per-call client timeout.
	resp, err := a.post(ctx, "/v1/chat/completions", body, &http.Client{})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("llamacpp: server returned %d: %s", resp.StatusCode, openaiwire.Truncate(strings.TrimSpace(string(raw)), 300))
	}
	ch := make(chan provider.StreamChunk, 32)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		openaiwire.ReadSSE(ctx, resp.Body, ch, openaiwire.SSEOptions{
			IncludeReasoning: a.cfg.KeepThinking,
			Finish: func(u openaiwire.Usage) provider.StreamChunk {
				return provider.StreamChunk{Done: true, TokensIn: u.PromptTokens, TokensOut: u.CompletionTokens}
			},
		})
	}()
	return ch, nil
}

// EstimateCost returns zero — local models have no per-token cost.
func (a *Adapter) EstimateCost(_ provider.TaskRequest) provider.CostEstimate {
	return provider.CostEstimate{}
}

// ---- provider.Pinger ----

// Ping hits GET /health — no inference, safe on slow thinking models.
func (a *Adapter) Ping(ctx context.Context) error {
	resp, err := a.get(ctx, "/health")
	if err != nil {
		return fmt.Errorf("llamacpp: ping: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("llamacpp: health returned %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

// ---- provider.ModelLister ----

// ListModels returns the model IDs from GET /v1/models.
func (a *Adapter) ListModels(ctx context.Context) ([]string, error) {
	resp, err := a.get(ctx, "/v1/models")
	if err != nil {
		return nil, fmt.Errorf("llamacpp: list models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("llamacpp: list models: server returned %d", resp.StatusCode)
	}
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("llamacpp: decode models: %w", err)
	}
	ids := make([]string, 0, len(body.Data))
	for _, m := range body.Data {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	return ids, nil
}

// ---- provider.Capable ----

// Capabilities probes GET /props (cached for propsCacheTTL) and merges config
// overrides. Unreachable server ⇒ whatever the config states, rest unknown.
func (a *Adapter) Capabilities(ctx context.Context) provider.Capabilities {
	caps := provider.Capabilities{
		Local:              true,
		SupportsJSONSchema: true,
		SupportsJSONMode:   true,
		ExactTokenCount:    true,
		ContextWindow:      a.cfg.ContextWindow,
		Slots:              a.cfg.MaxConcurrent,
		Model:              a.cfg.Model,
	}
	if p := a.loadProps(ctx); p != nil {
		if caps.ContextWindow == 0 {
			caps.ContextWindow = p.DefaultGenerationSettings.NCtx
		}
		if caps.Slots == 0 {
			caps.Slots = p.TotalSlots
		}
		if caps.Model == "" {
			caps.Model = p.modelName()
		}
		caps.SupportsTools = p.ChatTemplateCaps.SupportsTools
		if a.cfg.Reasoning == nil {
			caps.Reasoning = looksLikeReasoningModel(p.modelName()) || looksLikeReasoningModel(a.cfg.Model)
		}
	} else if a.cfg.Reasoning == nil {
		caps.Reasoning = looksLikeReasoningModel(a.cfg.Model)
	}
	if a.cfg.Reasoning != nil {
		caps.Reasoning = *a.cfg.Reasoning
	}
	if a.cfg.MaxOutputTokens > 0 {
		caps.MaxOutputTokens = a.cfg.MaxOutputTokens
	}
	return caps
}

// ---- provider.SlotLimiter ----

// MaxConcurrent returns the configured or probed slot count (0 = unknown).
// Uses a short probe timeout so a dead server never blocks the scheduler.
func (a *Adapter) MaxConcurrent() int {
	if a.cfg.MaxConcurrent > 0 {
		return a.cfg.MaxConcurrent
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if p := a.loadProps(ctx); p != nil {
		return p.TotalSlots
	}
	return 0
}

// ---- provider.TokenCounter ----

// CountTokens calls POST /tokenize and memoises by content hash. Falls back to
// the shared heuristic (with an error) if the server is unreachable, so
// callers can degrade rather than fail.
func (a *Adapter) CountTokens(ctx context.Context, text string) (int, error) {
	if text == "" {
		return 0, nil
	}
	key := sha256.Sum256([]byte(text))
	a.tokMu.Lock()
	if n, ok := a.tokCache[key]; ok {
		a.tokMu.Unlock()
		return n, nil
	}
	a.tokMu.Unlock()

	tctx, cancel := context.WithTimeout(ctx, tokenizeTimeout)
	defer cancel()
	body := map[string]any{"content": text, "add_special": false, "with_pieces": false}
	if a.cfg.Model != "" {
		body["model"] = a.cfg.Model // required in router mode; ignored otherwise
	}
	resp, err := a.post(tctx, "/tokenize", body, a.client)
	if err != nil {
		return provider.HeuristicTokenCount(text), fmt.Errorf("llamacpp: tokenize: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return provider.HeuristicTokenCount(text), fmt.Errorf("llamacpp: tokenize returned %d", resp.StatusCode)
	}
	var out struct {
		Tokens []json.RawMessage `json:"tokens"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return provider.HeuristicTokenCount(text), fmt.Errorf("llamacpp: decode tokenize: %w", err)
	}
	n := len(out.Tokens)

	a.tokMu.Lock()
	if len(a.tokOrder) >= tokenizeCacheSize {
		oldest := a.tokOrder[0]
		a.tokOrder = a.tokOrder[1:]
		delete(a.tokCache, oldest)
	}
	a.tokCache[key] = n
	a.tokOrder = append(a.tokOrder, key)
	a.tokMu.Unlock()
	return n, nil
}

// ---- internals ----

// serverProps is the subset of GET /props Phoenix cares about.
type serverProps struct {
	// Role is "router" when llama-server runs in multi-model router mode
	// (--models-dir); the root /props then carries no model information and
	// per-model props live at /props?model=<id>.
	Role                      string `json:"role"`
	DefaultGenerationSettings struct {
		NCtx int `json:"n_ctx"`
	} `json:"default_generation_settings"`
	TotalSlots       int    `json:"total_slots"`
	ModelPath        string `json:"model_path"`
	ModelAlias       string `json:"model_alias"`
	ChatTemplateCaps struct {
		SupportsTools bool `json:"supports_tools"`
	} `json:"chat_template_caps"`
}

func (p *serverProps) isRouter() bool { return p != nil && p.Role == "router" }

func (p *serverProps) modelName() string {
	if p.isRouter() {
		return "" // router alias is "llama-server", not a model
	}
	if p.ModelAlias != "" {
		return p.ModelAlias
	}
	if p.ModelPath != "" && p.ModelPath != "none" {
		parts := strings.Split(p.ModelPath, "/")
		return parts[len(parts)-1]
	}
	return ""
}

// loadProps returns the (cached) server props. In router mode it resolves the
// configured model's own props via /props?model=… — but only if that model is
// already loaded, so a background health tick never forces a multi-GB model
// into memory. Once a task has loaded the model, the next probe picks it up.
func (a *Adapter) loadProps(ctx context.Context) *serverProps {
	a.propsMu.Lock()
	defer a.propsMu.Unlock()
	if a.props != nil && time.Since(a.propsFetched) < propsCacheTTL {
		return a.props
	}
	p := a.fetchProps(ctx, "/props")
	if p == nil {
		return a.props // stale is better than nothing
	}
	if p.isRouter() && a.cfg.Model != "" && a.routerModelLoaded(ctx, a.cfg.Model) {
		if mp := a.fetchProps(ctx, "/props?model="+url.QueryEscape(a.cfg.Model)); mp != nil && mp.DefaultGenerationSettings.NCtx > 0 {
			mp.Role = "" // resolved to a concrete model
			p = mp
		}
	}
	a.props = p
	a.propsFetched = time.Now()
	return a.props
}

func (a *Adapter) fetchProps(ctx context.Context, path string) *serverProps {
	resp, err := a.get(ctx, path)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var p serverProps
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return nil
	}
	return &p
}

// routerModelLoaded reports whether the router currently has the model
// resident (GET /models → status.value == "loaded").
func (a *Adapter) routerModelLoaded(ctx context.Context, model string) bool {
	resp, err := a.get(ctx, "/models")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var body struct {
		Data []struct {
			ID     string `json:"id"`
			Status struct {
				Value string `json:"value"`
			} `json:"status"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return false
	}
	for _, m := range body.Data {
		if m.ID == model {
			return m.Status.Value == "loaded"
		}
	}
	return false
}

// InvalidateProps drops the cached /props so the next Capabilities call
// re-probes (e.g. after the user restarts llama-server with a new --ctx-size).
func (a *Adapter) InvalidateProps() {
	a.propsMu.Lock()
	a.props = nil
	a.propsMu.Unlock()
}

func (a *Adapter) buildRequest(req provider.TaskRequest, stream bool) openaiwire.ChatRequest {
	msgs := make([]openaiwire.ChatMessage, 0, len(req.Context)+2)
	if req.SystemPrompt != "" {
		msgs = append(msgs, openaiwire.ChatMessage{Role: "system", Content: req.SystemPrompt})
	}
	for _, m := range req.Context {
		role := m.Role
		if role != "assistant" && role != "system" {
			role = "user"
		}
		msgs = append(msgs, openaiwire.ChatMessage{Role: role, Content: m.Content})
	}
	msgs = append(msgs, openaiwire.ChatMessage{Role: "user", Content: req.Prompt})

	cr := openaiwire.ChatRequest{
		Model:    a.cfg.Model,
		Messages: msgs,
		Stream:   stream,
	}
	if stream {
		cr.StreamOptions = &openaiwire.StreamOptions{IncludeUsage: true}
	}

	// Output cap: request → config → default. -1 means "unlimited" (omit).
	maxOut := a.cfg.MaxOutputTokens
	if req.MaxOutputTokens > 0 {
		maxOut = req.MaxOutputTokens
	}
	if maxOut == 0 {
		maxOut = defaultMaxOutputTokens
	}
	if maxOut > 0 {
		cr.MaxTokens = maxOut
	}

	if req.Temperature != nil {
		cr.Temperature = req.Temperature
	} else if a.cfg.Temperature != nil {
		cr.Temperature = a.cfg.Temperature
	}
	cr.Stop = req.StopSequences

	cache := true
	if a.cfg.CachePrompt != nil {
		cache = *a.cfg.CachePrompt
	}
	cr.CachePrompt = &cache

	if len(req.ResponseSchema) > 0 {
		cr.ResponseFormat = &openaiwire.ResponseFormat{
			Type:       "json_schema",
			JSONSchema: &openaiwire.JSONSchemaSpec{Name: "phoenix_response", Schema: req.ResponseSchema, Strict: true},
		}
	}
	return cr
}

func (a *Adapter) post(ctx context.Context, path string, body any, client *http.Client) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("llamacpp: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.BaseURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("llamacpp: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	a.auth(req)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llamacpp: %s: %w", path, err)
	}
	return resp, nil
}

func (a *Adapter) get(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.cfg.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	a.auth(req)
	return a.client.Do(req)
}

func (a *Adapter) auth(req *http.Request) {
	if a.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.cfg.APIKey)
	}
}

// looksLikeReasoningModel is a name heuristic for thinking models. Users can
// override with Config.Reasoning.
func looksLikeReasoningModel(name string) bool {
	n := strings.ToLower(name)
	for _, kw := range []string{"qwen3", "deepseek-r1", "-r1", "r1-", "reason", "think", "gpt-oss", "magistral", "qwq"} {
		if strings.Contains(n, kw) {
			return true
		}
	}
	return false
}
