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
