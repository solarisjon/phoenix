package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/solarisjon/phoenix/internal/provider"
)

// RepairStructured makes exactly ONE follow-up call asking the model to
// re-emit a required structured result that failed to parse. It is used for
// results Phoenix cannot proceed without (orchestrator plan, agent generation,
// health classification) — never for optional markers like memos.
//
// The repair request is deliberately minimal: a terse system prompt, the
// previous (broken) output, the parse error, and the exact format wanted, with
// the JSON schema attached so constrained backends can't get it wrong twice.
// It runs on the SAME provider that produced the original output (it saw the
// original prompt); callers count the attempt in tasks.repair_attempts.
//
// Returns the raw repaired output; callers re-run their normal parser on it.
func RepairStructured(ctx context.Context, prov provider.Provider, prevOutput string, parseErr error, schema json.RawMessage, what string) (string, error) {
	if prov == nil {
		return "", fmt.Errorf("repair: no provider")
	}
	prev := strings.TrimSpace(prevOutput)
	if len(prev) > 6000 {
		prev = prev[:6000] + "\n[… truncated]"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Your previous reply could not be used: %v.\n\n", parseErr)
	fmt.Fprintf(&b, "Reply again with ONLY %s — a single JSON object, no prose, no Markdown fences, no explanation.\n", what)
	if len(schema) > 0 {
		b.WriteString("It must match this JSON Schema exactly:\n")
		b.Write(schema)
		b.WriteString("\n")
	}
	if prev != "" {
		b.WriteString("\nYour previous reply was:\n---\n")
		b.WriteString(prev)
		b.WriteString("\n---\n")
	}
	resp, err := prov.Execute(ctx, provider.TaskRequest{
		SystemPrompt:    "You output only the requested JSON object. Nothing else.",
		Prompt:          b.String(),
		ResponseSchema:  schema,
		MaxOutputTokens: 2048,
	})
	if err != nil {
		return "", fmt.Errorf("repair call failed: %w", err)
	}
	return resp.Output, nil
}
