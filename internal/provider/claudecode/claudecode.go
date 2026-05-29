// Package claudecode provides a Provider adapter that runs tasks via the
// Claude Code CLI (https://claude.ai/code). This gives Phoenix agents access
// to Claude Code's full toolchain: file system access, web search, MCP
// servers, code execution, and any other tools Claude Code supports.
//
// Tasks are executed as:
//
//	claude --print --output-format stream-json --verbose \
//	  [--model <model>] [--system-prompt <prompt>] [--add-dir <dir>] \
//	  [--dangerously-skip-permissions] [--max-budget-usd <n>] \
//	  "<prompt>"
//
// The adapter streams newline-delimited JSON events from stdout, collecting
// text content from assistant messages and extracting token counts and cost
// from the final result event.
package claudecode

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"

	"github.com/solarisjon/phoenix/internal/provider"
)

// Config holds the configuration for the Claude Code adapter.
type Config struct {
	// BinaryPath is the path to the claude binary.
	// Defaults to "claude" (resolved via PATH).
	BinaryPath string `json:"binary_path"`

	// Model selects the model to use, e.g. "claude-opus-4-5" or "sonnet".
	// If empty, Claude Code uses its configured default.
	Model string `json:"model"`

	// WorkingDir sets the directory Claude Code runs in.
	// Useful for code tasks so the agent sees the right files.
	// Supports ${ENV_VAR} expansion (handled by the registry before reaching here).
	WorkingDir string `json:"working_dir"`

	// AddDirs are additional directories Claude Code is granted tool access to.
	// Each entry is passed as a separate --add-dir flag.
	AddDirs []string `json:"add_dirs"`

	// MaxBudgetUSD caps the maximum spend per task. Zero means no cap.
	MaxBudgetUSD float64 `json:"max_budget_usd"`

	// DangerouslySkipPermissions auto-approves tool use without prompting.
	// Only enable in trusted, sandboxed environments.
	DangerouslySkipPermissions bool `json:"dangerously_skip_permissions"`

	// AllowedTools restricts which tools Claude Code may use, e.g. ["Bash", "Read", "Write"].
	// If empty, Claude Code uses its defaults.
	AllowedTools []string `json:"allowed_tools"`

	// DisallowedTools explicitly blocks specific tools, e.g. ["Bash(git *)"].
	DisallowedTools []string `json:"disallowed_tools"`

	// MCPConfig is a JSON string or path to an MCP configuration file.
	// Passed as --mcp-config if non-empty.
	MCPConfig string `json:"mcp_config"`

	// ExtraArgs are passed verbatim to the claude CLI after all other flags.
	ExtraArgs []string `json:"extra_args"`
}

// Adapter implements provider.Provider using the Claude Code CLI.
type Adapter struct {
	cfg Config
}

// New creates an Adapter from a JSON config blob.
func New(configJSON string) (*Adapter, error) {
	var cfg Config
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return nil, fmt.Errorf("parse claudecode config: %w", err)
	}
	if cfg.BinaryPath == "" {
		cfg.BinaryPath = "claude"
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

// StreamExecute runs a task via Claude Code and streams output chunks.
func (a *Adapter) StreamExecute(ctx context.Context, req provider.TaskRequest) (<-chan provider.StreamChunk, error) {
	args, err := a.buildArgs(req)
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, a.cfg.BinaryPath, args...)
	switch {
	case req.WorkingDir != "":
		cmd.Dir = req.WorkingDir
	case a.cfg.WorkingDir != "":
		cmd.Dir = a.cfg.WorkingDir
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("claudecode: stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("claudecode: stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("claudecode: start: %w", err)
	}

	ch := make(chan provider.StreamChunk, 64)

	go func() {
		defer close(ch)
		defer func() {
			io.Copy(io.Discard, stderr) //nolint:errcheck
			if err := cmd.Wait(); err != nil {
				log.Printf("claudecode: process exited: %v", err)
			}
		}()
		a.parseStream(ctx, stdout, ch)
	}()

	return ch, nil
}

// EstimateCost returns zero — Claude Code reports actual cost after execution.
func (a *Adapter) EstimateCost(_ provider.TaskRequest) provider.CostEstimate {
	return provider.CostEstimate{}
}

// ---- Internal helpers ----

func (a *Adapter) buildArgs(req provider.TaskRequest) ([]string, error) {
	args := []string{"--print", "--output-format", "stream-json", "--verbose"}

	if a.cfg.Model != "" {
		args = append(args, "--model", a.cfg.Model)
	}

	// System prompt: use --system-prompt if provided, otherwise --append-system-prompt
	// to layer on top of Claude Code's default.
	if req.SystemPrompt != "" {
		args = append(args, "--system-prompt", req.SystemPrompt)
	}

	for _, dir := range a.cfg.AddDirs {
		args = append(args, "--add-dir", dir)
	}

	if a.cfg.DangerouslySkipPermissions {
		args = append(args, "--dangerously-skip-permissions")
	}

	if a.cfg.MaxBudgetUSD > 0 {
		args = append(args, "--max-budget-usd", fmt.Sprintf("%.4f", a.cfg.MaxBudgetUSD))
	}

	if len(a.cfg.AllowedTools) > 0 {
		args = append(args, "--allowed-tools", strings.Join(a.cfg.AllowedTools, ","))
	}
	if len(a.cfg.DisallowedTools) > 0 {
		args = append(args, "--disallowed-tools", strings.Join(a.cfg.DisallowedTools, ","))
	}

	if a.cfg.MCPConfig != "" {
		args = append(args, "--mcp-config", a.cfg.MCPConfig)
	}

	args = append(args, a.cfg.ExtraArgs...)

	// Build the user-facing prompt, incorporating any context turns.
	args = append(args, a.buildPrompt(req))

	return args, nil
}

func (a *Adapter) buildPrompt(req provider.TaskRequest) string {
	if len(req.Context) == 0 {
		return req.Prompt
	}

	// Prepend context turns so Claude Code has conversation history.
	var b strings.Builder
	for _, m := range req.Context {
		b.WriteString(fmt.Sprintf("<%s>\n%s\n</%s>\n\n", m.Role, m.Content, m.Role))
	}
	b.WriteString(req.Prompt)
	return b.String()
}

// ---- JSON event parsing ----
// Claude Code --output-format stream-json emits newline-delimited JSON objects.
//
// Key event types:
//   - system/init      → session setup (tools, model, etc.)
//   - assistant        → message from the model, may contain text content
//   - tool_use         → tool call (Bash, Read, Write, etc.)
//   - tool_result      → result of a tool call
//   - result           → final summary with cost, usage, is_error

type ccEvent struct {
	Type    string          `json:"type"`
	Subtype string          `json:"subtype"`
	Message *ccMessage      `json:"message"`
	IsError bool            `json:"is_error"`
	Result  string          `json:"result"`
	Error   string          `json:"error"`
	TotalCostUSD float64    `json:"total_cost_usd"`
	Usage   *ccUsage        `json:"usage"`
	Raw     json.RawMessage `json:"-"`
}

type ccMessage struct {
	Role    string      `json:"role"`
	Content []ccContent `json:"content"`
	Usage   *ccUsage    `json:"usage"`
	Error   string      `json:"error"` // set on auth/api errors
}

type ccContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type ccUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

func (a *Adapter) parseStream(ctx context.Context, r io.Reader, ch chan<- provider.StreamChunk) {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 4*1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			ch <- provider.StreamChunk{Error: ctx.Err(), Done: true}
			return
		default:
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var ev ccEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			// Non-JSON — pass through as plain text.
			ch <- provider.StreamChunk{Content: line + "\n"}
			continue
		}

		switch ev.Type {
		case "assistant":
			if ev.Message == nil {
				continue
			}
			// Check for embedded errors (e.g. auth failure, overload).
			if ev.Error != "" {
				ch <- provider.StreamChunk{
					Error: fmt.Errorf("claudecode: %s", ev.Error),
					Done:  true,
				}
				return
			}
			// Emit text content blocks.
			for _, block := range ev.Message.Content {
				if block.Type == "text" && block.Text != "" {
					ch <- provider.StreamChunk{Content: block.Text}
				}
			}

		case "result":
			if ev.IsError {
				errMsg := ev.Result
				if errMsg == "" {
					errMsg = "claude code task failed"
				}
				ch <- provider.StreamChunk{
					Error: fmt.Errorf("claudecode: %s", errMsg),
					Done:  true,
				}
				return
			}
			if ev.TotalCostUSD > 0 || ev.Usage != nil {
				tokIn, tokOut := 0, 0
				if ev.Usage != nil {
					tokIn = ev.Usage.InputTokens
					tokOut = ev.Usage.OutputTokens
				}
				log.Printf("claudecode: completed — input=%d output=%d cost=$%.6f",
					tokIn, tokOut, ev.TotalCostUSD)
			}

		case "system":
			// init event — log model info for debugging.
			if ev.Subtype == "init" {
				log.Printf("claudecode: session init (type=system/init)")
			}

		default:
			// tool_use, tool_result, and unknown types are silently ignored.
		}
	}

	if err := scanner.Err(); err != nil {
		ch <- provider.StreamChunk{Error: fmt.Errorf("claudecode stream: %w", err), Done: true}
		return
	}

	ch <- provider.StreamChunk{Done: true}
}
