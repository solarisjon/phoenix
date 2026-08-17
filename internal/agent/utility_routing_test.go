package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/provider"
	"github.com/solarisjon/phoenix/internal/provider/registry"
)

// multiProviderRepo is a fakeProviderRepo that knows several records.
type multiProviderRepo struct{ recs []*model.Provider }

func (m *multiProviderRepo) List(_ context.Context, _ string) ([]*model.Provider, error) {
	return m.recs, nil
}
func (m *multiProviderRepo) Get(_ context.Context, id string) (*model.Provider, error) {
	for _, r := range m.recs {
		if r.ID == id {
			return r, nil
		}
	}
	return nil, nil
}
func (m *multiProviderRepo) Create(_ context.Context, _ *model.Provider) error { return nil }
func (m *multiProviderRepo) Update(_ context.Context, _ *model.Provider) error { return nil }
func (m *multiProviderRepo) Delete(_ context.Context, _ string) error          { return nil }
func (m *multiProviderRepo) UpdateHealth(_ context.Context, _, _ string, _ *int64, _ string) error {
	return nil
}
func (m *multiProviderRepo) UpdateAllowedModels(_ context.Context, _ string, _ []model.ModelEntry) error {
	return nil
}

// recordingProvider remembers the last prompt it was asked to run.
type recordingProvider struct {
	mockProvider
	lastPrompt string
}

func (r *recordingProvider) Execute(ctx context.Context, req provider.TaskRequest) (provider.TaskResponse, error) {
	r.lastPrompt = req.Prompt
	return r.mockProvider.Execute(ctx, req)
}

// TestFollowUpSummariser_UsesHelperModel: with a helper provider configured in
// settings, chain summarisation runs on it, not on the task's provider.
func TestFollowUpSummariser_UsesHelperModel(t *testing.T) {
	taskProv := &recordingProvider{mockProvider: mockProvider{output: "task answer", chunks: []string{"task answer"}}}
	helperProv := &recordingProvider{mockProvider: mockProvider{output: "SUMMARY", chunks: []string{"SUMMARY"}}}

	provRepo := &multiProviderRepo{recs: []*model.Provider{
		{ID: "prov-1", Type: model.ProviderTypeLLM, Config: `{}`},
		{ID: "prov-helper", Type: model.ProviderTypeLLM, Config: `{}`},
	}}
	reg := registry.NewRegistry(provRepo)
	reg.InjectForTest("prov-1", taskProv)
	reg.InjectForTest("prov-helper", helperProv)

	// A long chain (3 turns, > 8000 chars) so summarisation triggers.
	big := strings.Repeat("blah ", 700)
	root := &model.Task{ID: "root", ProjectID: "proj-1", AgentID: "agent-1", Title: "Root", Output: `{"text":"` + big + `"}`, Status: model.TaskStatusCompleted}
	mid := &model.Task{ID: "mid", ProjectID: "proj-1", AgentID: "agent-1", Title: "Mid", FollowUpOf: &root.ID, Output: `{"text":"` + big + `"}`, Status: model.TaskStatusCompleted}
	last := &model.Task{ID: "last", ProjectID: "proj-1", AgentID: "agent-1", Title: "Last", FollowUpOf: &root.ID, Output: `{"text":"` + big + `"}`, Status: model.TaskStatusCompleted}
	newTask := &model.Task{ID: "new", ProjectID: "proj-1", AgentID: "agent-1", Title: "Next", FollowUpOf: &root.ID, Status: model.TaskStatusPending, Input: "{}", Output: "{}"}

	taskRepo := &chainTaskRepo{memTaskRepo: newMemTaskRepo(root, mid, last, newTask), chain: []*model.Task{root, mid, last}}
	settings := &fakeSettingsRepo{s: &model.SystemSettings{UtilityProviderID: "prov-helper"}}
	runner := New(newMemAgentRepo(makeAgent()), taskRepo, &summariseProjectRepo{}, settings, nil, reg, nil)
	runner.SetProviderRepo(provRepo)

	ec := &executionContext{agent: makeAgent(), proj: &model.Project{ID: "proj-1", ContextSummarisation: true}, prov: taskProv}
	req, err := runner.buildTaskRequest(context.Background(), newTask, ec, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(helperProv.lastPrompt, "Summarise") {
		t.Errorf("helper provider was not asked to summarise; got %q", helperProv.lastPrompt)
	}
	if taskProv.lastPrompt != "" {
		t.Errorf("task provider should not have been used for the summary")
	}
	if !strings.Contains(req.Prompt, "SUMMARY") {
		t.Errorf("summary not injected into prompt")
	}
}

type chainTaskRepo struct {
	*memTaskRepo
	chain []*model.Task
}

func (c *chainTaskRepo) ListFollowUpChain(_ context.Context, _ string) ([]*model.Task, error) {
	return c.chain, nil
}

type fakeSettingsRepo struct{ s *model.SystemSettings }

func (f *fakeSettingsRepo) Get(_ context.Context) (*model.SystemSettings, error) { return f.s, nil }
func (f *fakeSettingsRepo) Save(_ context.Context, s *model.SystemSettings) error {
	f.s = s
	return nil
}
func (f *fakeSettingsRepo) GetRaw(_ context.Context, _ string) (string, error) { return "", nil }
func (f *fakeSettingsRepo) SetRaw(_ context.Context, _, _ string) error        { return nil }

// summariseProjectRepo returns a project with context summarisation on.
type summariseProjectRepo struct{ mockProjectRepo }

func (r *summariseProjectRepo) Get(_ context.Context, id string) (*model.Project, error) {
	return &model.Project{ID: id, Name: "P", ContextSummarisation: true}, nil
}

func TestPlanningModelFor(t *testing.T) {
	provRepo := &multiProviderRepo{recs: []*model.Provider{
		{ID: "hosted", Type: model.ProviderTypeLLM, AllowedModels: []model.ModelEntry{
			{ModelID: "gpt-4o", CapabilityTier: model.ModelTierPlanning, InputCostPer1K: 0.005, OutputCostPer1K: 0.015},
		}},
		{ID: "local", Type: model.ProviderTypeLLM, AllowedModels: []model.ModelEntry{
			{ModelID: "qwen3-14b", CapabilityTier: model.ModelTierPlanning}, // free → cheapest planning model
		}},
	}}
	reg := registry.NewRegistry(provRepo)
	runner := New(newMemAgentRepo(), newMemTaskRepo(), &mockProjectRepo{}, nil, nil, reg, nil)
	runner.SetProviderRepo(provRepo)

	if got := runner.planningModelFor(context.Background(), "local"); got != "qwen3-14b" {
		t.Errorf("local planning model = %q", got)
	}
	// Selection lands on a different provider → no override.
	if got := runner.planningModelFor(context.Background(), "hosted"); got != "" {
		t.Errorf("hosted should get no override (cheapest planning model is on 'local'), got %q", got)
	}
}

func TestResolveSubtaskRouting_ExistingAgentKeepsProvider(t *testing.T) {
	provRepo := &multiProviderRepo{recs: []*model.Provider{
		{ID: "hosted", Type: model.ProviderTypeLLM, AllowedModels: []model.ModelEntry{
			{ModelID: "gpt-4o-mini", CapabilityTier: model.ModelTierFast, InputCostPer1K: 0.00015, OutputCostPer1K: 0.0006},
		}},
		{ID: "local", Type: model.ProviderTypeLLM, AllowedModels: []model.ModelEntry{
			{ModelID: "qwen3-4b", CapabilityTier: model.ModelTierFast},
			{ModelID: "qwen3-14b", CapabilityTier: model.ModelTierStandard},
		}},
	}}
	o := NewOrchestrator(newMemAgentRepo(), newMemTaskRepo(), &mockProjectRepo{}, provRepo, nil, registry.NewRegistry(provRepo), nil)
	writer := &model.Agent{ID: "writer", Name: "Writer", Behaviour: "You write documentation and blog posts.", ProviderID: "hosted", Status: model.AgentStatusActive}
	coder := &model.Agent{ID: "coder", Name: "Coder", Behaviour: "You write code, fix bugs and refactor.", ProviderID: "local", Status: model.AgentStatusActive}
	agents := []*model.Agent{writer, coder}
	provs, _ := provRepo.List(context.Background(), "")

	// "write" domain, low complexity → cheapest fast model is local/qwen3-4b,
	// but the best-matching agent (writer) is on "hosted" → no model override.
	agentID, mo, pid, err := o.resolveSubtaskRouting(context.Background(), routedSubtask{OrchestrationSubtask: model.OrchestrationSubtask{Title: "Write docs", Domain: "write", Complexity: "low"}}, agents, provs, "proj", 0)
	if err != nil || agentID != "writer" || mo != "" || pid != "hosted" {
		t.Errorf("writer routing = (%s,%q,%s,%v)", agentID, mo, pid, err)
	}
	// "code" domain, medium → standard tier → local/qwen3-14b, coder is on local → override applied.
	agentID, mo, pid, err = o.resolveSubtaskRouting(context.Background(), routedSubtask{OrchestrationSubtask: model.OrchestrationSubtask{Title: "Fix bug", Domain: "code", Complexity: "medium"}}, agents, provs, "proj", 1)
	if err != nil || agentID != "coder" || mo != "qwen3-14b" || pid != "local" {
		t.Errorf("coder routing = (%s,%q,%s,%v)", agentID, mo, pid, err)
	}
}

// scriptedProvider returns queued outputs in order (for classifier + repair tests).
type scriptedProvider struct {
	mockProvider
	outputs []string
	prompts []string
}

func (s *scriptedProvider) Execute(_ context.Context, req provider.TaskRequest) (provider.TaskResponse, error) {
	s.prompts = append(s.prompts, req.Prompt)
	if len(s.outputs) == 0 {
		return provider.TaskResponse{Output: ""}, nil
	}
	out := s.outputs[0]
	s.outputs = s.outputs[1:]
	return provider.TaskResponse{Output: out}, nil
}

func newClassifierRunner(t *testing.T, helper provider.Provider) *Runner {
	t.Helper()
	provRepo := &multiProviderRepo{recs: []*model.Provider{
		{ID: "prov-1", Type: model.ProviderTypeLLM, Config: `{}`},
		{ID: "prov-helper", Type: model.ProviderTypeLLM, Config: `{}`},
	}}
	reg := registry.NewRegistry(provRepo)
	reg.InjectForTest("prov-1", &mockProvider{output: "task"})
	reg.InjectForTest("prov-helper", helper)
	settings := &fakeSettingsRepo{s: &model.SystemSettings{UtilityProviderID: "prov-helper"}}
	r := New(newMemAgentRepo(makeAgent()), newMemTaskRepo(), &mockProjectRepo{}, settings, nil, reg, nil)
	r.SetProviderRepo(provRepo)
	return r
}

func TestClassifyHealth(t *testing.T) {
	task := &model.Task{ID: "t", Title: "Disk check", Source: "monitor"}

	// 1. Marker present → no LLM call.
	helper := &scriptedProvider{}
	r := newClassifierRunner(t, helper)
	sig, why, rep := r.classifyHealth(context.Background(), task, "prov-1", "all fine\nHEALTH_SIGNAL: all_clear\nHEALTH_REASON: nominal")
	if sig != "all_clear" || why != "nominal" || rep != 0 || len(helper.prompts) != 0 {
		t.Errorf("marker path: %q %q %d calls=%d", sig, why, rep, len(helper.prompts))
	}

	// 2. No marker → classifier on the helper model, schema JSON reply.
	helper = &scriptedProvider{outputs: []string{`{"signal":"needs_attention","reason":"disk at 95%"}`}}
	r = newClassifierRunner(t, helper)
	sig, why, rep = r.classifyHealth(context.Background(), task, "prov-1", "Root volume is at 95% and growing. No errors in logs.")
	if sig != "needs_attention" || why != "classified: disk at 95%" || rep != 0 {
		t.Errorf("classifier path: %q %q %d", sig, why, rep)
	}
	if len(helper.prompts) != 1 || !strings.Contains(helper.prompts[0], "Root volume is at 95%") {
		t.Errorf("classifier prompt: %v", helper.prompts)
	}

	// 3. Classifier replies garbage first, valid after one repair.
	helper = &scriptedProvider{outputs: []string{"Sure! I think it's fine.", "```json\n{\"signal\": \"all_clear\", \"reason\": \"no issues\"}\n```"}}
	r = newClassifierRunner(t, helper)
	sig, why, rep = r.classifyHealth(context.Background(), task, "prov-1", "Everything responded within SLA.")
	if sig != "all_clear" || why != "classified: no issues" || rep != 1 {
		t.Errorf("repair path: %q %q %d", sig, why, rep)
	}
	if len(helper.prompts) != 2 || !strings.Contains(helper.prompts[1], "could not be used") {
		t.Errorf("repair prompt missing: %v", helper.prompts)
	}

	// 4. Classifier fails twice → needs_attention with explicit reason (never a silent all_clear).
	helper = &scriptedProvider{outputs: []string{"nope", "still nope"}}
	r = newClassifierRunner(t, helper)
	sig, why, rep = r.classifyHealth(context.Background(), task, "prov-1", "Some report text.")
	if sig != "needs_attention" || !strings.Contains(why, "could not be parsed") || rep != 1 {
		t.Errorf("failure path: %q %q %d", sig, why, rep)
	}

	// 5. Empty output → needs_attention, no call.
	helper = &scriptedProvider{}
	r = newClassifierRunner(t, helper)
	sig, _, _ = r.classifyHealth(context.Background(), task, "prov-1", "   ")
	if sig != "needs_attention" || len(helper.prompts) != 0 {
		t.Errorf("empty output path: %q calls=%d", sig, len(helper.prompts))
	}
}

// TestHandleOrchestrationComplete_RepairsPlan: a plan that isn't valid JSON is
// repaired once on the orchestrator agent's provider; the repaired plan is
// persisted and repair_attempts is recorded. A second failure leaves the task
// completed with a clear last_error and no spawn.
func TestHandleOrchestrationComplete_RepairsPlan(t *testing.T) {
	orchAgent := &model.Agent{ID: "orch", Name: "Orchestrator", ProviderID: "prov-1", IsOrchestrator: true, Status: model.AgentStatusActive}
	provRepo := &multiProviderRepo{recs: []*model.Provider{{ID: "prov-1", Type: model.ProviderTypeLLM, Config: `{}`}}}
	reg := registry.NewRegistry(provRepo)
	good := `{"confidence":0.9,"rationale":"fine","subtasks":[]}`
	prov := &scriptedProvider{outputs: []string{good}}
	reg.InjectForTest("prov-1", prov)

	task := &model.Task{ID: "orch-task", ProjectID: "proj", AgentID: "orch", Title: "Plan it", TaskType: model.TaskTypeOrchestration, Status: model.TaskStatusCompleted}
	taskRepo := newMemTaskRepo(task)
	settings := &fakeSettingsRepo{s: &model.SystemSettings{OrchestratorConfidenceThreshold: 0.5}}
	o := NewOrchestrator(newMemAgentRepo(orchAgent), taskRepo, &mockProjectRepo{}, provRepo, settings, reg, nil)

	// Broken first output (prose + truncated JSON) → repair → good plan.
	o.HandleOrchestrationComplete(context.Background(), task, "Sure! Here's the plan: {\"confidence\": 0.9, \"rationale\": \"fi", nil, "", nil)
	got, _ := taskRepo.Get(context.Background(), task.ID)
	if got.RepairAttempts != 1 {
		t.Errorf("repair_attempts = %d, want 1", got.RepairAttempts)
	}
	if !strings.Contains(got.OrchestrationPlan, `"confidence":0.9`) {
		t.Errorf("repaired plan not persisted: %q", got.OrchestrationPlan)
	}
	if len(prov.prompts) != 1 || !strings.Contains(prov.prompts[0], "could not be used") {
		t.Errorf("repair prompt = %v", prov.prompts)
	}

	// Second scenario: repair also fails → last_error set, no plan.
	task2 := &model.Task{ID: "orch-task-2", ProjectID: "proj", AgentID: "orch", Title: "Plan it", TaskType: model.TaskTypeOrchestration, Status: model.TaskStatusCompleted}
	_ = taskRepo.Create(context.Background(), task2)
	prov.outputs = []string{"still not json"}
	o.HandleOrchestrationComplete(context.Background(), task2, "not json either", nil, "", nil)
	got2, _ := taskRepo.Get(context.Background(), task2.ID)
	if got2.RepairAttempts != 1 || got2.OrchestrationPlan != "" || !strings.Contains(got2.LastError, "could not be parsed") {
		t.Errorf("failed repair: attempts=%d plan=%q last_error=%q", got2.RepairAttempts, got2.OrchestrationPlan, got2.LastError)
	}
}
