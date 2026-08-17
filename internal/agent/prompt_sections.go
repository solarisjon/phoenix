package agent

import (
	"fmt"
	"sort"
	"strings"

	"github.com/solarisjon/phoenix/internal/provider"
)

// This file is the section model behind prompt budgeting (local-models
// phase 2). Prompt assembly is still a sequence of string-building injectors
// (AssembleRequest, InjectSkills, InjectMemories, …) — but the runner now
// records what each one contributed as a PromptSection, so the assembled
// prompt can be re-rendered with sections dropped or shrunk to fit a model's
// context window. When nothing needs trimming, Render() reproduces the
// injectors' output byte for byte (guarded by prompt_golden_test.go).

// Section priorities. 0 = mandatory (never trimmed). Higher numbers are
// trimmed first. Ties keep insertion order (later-injected first).
const (
	PriorityMandatory = 0
	PriorityFollowUp  = 1 // follow-up chain context (summarise / drop oldest)
	PrioritySkills    = 2 // skill instructions (full → outline → description → drop)
	PriorityMemories  = 3 // recalled memories (truncate → drop)
	PrioritySpawnHire = 4 // spawn/hire API instructions (drop)
	PriorityObsidian  = 5 // Obsidian vault routing (drop)
)

// Shrinker returns a smaller rendering of a section for the given level
// (1, 2, 3 …). ok=false means "no smaller form — drop the section". Level 0 is
// never requested. Implementations must return the FULL replacement text for
// the section (including any leading separator the original carried).
type Shrinker func(level int) (text string, ok bool)

// PromptSection is one contiguous contribution to the assembled prompt.
type PromptSection struct {
	Key      string // stable identifier for logs/UI: "behaviour", "skills", "memories", …
	Text     string // exact bytes contributed (separators included)
	Priority int    // 0 = mandatory
	Shrink   Shrinker
	dropped  bool
}

// PromptAssembly holds the sections in the shape the injectors build them:
//
//	system = TrimSpace(join(Base)) + join(Appended)
//	user   = join(UserPrefix) + UserBase
//
// Base are the sections written by AssembleRequest (each carrying its own
// trailing separator, trimmed as a whole exactly like the original builder).
// Appended are later system injections (each starting with its own "\n\n").
// UserPrefix are prepended user-prompt blocks (follow-up context).
type PromptAssembly struct {
	Base       []PromptSection
	Appended   []PromptSection
	UserPrefix []PromptSection
	UserBase   string
	WorkingDir string
	Extra      provider.TaskRequest // non-text fields to carry through (schema, controls)
}

// Render produces the provider request exactly as the sequential injectors
// would have.
func (pa *PromptAssembly) Render() provider.TaskRequest {
	var sys strings.Builder
	for _, s := range pa.Base {
		if !s.dropped {
			sys.WriteString(s.Text)
		}
	}
	system := strings.TrimSpace(sys.String())
	for _, s := range pa.Appended {
		if !s.dropped {
			system += s.Text
		}
	}
	var user strings.Builder
	for _, s := range pa.UserPrefix {
		if !s.dropped {
			user.WriteString(s.Text)
		}
	}
	user.WriteString(pa.UserBase)

	req := pa.Extra
	req.SystemPrompt = system
	req.Prompt = user.String()
	req.WorkingDir = pa.WorkingDir
	return req
}

// Apply runs an injector against the current rendering and records whatever
// it added as a section with the given key/priority/shrinker. It supports
// the two shapes injectors actually use: append to the system prompt (with
// an optional TrimRight of the previous tail) and prepend to the user prompt.
// An injector that changes nothing records nothing.
func (pa *PromptAssembly) Apply(key string, priority int, shrink Shrinker, inject func(provider.TaskRequest) provider.TaskRequest) {
	before := pa.Render()
	after := inject(before)

	// ---- system: append (tolerating a trimmed tail on the previous text) ----
	if after.SystemPrompt != before.SystemPrompt {
		cp := commonPrefixLen(before.SystemPrompt, after.SystemPrompt)
		// Attribute a run of shared trailing newlines to the NEW section, so
		// each recorded delta owns its own "\n\n" separator and dropping a
		// neighbour never glues two sections together.
		for cp > 0 && cp <= len(before.SystemPrompt) && before.SystemPrompt[cp-1] == '\n' {
			cp--
		}
		if cut := len(before.SystemPrompt) - cp; cut > 0 {
			pa.trimSystemTail(cut)
		}
		if delta := after.SystemPrompt[cp:]; delta != "" {
			pa.Appended = append(pa.Appended, PromptSection{Key: key, Text: delta, Priority: priority, Shrink: shrink})
		}
	}

	// ---- user: prepend ----
	if after.Prompt != before.Prompt {
		if strings.HasSuffix(after.Prompt, before.Prompt) {
			delta := after.Prompt[:len(after.Prompt)-len(before.Prompt)]
			pa.UserPrefix = append([]PromptSection{{Key: key, Text: delta, Priority: priority, Shrink: shrink}}, pa.UserPrefix...)
		} else {
			// Unknown reshaping (not used by current injectors): fold into the
			// base so nothing is lost; it just can't be trimmed.
			pa.UserPrefix = nil
			pa.UserBase = after.Prompt
		}
	}

	// Carry non-text fields (schema, generation controls, working dir).
	pa.WorkingDir = after.WorkingDir
	after.SystemPrompt, after.Prompt, after.WorkingDir = "", "", ""
	pa.Extra = after
}

// trimSystemTail removes n bytes from the end of the rendered system prompt
// by shortening the last non-dropped appended section (or, if there is none,
// nothing — the base is already TrimSpace'd so injectors' TrimRight("\n") is
// a no-op there).
func (pa *PromptAssembly) trimSystemTail(n int) {
	for i := len(pa.Appended) - 1; i >= 0 && n > 0; i-- {
		s := &pa.Appended[i]
		if s.dropped {
			continue
		}
		if len(s.Text) > n {
			s.Text = s.Text[:len(s.Text)-n]
			return
		}
		n -= len(s.Text)
		s.Text = ""
	}
}

func commonPrefixLen(a, b string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	return i
}

// Trim records one budgeting action on a section.
type Trim struct {
	Section    string `json:"section"`
	Action     string `json:"action"` // "shrunk" | "dropped"
	FromTokens int    `json:"from_tokens"`
	ToTokens   int    `json:"to_tokens"`
	Level      int    `json:"level,omitempty"`
}

func (t Trim) String() string {
	if t.Action == "dropped" {
		return fmt.Sprintf("%s dropped (%d tokens)", t.Section, t.FromTokens)
	}
	return fmt.Sprintf("%s shrunk %d→%d tokens", t.Section, t.FromTokens, t.ToTokens)
}

// ErrPromptTooLarge is returned by Fit when the mandatory sections alone
// exceed the budget — the task cannot run on this model without changes.
type ErrPromptTooLarge struct {
	Need, Budget, ContextWindow int
	Sections                    []string
}

func (e *ErrPromptTooLarge) Error() string {
	return fmt.Sprintf("prompt does not fit the model's context window: mandatory sections need ~%d tokens but only %d are available (context %d minus reserved output); shorten the agent behaviour, task description or skill, or use a model with a larger context",
		e.Need, e.Budget, e.ContextWindow)
}

// framingOverhead approximates chat-template tokens (role tags, BOS/EOS).
const framingOverhead = 24

// TokenCount returns the assembly's current token total using count.
func (pa *PromptAssembly) TokenCount(count func(string) int) int {
	req := pa.Render()
	return count(req.SystemPrompt) + count(req.Prompt) + framingOverhead
}

// Fit shrinks and drops trimmable sections, least important first, until the
// rendered prompt fits within budget tokens. budget <= 0 means "unknown" and
// is a no-op. Returns the trims applied; ErrPromptTooLarge if the mandatory
// sections alone don't fit (the assembly is left with all trimmable sections
// removed so callers can still inspect it).
func (pa *PromptAssembly) Fit(budget int, count func(string) int) ([]Trim, error) {
	if budget <= 0 {
		return nil, nil
	}
	total := pa.TokenCount(count)
	if total <= budget {
		return nil, nil
	}

	// Candidates: all trimmable sections, most-expendable first.
	type cand struct {
		sec *PromptSection
		ord int
	}
	var cands []cand
	ord := 0
	for _, list := range [][]PromptSection{pa.Base, pa.Appended, pa.UserPrefix} {
		for i := range list {
			if list[i].Priority > PriorityMandatory && !list[i].dropped {
				cands = append(cands, cand{&list[i], ord})
			}
			ord++
		}
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].sec.Priority != cands[j].sec.Priority {
			return cands[i].sec.Priority > cands[j].sec.Priority
		}
		return cands[i].ord > cands[j].ord // later-injected first
	})

	var trims []Trim
	for _, c := range cands {
		sec := c.sec
		from := count(sec.Text)
		for level := 1; ; level++ {
			var txt string
			var ok bool
			if sec.Shrink != nil {
				txt, ok = sec.Shrink(level)
			}
			if !ok || strings.TrimSpace(txt) == "" {
				sec.dropped = true
				trims = append(trims, Trim{Section: sec.Key, Action: "dropped", FromTokens: from})
				break
			}
			if txt == sec.Text {
				continue // shrinker returned same text; ask for the next level
			}
			sec.Text = txt
			to := count(txt)
			trims = append(trims, Trim{Section: sec.Key, Action: "shrunk", FromTokens: from, ToTokens: to, Level: level})
			total = pa.TokenCount(count)
			if total <= budget {
				return coalesceTrims(trims), nil
			}
			from = to
		}
		total = pa.TokenCount(count)
		if total <= budget {
			return coalesceTrims(trims), nil
		}
	}

	// Only mandatory sections remain and it still doesn't fit.
	var keys []string
	for _, list := range [][]PromptSection{pa.Base, pa.Appended, pa.UserPrefix} {
		for _, s := range list {
			if !s.dropped {
				keys = append(keys, s.Key)
			}
		}
	}
	return coalesceTrims(trims), &ErrPromptTooLarge{Need: total, Budget: budget, Sections: keys}
}

// coalesceTrims merges successive shrinks of the same section into one entry
// (from the original size to the final size) so the UI shows one line per
// section.
func coalesceTrims(trims []Trim) []Trim {
	var out []Trim
	idx := map[string]int{}
	for _, t := range trims {
		if i, ok := idx[t.Section]; ok {
			out[i].Action = t.Action
			out[i].ToTokens = t.ToTokens
			out[i].Level = t.Level
			continue
		}
		idx[t.Section] = len(out)
		out = append(out, t)
	}
	return out
}

// ---- Shrink helpers used by injectors ----

// leadingSeparator returns the leading whitespace of a recorded delta so a
// shrunk replacement can keep the same join with its predecessor.
func leadingSeparator(text string) string {
	i := 0
	for i < len(text) && (text[i] == '\n' || text[i] == ' ' || text[i] == '\t') {
		i++
	}
	return text[:i]
}

// truncateHead keeps roughly the first frac of text (by bytes, on a line
// boundary) and appends an ellipsis marker.
func truncateHead(text string, frac float64) string {
	if frac >= 1 {
		return text
	}
	keep := int(float64(len(text)) * frac)
	if keep <= 0 {
		return ""
	}
	if keep >= len(text) {
		return text
	}
	cut := strings.LastIndexByte(text[:keep], '\n')
	if cut < keep/2 {
		cut = keep
	}
	return strings.TrimRight(text[:cut], "\n") + "\n[… truncated to fit the model's context window]"
}
