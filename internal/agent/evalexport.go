package agent

// Thin exported wrappers over the unexported protocol parsers, for the model
// evaluation harness (internal/agent/eval). They exist so the harness scores
// a model with EXACTLY the parsers production uses — no re-implementation.

// ParsedMemo is a memo block as the runner would extract it.
type ParsedMemo struct {
	Title, Body string
	High        bool
}

// ParseMemos returns the MEMO_START…MEMO_END blocks in output.
func ParseMemos(output string) []ParsedMemo {
	var out []ParsedMemo
	for _, m := range parseMemoBlocks(output) {
		out = append(out, ParsedMemo{Title: m.title, Body: m.body, High: m.priority == "high"})
	}
	return out
}

// ParseHealthMarker returns the HEALTH_SIGNAL / HEALTH_REASON pair, or "" when
// no valid marker is present (the runner would then call the classifier).
func ParseHealthMarker(output string) (signal, reason string) { return deriveHealthSignal(output) }

// ParseNextAction returns the NEXT_ACTION body, if any.
func ParseNextAction(output string) (string, bool) { return parseNextAction(output) }

// ParseTaskComplete reports whether a TASK_COMPLETE line is present.
func ParseTaskComplete(output string) bool { return parseTaskComplete(output) }

// ParseGuardrailTrigger returns the GUARDRAIL_TRIGGERED reason, or "".
func ParseGuardrailTrigger(output string) string { return extractGuardrailTrigger(output) }

// ParsePlanSummary parses an orchestrator plan and returns its confidence and
// subtask count (error when it cannot be parsed).
func ParsePlanSummary(output string) (confidence float64, subtasks int, err error) {
	p, err := parseRoutedPlan(output)
	if err != nil {
		return 0, 0, err
	}
	return p.Confidence, len(p.Subtasks), nil
}
