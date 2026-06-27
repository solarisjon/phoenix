// Package pi provides a Provider adapter that runs tasks via the pi CLI
// (https://github.com/earendil-works/pi). This gives Phoenix agents access
// to pi's full toolchain: file system access, bash execution, MCP servers,
// extensions, and any other tools pi supports.
//
// Tasks are executed as:
//
//	pi --print --mode json [--model <model>] [--system-prompt <prompt>]
//	   [--no-tools] [--tools <list>] [--exclude-tools <list>]
//	   [--thinking <level>] "<prompt>"
//
// The adapter streams newline-delimited JSON events from stdout, collecting
// text deltas from message_update events and usage from message_end events.
package pi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"sync"

	"github.com/solarisjon/phoenix/internal/provider"
)

// Config holds the configuration for the pi adapter.
type Config struct {
	// BinaryPath is the path to the pi binary.
	// Defaults to "pi" (resolved via PATH).
	BinaryPath string `json:"binary_path"`

	// Model selects the provider/model to use, e.g. "llm-proxy/claude-sonnet-4.6"
	// or a bare model pattern. If empty, pi uses its configured default.
	Model string `json:"model"`

	// WorkingDir sets the directory pi runs in.
	// Supports ${ENV_VAR} expansion (handled by the registry before reaching here).
	WorkingDir string `json:"working_dir"`

	// Thinking sets the thinking level: off, minimal, low, medium, high, xhigh.
	// If empty, pi uses its default.
	Thinking string `json:"thinking"`

	// Tools is an allowlist of tool names to enable (comma-separated).
	// If empty, pi uses its defaults.
	Tools string `json:"tools"`

	// ExcludeTools is a denylist of tool names to disable (comma-separated).
	ExcludeTools string `json:"exclude_tools"`

	// NoTools disables all tools if true.
	NoTools bool `json:"no_tools"`

	// AllowSession enables pi session persistence. Default false —
	// Phoenix manages task state so each task runs fresh with no_session.
	AllowSession bool `json:"allow_session"`

	// ExtraArgs are passed verbatim to the pi CLI after all other flags.
	ExtraArgs []string `json:"extra_args"`
}

// Adapter implements provider.Provider using the pi CLI.
type Adapter struct {
	cfg Config
}

// New creates an Adapter from a JSON config blob.
func New(configJSON string) (*Adapter, error) {
	var cfg Config
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return nil, fmt.Errorf("parse pi config: %w", err)
	}
	if cfg.BinaryPath == "" {
		cfg.BinaryPath = "pi"
	}
	return &Adapter{cfg: cfg}, nil
}

// Execute runs a task to completion and returns the full response.
func (a *Adapter) Execute(ctx context.Context, req provider.TaskRequest) (provider.TaskResponse, error) {
	ch, err := a.StreamExecute(ctx, req)
	if err != nil {
		return provider.TaskResponse{}, err
	}

	var sb strings.Builder
	for chunk := range ch {
		if chunk.Error != nil {
			return provider.TaskResponse{}, chunk.Error
		}
		sb.WriteString(chunk.Content)
	}

	return provider.TaskResponse{Output: sb.String()}, nil
}

// StreamExecute runs a task via pi and streams output chunks.
func (a *Adapter) StreamExecute(ctx context.Context, req provider.TaskRequest) (<-chan provider.StreamChunk, error) {
	// Build the prompt and always deliver it via stdin. This avoids ARG_MAX
	// limits that silently kill long prompts (e.g. follow-up chains with
	// injected parent output). pi reads from stdin when no positional prompt
	// argument is provided.
	promptText := a.buildPrompt(req)
	args := a.buildArgs(req)

	cmd := exec.CommandContext(ctx, a.cfg.BinaryPath, args...)
	switch {
	case req.WorkingDir != "":
		cmd.Dir = req.WorkingDir
	case a.cfg.WorkingDir != "":
		cmd.Dir = a.cfg.WorkingDir
	}
	cmd.Stdin = strings.NewReader(promptText)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("pi: stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("pi: stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("pi: start: %w", err)
	}

	ch := make(chan provider.StreamChunk, 64)
	pid := cmd.Process.Pid

	go func() {
		defer close(ch)

		// Collect stderr concurrently so it doesn't block stdout reads.
		var stderrBuf bytes.Buffer
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			io.Copy(&stderrBuf, stderr) //nolint:errcheck
		}()

		// First chunk carries the PID; no content.
		ch <- provider.StreamChunk{PID: pid}
		outputCount := a.parseStream(ctx, stdout, ch)

		wg.Wait()
		if err := cmd.Wait(); err != nil {
			slog.Debug("pi: process exited", "error", err)
		}

		if outputCount == 0 {
			// No text output — surface stderr so the user can see the actual
			// reason (auth error, rate limit, model error, etc.).
			if stderrMsg := strings.TrimSpace(stderrBuf.String()); stderrMsg != "" {
				slog.Debug("pi: stderr", "msg", stderrMsg)
				ch <- provider.StreamChunk{Error: fmt.Errorf("pi: no output — stderr: %s", stderrMsg)}
				return
			}
		}
	}()

	return ch, nil
}

// EstimateCost returns zero — pi reports actual cost after execution.
func (a *Adapter) EstimateCost(_ provider.TaskRequest) provider.CostEstimate {
	return provider.CostEstimate{}
}

// ListModels runs `pi --list-models` and parses the tabular output.
// Output format: "provider  model  context  max-out  thinking  images"
// Implements provider.ModelLister.
func (a *Adapter) ListModels(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, a.cfg.BinaryPath, "--list-models")
	// pi writes the model table to stderr (same pipe as deprecation warnings).
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("pi: list-models: %w", err)
	}

	var models []string
	for _, line := range strings.Split(string(out), "\n") {
		// Skip header, blank lines, and deprecation warnings
		if line == "" || strings.HasPrefix(line, "provider") || strings.HasPrefix(line, "Deprecation") {
			continue
		}
		// Columns are whitespace-separated; model is the second column
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			models = append(models, fields[1])
		}
	}
	return models, nil
}

// ---- Internal helpers ----

// buildArgs assembles the pi CLI arguments. The prompt is always delivered
// via stdin, so no positional prompt argument is appended here.
func (a *Adapter) buildArgs(req provider.TaskRequest) []string {
	args := []string{"--print", "--mode", "json"}

	if a.cfg.Model != "" {
		args = append(args, "--model", a.cfg.Model)
	}
	if req.SystemPrompt != "" {
		args = append(args, "--system-prompt", req.SystemPrompt)
	}
	if a.cfg.Thinking != "" {
		args = append(args, "--thinking", a.cfg.Thinking)
	}
	if a.cfg.NoTools {
		args = append(args, "--no-tools")
	} else {
		if a.cfg.Tools != "" {
			args = append(args, "--tools", a.cfg.Tools)
		}
		if a.cfg.ExcludeTools != "" {
			args = append(args, "--exclude-tools", a.cfg.ExcludeTools)
		}
	}
	// Always run without session persistence unless explicitly enabled.
	// Phoenix manages task state; each task should start fresh.
	if !a.cfg.AllowSession {
		args = append(args, "--no-session")
	}
	args = append(args, a.cfg.ExtraArgs...)
	// Prompt is delivered via stdin — no positional argument appended.
	return args
}

func (a *Adapter) buildPrompt(req provider.TaskRequest) string {
	if len(req.Context) == 0 {
		return req.Prompt
	}
	var b strings.Builder
	for _, m := range req.Context {
		b.WriteString(fmt.Sprintf("<%s>\n%s\n</%s>\n\n", m.Role, m.Content, m.Role))
	}
	b.WriteString(req.Prompt)
	return b.String()
}

// ---- JSON event parsing ----
// pi --mode json emits newline-delimited JSON objects.
//
// Key event types we care about:
//   - message_update  → assistantMessageEvent with type "text_delta" contains streaming text
//   - message_end     → final assistant message with usage stats
//   - agent_end       → signals completion

type piEvent struct {
	Type                 string              `json:"type"`
	AssistantMessageEvent *piMessageEvent    `json:"assistantMessageEvent"`
	Message              *piMessage          `json:"message"`
}

type piMessageEvent struct {
	Type  string `json:"type"`  // "text_start", "text_delta", "text_end", etc.
	Delta string `json:"delta"` // only on text_delta
}

type piMessage struct {
	Role    string      `json:"role"`
	Content []piContent `json:"content"`
	Usage   *piUsage    `json:"usage"`
}

type piContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type piUsage struct {
	Input  int     `json:"input"`
	Output int     `json:"output"`
	Cost   *piCost `json:"cost"`
}

type piCost struct {
	Total float64 `json:"total"`
}

// parseStream reads pi NDJSON events from r, sends content chunks to ch,
// and returns the number of text-delta chunks emitted.
func (a *Adapter) parseStream(ctx context.Context, r io.Reader, ch chan<- provider.StreamChunk) int {
	var outputCount int
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 4*1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			ch <- provider.StreamChunk{Error: ctx.Err(), Done: true}
			return outputCount
		default:
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var ev piEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			// Non-JSON — pass through (e.g. deprecation warnings on stderr leaking).
			continue
		}

		switch ev.Type {
		case "message_update":
			if ev.AssistantMessageEvent == nil {
				continue
			}
			if ev.AssistantMessageEvent.Type == "text_delta" && ev.AssistantMessageEvent.Delta != "" {
				ch <- provider.StreamChunk{Content: ev.AssistantMessageEvent.Delta}
				outputCount++
			}

		case "message_end":
			if ev.Message == nil || ev.Message.Role != "assistant" {
				continue
			}
			if ev.Message.Usage != nil {
				cost := 0.0
				if ev.Message.Usage.Cost != nil {
					cost = ev.Message.Usage.Cost.Total
				}
				slog.Debug("pi: message end", "input_tokens", ev.Message.Usage.Input, "output_tokens", ev.Message.Usage.Output, "cost_usd", cost)
			}

		case "agent_end":
			// Normal completion — nothing to do, stream ends naturally.

		case "session", "agent_start", "turn_start", "turn_end",
			"message_start", "tool_use", "tool_result":
			// Informational — ignored.

		default:
			// Unknown event types silently ignored for forward compatibility.
		}
	}

	if err := scanner.Err(); err != nil {
		ch <- provider.StreamChunk{Error: fmt.Errorf("pi stream: %w", err), Done: true}
		return outputCount
	}

	ch <- provider.StreamChunk{Done: true}
	return outputCount
}
