package openaiwire

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/solarisjon/phoenix/internal/provider"
)

func collect(t *testing.T, body string, opts SSEOptions) (text string, done provider.StreamChunk) {
	t.Helper()
	ch := make(chan provider.StreamChunk, 64)
	ReadSSE(context.Background(), strings.NewReader(body), ch, opts)
	close(ch)
	var sb strings.Builder
	for c := range ch {
		if c.Done {
			done = c
			continue
		}
		sb.WriteString(c.Content)
	}
	return sb.String(), done
}

func TestReadSSE_ContentUsageAndDone(t *testing.T) {
	body := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"Hel"}}]}`,
		`data: {"choices":[{"delta":{"content":"lo"},"finish_reason":"stop"}]}`,
		`data: {"choices":[],"usage":{"prompt_tokens":11,"completion_tokens":2}}`,
		`data: [DONE]`,
	}, "\n\n")
	var got Usage
	text, done := collect(t, body, SSEOptions{Finish: func(u Usage) provider.StreamChunk {
		got = u
		return provider.StreamChunk{Done: true, TokensIn: u.PromptTokens, TokensOut: u.CompletionTokens, CostUSD: 0.5}
	}})
	if text != "Hello" {
		t.Errorf("text = %q", text)
	}
	if !done.Done || done.TokensIn != 11 || done.TokensOut != 2 || done.CostUSD != 0.5 {
		t.Errorf("done chunk = %+v", done)
	}
	if got.FinishReason != "stop" {
		t.Errorf("finish reason = %q", got.FinishReason)
	}
}

func TestReadSSE_ReasoningStrippedUnlessAsked(t *testing.T) {
	body := `data: {"choices":[{"delta":{"reasoning_content":"thinking…"}}]}` + "\n" +
		`data: {"choices":[{"delta":{"content":"answer"}}]}` + "\n" + `data: [DONE]`
	text, _ := collect(t, body, SSEOptions{})
	if text != "answer" {
		t.Errorf("reasoning leaked: %q", text)
	}
	text, _ = collect(t, body, SSEOptions{IncludeReasoning: true})
	if text != "thinking…answer" {
		t.Errorf("reasoning not included: %q", text)
	}
}

func TestReadSSE_NoDoneMarkerStillFinishes(t *testing.T) {
	body := `data: {"choices":[{"delta":{"content":"x"}}]}` + "\n" + `data: garbage`
	text, done := collect(t, body, SSEOptions{})
	if text != "x" || !done.Done {
		t.Errorf("text=%q done=%+v", text, done)
	}
}

func TestChatRequest_OmitsUnset(t *testing.T) {
	raw, _ := json.Marshal(ChatRequest{Model: "m", Messages: []ChatMessage{{Role: "user", Content: "hi"}}})
	for _, k := range []string{"max_tokens", "temperature", "stop", "response_format", "cache_prompt", "system"} {
		if strings.Contains(string(raw), `"`+k+`"`) {
			t.Errorf("unexpected %s in %s", k, raw)
		}
	}
	cp := true
	raw, _ = json.Marshal(ChatRequest{Model: "m", CachePrompt: &cp, ResponseFormat: &ResponseFormat{Type: "json_schema", JSONSchema: &JSONSchemaSpec{Name: "x", Schema: json.RawMessage(`{"type":"object"}`)}}})
	if !strings.Contains(string(raw), `"cache_prompt":true`) || !strings.Contains(string(raw), `"json_schema":{"name":"x","schema":{"type":"object"}}`) {
		t.Errorf("extensions missing: %s", raw)
	}
}

func TestParseCompletion(t *testing.T) {
	c, err := ParseCompletion([]byte(`{"choices":[{"message":{"content":"ok","reasoning_content":"why"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1}}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Text() != "ok" || c.Reasoning() != "why" || c.Usage.PromptTokens != 3 {
		t.Errorf("parsed = %+v", c)
	}
	if (Completion{}).Text() != "" {
		t.Error("empty completion text")
	}
}
