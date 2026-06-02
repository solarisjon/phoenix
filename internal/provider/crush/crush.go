// Package crush provides a Provider adapter that runs tasks via the crush CLI
// (https://github.com/charmbracelet/crush). Crush is a terminal-first AI
// assistant that supports MCP servers, file system tools, and code execution.
//
// Tasks are executed as:
//
//	crush run --quiet [--model <model>] [--cwd <dir>] [--yolo] [extra...] "<prompt>"
//
// System prompt delivery: crush reads an AGENTS.md file from the working
// directory at startup (configured via initialize_as = "AGENTS.md" in crush.json).
// The adapter writes the system prompt to AGENTS.md before running and removes
// it (or strips the appended section) when the task completes.
//
// Output: crush writes plain text to stdout. The adapter streams it line by
// line as content chunks. Crush does not expose token counts or cost in
// non-interactive mode, so CostUSD is always 0.
package crush

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/solarisjon/phoenix/internal/provider"
)

// Config holds the configuration for the crush adapter.
type Config struct {
	// BinaryPath is the path to the crush binary.
	// Defaults to "crush" (resolved via PATH).
	BinaryPath string `json:"binary_path"`

	// Model selects the model, e.g. "anthropic/claude-sonnet-4-5" or "sonnet".
	// Accepts 'model' or 'provider/model' format.
	// If empty, crush uses its configured default.
	Model string `json:"model"`

	// WorkingDir sets the directory crush runs in (--cwd).
	// If set and no project working_dir override is provided, used as the base dir.
	WorkingDir string `json:"working_dir"`

	// ExtraArgs are passed verbatim to the crush CLI after all other flags.
	ExtraArgs []string `json:"extra_args"`
}

// Adapter implements provider.Provider using the crush CLI.
type Adapter struct {
	cfg Config
}

// New creates an Adapter from a JSON config blob.
func New(configJSON string) (*Adapter, error) {
	var cfg Config
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return nil, fmt.Errorf("crush: parse config: %w", err)
	}
	if cfg.BinaryPath == "" {
		cfg.BinaryPath = "crush"
	}
	return &Adapter{cfg: cfg}, nil
}

// Execute runs a task synchronously and returns the complete response.
func (a *Adapter) Execute(ctx context.Context, req provider.TaskRequest) (provider.TaskResponse, error) {
	ch, err := a.StreamExecute(ctx, req)
	if err != nil {
		return provider.TaskResponse{}, err
	}
	var parts []string
	for chunk := range ch {
		if chunk.Error != nil {
			return provider.TaskResponse{}, chunk.Error
		}
		if chunk.Content != "" {
			parts = append(parts, chunk.Content)
		}
	}
	return provider.TaskResponse{Output: strings.Join(parts, "")}, nil
}

// StreamExecute runs a task and streams output line by line as plain text chunks.
func (a *Adapter) StreamExecute(ctx context.Context, req provider.TaskRequest) (<-chan provider.StreamChunk, error) {
	workDir, cleanup, err := a.prepareWorkDir(req)
	if err != nil {
		return nil, fmt.Errorf("crush: prepare workdir: %w", err)
	}

	promptText := req.Prompt
	useStdin := len(promptText) > maxArgPromptBytes
	args := a.buildArgs(req, workDir, !useStdin)
	cmd := exec.CommandContext(ctx, a.cfg.BinaryPath, args...)
	cmd.Dir = workDir
	if useStdin {
		cmd.Stdin = strings.NewReader(promptText)
	}
	// Discard stderr — MCP init errors and skill warnings are not useful to the runner.
	cmd.Stderr = io.Discard

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("crush: stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cleanup()
		return nil, fmt.Errorf("crush: start: %w", err)
	}

	ch := make(chan provider.StreamChunk, 64)
	pid := cmd.Process.Pid

	go func() {
		defer close(ch)
		defer cleanup()
		defer func() {
			if err := cmd.Wait(); err != nil {
				log.Printf("crush: process exited: %v", err)
			}
		}()

		// First chunk carries the PID for crash recovery.
		ch <- provider.StreamChunk{PID: pid}

		scanner := bufio.NewScanner(stdout)
		// Allow lines up to 1MB (crush may output large code blocks).
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

		lineCount := 0
		for scanner.Scan() {
			if ctx.Err() != nil {
				return
			}
			ch <- provider.StreamChunk{Content: scanner.Text() + "\n"}
			lineCount++
		}
		if err := scanner.Err(); err != nil && ctx.Err() == nil {
			ch <- provider.StreamChunk{Error: fmt.Errorf("crush: read stdout: %w", err)}
			return
		}
		log.Printf("crush: completed — %d lines of output", lineCount)
	}()

	return ch, nil
}

// prepareWorkDir resolves the working directory and installs the system prompt
// as AGENTS.md. Returns the effective working dir, a cleanup func, and any error.
//
// Cleanup behaviour:
//   - If a fresh temp dir was created: removes the entire temp dir.
//   - If AGENTS.md was written to the project dir: deletes it.
//   - If AGENTS.md already existed and we appended to it: strips the appended section.
//   - If no system prompt: no-op cleanup.
func (a *Adapter) prepareWorkDir(req provider.TaskRequest) (workDir string, cleanup func(), err error) {
	noop := func() {}

	// Resolve base working directory.
	baseDir := req.WorkingDir
	if baseDir == "" {
		baseDir = a.cfg.WorkingDir
	}

	if req.SystemPrompt == "" {
		// No system prompt — use baseDir or a minimal temp dir.
		if baseDir == "" {
			td, err := os.MkdirTemp("", "crush-task-*")
			if err != nil {
				return "", noop, err
			}
			return td, func() { os.RemoveAll(td) }, nil
		}
		return baseDir, noop, nil
	}

	// We have a system prompt — write it to AGENTS.md.
	agentsSection := req.SystemPrompt

	if baseDir == "" {
		// No project dir — write to a temp dir and clean it all up after.
		td, err := os.MkdirTemp("", "crush-task-*")
		if err != nil {
			return "", noop, err
		}
		agentsPath := filepath.Join(td, "AGENTS.md")
		if err := os.WriteFile(agentsPath, []byte(agentsSection), 0644); err != nil {
			os.RemoveAll(td)
			return "", noop, fmt.Errorf("crush: write AGENTS.md: %w", err)
		}
		return td, func() { os.RemoveAll(td) }, nil
	}

	// Project dir is set. Check if AGENTS.md already exists.
	agentsPath := filepath.Join(baseDir, "AGENTS.md")
	existing, readErr := os.ReadFile(agentsPath)
	alreadyExists := readErr == nil

	const marker = "<!-- phoenix-agent -->"
	appendedContent := "\n\n" + marker + "\n" + agentsSection + "\n" + marker + "\n"

	if alreadyExists {
		// Append a marked section and strip it on cleanup.
		f, err := os.OpenFile(agentsPath, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return "", noop, fmt.Errorf("crush: open AGENTS.md: %w", err)
		}
		_, writeErr := f.WriteString(appendedContent)
		f.Close()
		if writeErr != nil {
			return "", noop, fmt.Errorf("crush: append AGENTS.md: %w", writeErr)
		}
		// Cleanup: restore original content.
		originalContent := make([]byte, len(existing))
		copy(originalContent, existing)
		cleanup = func() {
			if err := os.WriteFile(agentsPath, originalContent, 0644); err != nil {
				log.Printf("crush: restore AGENTS.md: %v", err)
			}
		}
	} else {
		// Write fresh AGENTS.md and delete it on cleanup.
		if err := os.WriteFile(agentsPath, []byte(agentsSection), 0644); err != nil {
			return "", noop, fmt.Errorf("crush: write AGENTS.md: %w", err)
		}
		cleanup = func() {
			// Only remove if it still contains our content (not modified externally).
			content, err := os.ReadFile(agentsPath)
			if err == nil && bytes.Equal(bytes.TrimSpace(content), bytes.TrimSpace([]byte(agentsSection))) {
				os.Remove(agentsPath)
			}
		}
	}

	return baseDir, cleanup, nil
}

// maxArgPromptBytes is the threshold above which we write the prompt to stdin
// instead of as a CLI argument, avoiding ARG_MAX issues with long follow-up
// prompts that contain injected parent-task output.
const maxArgPromptBytes = 8192

// buildArgs constructs the crush run argument list. When includePrompt is
// false the prompt is expected via stdin (for long prompts).
func (a *Adapter) buildArgs(req provider.TaskRequest, workDir string, includePrompt bool) []string {
	args := []string{"run", "--quiet"}

	if a.cfg.Model != "" {
		args = append(args, "--model", a.cfg.Model)
	}
	if workDir != "" {
		args = append(args, "--cwd", workDir)
	}
	args = append(args, a.cfg.ExtraArgs...)
	if includePrompt {
		args = append(args, req.Prompt)
	}
	return args
}

// EstimateCost returns zero — crush does not expose token counts in non-interactive mode.
func (a *Adapter) EstimateCost(_ provider.TaskRequest) provider.CostEstimate {
	return provider.CostEstimate{}
}

// ListModels runs `crush models` and returns one model name per line.
// Implements provider.ModelLister.
func (a *Adapter) ListModels(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, a.cfg.BinaryPath, "models")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("crush models: %w", err)
	}
	var models []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			models = append(models, line)
		}
	}
	return models, nil
}
