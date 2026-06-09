package agent

// runner_concurrency_test.go exercises MaxConcurrent limiting and drainQueue
// sequential processing, covering the two concurrency fixes in runner.go.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/provider"
	"github.com/solarisjon/phoenix/internal/provider/registry"
)

// blockingProvider holds each StreamExecute call until the caller closes the
// returned release channel, allowing tests to control task interleaving.
type blockingProvider struct {
	mu       sync.Mutex
	releases []chan struct{}
}

func (b *blockingProvider) Execute(_ context.Context, _ provider.TaskRequest) (provider.TaskResponse, error) {
	return provider.TaskResponse{Output: "ok"}, nil
}

func (b *blockingProvider) StreamExecute(_ context.Context, _ provider.TaskRequest) (<-chan provider.StreamChunk, error) {
	release := make(chan struct{})
	b.mu.Lock()
	b.releases = append(b.releases, release)
	b.mu.Unlock()

	ch := make(chan provider.StreamChunk, 2)
	go func() {
		<-release
		ch <- provider.StreamChunk{Content: "done", Done: true}
		close(ch)
	}()
	return ch, nil
}

func (b *blockingProvider) EstimateCost(_ provider.TaskRequest) provider.CostEstimate {
	return provider.CostEstimate{}
}

// releaseNth releases the nth task (0-indexed) that has called StreamExecute.
// It blocks briefly until the release slot is available.
func (b *blockingProvider) releaseNth(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		b.mu.Lock()
		if len(b.releases) > n {
			rel := b.releases[n]
			b.mu.Unlock()
			close(rel)
			return
		}
		b.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("timed out waiting for release slot %d", n)
}

// waitForNReleases waits until n tasks have called StreamExecute (i.e. are running).
func (b *blockingProvider) waitForNReleases(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		b.mu.Lock()
		count := len(b.releases)
		b.mu.Unlock()
		if count >= n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	b.mu.Lock()
	count := len(b.releases)
	b.mu.Unlock()
	t.Errorf("timed out: want %d running tasks, got %d", n, count)
}

// makeTaskForAgent creates a unique pending task assigned to the given agent.
func makeTaskForAgent(agentID, projectID string) *model.Task {
	return &model.Task{
		ID:          uuid.New().String(),
		ProjectID:   projectID,
		AgentID:     agentID,
		Title:       "Concurrent test task",
		Description: "test",
		Status:      model.TaskStatusPending,
		Input:       "{}",
		Output:      "{}",
		CreatedAt:   time.Now(),
	}
}

// buildRunnerWithBlockingProvider constructs a runner backed by a blockingProvider
// and an agent with the specified MaxConcurrent limit (0 = unlimited).
func buildRunnerWithBlockingProvider(t *testing.T, bprov *blockingProvider, maxConcurrent int) (*Runner, *memTaskRepo, *model.Agent) {
	t.Helper()

	ag := &model.Agent{
		ID:            "agent-cc",
		Name:          "ConcurrencyAgent",
		Persona:       "You are a test agent.",
		Instructions:  "Do the task.",
		ProviderID:    "prov-cc",
		Status:        model.AgentStatusActive,
		MaxConcurrent: maxConcurrent,
	}
	agentRepo := newMemAgentRepo(ag)
	taskRepo := &memTaskRepo{tasks: make(map[string]*model.Task)}

	fakeProvRepo := &fakeProviderRepo{
		record: &model.Provider{
			ID:     "prov-cc",
			Name:   "BlockingMock",
			Type:   model.ProviderTypeLLM,
			Config: `{"endpoint":"http://mock.local"}`,
		},
	}
	reg := registry.NewRegistry(fakeProvRepo)
	reg.InjectForTest("prov-cc", bprov)

	runner := New(agentRepo, taskRepo, &mockProjectRepo{}, nil, nil, reg, nil)
	return runner, taskRepo, ag
}

// TestMaxConcurrent_QueuesBeyondLimit verifies that when MaxConcurrent=1 a
// second RunTask queues the task instead of starting it immediately.
func TestMaxConcurrent_QueuesBeyondLimit(t *testing.T) {
	bprov := &blockingProvider{}
	runner, taskRepo, ag := buildRunnerWithBlockingProvider(t, bprov, 1)

	task1 := makeTaskForAgent(ag.ID, "proj-cc")
	task2 := makeTaskForAgent(ag.ID, "proj-cc")
	taskRepo.Create(context.Background(), task1)
	taskRepo.Create(context.Background(), task2)

	// Start task1 — should begin executing (block inside StreamExecute).
	if err := runner.RunTask(context.Background(), task1.ID); err != nil {
		t.Fatalf("RunTask task1: %v", err)
	}
	bprov.waitForNReleases(t, 1) // task1 is inside StreamExecute

	// Start task2 — MaxConcurrent=1, so it must be queued, not running.
	if err := runner.RunTask(context.Background(), task2.ID); err != nil {
		t.Fatalf("RunTask task2: %v", err)
	}

	// Give a moment for any race to manifest.
	time.Sleep(50 * time.Millisecond)

	t2, _ := taskRepo.Get(context.Background(), task2.ID)
	if t2.Status != model.TaskStatusQueued {
		t.Errorf("task2 status = %s, want %s", t2.Status, model.TaskStatusQueued)
	}

	// Verify only one StreamExecute call happened (task1 only).
	bprov.mu.Lock()
	n := len(bprov.releases)
	bprov.mu.Unlock()
	if n != 1 {
		t.Errorf("StreamExecute called %d times, want 1", n)
	}

	// Clean up: release task1 so the runner doesn't hang after the test.
	bprov.releaseNth(t, 0)
	waitForStatus(t, taskRepo, task1.ID, model.TaskStatusCompleted)
	// drainQueue should now start task2.
	waitForStatus(t, taskRepo, task2.ID, model.TaskStatusRunning)
	bprov.releaseNth(t, 1)
	waitForStatus(t, taskRepo, task2.ID, model.TaskStatusCompleted)
}

// TestDrainQueue_StartsNextAfterCompletion verifies that when the first task
// completes, drainQueue automatically starts the queued second task without
// any further RunTask call.
func TestDrainQueue_StartsNextAfterCompletion(t *testing.T) {
	bprov := &blockingProvider{}
	runner, taskRepo, ag := buildRunnerWithBlockingProvider(t, bprov, 1)

	task1 := makeTaskForAgent(ag.ID, "proj-drain")
	task2 := makeTaskForAgent(ag.ID, "proj-drain")
	task1.CreatedAt = time.Now()
	task2.CreatedAt = time.Now().Add(time.Millisecond) // ensure queue order
	taskRepo.Create(context.Background(), task1)
	taskRepo.Create(context.Background(), task2)

	if err := runner.RunTask(context.Background(), task1.ID); err != nil {
		t.Fatalf("RunTask task1: %v", err)
	}
	bprov.waitForNReleases(t, 1)

	if err := runner.RunTask(context.Background(), task2.ID); err != nil {
		t.Fatalf("RunTask task2: %v", err)
	}

	// task2 should be queued.
	time.Sleep(20 * time.Millisecond)
	t2, _ := taskRepo.Get(context.Background(), task2.ID)
	if t2.Status != model.TaskStatusQueued {
		t.Fatalf("pre-condition: task2 status = %s, want queued", t2.Status)
	}

	// Release task1 — drainQueue must pick up task2 automatically.
	bprov.releaseNth(t, 0)
	waitForStatus(t, taskRepo, task1.ID, model.TaskStatusCompleted)

	// task2 should now reach Running and then Completed.
	waitForStatus(t, taskRepo, task2.ID, model.TaskStatusRunning)
	bprov.releaseNth(t, 1)
	waitForStatus(t, taskRepo, task2.ID, model.TaskStatusCompleted)
}

// TestUnlimitedConcurrency_RunsBothImmediately verifies that MaxConcurrent=0
// (unlimited) starts two tasks simultaneously without queuing either.
func TestUnlimitedConcurrency_RunsBothImmediately(t *testing.T) {
	bprov := &blockingProvider{}
	runner, taskRepo, ag := buildRunnerWithBlockingProvider(t, bprov, 0) // 0 = unlimited

	task1 := makeTaskForAgent(ag.ID, "proj-unlimited")
	task2 := makeTaskForAgent(ag.ID, "proj-unlimited")
	taskRepo.Create(context.Background(), task1)
	taskRepo.Create(context.Background(), task2)

	if err := runner.RunTask(context.Background(), task1.ID); err != nil {
		t.Fatalf("RunTask task1: %v", err)
	}
	if err := runner.RunTask(context.Background(), task2.ID); err != nil {
		t.Fatalf("RunTask task2: %v", err)
	}

	// Both should reach Running (StreamExecute) without either being queued.
	bprov.waitForNReleases(t, 2)

	// Clean up.
	bprov.releaseNth(t, 0)
	bprov.releaseNth(t, 1)
	waitForStatus(t, taskRepo, task1.ID, model.TaskStatusCompleted)
	waitForStatus(t, taskRepo, task2.ID, model.TaskStatusCompleted)
}
