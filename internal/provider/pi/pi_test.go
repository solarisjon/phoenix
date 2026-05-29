package pi

import (
	"context"
	"strings"
	"testing"

	"github.com/solarisjon/phoenix/internal/provider"
)

func TestNew_Defaults(t *testing.T) {
	a, err := New(`{}`)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a.cfg.BinaryPath != "pi" {
		t.Errorf("BinaryPath = %q, want pi", a.cfg.BinaryPath)
	}
}

func TestNew_InvalidJSON(t *testing.T) {
	_, err := New(`not json`)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestBuildArgs_MinimalConfig(t *testing.T) {
	a, _ := New(`{}`)
	req := provider.TaskRequest{Prompt: "hello"}
	args := a.buildArgs(req)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--print") {
		t.Error("missing --print")
	}
	if !strings.Contains(joined, "--mode json") {
		t.Error("missing --mode json")
	}
	// --no-session must be present by default so pi runs non-interactively
	if !strings.Contains(joined, "--no-session") {
		t.Errorf("missing --no-session in default config; args: %s", joined)
	}
	if args[len(args)-1] != "hello" {
		t.Errorf("last arg = %q, want hello", args[len(args)-1])
	}
}

func TestBuildArgs_AllowSession(t *testing.T) {
	// When allow_session=true, --no-session must NOT be present
	a, _ := New(`{"allow_session":true}`)
	args := a.buildArgs(provider.TaskRequest{Prompt: "p"})
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "--no-session") {
		t.Errorf("--no-session present when allow_session=true; args: %s", joined)
	}
}

func TestBuildArgs_WithModel(t *testing.T) {
	a, _ := New(`{"model":"llm-proxy/claude-sonnet-4.6"}`)
	args := a.buildArgs(provider.TaskRequest{Prompt: "p"})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--model llm-proxy/claude-sonnet-4.6") {
		t.Errorf("missing --model in: %s", joined)
	}
}

func TestBuildArgs_Thinking(t *testing.T) {
	a, _ := New(`{"thinking":"high"}`)
	args := a.buildArgs(provider.TaskRequest{Prompt: "p"})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--thinking high") {
		t.Errorf("missing --thinking in: %s", joined)
	}
}

func TestBuildArgs_NoTools(t *testing.T) {
	a, _ := New(`{"no_tools":true}`)
	args := a.buildArgs(provider.TaskRequest{Prompt: "p"})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--no-tools") {
		t.Errorf("missing --no-tools in: %s", joined)
	}
}

func TestBuildArgs_SystemPrompt(t *testing.T) {
	a, _ := New(`{}`)
	req := provider.TaskRequest{SystemPrompt: "You are a bot.", Prompt: "hi"}
	args := a.buildArgs(req)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--system-prompt") {
		t.Errorf("missing --system-prompt in: %s", joined)
	}
}

func runParseStream(a *Adapter, input string) <-chan provider.StreamChunk {
	ch := make(chan provider.StreamChunk, 64)
	go func() {
		defer close(ch)
		a.parseStream(context.Background(), strings.NewReader(input), ch)
	}()
	return ch
}

func TestParseStream_TextDeltas(t *testing.T) {
	a, _ := New(`{}`)
	events := strings.Join([]string{
		`{"type":"session","version":3,"id":"abc"}`,
		`{"type":"agent_start"}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"Hello "},"message":{"role":"assistant","content":[]}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"world!"},"message":{"role":"assistant","content":[]}}`,
		`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"Hello world!"}],"usage":{"input":10,"output":5,"cost":{"total":0.0001}}}}`,
		`{"type":"agent_end","messages":[],"willRetry":false}`,
	}, "\n") + "\n"

	var assembled strings.Builder
	for chunk := range runParseStream(a, events) {
		if chunk.Error != nil {
			t.Fatalf("unexpected error: %v", chunk.Error)
		}
		assembled.WriteString(chunk.Content)
	}
	if assembled.String() != "Hello world!" {
		t.Errorf("assembled = %q, want %q", assembled.String(), "Hello world!")
	}
}

func TestParseStream_NonJSONSkipped(t *testing.T) {
	a, _ := New(`{}`)
	// Deprecation warnings from pi arrive on stderr but could bleed to stdout in rare cases.
	input := "Deprecation warning: something\n" +
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"ok"},"message":{"role":"assistant","content":[]}}` + "\n"

	var assembled strings.Builder
	for chunk := range runParseStream(a, input) {
		assembled.WriteString(chunk.Content)
	}
	if assembled.String() != "ok" {
		t.Errorf("assembled = %q, want ok", assembled.String())
	}
}

func TestParseStream_ContextCancellation(t *testing.T) {
	a, _ := New(`{}`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	input := `{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"hi"},"message":{}}` + "\n"

	ch := make(chan provider.StreamChunk, 64)
	go func() {
		defer close(ch)
		a.parseStream(ctx, strings.NewReader(input), ch)
	}()

	var gotErr error
	for chunk := range ch {
		if chunk.Error != nil {
			gotErr = chunk.Error
		}
	}
	if gotErr == nil {
		t.Fatal("expected cancellation error")
	}
}

func TestEstimateCost(t *testing.T) {
	a, _ := New(`{}`)
	est := a.EstimateCost(provider.TaskRequest{Prompt: "test"})
	if est.EstimatedCostUSD != 0 {
		t.Errorf("expected 0, got %v", est.EstimatedCostUSD)
	}
}
