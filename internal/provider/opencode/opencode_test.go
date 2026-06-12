package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/solarisjon/phoenix/internal/provider"
)

// ---- Config tests ----

func TestNew_Defaults(t *testing.T) {
	a, err := New(`{}`)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a.cfg.BinaryPath != "opencode" {
		t.Errorf("BinaryPath = %q, want opencode", a.cfg.BinaryPath)
	}
}

func TestNew_InvalidJSON(t *testing.T) {
	_, err := New(`not json`)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestNew_CustomConfig(t *testing.T) {
	cfg := map[string]interface{}{
		"binary_path":                  "/usr/local/bin/opencode",
		"model":                        "llm-proxy/claude-sonnet-4.6",
		"agent":                        "my-agent",
		"working_dir":                  "/tmp/workspace",
		"dangerously_skip_permissions": true,
	}
	data, _ := json.Marshal(cfg)
	a, err := New(string(data))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a.cfg.Model != "llm-proxy/claude-sonnet-4.6" {
		t.Errorf("Model = %q", a.cfg.Model)
	}
	if !a.cfg.DangerouslySkipPermissions {
		t.Error("DangerouslySkipPermissions should be true")
	}
}

// ---- Prompt assembly tests ----

func TestBuildPrompt_SystemAndUser(t *testing.T) {
	a, _ := New(`{}`)
	req := provider.TaskRequest{
		SystemPrompt: "You are an expert engineer.",
		Prompt:       "Review this code.",
	}
	prompt := a.buildPrompt(req)
	if !strings.Contains(prompt, "<system>") {
		t.Error("expected <system> tag")
	}
	if !strings.Contains(prompt, "You are an expert engineer.") {
		t.Error("expected persona in prompt")
	}
	if !strings.Contains(prompt, "Review this code.") {
		t.Error("expected task prompt")
	}
}

func TestBuildPrompt_NoSystemPrompt(t *testing.T) {
	a, _ := New(`{}`)
	req := provider.TaskRequest{Prompt: "Just the task."}
	prompt := a.buildPrompt(req)
	if strings.Contains(prompt, "<system>") {
		t.Error("should not include system tags when SystemPrompt is empty")
	}
	if prompt != "Just the task." {
		t.Errorf("prompt = %q", prompt)
	}
}

func TestBuildPrompt_WithContext(t *testing.T) {
	a, _ := New(`{}`)
	req := provider.TaskRequest{
		Prompt: "Continue.",
		Context: []provider.Message{
			{Role: "user", Content: "Previous message."},
			{Role: "assistant", Content: "Previous response."},
		},
	}
	prompt := a.buildPrompt(req)
	if !strings.Contains(prompt, "<user>") {
		t.Error("expected user context tag")
	}
	if !strings.Contains(prompt, "Previous message.") {
		t.Error("expected context content")
	}
}

// ---- Args building tests ----

func TestBuildArgs_MinimalConfig(t *testing.T) {
	a, _ := New(`{}`)
	args := a.buildArgs("hello")
	if args[0] != "run" {
		t.Errorf("args[0] = %q, want run", args[0])
	}
	if args[1] != "--format" || args[2] != "json" {
		t.Errorf("missing --format json in args: %v", args)
	}
	if args[len(args)-1] != "hello" {
		t.Errorf("last arg = %q, want hello", args[len(args)-1])
	}
	// Should NOT contain --model or --agent
	for _, a := range args {
		if a == "--model" || a == "--agent" {
			t.Errorf("unexpected flag %q when not configured", a)
		}
	}
}

func TestBuildArgs_WithModel(t *testing.T) {
	a, _ := New(`{"model":"llm-proxy/claude-sonnet-4.6","agent":"my-agent"}`)
	args := a.buildArgs("prompt")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--model llm-proxy/claude-sonnet-4.6") {
		t.Errorf("missing --model in: %s", joined)
	}
	if !strings.Contains(joined, "--agent my-agent") {
		t.Errorf("missing --agent in: %s", joined)
	}
}

func TestBuildArgs_DangerouslySkipPermissions(t *testing.T) {
	a, _ := New(`{"dangerously_skip_permissions":true}`)
	args := a.buildArgs("prompt")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--dangerously-skip-permissions") {
		t.Errorf("missing flag in: %s", joined)
	}
}

func TestBuildArgs_ExtraArgs(t *testing.T) {
	a, _ := New(`{"extra_args":["--thinking","--variant","high"]}`)
	args := a.buildArgs("prompt")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--thinking") {
		t.Errorf("missing --thinking in: %s", joined)
	}
}

// ---- Stream parsing tests ----

func runParseStream(a *Adapter, input string) <-chan provider.StreamChunk {
	ch := make(chan provider.StreamChunk, 64)
	go func() {
		defer close(ch)
		a.parseStream(context.Background(), strings.NewReader(input), ch)
	}()
	return ch
}

func TestParseStream_TextEvents(t *testing.T) {
	a, _ := New(`{}`)
	events := strings.Join([]string{
		`{"type":"step_start","part":{}}`,
		`{"type":"text","part":{"text":"Hello "}}`,
		`{"type":"text","part":{"text":"world!"}}`,
		`{"type":"step_finish","part":{"cost":0.001,"tokens":{"input":10,"output":5,"total":15}}}`,
	}, "\n") + "\n"

	var texts []string
	for chunk := range runParseStream(a, events) {
		if chunk.Error != nil {
			t.Fatalf("unexpected error: %v", chunk.Error)
		}
		if chunk.Content != "" {
			texts = append(texts, chunk.Content)
		}
	}
	if strings.Join(texts, "") != "Hello world!" {
		t.Errorf("assembled = %q", strings.Join(texts, ""))
	}
}

func TestParseStream_ErrorEvent(t *testing.T) {
	a, _ := New(`{}`)
	events := `{"type":"error","error":{"name":"UnknownError","data":{"message":"Model not found"}}}`

	var gotErr error
	for chunk := range runParseStream(a, events) {
		if chunk.Error != nil {
			gotErr = chunk.Error
		}
	}
	if gotErr == nil {
		t.Fatal("expected error chunk")
	}
	if !strings.Contains(gotErr.Error(), "Model not found") {
		t.Errorf("error = %q, want 'Model not found'", gotErr)
	}
}

func TestParseStream_NonJSONLines(t *testing.T) {
	a, _ := New(`{}`)
	input := `{"type":"text","part":{"text":"structured "}}` + "\n" + `plain text line` + "\n"

	var assembled strings.Builder
	for chunk := range runParseStream(a, input) {
		assembled.WriteString(chunk.Content)
	}
	if !strings.Contains(assembled.String(), "structured") {
		t.Error("expected structured text")
	}
	if !strings.Contains(assembled.String(), "plain text line") {
		t.Error("expected plain text passthrough")
	}
}

func TestParseStream_ContextCancellation(t *testing.T) {
	a, _ := New(`{}`)
	ctx, cancel := context.WithCancel(context.Background())

	// A stream with a text event followed by a long pause — simulated by
	// a pipe we close after cancelling.
	input := `{"type":"text","part":{"text":"partial"}}` + "\n"

	// Cancel before giving it any blocking input.
	cancel()

	var gotErr error
	for chunk := range runParseStreamCtx(a, ctx, input) {
		if chunk.Error != nil {
			gotErr = chunk.Error
		}
	}
	if gotErr == nil {
		t.Fatal("expected cancellation error")
	}
}

func runParseStreamCtx(a *Adapter, ctx context.Context, input string) <-chan provider.StreamChunk {
	ch := make(chan provider.StreamChunk, 64)
	go func() {
		defer close(ch)
		a.parseStream(ctx, strings.NewReader(input), ch)
	}()
	return ch
}

func TestEstimateCost(t *testing.T) {
	a, _ := New(`{}`)
	est := a.EstimateCost(provider.TaskRequest{Prompt: "test"})
	// opencode doesn't estimate pre-run — always zero.
	if est.EstimatedCostUSD != 0 {
		t.Errorf("expected 0, got %v", est.EstimatedCostUSD)
	}
}

// ---- Integration test (skipped unless opencode is available) ----

func TestIntegration_RealOpencode(t *testing.T) {
	if os.Getenv("PHOENIX_INTEGRATION_TESTS") != "1" {
		t.Skip("set PHOENIX_INTEGRATION_TESTS=1 to run integration tests")
	}
	if _, err := exec.LookPath("opencode"); err != nil {
		t.Skip("opencode not in PATH, skipping integration test")
	}

	a, err := New(`{}`)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req := provider.TaskRequest{
		SystemPrompt: "You are a concise assistant. Answer in one sentence only.",
		Prompt:       "What is 2+2? Answer with just the number.",
	}

	resp, err := a.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.Output == "" {
		t.Error("expected non-empty output")
	}
	if !strings.Contains(resp.Output, "4") {
		t.Errorf("expected '4' in output, got: %q", resp.Output)
	}
	fmt.Printf("Integration output: %q\n", resp.Output)
}
