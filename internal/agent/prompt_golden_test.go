package agent

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/provider"
)

// Golden tests pin the exact text of assembled prompts. They exist so the
// section-based refactor (local-models phase 2) and any later change to
// prompt assembly is a *deliberate* decision: run with -update to regenerate,
// then review the diff in git.
//
//	go test ./internal/agent -run TestPromptGolden -update

var updateGolden = flag.Bool("update", false, "rewrite golden files")

type goldenCase struct {
	name  string
	build func(t *testing.T) provider.TaskRequest
}

func goldenAgent() *model.Agent {
	return &model.Agent{ID: "agent-1", Name: "Ops", Behaviour: "You are a careful ops assistant. Be concise.", ProviderID: "prov-1"}
}

func goldenTask() *model.Task {
	return &model.Task{ID: "task-1", ProjectID: "proj-1", AgentID: "agent-1", Title: "Check disk", Description: "Report free space on /.", Input: "{}", Status: model.TaskStatusPending}
}

// goldenRunner returns a runner wired to a mock provider (byte output is not
// executed — we only call buildTaskRequest).
func goldenRunner(t *testing.T, tasks ...*model.Task) *Runner {
	t.Helper()
	all := append([]*model.Task{goldenTask()}, tasks...)
	r, _ := runnerWithMock(t, &mockProvider{output: "ok"}, all[0])
	for _, extra := range all[1:] {
		_ = r.tasks.Create(context.Background(), extra)
	}
	return r
}

func goldenCases() []goldenCase {
	return []goldenCase{
		{"base_behaviour", func(t *testing.T) provider.TaskRequest {
			return AssembleRequest(goldenAgent(), goldenTask(), nil, "", "")
		}},
		{"legacy_persona_guardrails", func(t *testing.T) provider.TaskRequest {
			a := &model.Agent{ID: "agent-1", Persona: "Expert SRE.", Instructions: "Be precise.", Guardrails: "No hallucinations.", HardGuardrails: "Never delete files."}
			return AssembleRequest(a, goldenTask(), &model.Project{Objective: "Keep prod healthy."}, "", "")
		}},
		{"monitor_task", func(t *testing.T) provider.TaskRequest {
			tk := goldenTask()
			tk.Source = "monitor"
			return AssembleRequest(goldenAgent(), tk, &model.Project{Objective: "Watch the disks."}, "", "")
		}},
		{"spawn_hire_global_guardrails", func(t *testing.T) provider.TaskRequest {
			a := goldenAgent()
			a.CanSpawnAgents, a.CanHireAgents = true, true
			return AssembleRequest(a, goldenTask(), nil, "Never exfiltrate secrets.", "http://phoenix.local:8080")
		}},
		{"task_with_input", func(t *testing.T) provider.TaskRequest {
			tk := goldenTask()
			tk.Input = `{"host":"web-1"}`
			return AssembleRequest(goldenAgent(), tk, nil, "", "")
		}},
		{"runner_react_obsidian_memories_guardrails", func(t *testing.T) provider.TaskRequest {
			r := goldenRunner(t)
			a := goldenAgent()
			a.CanSpawnAgents = true
			proj := &model.Project{ID: "proj-1", Objective: "Keep things healthy", ReactMode: true, MaxIterations: 3}
			ec := &executionContext{agent: a, proj: proj, prov: &mockProvider{}}
			req, err := r.buildTaskRequest(context.Background(), goldenTask(), ec, "Never exfiltrate secrets.")
			if err != nil {
				t.Fatal(err)
			}
			req = InjectObsidianVaults(req, []*model.ObsidianVault{{Name: "Wiki", Path: "/vault/wiki", Enabled: true, Context: "engineering notes"}})
			req = InjectMemories(req, "- Disk / was at 91% last week.")
			return req
		}},
		{"runner_followup_chain", func(t *testing.T) provider.TaskRequest {
			parent := &model.Task{ID: "task-0", ProjectID: "proj-1", AgentID: "agent-1", Title: "First look", Output: `{"text":"Disk / is at 91%."}`, Status: model.TaskStatusCompleted}
			r := goldenRunner(t, parent)
			tk := goldenTask()
			tk.FollowUpOf = &parent.ID
			tk.Title = "Dig deeper"
			tk.Description = "Which directories are largest?"
			ec := &executionContext{agent: goldenAgent(), proj: &model.Project{ID: "proj-1"}, prov: &mockProvider{}}
			req, err := r.buildTaskRequest(context.Background(), tk, ec, "")
			if err != nil {
				t.Fatal(err)
			}
			return req
		}},
		{"skills_injected", func(t *testing.T) provider.TaskRequest {
			sk := &model.Skill{ID: "s1", Slug: "morning_coffee", Name: "Morning Coffee", Description: "Brew status.", Instructions: "1. Check the pot.\n2. Report.", Enabled: true}
			tk := goldenTask()
			tk.Title = "Run the morning_coffee skill"
			req := AssembleRequest(goldenAgent(), tk, nil, "", "")
			req, _ = InjectSkills(req, []*model.Skill{sk}, tk, nil)
			return req
		}},
		// ---- compact profile ----
		{"compact_base_behaviour", func(t *testing.T) provider.TaskRequest {
			return AssembleRequestOpts(goldenAgent(), goldenTask(), nil, "", "", PromptOptions{Profile: model.PromptProfileCompact})
		}},
		{"compact_monitor_task", func(t *testing.T) provider.TaskRequest {
			tk := goldenTask()
			tk.Source = "monitor"
			return AssembleRequestOpts(goldenAgent(), tk, &model.Project{Objective: "Watch the disks."}, "", "", PromptOptions{Profile: model.PromptProfileCompact})
		}},
		{"compact_spawn_hire_report_task", func(t *testing.T) provider.TaskRequest {
			a := goldenAgent()
			a.CanSpawnAgents, a.CanHireAgents = true, true
			tk := goldenTask()
			tk.Title = "Write a report on disk usage"
			return AssembleRequestOpts(a, tk, nil, "Never exfiltrate secrets.", "http://phoenix.local:8080", PromptOptions{Profile: model.PromptProfileCompact})
		}},
		{"compact_react_loop", func(t *testing.T) provider.TaskRequest {
			req := AssembleRequestOpts(goldenAgent(), goldenTask(), nil, "", "", PromptOptions{Profile: model.PromptProfileCompact})
			return InjectReactLoopInstructionsOpts(req, 5, 1, PromptOptions{Profile: model.PromptProfileCompact})
		}},
		{"builtin_critic", func(t *testing.T) provider.TaskRequest {
			orig := goldenTask()
			orig.Output = `{"text":"Everything is fine."}`
			return BuildBuiltinCriticRequest(orig)
		}},
	}
}

func renderRequest(req provider.TaskRequest) string {
	var b strings.Builder
	b.WriteString("=== SYSTEM ===\n")
	b.WriteString(req.SystemPrompt)
	b.WriteString("\n=== USER ===\n")
	b.WriteString(req.Prompt)
	if len(req.Context) > 0 {
		b.WriteString("\n=== CONTEXT ===\n")
		for _, m := range req.Context {
			b.WriteString(fmt.Sprintf("[%s] %s\n", m.Role, m.Content))
		}
	}
	b.WriteString("\n")
	return b.String()
}

func TestPromptGolden(t *testing.T) {
	dir := filepath.Join("testdata", "prompts")
	if *updateGolden {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range goldenCases() {
		t.Run(c.name, func(t *testing.T) {
			got := renderRequest(c.build(t))
			path := filepath.Join(dir, c.name+".golden")
			if *updateGolden {
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("missing golden %s (run with -update): %v", path, err)
			}
			if string(want) != got {
				t.Errorf("prompt for %s differs from golden.\n--- want ---\n%s\n--- got ---\n%s", c.name, want, got)
			}
		})
	}
}
