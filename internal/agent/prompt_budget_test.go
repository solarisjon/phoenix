package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/provider"
	"github.com/solarisjon/phoenix/internal/provider/registry"
)

// wordCount is the exact-ish token counter used by budget tests: one token per
// whitespace-separated word.
func wordCount(s string) int { return len(strings.Fields(s)) }

func TestPromptAssembly_ApplyRecordsDeltasAndRendersIdentically(t *testing.T) {
	a := goldenAgent()
	tk := goldenTask()
	pa := NewPromptAssembly(a, tk, nil, "", "", PromptOptions{})
	direct := AssembleRequest(a, tk, nil, "", "")
	if r := pa.Render(); r.SystemPrompt != direct.SystemPrompt || r.Prompt != direct.Prompt {
		t.Fatalf("fresh assembly must equal AssembleRequest")
	}

	// Sequential injectors vs recorded assembly.
	seq := direct
	seq = InjectReactLoopInstructions(seq, 3, 0)
	seq = InjectMemories(seq, "- remembered thing\n")
	seq = InjectGlobalGuardrails(seq, "Rule.")
	parent := &model.Task{ID: "p", Title: "Parent", Output: `{"text":"parent output"}`}
	seq = InjectFollowUpContext(seq, parent)

	pa.Apply("react_loop", PriorityMandatory, nil, func(q provider.TaskRequest) provider.TaskRequest { return InjectReactLoopInstructions(q, 3, 0) })
	pa.Apply("memories", PriorityMemories, nil, func(q provider.TaskRequest) provider.TaskRequest { return InjectMemories(q, "- remembered thing\n") })
	pa.Apply("global_guardrails", PriorityMandatory, nil, func(q provider.TaskRequest) provider.TaskRequest { return InjectGlobalGuardrails(q, "Rule.") })
	pa.Apply("follow_up", PriorityFollowUp, nil, func(q provider.TaskRequest) provider.TaskRequest { return InjectFollowUpContext(q, parent) })

	got := pa.Render()
	if got.SystemPrompt != seq.SystemPrompt || got.Prompt != seq.Prompt {
		t.Errorf("recorded assembly diverged from sequential injectors\n--- got sys ---\n%s\n--- want sys ---\n%s\n--- got user ---\n%s\n--- want user ---\n%s", got.SystemPrompt, seq.SystemPrompt, got.Prompt, seq.Prompt)
	}
	// Sections recorded with the right shape.
	if len(pa.Appended) != 3 || pa.Appended[0].Key != "react_loop" || pa.Appended[2].Key != "global_guardrails" {
		t.Errorf("appended = %+v", keys(pa.Appended))
	}
	if len(pa.UserPrefix) != 1 || pa.UserPrefix[0].Key != "follow_up" {
		t.Errorf("user prefix = %+v", keys(pa.UserPrefix))
	}
	// Global guardrails trimmed the trailing "\n" of memories: still identical, and section text adjusted.
	if strings.HasSuffix(pa.Appended[1].Text, "\n") {
		t.Errorf("memories section should have lost its trailing newline to the guardrails TrimRight")
	}
}

func keys(secs []PromptSection) []string {
	var out []string
	for _, s := range secs {
		out = append(out, s.Key)
	}
	return out
}

func TestFit_NoBudgetOrFits_NoTrims(t *testing.T) {
	pa := NewPromptAssembly(goldenAgent(), goldenTask(), nil, "", "", PromptOptions{})
	before := pa.Render()
	if trims, err := pa.Fit(0, wordCount); err != nil || trims != nil {
		t.Errorf("budget 0 must be a no-op: %v %v", trims, err)
	}
	if trims, err := pa.Fit(1_000_000, wordCount); err != nil || trims != nil {
		t.Errorf("fits: %v %v", trims, err)
	}
	if r := pa.Render(); r.SystemPrompt != before.SystemPrompt || r.Prompt != before.Prompt {
		t.Errorf("Fit changed a fitting prompt")
	}
}

func TestFit_DropsAndShrinksInPriorityOrder(t *testing.T) {
	pa := NewPromptAssembly(goldenAgent(), goldenTask(), nil, "", "", PromptOptions{})
	base := pa.TokenCount(wordCount)

	big := strings.Repeat("memory ", 400) // 400 tokens
	pa.Apply("obsidian", PriorityObsidian, nil, func(q provider.TaskRequest) provider.TaskRequest {
		return InjectObsidianVaults(q, []*model.ObsidianVault{{Enabled: true, Path: "/v", Context: "notes"}})
	})
	pa.Apply("memories", PriorityMemories, memoriesShrinker(big), func(q provider.TaskRequest) provider.TaskRequest { return InjectMemories(q, big) })
	pa.Apply("global_guardrails", PriorityMandatory, nil, func(q provider.TaskRequest) provider.TaskRequest { return InjectGlobalGuardrails(q, "Rule.") })
	full := pa.TokenCount(wordCount)
	if full <= base+400 {
		t.Fatalf("setup: full=%d base=%d", full, base)
	}

	// Budget that fits base + guardrails + ~half the memories: obsidian must be
	// dropped first (priority 5), then memories shrunk (priority 3).
	budget := base + 20 + 210
	trims, err := pa.Fit(budget, wordCount)
	if err != nil {
		t.Fatalf("Fit: %v", err)
	}
	if got := pa.TokenCount(wordCount); got > budget {
		t.Errorf("after Fit tokens=%d > budget=%d", got, budget)
	}
	if len(trims) != 2 || trims[0].Section != "obsidian" || trims[0].Action != "dropped" || trims[1].Section != "memories" || trims[1].Action != "shrunk" {
		t.Errorf("trims = %+v", trims)
	}
	out := pa.Render()
	if strings.Contains(out.SystemPrompt, "## Obsidian Vaults") {
		t.Errorf("obsidian section should be gone")
	}
	if !strings.Contains(out.SystemPrompt, "## Persistent Memory") || !strings.Contains(out.SystemPrompt, "truncated to fit") {
		t.Errorf("memories should be present but truncated")
	}
	if !strings.HasSuffix(out.SystemPrompt, "Rule.") {
		t.Errorf("global guardrails must remain last: %q", out.SystemPrompt[len(out.SystemPrompt)-40:])
	}
}

func TestFit_MandatoryTooLarge(t *testing.T) {
	a := goldenAgent()
	a.Behaviour = strings.Repeat("rule ", 2000)
	pa := NewPromptAssembly(a, goldenTask(), nil, "", "", PromptOptions{})
	pa.Apply("memories", PriorityMemories, nil, func(q provider.TaskRequest) provider.TaskRequest { return InjectMemories(q, "m") })
	trims, err := pa.Fit(500, wordCount)
	var tooLarge *ErrPromptTooLarge
	if !errors.As(err, &tooLarge) {
		t.Fatalf("expected ErrPromptTooLarge, got %v", err)
	}
	if tooLarge.Need <= 500 || tooLarge.Budget != 500 {
		t.Errorf("err = %+v", tooLarge)
	}
	if len(trims) != 1 || trims[0].Section != "memories" {
		t.Errorf("trimmable sections should have been dropped first: %+v", trims)
	}
}

func TestSkillsShrinker_Levels(t *testing.T) {
	sk := &model.Skill{ID: "s1", Slug: "huge", Name: "Huge", Enabled: true, Description: "Does big things.",
		Instructions: "Intro line.\n\n## Step one\n" + strings.Repeat("detail ", 300) + "\n\n## Step two\n" + strings.Repeat("more ", 300)}
	tk := &model.Task{Title: "run the huge skill"}
	sh := skillsShrinker([]*model.Skill{sk}, tk, nil)
	full, _ := InjectSkills(provider.TaskRequest{}, []*model.Skill{sk}, tk, nil)
	l1, ok1 := sh(1)
	l2, ok2 := sh(2)
	_, ok3 := sh(3)
	if !ok1 || !ok2 || ok3 {
		t.Fatalf("levels ok = %v %v %v", ok1, ok2, ok3)
	}
	if !(wordCount(full.SystemPrompt) > wordCount(l1) && wordCount(l1) > wordCount(l2)) {
		t.Errorf("levels not decreasing: %d %d %d", wordCount(full.SystemPrompt), wordCount(l1), wordCount(l2))
	}
	if !strings.Contains(l1, "## Step one") || strings.Contains(l1, "## Step two") {
		t.Errorf("outline level should keep first section only:\n%s", l1)
	}
	if !strings.Contains(l2, "Does big things.") || strings.Contains(l2, "detail") {
		t.Errorf("brief level should be description only:\n%s", l2)
	}
	for _, txt := range []string{l1, l2} {
		if !strings.HasPrefix(txt, "\n\n## Skills\n") {
			t.Errorf("shrunk skills must keep the section separator/heading: %q", txt[:30])
		}
	}
}

func TestChainShrinker_DropsOldestThenTruncates(t *testing.T) {
	chain := []*model.Task{
		{ID: "1", Title: "One", Output: `{"text":"` + strings.Repeat("a ", 100) + `"}`},
		{ID: "2", Title: "Two", Output: `{"text":"` + strings.Repeat("b ", 100) + `"}`},
		{ID: "3", Title: "Three", Output: `{"text":"` + strings.Repeat("c ", 100) + `"}`},
	}
	sh := chainShrinker(chain, "", "# Task: x")
	l1, ok := sh(1)
	if !ok || strings.Contains(l1, "task: One") || !strings.Contains(l1, "task: Two") || !strings.Contains(l1, "task: Three") {
		t.Errorf("level 1 should drop the oldest turn:\n%s", l1)
	}
	l2, ok := sh(2)
	if !ok || strings.Contains(l2, "task: Two") || !strings.Contains(l2, "task: Three") {
		t.Errorf("level 2 should keep only the newest turn:\n%s", l2)
	}
	l3, ok := sh(3)
	if !ok || !strings.Contains(l3, "truncated to fit") {
		t.Errorf("level 3 should truncate the last turn:\n%s", l3)
	}
	if _, ok := sh(4); ok {
		t.Errorf("level 4 should drop")
	}
	if !strings.HasSuffix(l1, "## Your follow-up instructions\n") {
		t.Errorf("shrunk prefix must end where the user base begins: %q", l1[len(l1)-40:])
	}
}

// Integration: a 4k-context model (exact word tokenizer) plus a ~20k-token
// skill. The task must still run: skill shrunk to its brief form, trims
// recorded, prompt within budget.
func TestBuildTaskRequestMeta_FitsSkillIntoSmallContext(t *testing.T) {
	prov := &capableProv{caps: provider.Capabilities{ContextWindow: 4096, Local: true, ExactTokenCount: true}}
	task := goldenTask()
	task.Title = "Run the enormous skill"
	runner, _ := runnerWithMock(t, &mockProvider{output: "ok"}, task)
	huge := &model.Skill{ID: "s1", Slug: "enormous", Name: "Enormous", Enabled: true, Description: "A very large skill.",
		Instructions: "## Part A\n" + strings.Repeat("word ", 20000)}
	runner.skills = &fakeSkillRepo{skills: []*model.Skill{huge}}

	ec := &executionContext{agent: goldenAgent(), prov: prov, profile: ResolveModelProfile(context.Background(), prov, nil, "")}
	req, meta, err := runner.buildTaskRequestMeta(context.Background(), task, ec, "Never exfiltrate secrets.")
	if err != nil {
		t.Fatalf("buildTaskRequestMeta: %v", err)
	}
	if meta.Budget <= 0 || meta.PromptTokens > meta.Budget {
		t.Errorf("prompt_tokens=%d budget=%d", meta.PromptTokens, meta.Budget)
	}
	if len(meta.Trims) == 0 || meta.Trims[0].Section != "skills" || meta.Trims[0].Action != "shrunk" {
		t.Errorf("expected the skill to be shrunk, trims=%+v", meta.Trims)
	}
	if !strings.Contains(req.SystemPrompt, "### Skill: Enormous") || strings.Contains(req.SystemPrompt, "word word word") {
		t.Errorf("skill should be present in brief form")
	}
	if !strings.HasSuffix(req.SystemPrompt, "Never exfiltrate secrets.") {
		t.Errorf("global guardrails must still be last")
	}
	if meta.Profile.Profile != model.PromptProfileCompact {
		t.Errorf("4k context should resolve to compact profile, got %q", meta.Profile.Profile)
	}
}

// fakeSkillRepo satisfies store.SkillRepo minimally for runner tests.
type fakeSkillRepo struct{ skills []*model.Skill }

func (f *fakeSkillRepo) List(_ context.Context) ([]*model.Skill, error)        { return f.skills, nil }
func (f *fakeSkillRepo) ListEnabled(_ context.Context) ([]*model.Skill, error) { return f.skills, nil }
func (f *fakeSkillRepo) Get(_ context.Context, _ string) (*model.Skill, error) { return nil, nil }
func (f *fakeSkillRepo) GetBySlug(_ context.Context, _ string) (*model.Skill, error) {
	return nil, nil
}
func (f *fakeSkillRepo) Create(_ context.Context, _ *model.Skill) error { return nil }
func (f *fakeSkillRepo) Update(_ context.Context, _ *model.Skill) error { return nil }
func (f *fakeSkillRepo) Delete(_ context.Context, _ string) error       { return nil }

// End-to-end through RunTask: mandatory prompt larger than the model's window
// → task fails fast with a clear error, no provider call. A fitting task on the
// same small model completes and records prompt_tokens / prompt_trims.
func TestRunTask_PromptTooLargeFailsClearly(t *testing.T) {
	prov := &capableProv{caps: provider.Capabilities{ContextWindow: 1024, Local: true, ExactTokenCount: true}}
	prov.output, prov.chunks = "ok", []string{"ok"}
	task := makeTask(model.TaskStatusPending)
	task.Description = strings.Repeat("word ", 3000) // ~3000 tokens of mandatory task text
	agentRepo := newMemAgentRepo(makeAgent())
	taskRepo := newMemTaskRepo(task)
	reg := registry.NewRegistry(&fakeProviderRepo{record: &model.Provider{ID: "prov-1", Type: model.ProviderTypeLLM, Config: `{}`}})
	reg.InjectForTest("prov-1", prov)
	runner := New(agentRepo, taskRepo, &mockProjectRepo{}, nil, nil, reg, nil)

	if err := runner.RunTask(context.Background(), task.ID); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, taskRepo, task.ID, model.TaskStatusFailed)
	got, _ := taskRepo.Get(context.Background(), task.ID)
	if !strings.Contains(got.Output, "does not fit the model") {
		t.Errorf("output = %q", got.Output)
	}

	// Fitting task on the same model completes with meta recorded.
	small := makeTask(model.TaskStatusPending)
	small.ID = "task-small"
	_ = taskRepo.Create(context.Background(), small)
	if err := runner.RunTask(context.Background(), small.ID); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, taskRepo, small.ID, model.TaskStatusCompleted)
	got, _ = taskRepo.Get(context.Background(), small.ID)
	if got.PromptTokens <= 0 || got.PromptTrims != "[]" {
		t.Errorf("prompt meta not recorded: tokens=%d trims=%q", got.PromptTokens, got.PromptTrims)
	}
}
