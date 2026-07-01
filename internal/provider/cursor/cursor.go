// Package cursor provides a Provider adapter that runs tasks via the
// Cursor CLI (https://cursor.com). This gives Phoenix agents access to
// Cursor's agentic toolchain: file system access, terminal execution,
// web search, and any other tools Cursor supports.
//
// Tasks are executed as:
//
//	cursor [--model <model>] [extra_args...] "<prompt>"
//
// The adapter streams plain-text output line-by-line from stdout.
// Token counts and cost are not reported (Cursor does not expose them
// via CLI). Use ExtraArgs to pass any additional flags your Cursor
// installation requires (e.g. "--headless", "--no-gui").
package cursor

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

// Config holds the configuration for the Cursor adapter.
type Config struct {
	// BinaryPath is the path to the cursor binary.
	// Defaults to "cursor" (resolved via PATH).
	BinaryPath string `json:"binary_path"`

	// Model selects the model Cursor should use, e.g. "claude-sonnet-4-5".
	// If empty, Cursor uses its configured default.
	Model string `json:"model"`

	// WorkingDir sets the directory Cursor runs in.
	// Supports ${ENV_VAR} expansion (handled by the registry before reaching here).
	WorkingDir string `json:"working_dir"`

	// ExtraArgs are passed verbatim to the cursor CLI before the prompt.
	// Use this to pass flags like "--headless" or "--no-gui" that your
	// Cursor version requires for non-interactive operation.
	ExtraArgs []string `json:"extra_args"`
}

// Adapter implements provider.Provider using the Cursor CLI.
type Adapter struct {
	cfg Config
}

// New creates an Adapter from a JSON config blob.
func New(configJSON string) (*Adapter, error) {
	var cfg Config
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return nil, fmt.Errorf("parse cursor config: %w", err)
	}
	if cfg.BinaryPath == "" {
		cfg.BinaryPath = "cursor"
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

// StreamExecute runs a task via Cursor and streams output chunks.
func (a *Adapter) StreamExecute(ctx context.Context, req provider.TaskRequest) (<-chan provider.StreamChunk, error) {
	prompt := a.buildPrompt(req)
	args := a.buildArgs(prompt)

	// Use exec.Command (not CommandContext) so we can manage process-group
	// termination ourselves — Cursor may spawn child processes that inherit
	// the stdout pipe and keep it alive after the parent exits.
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
		return nil, fmt.Errorf("cursor: stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("cursor: stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("cursor: start: %w", err)
	}

	pid := cmd.Process.Pid
	ch := make(chan provider.StreamChunk, 64)

	go func() {
		<-ctx.Done()
		if pgid, err := syscall.Getpgid(pid); err == nil {
			if killErr := syscall.Kill(-pgid, syscall.SIGKILL); killErr != nil {
				slog.Error("cursor: kill process group", "pgid", pgid, "error", killErr)
			}
		}
	}()

	go func() {
		defer close(ch)

		var stderrBuf strings.Builder
		stderrDone := make(chan struct{})
		go func() {
			defer close(stderrDone)
			io.Copy(&stderrBuf, stderr) //nolint:errcheck
		}()

		ch <- provider.StreamChunk{PID: pid}
		gotOutput := a.streamText(ctx, stdout, ch)

		<-stderrDone

		if err := cmd.Wait(); err != nil {
			slog.Debug("cursor: process exited", "error", err)
		}

		if !gotOutput {
			if msg := strings.TrimSpace(stderrBuf.String()); msg != "" {
				slog.Debug("cursor: stderr", "msg", msg)
				ch <- provider.StreamChunk{Error: fmt.Errorf("cursor: no output — %s", msg), Done: true}
			}
		}
	}()

	return ch, nil
}

// EstimateCost returns zero — Cursor does not expose cost via CLI.
func (a *Adapter) EstimateCost(_ provider.TaskRequest) provider.CostEstimate {
	return provider.CostEstimate{}
}

// ---- Internal helpers ----

func (a *Adapter) buildPrompt(req provider.TaskRequest) string {
	var b strings.Builder

	if req.SystemPrompt != "" {
		b.WriteString("<system>\n")
		b.WriteString(req.SystemPrompt)
		b.WriteString("\n</system>\n\n")
	}

	for _, m := range req.Context {
		b.WriteString(fmt.Sprintf("<%s>\n%s\n</%s>\n\n", m.Role, m.Content, m.Role))
	}

	b.WriteString(req.Prompt)
	return b.String()
}

func (a *Adapter) buildArgs(prompt string) []string {
	var args []string

	if a.cfg.Model != "" {
		args = append(args, "--model", a.cfg.Model)
	}

	args = append(args, a.cfg.ExtraArgs...)
	args = append(args, prompt)

	return args
}

// streamText reads plain-text output line-by-line and sends chunks on ch.
// Returns true if any content was emitted.
func (a *Adapter) streamText(ctx context.Context, r io.Reader, ch chan<- provider.StreamChunk) bool {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 4*1024*1024)

	var gotOutput bool

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			ch <- provider.StreamChunk{Error: ctx.Err(), Done: true}
			return gotOutput
		default:
		}

		line := scanner.Text()
		if line != "" {
			ch <- provider.StreamChunk{Content: line + "\n"}
			gotOutput = true
		}
	}

	if err := scanner.Err(); err != nil {
		ch <- provider.StreamChunk{Error: fmt.Errorf("cursor stream: %w", err), Done: true}
		return gotOutput
	}

	ch <- provider.StreamChunk{Done: true}
	return gotOutput
}
