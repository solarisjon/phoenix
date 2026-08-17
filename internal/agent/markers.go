package agent

import "strings"

// This file holds the shared, tolerant primitives used by every parser that
// looks for Phoenix protocol markers in model output (MEMO_START, HEALTH_SIGNAL:,
// ARTIFACT_START, NEXT_ACTION:, TASK_COMPLETE:, GUARDRAIL_TRIGGERED: …).
//
// Frontier models reproduce markers exactly; smaller local models frequently
// decorate them ("**MEMO_START**", "`HEALTH_SIGNAL:` all_clear", "- Title: x",
// "memo_start"), so the matchers below are:
//   - line-anchored (a marker must begin the line, after decoration),
//   - case-insensitive on the marker word,
//   - tolerant of Markdown emphasis, code spans, headings, blockquotes and
//     bullets around the marker,
//   - tolerant of an optional trailing colon on bare markers.
//
// They are deliberately NOT tolerant of the marker appearing mid-sentence —
// that is how a model quoting the instructions ("emit TASK_COMPLETE: when done")
// used to trigger the loop terminator.

// leadDecoration are characters stripped from the start of a line before
// marker matching: emphasis, code spans, headings, blockquotes, bullets.
const leadDecoration = "*_`#>-+ \t"

// wrapDecoration are characters stripped from around a value/marker.
const wrapDecoration = "*_` \t"

// markerLine reports whether line begins with marker (case-insensitive, after
// stripping Markdown decoration) and returns the remainder of the line after
// the marker, any following separator, and surrounding decoration.
//
// Tolerances, all observed in real small-model output:
//   - case: "health_signal: x"
//   - underscores written as spaces or hyphens: "Health signal: x", "memo-start"
//   - a trailing ":" on marker is optional in the input, and the separator may
//     be ":", "—", "–" or "-": "**Health signal** — needs_attention"
//   - Markdown decoration around the marker: "**HEALTH_SIGNAL:**", "`MEMO_START`"
//
// The marker must end at a word boundary: "MEMO_START" does not match
// "MEMO_STARTED".
func markerLine(line, marker string) (rest string, ok bool) {
	marker = strings.TrimSuffix(marker, ":")
	s := strings.TrimLeft(line, leadDecoration)
	n := matchMarkerPrefix(s, marker)
	if n < 0 {
		return "", false
	}
	rest = s[n:]
	if rest != "" && isWordByte(rest[0]) {
		return "", false
	}
	rest = strings.TrimLeft(rest, wrapDecoration)
	for _, sep := range []string{":", "—", "–", "-"} {
		if strings.HasPrefix(rest, sep) {
			rest = rest[len(sep):]
			break
		}
	}
	rest = strings.Trim(rest, wrapDecoration)
	return rest, true
}

// matchMarkerPrefix returns the number of bytes of s consumed by marker
// (case-insensitive; '_' in marker matches '_', ' ' or '-' in s), or -1.
func matchMarkerPrefix(s, marker string) int {
	i := 0
	for j := 0; j < len(marker); j++ {
		if i >= len(s) {
			return -1
		}
		m, c := marker[j], s[i]
		switch {
		case m == '_':
			if c != '_' && c != ' ' && c != '-' {
				return -1
			}
		case lower(m) != lower(c):
			return -1
		}
		i++
	}
	return i
}

func lower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}

// isBareMarker reports whether the line is exactly the marker (allowing
// decoration and an optional trailing colon), e.g. "**MEMO_START**".
func isBareMarker(line, marker string) bool {
	rest, ok := markerLine(line, marker)
	return ok && rest == ""
}

// headerValue matches "Key: value" header lines inside a block (Title:,
// Priority:, Path:, …) with the same tolerance as markerLine. Returns the
// value with surrounding decoration removed.
func headerValue(line, key string) (string, bool) {
	rest, ok := markerLine(line, key)
	if !ok {
		return "", false
	}
	return rest, true
}

func isWordByte(c byte) bool {
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// normaliseEnum lower-cases a marker value and folds spaces/hyphens to
// underscores so "Needs Attention", "needs-attention" and "NEEDS_ATTENTION"
// all become "needs_attention".
func normaliseEnum(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	v = strings.Trim(v, wrapDecoration+".!")
	v = strings.NewReplacer(" ", "_", "-", "_").Replace(v)
	return v
}

// ExtractJSONObject pulls the first JSON object out of free-form model output.
// The model may wrap it in a ```json … ``` (or bare ```) code block, prefix it
// with prose ("Sure! Here is the plan:"), or return it bare. Returns "" when
// no object can be located. It does not validate the JSON — callers unmarshal
// and report parse errors themselves.
//
// Precedence: first fenced block whose content starts with "{" → first
// balanced { … } span in the raw text → "".
func ExtractJSONObject(output string) string {
	// Fenced code blocks first (```json or ```).
	rest := output
	for {
		start := strings.Index(rest, "```")
		if start == -1 {
			break
		}
		start += 3
		// Skip an optional language tag on the fence line.
		if nl := strings.IndexByte(rest[start:], '\n'); nl != -1 {
			tag := strings.TrimSpace(rest[start : start+nl])
			if tag != "" && !strings.HasPrefix(tag, "{") {
				start += nl + 1
			}
		}
		end := strings.Index(rest[start:], "```")
		if end == -1 {
			break
		}
		body := strings.TrimSpace(rest[start : start+end])
		if strings.HasPrefix(body, "{") {
			if obj := balancedObject(body); obj != "" {
				return obj
			}
		}
		rest = rest[start+end+3:]
	}
	// Bare: first balanced object anywhere.
	if i := strings.IndexByte(output, '{'); i != -1 {
		return balancedObject(output[i:])
	}
	return ""
}

// balancedObject returns the shortest prefix of s that is a balanced JSON
// object (s must start with '{'), honouring string literals and escapes.
// Falls back to first-{…last-} when braces never balance (truncated output),
// which lets json.Unmarshal produce a useful error.
func balancedObject(s string) string {
	if !strings.HasPrefix(s, "{") {
		return ""
	}
	depth := 0
	inStr := false
	esc := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[:i+1]
			}
		}
	}
	// Unbalanced (truncated) — return through the last brace so the caller's
	// json.Unmarshal error names the real problem.
	if end := strings.LastIndexByte(s, '}'); end != -1 {
		return s[:end+1]
	}
	return s
}
