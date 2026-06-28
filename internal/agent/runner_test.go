package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/provider"
	"github.com/solarisjon/phoenix/internal/provider/registry"
)

// ---- Mock provider ----

type mockProvider struct {
	output  string
	err     error
	chunks  []string
	costUSD float64
}

func (m *mockProvider) Execute(_ context.Context, _ provider.TaskRequest) (provider.TaskResponse, error) {
	if m.err != nil {
		return provider.TaskResponse{}, m.err
	}
	return provider.TaskResponse{Output: m.output, TokensIn: 10, TokensOut: 5, CostUSD: m.costUSD}, nil
}

func (m *mockProvider) StreamExecute(_ context.Context, _ provider.TaskRequest) (<-chan provider.StreamChunk, error) {
	if m.err != nil {
		return nil, m.err
	}
	ch := make(chan provider.StreamChunk, len(m.chunks)+1)
	for _, c := range m.chunks {
		ch <- provider.StreamChunk{Content: c}
	}
	ch <- provider.StreamChunk{Done: true}
	close(ch)
	return ch, nil
}

func (m *mockProvider) EstimateCost(_ provider.TaskRequest) provider.CostEstimate {
	return provider.CostEstimate{EstimatedCostUSD: m.costUSD}
}

// ---- Mock store repos ----

type memAgentRepo struct {
	agents map[string]*model.Agent
}

func newMemAgentRepo(agents ...*model.Agent) *memAgentRepo {
	r := &memAgentRepo{agents: make(map[string]*model.Agent)}
	for _, a := range agents {
		r.agents[a.ID] = a
	}
	return r
}

func (r *memAgentRepo) List(_ context.Context) ([]*model.Agent, error) {
	out := make([]*model.Agent, 0, len(r.agents))
	for _, a := range r.agents {
		out = append(out, a)
	}
	return out, nil
}
func (r *memAgentRepo) Get(_ context.Context, id string) (*model.Agent, error) {
	return r.agents[id], nil
}
func (r *memAgentRepo) Create(_ context.Context, a *model.Agent) error {
	r.agents[a.ID] = a
	return nil
}
func (r *memAgentRepo) Update(_ context.Context, a *model.Agent) error {
	r.agents[a.ID] = a
	return nil
}
func (r *memAgentRepo) Delete(_ context.Context, id string) error {
	delete(r.agents, id)
	return nil
}
func (r *memAgentRepo) Search(_ context.Context, _ string) ([]*model.Agent, error) {
	return nil, nil
}

type memTaskRepo struct {
	tasks map[string]*model.Task
}

func newMemTaskRepo(tasks ...*model.Task) *memTaskRepo {
	r := &memTaskRepo{tasks: make(map[string]*model.Task)}
	for _, t := range tasks {
		r.tasks[t.ID] = t
	}
	return r
}

func (r *memTaskRepo) List(_ context.Context, projectID string) ([]*model.Task, error) {
	var out []*model.Task
	for _, t := range r.tasks {
		if t.ProjectID == projectID {
			out = append(out, t)
		}
	}
	return out, nil
}
func (r *memTaskRepo) ListByProject(_ context.Context, projectID string, status model.TaskStatus, limit int) ([]*model.Task, error) {
	var out []*model.Task
	for _, t := range r.tasks {
		if t.ProjectID == projectID && (status == "" || t.Status == status) {
			out = append(out, t)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (r *memTaskRepo) ListAll(_ context.Context) ([]*model.Task, error) {
	var out []*model.Task
	for _, t := range r.tasks {
		out = append(out, t)
	}
	return out, nil
}

func (r *memTaskRepo) ListByStatus(_ context.Context, s model.TaskStatus) ([]*model.Task, error) {
	var out []*model.Task
	for _, t := range r.tasks {
		if t.Status == s {
			out = append(out, t)
		}
	}
	return out, nil
}

func (r *memTaskRepo) ListByStatuses(_ context.Context, statuses []model.TaskStatus) ([]*model.Task, error) {
	set := make(map[model.TaskStatus]bool, len(statuses))
	for _, s := range statuses {
		set[s] = true
	}
	var out []*model.Task
	for _, t := range r.tasks {
		if set[t.Status] {
			out = append(out, t)
		}
	}
	return out, nil
}
func (r *memTaskRepo) ListByAgent(_ context.Context, agentID string) ([]*model.Task, error) {
	var out []*model.Task
	for _, t := range r.tasks {
		if t.AgentID == agentID {
			out = append(out, t)
		}
	}
	return out, nil
}
func (r *memTaskRepo) Search(_ context.Context, query string) ([]*model.Task, error) {
	var out []*model.Task
	q := strings.ToLower(strings.Trim(query, `"`))
	for _, t := range r.tasks {
		if strings.Contains(strings.ToLower(t.Title), q) ||
			strings.Contains(strings.ToLower(t.Description), q) ||
			strings.Contains(strings.ToLower(t.Output), q) {
			out = append(out, t)
		}
	}
	return out, nil
}
func (r *memTaskRepo) Get(_ context.Context, id string) (*model.Task, error) {
	return r.tasks[id], nil
}
func (r *memTaskRepo) Create(_ context.Context, t *model.Task) error {
	r.tasks[t.ID] = t
	return nil
}
func (r *memTaskRepo) Update(_ context.Context, t *model.Task) error {
	r.tasks[t.ID] = t
	return nil
}
func (r *memTaskRepo) Delete(_ context.Context, id string) error {
	delete(r.tasks, id)
	return nil
}
func (r *memTaskRepo) NextQueuedTask(_ context.Context, agentID string) (*model.Task, error) {
	var oldest *model.Task
	for _, t := range r.tasks {
		if t.AgentID == agentID && t.Status == model.TaskStatusQueued {
			if oldest == nil || t.CreatedAt.Before(oldest.CreatedAt) {
				oldest = t
			}
		}
	}
	return oldest, nil
}
func (r *memTaskRepo) CancelQueuedTask(_ context.Context, taskID string) (bool, error) {
	t, ok := r.tasks[taskID]
	if !ok || t.Status != model.TaskStatusQueued {
		return false, nil
	}
	t.Status = model.TaskStatusFailed
	now := time.Now()
	t.CompletedAt = &now
	return true, nil
}

func (r *memTaskRepo) ListCompletedForInbox(_ context.Context, limit int) ([]*model.Task, error) {
	var out []*model.Task
	for _, t := range r.tasks {
		if t.Status == model.TaskStatusCompleted {
			out = append(out, t)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *memTaskRepo) FindByPromptHash(_ context.Context, projectID, hash string) (*model.Task, error) {
	for _, t := range r.tasks {
		if t.ProjectID == projectID && t.PromptHash == hash && t.Status == model.TaskStatusCompleted {
			return t, nil
		}
	}
	return nil, nil
}

func (r *memTaskRepo) ProjectSpendForPeriod(_ context.Context, _ string, _ string) (float64, error) {
	return 0, nil
}

func (r *memTaskRepo) ForceFailTask(_ context.Context, taskID string) (bool, error) {
	t, ok := r.tasks[taskID]
	if !ok {
		return false, nil
	}
	if t.Status == model.TaskStatusCompleted || t.Status == model.TaskStatusFailed {
		return false, nil
	}
	t.Status = model.TaskStatusFailed
	return true, nil
}

func (r *memTaskRepo) ListProjectHistory(_ context.Context, projectID string) ([]*model.Task, error) {
	var out []*model.Task
	for _, t := range r.tasks {
		if t.ProjectID == projectID && t.Status == model.TaskStatusCompleted {
			out = append(out, t)
		}
	}
	return out, nil
}

func (r *memTaskRepo) LastMonitorRunAt(_ context.Context, projectID string) (*time.Time, error) {
	var latest *time.Time
	for _, t := range r.tasks {
		if t.ProjectID == projectID && t.Source == "monitor" {
			ts := t.CreatedAt
			if latest == nil || ts.After(*latest) {
				cp := ts
				latest = &cp
			}
		}
	}
	return latest, nil
}

func (r *memTaskRepo) ListTimedOut(_ context.Context) ([]*model.Task, error) {
	return nil, nil
}

func (r *memTaskRepo) ListFollowUpChain(_ context.Context, rootTaskID string) ([]*model.Task, error) {
	root, ok := r.tasks[rootTaskID]
	if !ok {
		return nil, nil
	}
	return []*model.Task{root}, nil
}

func (r *memTaskRepo) SaveSummaryCache(_ context.Context, taskID, summary string) error {
	if t, ok := r.tasks[taskID]; ok {
		t.SummaryCache = summary
	}
	return nil
}

func (r *memTaskRepo) HasActiveTaskForProject(_ context.Context, projectID string) (bool, error) {
	for _, t := range r.tasks {
		if t.ProjectID == projectID && (t.Status == model.TaskStatusRunning || t.Status == model.TaskStatusQueued) {
			return true, nil
		}
	}
	return false, nil
}

func (r *memTaskRepo) BumpPriority(_ context.Context, _ string) error { return nil }
func (r *memTaskRepo) UnlockDependents(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

// ---- Mock project repo ----

type mockProjectRepo struct{}

func (r *mockProjectRepo) List(_ context.Context) ([]*model.Project, error) { return nil, nil }
func (r *mockProjectRepo) ListByKind(_ context.Context, _ string) ([]*model.Project, error) {
	return nil, nil
}
func (r *mockProjectRepo) Get(_ context.Context, id string) (*model.Project, error) {
	return &model.Project{ID: id, Name: "Test Project"}, nil
}
func (r *mockProjectRepo) Create(_ context.Context, _ *model.Project) error  { return nil }
func (r *mockProjectRepo) Update(_ context.Context, _ *model.Project) error  { return nil }
func (r *mockProjectRepo) Delete(_ context.Context, _ string) error              { return nil }
func (r *mockProjectRepo) DeleteWithTasks(_ context.Context, _ string) error     { return nil }
func (r *mockProjectRepo) ListByStatus(_ context.Context, _, _ string) ([]*model.Project, error) {
	return nil, nil
}
func (r *mockProjectRepo) AssignAgent(_ context.Context, _, _ string) (bool, error) { return false, nil }
func (r *mockProjectRepo) IsAgentAssigned(_ context.Context, _, _ string) (bool, error) { return true, nil }
func (r *mockProjectRepo) RemoveAgent(_ context.Context, _, _ string) error  { return nil }
func (r *mockProjectRepo) ListAgents(_ context.Context, _ string) ([]*model.Agent, error) {
	return nil, nil
}
func (r *mockProjectRepo) Search(_ context.Context, _ string) ([]*model.Project, error) {
	return nil, nil
}

// ---- Helpers ----

func makeAgent() *model.Agent {
	return &model.Agent{
		ID:           "agent-1",
		Name:         "Test Agent",
		Persona:      "You are a test agent.",
		Instructions: "Do the task.",
		Guardrails:   "Be safe.",
		ProviderID:   "prov-1",
		Status:       model.AgentStatusActive,
	}
}

func makeTask(status model.TaskStatus) *model.Task {
	return &model.Task{
		ID:          "task-1",
		ProjectID:   "proj-1",
		AgentID:     "agent-1",
		Title:       "Test Task",
		Description: "Do something useful.",
		Status:      status,
		Input:       "{}",
		Output:      "{}",
	}
}

// runnerWithMock builds a Runner wired to a mock provider, using an in-process
// registry shim so we avoid the concrete *registry.Registry dependency.
func runnerWithMock(t *testing.T, prov *mockProvider, task *model.Task) (*Runner, *memTaskRepo) {
	t.Helper()

	agentRepo := newMemAgentRepo(makeAgent())
	taskRepo := newMemTaskRepo(task)

	// Build a real registry backed by a fake ProviderRepo that returns a record
	// pointing at our mock. We intercept via a fake store.ProviderRepo.
	fakeProvRepo := &fakeProviderRepo{
		record: &model.Provider{
			ID:     "prov-1",
			Name:   "Mock",
			Type:   model.ProviderTypeLLM,
			Config: `{"endpoint":"http://mock.local"}`,
		},
	}
	reg := registry.NewRegistry(fakeProvRepo)
	// Pre-warm the registry cache with our mock provider.
	reg.InjectForTest("prov-1", prov)

	runner := New(agentRepo, taskRepo, &mockProjectRepo{}, nil, nil, reg, nil)
	return runner, taskRepo
}

// fakeProviderRepo satisfies store.ProviderRepo for registry construction.
type fakeProviderRepo struct {
	record *model.Provider
}

func (f *fakeProviderRepo) List(_ context.Context) ([]*model.Provider, error) {
	return []*model.Provider{f.record}, nil
}
func (f *fakeProviderRepo) Get(_ context.Context, _ string) (*model.Provider, error) {
	return f.record, nil
}
func (f *fakeProviderRepo) Create(_ context.Context, _ *model.Provider) error { return nil }
func (f *fakeProviderRepo) Update(_ context.Context, _ *model.Provider) error { return nil }
func (f *fakeProviderRepo) Delete(_ context.Context, _ string) error          { return nil }
func (f *fakeProviderRepo) UpdateHealth(_ context.Context, _, _ string, _ *int64, _ string) error {
	return nil
}

// waitForStatus polls until the task reaches the expected status or times out.
func waitForStatus(t *testing.T, repo *memTaskRepo, taskID string, want model.TaskStatus) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		task, _ := repo.Get(context.Background(), taskID)
		if task != nil && task.Status == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	task, _ := repo.Get(context.Background(), taskID)
	got := model.TaskStatus("nil")
	if task != nil {
		got = task.Status
	}
	t.Errorf("task %s: want status %s, got %s (timed out)", taskID, want, got)
}

// ---- Tests ----

func TestRunTask_Success(t *testing.T) {
	prov := &mockProvider{
		chunks:  []string{"Hello", " world"},
		output:  "Hello world",
		costUSD: 0.001,
	}
	task := makeTask(model.TaskStatusPending)
	runner, taskRepo := runnerWithMock(t, prov, task)

	if err := runner.RunTask(context.Background(), task.ID); err != nil {
		t.Fatalf("RunTask: %v", err)
	}

	waitForStatus(t, taskRepo, task.ID, model.TaskStatusCompleted)

	got, _ := taskRepo.Get(context.Background(), task.ID)
	if got.CostUSD != 0.001 {
		t.Errorf("CostUSD = %v, want 0.001", got.CostUSD)
	}
	if got.StartedAt == nil {
		t.Error("StartedAt should be set")
	}
	if got.CompletedAt == nil {
		t.Error("CompletedAt should be set")
	}
}

func TestRunTask_NotPending(t *testing.T) {
	prov := &mockProvider{output: "ok"}
	task := makeTask(model.TaskStatusRunning)
	runner, _ := runnerWithMock(t, prov, task)

	err := runner.RunTask(context.Background(), task.ID)
	if err == nil {
		t.Fatal("expected error for non-pending task")
	}
}

func TestRunTask_ProviderError(t *testing.T) {
	prov := &mockProvider{err: fmt.Errorf("provider unavailable")}
	task := makeTask(model.TaskStatusPending)
	runner, taskRepo := runnerWithMock(t, prov, task)

	if err := runner.RunTask(context.Background(), task.ID); err != nil {
		t.Fatalf("RunTask: %v", err)
	}

	waitForStatus(t, taskRepo, task.ID, model.TaskStatusFailed)
}

func TestRunTask_Cancellation(t *testing.T) {
	// Provider that blocks until context is done.
	prov := &mockProvider{chunks: []string{}} // empty chunks → Done immediately
	task := makeTask(model.TaskStatusPending)
	runner, taskRepo := runnerWithMock(t, prov, task)

	ctx, cancel := context.WithCancel(context.Background())
	if err := runner.RunTask(ctx, task.ID); err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	cancel()

	// Task should end up completed or failed (not stuck running).
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := taskRepo.Get(context.Background(), task.ID)
		if got != nil && got.Status != model.TaskStatusRunning && got.Status != model.TaskStatusQueued {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("task did not terminate after context cancellation")
}

func TestRunTask_StreamEvents(t *testing.T) {
	prov := &mockProvider{
		chunks:  []string{"chunk1", "chunk2"},
		output:  "chunk1chunk2",
		costUSD: 0.0,
	}
	task := makeTask(model.TaskStatusPending)

	agentRepo := newMemAgentRepo(makeAgent())
	taskRepo := newMemTaskRepo(task)
	fakeProvRepo := &fakeProviderRepo{
		record: &model.Provider{ID: "prov-1", Type: model.ProviderTypeLLM, Config: `{"endpoint":"http://mock.local"}`},
	}
	reg := registry.NewRegistry(fakeProvRepo)
	reg.InjectForTest("prov-1", prov)

	var events []StreamEvent
	var mu sync.Mutex
	handler := func(ev StreamEvent) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	}

	runner := New(agentRepo, taskRepo, &mockProjectRepo{}, nil, nil, reg, handler)
	if err := runner.RunTask(context.Background(), task.ID); err != nil {
		t.Fatalf("RunTask: %v", err)
	}

	waitForStatus(t, taskRepo, task.ID, model.TaskStatusCompleted)

	mu.Lock()
	defer mu.Unlock()

	chunkCount := 0
	doneCount := 0
	for _, ev := range events {
		if ev.Chunk != nil {
			chunkCount++
		}
		if ev.StatusDone != nil {
			doneCount++
		}
	}
	if chunkCount == 0 {
		t.Error("expected at least one chunk event")
	}
	if doneCount == 0 {
		t.Error("expected at least one done event")
	}
}

func TestActiveTasksAndShutdown(t *testing.T) {
	bgCtx, bgCancel := context.WithCancel(context.Background())
	runner := &Runner{
		cancels:  make(map[string]context.CancelCauseFunc),
		bgCtx:    bgCtx,
		bgCancel: bgCancel,
	}

	ctx, cancel := context.WithCancelCause(bgCtx)
	runner.mu.Lock()
	runner.cancels["task-x"] = cancel
	runner.mu.Unlock()
	_ = ctx

	ids := runner.ActiveTasks()
	if len(ids) != 1 || ids[0] != "task-x" {
		t.Errorf("ActiveTasks = %v, want [task-x]", ids)
	}

	runner.Shutdown()
	if len(runner.cancels) != 0 {
		t.Error("expected empty cancels after shutdown")
	}
	// bgCtx should be cancelled
	select {
	case <-bgCtx.Done():
	default:
		t.Error("expected bgCtx to be cancelled after Shutdown")
	}
}
