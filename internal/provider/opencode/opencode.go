// Package opencode provides a Provider adapter that runs tasks via the
// opencode CLI tool (https://opencode.ai). This gives Phoenix agents
// access to opencode's full toolchain: MCP servers, file system access,
// web search, code execution, and any other tools opencode supports.
//
// Tasks are executed as:
//
//	opencode run --format json [--model <model>] [--agent <agent>] [--dir <dir>] "<prompt>"
//
// The adapter streams JSON events from stdout, collecting text chunks
// and extracting token counts and cost from the step_finish event.
package opencode

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"syscall"

	"github.com/solarisjon/phoenix/internal/provider"
)

// Config holds the configuration for the opencode adapter.
type Config struct {
	// BinaryPath is the path to the opencode binary.
	// Defaults to "opencode" (resolved via PATH).
	BinaryPath string `json:"binary_path"`

	// Model selects the provider/model to use, e.g. "llm-proxy/claude-sonnet-4.6".
	// If empty, opencode uses its configured default.
	Model string `json:"model"`

	// Agent selects a named opencode agent configuration.
	// If empty, opencode uses its default agent.
	Agent string `json:"agent"`

	// WorkingDir sets the directory opencode runs in.
	// Useful for code tasks so the agent sees the right files.
	// Supports ${ENV_VAR} expansion (handled by the registry before reaching here).
	WorkingDir string `json:"working_dir"`

	// DangerouslySkipPermissions auto-approves tool use without prompting.
	// Only enable in trusted, sandboxed environments.
	DangerouslySkipPermissions bool `json:"dangerously_skip_permissions"`

	// ExtraArgs are passed verbatim to the opencode CLI after all other flags.
	ExtraArgs []string `json:"extra_args"`
}

// Adapter implements provider.Provider using the opencode CLI.
type Adapter struct {
	cfg Config
}

// New creates an Adapter from a JSON config blob.
func New(configJSON string) (*Adapter, error) {
	var cfg Config
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return nil, fmt.Errorf("parse opencode config: %w", err)
	}
	if cfg.BinaryPath == "" {
		cfg.BinaryPath = "opencode"
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
	var final provider.TaskResponse

	for chunk := range ch {
		if chunk.Error != nil {
			return provider.TaskResponse{}, chunk.Error
		}
		sb.WriteString(chunk.Content)
	}

	final.Output = sb.String()
	return final, nil
}

// StreamExecute runs a task via opencode and streams output chunks.
func (a *Adapter) StreamExecute(ctx context.Context, req provider.TaskRequest) (<-chan provider.StreamChunk, error) {
	prompt := a.buildPrompt(req)
	args := a.buildArgs(prompt)

	// Use exec.Command (not CommandContext) so we can manage process-group
	// termination ourselves. CommandContext only kills the direct PID; opencode
	// spawns child processes (nvim, tool helpers) that inherit the stdout pipe
	// and keep it alive indefinitely after the parent dies, causing the goroutine
	// to block on scanner.Scan() until those children happen to exit on their own.
	cmd := exec.Command(a.cfg.BinaryPath, args...) //nolint:gosec
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	switch {
	case req.WorkingDir != "":
		cmd.Dir = req.WorkingDir
	case a.cfg.WorkingDir != "":
		cmd.Dir = a.cfg.WorkingDir
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("opencode: stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("opencode: stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("opencode: start: %w", err)
	}

	ch := make(chan provider.StreamChunk, 64)

	// Send PID as the first chunk so the runner can record it for crash recovery.
	pid := cmd.Process.Pid

	// Kill the entire process group when the context is done. This ensures
	// all child processes spawned by opencode (e.g. nvim subprocesses for file
	// editing) are also terminated, closing their inherited stdout pipes and
	// unblocking scanner.Scan() promptly.
	go func() {
		<-ctx.Done()
		if pgid, err := syscall.Getpgid(pid); err == nil {
			if killErr := syscall.Kill(-pgid, syscall.SIGKILL); killErr != nil {
				slog.Error("opencode: kill process group", "pgid", pgid, "error", killErr)
			}
		}
	}()

	go func() {
		defer close(ch)

		// Collect stderr concurrently so it doesn't block stdout reads.
		var stderrBuf strings.Builder
		stderrDone := make(chan struct{})
		go func() {
			defer close(stderrDone)
			io.Copy(&stderrBuf, stderr) //nolint:errcheck
		}()

		// First chunk carries the PID; no content.
		ch <- provider.StreamChunk{PID: pid}
		gotOutput := a.parseStream(ctx, stdout, ch)

		<-stderrDone

		if err := cmd.Wait(); err != nil {
			slog.Debug("opencode: process exited", "error", err)
		}

		// If opencode produced no text output, surface stderr so the caller
		// can see startup / auth errors (e.g. "Unknown authorization credentials").
		if !gotOutput {
			if stderrMsg := strings.TrimSpace(stderrBuf.String()); stderrMsg != "" {
				slog.Debug("opencode: stderr", "msg", stderrMsg)
				ch <- provider.StreamChunk{Error: fmt.Errorf("opencode: no output — %s", stderrMsg), Done: true}
			}
		}
	}()

	return ch, nil
}

// EstimateCost returns zero — opencode reports actual cost after execution.
func (a *Adapter) EstimateCost(_ provider.TaskRequest) provider.CostEstimate {
	return provider.CostEstimate{}
}

// ---- Internal helpers ----

func (a *Adapter) buildPrompt(req provider.TaskRequest) string {
	var b strings.Builder

	// Inject the system prompt (persona + instructions + guardrails) as context.
	if req.SystemPrompt != "" {
		b.WriteString("<system>\n")
		b.WriteString(req.SystemPrompt)
		b.WriteString("\n</system>\n\n")
	}

	// Include prior context turns if present.
	for _, m := range req.Context {
		b.WriteString(fmt.Sprintf("<%s>\n%s\n</%s>\n\n", m.Role, m.Content, m.Role))
	}

	b.WriteString(req.Prompt)
	return b.String()
}

func (a *Adapter) buildArgs(prompt string) []string {
	args := []string{"run", "--format", "json"}

	if a.cfg.Model != "" {
		args = append(args, "--model", a.cfg.Model)
	}
	if a.cfg.Agent != "" {
		args = append(args, "--agent", a.cfg.Agent)
	}
	if a.cfg.DangerouslySkipPermissions {
		args = append(args, "--dangerously-skip-permissions")
	}
	args = append(args, a.cfg.ExtraArgs...)
	args = append(args, prompt)

	return args
}

// sanitisedPlaceholder is the string opencode injects when Claude returns a
// tool-use-only assistant turn with no text content. It satisfies the
// Anthropic protocol requirement that every assistant message have non-empty
// content, but it is an implementation detail of opencode's message history
// and should never appear in Phoenix task output.
const sanitisedPlaceholder = "[System: Empty message content sanitised to satisfy protocol]"

// stripSanitisedPlaceholders removes all occurrences of the opencode empty-
// message placeholder from text output before it is forwarded to Phoenix.
func stripSanitisedPlaceholders(s string) string {
	return strings.ReplaceAll(s, sanitisedPlaceholder, "")
}

// ---- JSON event parsing ----

// opencodeEvent is the envelope for all opencode JSON output lines.
type opencodeEvent struct {
	Type string          `json:"type"`
	Part json.RawMessage `json:"part"`
	Err  *opencodeError  `json:"error"`
}

type opencodeError struct {
	Name string `json:"name"`
	Data struct {
		Message string `json:"message"`
	} `json:"data"`
}

type textPart struct {
	Text string `json:"text"`
}

type stepFinishPart struct {
	Cost   float64 `json:"cost"`
	Tokens struct {
		Input  int `json:"input"`
		Output int `json:"output"`
		Total  int `json:"total"`
	} `json:"tokens"`
}

// parseStream reads opencode's JSON event stream and sends chunks on ch.
// Returns true if any text content was emitted (used to decide whether to
// surface stderr as an error when the process produces no output).
func (a *Adapter) parseStream(ctx context.Context, r io.Reader, ch chan<- provider.StreamChunk) bool {
	scanner := bufio.NewScanner(r)
	// Increase buffer for long lines (opencode can emit large JSON blobs).
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	// Accumulate cost and tokens across steps (multi-step tasks have multiple step_finish events).
	var totalCost float64
	var totalTokensIn, totalTokensOut int
	var gotOutput bool

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			ch <- provider.StreamChunk{Error: ctx.Err(), Done: true}
			return gotOutput
		default:
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var ev opencodeEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			// Non-JSON line — pass through as plain text.
			if line != "" {
				clean := stripSanitisedPlaceholders(line)
				if clean != "" {
					ch <- provider.StreamChunk{Content: clean + "\n"}
					gotOutput = true
				}
			}
			continue
		}

		switch ev.Type {
		case "text":
			var p textPart
			if err := json.Unmarshal(ev.Part, &p); err == nil && p.Text != "" {
				clean := stripSanitisedPlaceholders(p.Text)
				if clean != "" {
					ch <- provider.StreamChunk{Content: clean}
					gotOutput = true
				}
			}

		case "step_finish":
			var p stepFinishPart
			if err := json.Unmarshal(ev.Part, &p); err == nil {
				slog.Debug("opencode: step finished", "input_tokens", p.Tokens.Input, "output_tokens", p.Tokens.Output, "cost_usd", p.Cost)
				totalCost += p.Cost
				totalTokensIn += p.Tokens.Input
				totalTokensOut += p.Tokens.Output
			}

		case "error":
			msg := "opencode error"
			if ev.Err != nil {
				msg = ev.Err.Data.Message
				if msg == "" {
					msg = ev.Err.Name
				}
			}
			ch <- provider.StreamChunk{Error: fmt.Errorf("opencode: %s", msg), Done: true}
			return gotOutput

		case "step_start":
			// Ignore — just signals start of a step.

		default:
			// Unknown event types are silently ignored for forward compatibility.
		}
	}

	if err := scanner.Err(); err != nil {
		ch <- provider.StreamChunk{Error: fmt.Errorf("opencode stream: %w", err), Done: true}
		return gotOutput
	}

	ch <- provider.StreamChunk{
		Done:      true,
		CostUSD:   totalCost,
		TokensIn:  totalTokensIn,
		TokensOut: totalTokensOut,
	}
	return gotOutput
}

// ListModels runs `opencode models` and returns one model name per line.
// Implements provider.ModelLister.
func (a *Adapter) ListModels(ctx context.Context) ([]string, error) {
	bin := a.cfg.BinaryPath
	if bin == "" {
		bin = "opencode"
	}
	cmd := exec.CommandContext(ctx, bin, "models")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("opencode models: %w", err)
	}
	var models []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			models = append(models, line)
		}
	}
	return models, nil
}
