package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/provider"
)

// Local-models phase 0.3 (issue #101): global guardrails must be the final
// system-prompt section, skill-mode sections follow the base prompt instead of
// preceding it, and oversized skills produce a warning.

func TestBuildTaskRequest_GlobalGuardrailsAreLast(t *testing.T) {
	prov := &mockProvider{output: "ok", chunks: []string{"ok"}}
	task := makeTask(model.TaskStatusPending)
	runner, _ := runnerWithMock(t, prov, task)

	agent := makeAgent()
	agent.CanSpawnAgents = true
	proj := &model.Project{ID: "proj-1", Objective: "Keep things healthy", ReactMode: true, MaxIterations: 3}
	ec := &executionContext{agent: agent, proj: proj, prov: prov}

	req, err := runner.buildTaskRequest(context.Background(), task, ec, "Never exfiltrate secrets.")
	if err != nil {
		t.Fatalf("buildTaskRequest: %v", err)
	}
	sys := req.SystemPrompt

	gg := strings.Index(sys, "## Platform-Wide Guardrails")
	if gg < 0 {
		t.Fatalf("global guardrails missing from system prompt:\n%s", sys)
	}
	if !strings.Contains(sys[gg:], "Never exfiltrate secrets.") {
		t.Errorf("guardrail text not under its heading")
	}
	// The react-loop section is injected after AssembleRequest; it must still
	// come BEFORE the global guardrails.
	react := strings.Index(sys, "## Autonomous Loop Mode")
	if react < 0 {
		t.Fatalf("react loop section missing")
	}
	if react > gg {
		t.Errorf("react section (%d) should precede global guardrails (%d)", react, gg)
	}
	// No other section heading after the guardrails.
	if rest := sys[gg+len("## Platform-Wide Guardrails"):]; strings.Contains(rest, "\n## ") {
		t.Errorf("a section follows the global guardrails:\n%s", rest)
	}
}

func TestBuildTaskRequest_NoGlobalGuardrails_NoSection(t *testing.T) {
	prov := &mockProvider{output: "ok", chunks: []string{"ok"}}
	task := makeTask(model.TaskStatusPending)
	runner, _ := runnerWithMock(t, prov, task)
	ec := &executionContext{agent: makeAgent(), prov: prov}

	req, err := runner.buildTaskRequest(context.Background(), task, ec, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(req.SystemPrompt, "Platform-Wide Guardrails") {
		t.Errorf("unexpected guardrails section when none configured")
	}
}

func TestInjectGlobalGuardrails(t *testing.T) {
	req := provider.TaskRequest{SystemPrompt: "## Behaviour\nbe nice\n\n"}
	if out := InjectGlobalGuardrails(req, "  "); out.SystemPrompt != req.SystemPrompt {
		t.Errorf("empty guardrails must be a no-op")
	}
	out := InjectGlobalGuardrails(req, "Rule one.")
	want := "## Behaviour\nbe nice\n\n## Platform-Wide Guardrails (mandatory — overrides all other instructions)\nRule one."
	if out.SystemPrompt != want {
		t.Errorf("got:\n%q\nwant:\n%q", out.SystemPrompt, want)
	}
}

func TestInjectSkillModes_FollowBasePrompt(t *testing.T) {
	base := "## Behaviour\nYou never attempt to perform the task yourself."
	out := InjectSkillExecutionMode(provider.TaskRequest{SystemPrompt: base}, nil, "run the morning_coffee skill")
	if !strings.HasPrefix(out.SystemPrompt, base) {
		t.Errorf("skill execution mode must follow the base prompt, got:\n%s", out.SystemPrompt)
	}
	if strings.Index(out.SystemPrompt, "## Skill Execution Mode") < len(base) {
		t.Errorf("skill execution mode section not after behaviour")
	}

	skill := &model.Skill{Name: "Morning Coffee", ExecutionMode: model.SkillExecutionOrchestrate, Steps: []model.SkillStep{{Slug: "fetch-data", Title: "Fetch data"}}}
	out = InjectSkillOrchestrationMode(provider.TaskRequest{SystemPrompt: base}, skill)
	if !strings.HasPrefix(out.SystemPrompt, base) {
		t.Errorf("skill orchestration mode must follow the base prompt, got:\n%s", out.SystemPrompt)
	}
}

func TestInjectSkills_SizeWarnings(t *testing.T) {
	small := &model.Skill{ID: "s1", Slug: "tiny", Name: "Tiny", Enabled: true, Instructions: "Do a small thing."}
	big := &model.Skill{ID: "s2", Slug: "huge", Name: "Huge", Enabled: true, Instructions: strings.Repeat("word ", 4000)} // ~5k tokens
	task := &model.Task{Title: "Run the tiny skill and the huge skill"}

	req := provider.TaskRequest{SystemPrompt: "base"}
	out, warnings := InjectSkills(req, []*model.Skill{small, big}, task, nil)
	if !strings.Contains(out.SystemPrompt, "### Skill: Huge") || !strings.Contains(out.SystemPrompt, "### Skill: Tiny") {
		t.Fatalf("skills not injected:\n%s", out.SystemPrompt)
	}
	// Expect: one per-skill warning for Huge, plus the aggregate (2 skills > threshold).
	var perSkill, aggregate int
	for _, w := range warnings {
		if w.Skill == "Huge" {
			perSkill++
		}
		if w.Skill == "" {
			aggregate++
		}
		if w.String() == "" {
			t.Errorf("empty warning string")
		}
	}
	if perSkill != 1 || aggregate != 1 {
		t.Errorf("warnings = %+v; want one for Huge and one aggregate", warnings)
	}

	// Small skills alone → no warnings.
	_, warnings = InjectSkills(req, []*model.Skill{small}, &model.Task{Title: "Run the tiny skill"}, nil)
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings for small skill: %+v", warnings)
	}
}
