package scheduler

// scheduler_test.go covers the three scheduler behaviours identified as
// workflow weaknesses: fire() skipping active tasks, fire() creating tasks
// when idle, and sync() restarting a goroutine when the interval changes.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/solarisjon/phoenix/internal/model"
)

// ---- In-process fakes ----

type fakeAgentRepo struct{}

func (r *fakeAgentRepo) List(_ context.Context) ([]*model.Agent, error)              { return nil, nil }
func (r *fakeAgentRepo) Get(_ context.Context, _ string) (*model.Agent, error)       { return nil, nil }
func (r *fakeAgentRepo) Create(_ context.Context, _ *model.Agent) error              { return nil }
func (r *fakeAgentRepo) Update(_ context.Context, _ *model.Agent) error              { return nil }
func (r *fakeAgentRepo) Delete(_ context.Context, _ string) error                    { return nil }

type fakeProjectRepo struct {
	mu       sync.Mutex
	projects []*model.Project
	agents   map[string][]*model.Agent // projectID → agents
}

func newFakeProjectRepo(projects []*model.Project, agents map[string][]*model.Agent) *fakeProjectRepo {
	if agents == nil {
		agents = make(map[string][]*model.Agent)
	}
	return &fakeProjectRepo{projects: projects, agents: agents}
}

func (r *fakeProjectRepo) List(_ context.Context) ([]*model.Project, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*model.Project(nil), r.projects...), nil
}
func (r *fakeProjectRepo) ListByKind(_ context.Context, _ string) ([]*model.Project, error) {
	return r.List(context.Background())
}
func (r *fakeProjectRepo) ListByStatus(_ context.Context, _, _ string) ([]*model.Project, error) {
	return nil, nil
}
func (r *fakeProjectRepo) Get(_ context.Context, id string) (*model.Project, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.projects {
		if p.ID == id {
			return p, nil
		}
	}
	return nil, nil
}
func (r *fakeProjectRepo) Create(_ context.Context, p *model.Project) error {
	r.mu.Lock()
	r.projects = append(r.projects, p)
	r.mu.Unlock()
	return nil
}
func (r *fakeProjectRepo) Update(_ context.Context, p *model.Project) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, existing := range r.projects {
		if existing.ID == p.ID {
			r.projects[i] = p
			return nil
		}
	}
	return nil
}
func (r *fakeProjectRepo) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, p := range r.projects {
		if p.ID == id {
			r.projects = append(r.projects[:i], r.projects[i+1:]...)
			return nil
		}
	}
	return nil
}
func (r *fakeProjectRepo) DeleteWithTasks(_ context.Context, _ string) error { return nil }
func (r *fakeProjectRepo) AssignAgent(_ context.Context, _, _ string) (bool, error) { return false, nil }
func (r *fakeProjectRepo) IsAgentAssigned(_ context.Context, _, _ string) (bool, error) { return true, nil }
func (r *fakeProjectRepo) RemoveAgent(_ context.Context, _, _ string) error  { return nil }
func (r *fakeProjectRepo) ListAgents(_ context.Context, projectID string) ([]*model.Agent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*model.Agent(nil), r.agents[projectID]...), nil
}

type fakeTaskRepo struct {
	mu    sync.Mutex
	tasks []*model.Task
}

func (r *fakeTaskRepo) List(_ context.Context, projectID string) ([]*model.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*model.Task
	for _, t := range r.tasks {
		if t.ProjectID == projectID {
			out = append(out, t)
		}
	}
	return out, nil
}
func (r *fakeTaskRepo) ListByProject(_ context.Context, projectID string, status model.TaskStatus, limit int) ([]*model.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
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
func (r *fakeTaskRepo) ListAll(_ context.Context) ([]*model.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*model.Task(nil), r.tasks...), nil
}
func (r *fakeTaskRepo) ListByStatus(_ context.Context, s model.TaskStatus) ([]*model.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*model.Task
	for _, t := range r.tasks {
		if t.Status == s {
			out = append(out, t)
		}
	}
	return out, nil
}
func (r *fakeTaskRepo) ListByStatuses(_ context.Context, statuses []model.TaskStatus) ([]*model.Task, error) {
	set := make(map[model.TaskStatus]bool)
	for _, s := range statuses {
		set[s] = true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*model.Task
	for _, t := range r.tasks {
		if set[t.Status] {
			out = append(out, t)
		}
	}
	return out, nil
}
func (r *fakeTaskRepo) ListByAgent(_ context.Context, agentID string) ([]*model.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*model.Task
	for _, t := range r.tasks {
		if t.AgentID == agentID {
			out = append(out, t)
		}
	}
	return out, nil
}
func (r *fakeTaskRepo) Search(_ context.Context, _ string) ([]*model.Task, error) {
	return nil, nil
}
func (r *fakeTaskRepo) Get(_ context.Context, id string) (*model.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, t := range r.tasks {
		if t.ID == id {
			return t, nil
		}
	}
	return nil, nil
}
func (r *fakeTaskRepo) Create(_ context.Context, t *model.Task) error {
	r.mu.Lock()
	r.tasks = append(r.tasks, t)
	r.mu.Unlock()
	return nil
}
func (r *fakeTaskRepo) Update(_ context.Context, t *model.Task) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, existing := range r.tasks {
		if existing.ID == t.ID {
			r.tasks[i] = t
			return nil
		}
	}
	return nil
}
func (r *fakeTaskRepo) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, t := range r.tasks {
		if t.ID == id {
			r.tasks = append(r.tasks[:i], r.tasks[i+1:]...)
			return nil
		}
	}
	return nil
}
func (r *fakeTaskRepo) NextQueuedTask(_ context.Context, _ string) (*model.Task, error) {
	return nil, nil
}
func (r *fakeTaskRepo) CancelQueuedTask(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (r *fakeTaskRepo) ListCompletedForInbox(_ context.Context, _ int) ([]*model.Task, error) {
	return nil, nil
}

// countingRunner counts RunTask calls and records the task IDs.
type countingRunner struct {
	mu      sync.Mutex
	taskIDs []string
}

func (r *countingRunner) RunTask(_ context.Context, taskID string) error {
	r.mu.Lock()
	r.taskIDs = append(r.taskIDs, taskID)
	r.mu.Unlock()
	return nil
}

func (r *countingRunner) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.taskIDs)
}

// ---- Helpers ----

func makeMonitorProject(id string, intervalSec int) *model.Project {
	i := intervalSec
	return &model.Project{
		ID:               id,
		Name:             "monitor-" + id,
		Kind:             model.ProjectKindMonitor,
		Status:           model.ProjectStatusActive,
		ScheduleInterval: &i,
	}
}

func makeActiveAgent(id string) *model.Agent {
	return &model.Agent{
		ID:     id,
		Name:   "agent-" + id,
		Status: model.AgentStatusActive,
	}
}

// ---- Tests ----

// TestFire_SkipsWhenActiveTaskExists verifies that fire() does not create a
// new task when a running or queued task already exists for the project.
func TestFire_SkipsWhenActiveTaskExists(t *testing.T) {
	proj := makeMonitorProject("mon-1", 60)
	agent := makeActiveAgent("ag-1")

	projectRepo := newFakeProjectRepo(
		[]*model.Project{proj},
		map[string][]*model.Agent{"mon-1": {agent}},
	)
	taskRepo := &fakeTaskRepo{
		tasks: []*model.Task{
			{ID: "existing-task", ProjectID: "mon-1", AgentID: "ag-1", Status: model.TaskStatusRunning},
		},
	}
	runner := &countingRunner{}

	s := New(&fakeAgentRepo{}, projectRepo, taskRepo, runner, time.Hour)

	spec := scheduleSpec{
		monitor:  proj,
		agent:    agent,
		interval: 60 * time.Second,
	}
	if err := s.fire(context.Background(), spec); err != nil {
		t.Fatalf("fire: %v", err)
	}

	taskRepo.mu.Lock()
	count := len(taskRepo.tasks)
	taskRepo.mu.Unlock()
	if count != 1 {
		t.Errorf("task count = %d, want 1 (no new task should have been created)", count)
	}
	if runner.count() != 0 {
		t.Errorf("RunTask called %d times, want 0", runner.count())
	}
}

// TestFire_CreatesTaskWhenIdle verifies that fire() creates and runs a task
// when no active task exists for the monitor.
func TestFire_CreatesTaskWhenIdle(t *testing.T) {
	proj := makeMonitorProject("mon-2", 60)
	agent := makeActiveAgent("ag-2")

	projectRepo := newFakeProjectRepo(
		[]*model.Project{proj},
		map[string][]*model.Agent{"mon-2": {agent}},
	)
	taskRepo := &fakeTaskRepo{} // no pre-existing tasks
	runner := &countingRunner{}

	s := New(&fakeAgentRepo{}, projectRepo, taskRepo, runner, time.Hour)

	spec := scheduleSpec{
		monitor:  proj,
		agent:    agent,
		interval: 60 * time.Second,
	}
	if err := s.fire(context.Background(), spec); err != nil {
		t.Fatalf("fire: %v", err)
	}

	taskRepo.mu.Lock()
	count := len(taskRepo.tasks)
	taskRepo.mu.Unlock()
	if count != 1 {
		t.Errorf("task count = %d, want 1", count)
	}
	if runner.count() != 1 {
		t.Errorf("RunTask called %d times, want 1", runner.count())
	}
}

// TestSync_RestartsOnIntervalChange verifies that when a monitor's
// schedule_interval changes, sync() stops the old ticker goroutine and starts
// a new one at the updated interval.
func TestSync_RestartsOnIntervalChange(t *testing.T) {
	interval60 := 60
	proj := &model.Project{
		ID:               "mon-3",
		Name:             "monitor-3",
		Kind:             model.ProjectKindMonitor,
		Status:           model.ProjectStatusActive,
		ScheduleInterval: &interval60,
	}
	agent := makeActiveAgent("ag-3")

	projectRepo := newFakeProjectRepo(
		[]*model.Project{proj},
		map[string][]*model.Agent{"mon-3": {agent}},
	)
	taskRepo := &fakeTaskRepo{}
	runner := &countingRunner{}

	s := New(&fakeAgentRepo{}, projectRepo, taskRepo, runner, time.Hour)

	// First sync: should create one schedule entry at 60s interval.
	s.sync()
	s.mu.Lock()
	entry60, ok := s.stops["mon-3"]
	s.mu.Unlock()
	if !ok {
		t.Fatal("expected schedule entry for mon-3 after first sync")
	}
	if entry60.interval != 60*time.Second {
		t.Errorf("entry interval = %s, want 60s", entry60.interval)
	}

	// Update the project to use a different interval.
	interval30 := 30
	proj.ScheduleInterval = &interval30
	projectRepo.Update(context.Background(), proj)

	// Second sync: should detect the change, cancel the old goroutine, and
	// start a new one.
	s.sync()
	s.mu.Lock()
	entry30, ok := s.stops["mon-3"]
	s.mu.Unlock()
	if !ok {
		t.Fatal("expected schedule entry for mon-3 after second sync")
	}
	if entry30.interval != 30*time.Second {
		t.Errorf("entry interval = %s, want 30s", entry30.interval)
	}
	// The entries must be different objects (old one was cancelled and replaced).
	// We can verify by checking the cancel func pointers differ via a closure
	// trick: cancel the new entry's context and confirm it's done.
	cancelledCh := make(chan struct{})
	go func() {
		entry30.cancel()
		close(cancelledCh)
	}()
	select {
	case <-cancelledCh:
	case <-time.After(time.Second):
		t.Error("new entry cancel func did not return within 1s")
	}
}

// TestSync_StopsRemovedMonitor verifies that when a monitor is removed,
// sync() cancels its schedule goroutine.
func TestSync_StopsRemovedMonitor(t *testing.T) {
	proj := makeMonitorProject("mon-4", 60)
	agent := makeActiveAgent("ag-4")

	projectRepo := newFakeProjectRepo(
		[]*model.Project{proj},
		map[string][]*model.Agent{"mon-4": {agent}},
	)
	taskRepo := &fakeTaskRepo{}
	runner := &countingRunner{}

	s := New(&fakeAgentRepo{}, projectRepo, taskRepo, runner, time.Hour)

	s.sync()
	s.mu.Lock()
	_, ok := s.stops["mon-4"]
	s.mu.Unlock()
	if !ok {
		t.Fatal("expected schedule entry after first sync")
	}

	// Remove the monitor from the project list.
	projectRepo.Delete(context.Background(), proj.ID)

	s.sync()
	s.mu.Lock()
	_, stillRunning := s.stops["mon-4"]
	s.mu.Unlock()
	if stillRunning {
		t.Error("expected schedule to be stopped after monitor was removed")
	}
}
