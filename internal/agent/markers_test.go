package agent

import (
	"reflect"
	"strings"
	"testing"

	"github.com/solarisjon/phoenix/internal/model"
)

// These tests pin the tolerant marker parsing introduced for small/local models
// (local-models phase 0.2, issue #106). Every case is something a real 3–14B
// model has been observed to emit.

func TestMarkerLine(t *testing.T) {
	cases := []struct {
		line, marker string
		wantRest     string
		wantOK       bool
	}{
		{"HEALTH_SIGNAL: all_clear", "HEALTH_SIGNAL:", "all_clear", true},
		{"health_signal: all_clear", "HEALTH_SIGNAL:", "all_clear", true},
		{"**HEALTH_SIGNAL:** all_clear", "HEALTH_SIGNAL:", "all_clear", true},
		{"`HEALTH_SIGNAL:` all_clear", "HEALTH_SIGNAL:", "all_clear", true},
		{"### HEALTH_SIGNAL: all_clear", "HEALTH_SIGNAL:", "all_clear", true},
		{"> HEALTH_SIGNAL: all_clear", "HEALTH_SIGNAL:", "all_clear", true},
		{"- HEALTH_SIGNAL all_clear", "HEALTH_SIGNAL:", "all_clear", true},
		{"  HEALTH_SIGNAL :  all_clear  ", "HEALTH_SIGNAL:", "all_clear", true}, // space before colon
		{"MEMO_START", "MEMO_START", "", true},
		{"**MEMO_START**", "MEMO_START", "", true},
		{"MEMO_START:", "MEMO_START", "", true},
		{"MEMO_STARTED", "MEMO_START", "", false},
		{"Please emit HEALTH_SIGNAL: x", "HEALTH_SIGNAL:", "", false}, // mid-line: not anchored
		{"Title: **Disk almost full**", "Title:", "Disk almost full", true},
		// Paraphrased markers seen from Qwen2.5-14B: spaces for underscores, em-dash separator.
		{"**Health signal** — needs_attention", "HEALTH_SIGNAL:", "needs_attention", true},
		{"Health Signal: all_clear", "HEALTH_SIGNAL:", "all_clear", true},
		{"health-signal – failed", "HEALTH_SIGNAL:", "failed", true},
		{"Memo Start", "MEMO_START", "", true},
		{"Health signals are important", "HEALTH_SIGNAL:", "", false}, // word boundary still enforced
		{"- **Path:** `/tmp/report.md`", "Path:", "/tmp/report.md", true},
	}
	for _, c := range cases {
		rest, ok := markerLine(c.line, c.marker)
		if ok != c.wantOK || (ok && rest != c.wantRest) {
			t.Errorf("markerLine(%q,%q) = (%q,%v), want (%q,%v)", c.line, c.marker, rest, ok, c.wantRest, c.wantOK)
		}
	}
}

func TestParseTaskComplete_LineAnchored(t *testing.T) {
	yes := []string{
		"Did the work.\nTASK_COMPLETE: all done",
		"**TASK_COMPLETE:** finished",
		"task_complete: finished",
		"TASK_COMPLETE:",
	}
	no := []string{
		"When finished I will emit TASK_COMPLETE: with a summary.", // quoting the instruction
		"Remember: TASK_COMPLETE: is the terminator",
		"Still working on it.",
	}
	for _, s := range yes {
		if !parseTaskComplete(s) {
			t.Errorf("parseTaskComplete(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if parseTaskComplete(s) {
			t.Errorf("parseTaskComplete(%q) = true, want false", s)
		}
	}
}

func TestParseNextAction_Tolerant(t *testing.T) {
	cases := []struct {
		name, in, want string
		ok             bool
	}{
		{"canonical", "work\nNEXT_ACTION:\nCheck the logs\nEND_NEXT_ACTION\n", "Check the logs", true},
		{"inline", "NEXT_ACTION: check the logs", "check the logs", true},
		{"decorated", "**NEXT_ACTION:**\nCheck the logs\n**END_NEXT_ACTION**", "Check the logs", true},
		{"missing end marker", "NEXT_ACTION:\nCheck the logs and\nthen restart", "Check the logs and\nthen restart", true},
		{"last block wins", "The format is:\nNEXT_ACTION:\n<what next>\nEND_NEXT_ACTION\n\nNEXT_ACTION:\nRotate the key\nEND_NEXT_ACTION", "Rotate the key", true},
		{"stops at TASK_COMPLETE", "NEXT_ACTION:\nnothing more\nTASK_COMPLETE: done", "nothing more", true},
		{"mid-sentence mention ignored", "I will use NEXT_ACTION: later.", "", false},
		{"empty", "NEXT_ACTION:\nEND_NEXT_ACTION", "", false},
	}
	for _, c := range cases {
		got, ok := parseNextAction(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("%s: parseNextAction = (%q,%v), want (%q,%v)", c.name, got, ok, c.want, c.ok)
		}
	}
}

func TestExtractGuardrailTrigger_Tolerant(t *testing.T) {
	cases := map[string]string{
		"GUARDRAIL_TRIGGERED: would delete prod db":       "would delete prod db",
		"**GUARDRAIL_TRIGGERED:** would delete prod db":   "would delete prod db",
		"guardrail_triggered: x":                          "x",
		"GUARDRAIL_TRIGGERED:":                            "Hard guardrail triggered (no reason provided)",
		"If needed I would output GUARDRAIL_TRIGGERED: …": "",
	}
	for in, want := range cases {
		if got := extractGuardrailTrigger(in); got != want {
			t.Errorf("extractGuardrailTrigger(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDeriveHealthSignal_Tolerant(t *testing.T) {
	cases := []struct {
		name, in, wantSig, wantReason string
	}{
		{"decorated", "**HEALTH_SIGNAL:** needs_attention\n**HEALTH_REASON:** disk 95%", "needs_attention", "disk 95%"},
		{"space spelling", "HEALTH_SIGNAL: Needs Attention\nHEALTH_REASON: disk 95%", "needs_attention", "disk 95%"},
		{"code span", "`HEALTH_SIGNAL: all_clear`", "all_clear", ""},
		{"value on next line", "HEALTH_SIGNAL:\nall_clear", "all_clear", ""},
		{"last valid wins", "Format: HEALTH_SIGNAL: <value>\n...\nHEALTH_SIGNAL: failed\nHEALTH_REASON: API down", "failed", "API down"},
		{"reason a couple of lines later", "HEALTH_SIGNAL: needs_attention\n\nHEALTH_REASON: two errors", "needs_attention", "two errors"},
		{"trailing punctuation", "HEALTH_SIGNAL: all_clear.", "all_clear", ""},
		{"no marker, one keyword", "There was one error today.", "all_clear", "no HEALTH_SIGNAL emitted; no alert keywords found"},
		{"no marker, many keywords", "critical failure: service down and unreachable", "needs_attention", "no HEALTH_SIGNAL emitted; inferred from keywords: critical, failure, fail, down, unreachable"},
		{"paraphrased heading (Qwen2.5-14B)", "The string is not a palindrome.\n\n**Health signal** — needs_attention\n\nHEALTH_REASON: does not read the same backwards.", "needs_attention", "does not read the same backwards."},
	}
	for _, c := range cases {
		sig, reason := deriveHealthSignal(c.in)
		if sig != c.wantSig || reason != c.wantReason {
			t.Errorf("%s: = (%q,%q), want (%q,%q)", c.name, sig, reason, c.wantSig, c.wantReason)
		}
	}
}

func TestParseMemoBlocks_Tolerant(t *testing.T) {
	in := strings.Join([]string{
		"Some preamble.",
		"",
		"**MEMO_START**",
		"  Title: Disk almost full",
		"  Priority: HIGH",
		"Root volume at 95%.",
		"**MEMO_END**",
		"",
		"```",
		"memo_start",
		"- **Title:** Second one",
		"",
		"body line",
		"memo_end",
		"```",
		"",
		"MEMO_START",
		"No title header, body starts immediately",
		"and continues",
		"MEMO_END",
		"",
		"MEMO_START",
		"Title: Empty body is dropped",
		"MEMO_END",
	}, "\n")
	got := parseMemoBlocks(in)
	want := []parsedMemo{
		{title: "Disk almost full", body: "Root volume at 95%.", priority: model.MemoPriorityHigh},
		{title: "Second one", body: "body line", priority: model.MemoPriorityNormal},
		{title: "No title header, body starts immediately", body: "No title header, body starts immediately\nand continues", priority: model.MemoPriorityNormal},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseMemoBlocks =\n%#v\nwant\n%#v", got, want)
	}
}

func TestParseArtifactBlocks_Tolerant(t *testing.T) {
	in := strings.Join([]string{
		"**ARTIFACT_START**",
		"- **Type:** File",
		"- **Path:** `/tmp/report.md`",
		"- Title: Weekly report",
		"**ARTIFACT_END**",
		"artifact_start",
		"type: URL",
		"url: https://example.com/x",
		"artifact_end",
		"ARTIFACT_START",
		"Title: no type or path → dropped",
		"ARTIFACT_END",
	}, "\n")
	got := ParseArtifactBlocks(in)
	want := []ParsedArtifact{
		{ArtType: "file", Path: "/tmp/report.md", Title: "Weekly report"},
		{ArtType: "url", Path: "https://example.com/x"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseArtifactBlocks =\n%#v\nwant\n%#v", got, want)
	}
}

func TestExtractJSONObject(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"bare", `{"a":1}`, `{"a":1}`},
		{"fenced json", "Here:\n```json\n{\"a\":1}\n```\nthanks", `{"a":1}`},
		{"fenced plain", "```\n{\"a\":1}\n```", `{"a":1}`},
		{"prose prefix", `Sure! Here is the plan: {"a":{"b":2}} hope it helps`, `{"a":{"b":2}}`},
		{"braces in strings", `{"s":"}{","n":1}`, `{"s":"}{","n":1}`},
		{"escaped quotes", `{"s":"a\"}\"b"}`, `{"s":"a\"}\"b"}`},
		{"non-json fence then object", "```bash\nls\n```\n{\"a\":1}", `{"a":1}`},
		{"truncated", `{"a":1,"b":{"c":2}`, `{"a":1,"b":{"c":2}`},
		{"none", "no json here", ""},
	}
	for _, c := range cases {
		if got := ExtractJSONObject(c.in); got != c.want {
			t.Errorf("%s: ExtractJSONObject = %q, want %q", c.name, got, c.want)
		}
	}
}
