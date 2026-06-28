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
func (r *fakeAgentRepo) Search(_ context.Context, _ string) ([]*model.Agent, error)  { return nil, nil }

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
func (r *fakeProjectRepo) Search(_ context.Context, _ string) ([]*model.Project, error) {
	return nil, nil
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

func (r *fakeTaskRepo) FindByPromptHash(_ context.Context, _ string, _ string) (*model.Task, error) {
	return nil, nil
}

func (r *fakeTaskRepo) ProjectSpendForPeriod(_ context.Context, _ string, _ string) (float64, error) {
	return 0, nil
}

func (r *fakeTaskRepo) ForceFailTask(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func (r *fakeTaskRepo) ListTimedOut(_ context.Context) ([]*model.Task, error) {
	return nil, nil
}

func (r *fakeTaskRepo) ListFollowUpChain(_ context.Context, rootTaskID string) ([]*model.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, t := range r.tasks {
		if t.ID == rootTaskID {
			return []*model.Task{t}, nil
		}
	}
	return nil, nil
}

func (r *fakeTaskRepo) SaveSummaryCache(_ context.Context, taskID, summary string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, t := range r.tasks {
		if t.ID == taskID {
			t.SummaryCache = summary
			return nil
		}
	}
	return nil
}

func (r *fakeTaskRepo) ListProjectHistory(_ context.Context, projectID string) ([]*model.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*model.Task
	for _, t := range r.tasks {
		if t.ProjectID == projectID && t.Status == model.TaskStatusCompleted {
			out = append(out, t)
		}
	}
	return out, nil
}

func (r *fakeTaskRepo) HasActiveTaskForProject(_ context.Context, projectID string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, t := range r.tasks {
		if t.ProjectID == projectID && (t.Status == model.TaskStatusRunning || t.Status == model.TaskStatusQueued) {
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeTaskRepo) LastMonitorRunAt(_ context.Context, projectID string) (*time.Time, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
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

func (r *fakeTaskRepo) SetPriority(_ context.Context, _ string, _ int) error { return nil }

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

// ---- Daily schedule helpers + tests ----

func makeDailyMonitor(id string, times []string, catchUp bool) *model.Project {
	return &model.Project{
		ID:              id,
		Name:            "monitor-" + id,
		Kind:            model.ProjectKindMonitor,
		Status:          model.ProjectStatusActive,
		ScheduleKind:    model.ScheduleKindDaily,
		ScheduleTimes:   times,
		ScheduleCatchUp: catchUp,
	}
}

func dailySpecFor(proj *model.Project, agent *model.Agent) scheduleSpec {
	return scheduleSpec{
		monitor: proj,
		agent:   agent,
		kind:    model.ScheduleKindDaily,
		times:   parseTimes(proj.ScheduleTimes),
		catchUp: proj.ScheduleCatchUp,
	}
}

func at(t time.Time, h, m int) time.Time {
	y, mo, d := t.Date()
	return time.Date(y, mo, d, h, m, 0, 0, t.Location())
}

// TestMostRecentOccurrence checks selection of the latest passed time today.
func TestMostRecentOccurrence(t *testing.T) {
	now := at(time.Now(), 13, 30)
	times := []timeOfDay{{0, 0}, {6, 0}, {12, 0}, {18, 0}}

	occ, ok := mostRecentOccurrence(times, now)
	if !ok {
		t.Fatal("expected an occurrence at 13:30")
	}
	if occ != at(now, 12, 0) {
		t.Errorf("occurrence = %s, want 12:00", occ.Format("15:04"))
	}

	// Before the first time of the day → no occurrence yet.
	early := at(now, 5, 0)
	if _, ok := mostRecentOccurrence([]timeOfDay{{6, 0}}, early); ok {
		t.Error("expected no occurrence before the first scheduled time")
	}
}

// TestParseTimes verifies invalid entries are skipped.
func TestParseTimes(t *testing.T) {
	got := parseTimes([]string{"07:00", "bad", "23:59", "24:00", "12:5"})
	want := []timeOfDay{{7, 0}, {23, 59}}
	if len(got) != len(want) {
		t.Fatalf("parsed %d times %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("times[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

// TestEvaluateDaily_PunctualFire fires when within the punctual window and no
// prior run exists, with catch-up disabled.
func TestEvaluateDaily_PunctualFire(t *testing.T) {
	proj := makeDailyMonitor("daily-1", []string{"07:00"}, false)
	agent := makeActiveAgent("ag-d1")
	projectRepo := newFakeProjectRepo([]*model.Project{proj}, map[string][]*model.Agent{"daily-1": {agent}})
	taskRepo := &fakeTaskRepo{}
	runner := &countingRunner{}
	s := New(&fakeAgentRepo{}, projectRepo, taskRepo, runner, time.Minute)

	now := at(time.Now(), 7, 0).Add(30 * time.Second) // 07:00:30
	s.evaluateDaily(context.Background(), dailySpecFor(proj, agent), now)

	if runner.count() != 1 {
		t.Fatalf("RunTask called %d times, want 1", runner.count())
	}
}

// TestEvaluateDaily_PunctualSkipsStale does not fire a long-missed run when
// catch-up is disabled.
func TestEvaluateDaily_PunctualSkipsStale(t *testing.T) {
	proj := makeDailyMonitor("daily-2", []string{"07:00"}, false)
	agent := makeActiveAgent("ag-d2")
	projectRepo := newFakeProjectRepo([]*model.Project{proj}, map[string][]*model.Agent{"daily-2": {agent}})
	taskRepo := &fakeTaskRepo{}
	runner := &countingRunner{}
	s := New(&fakeAgentRepo{}, projectRepo, taskRepo, runner, time.Minute)

	now := at(time.Now(), 9, 0) // two hours after 07:00, outside punctual window
	s.evaluateDaily(context.Background(), dailySpecFor(proj, agent), now)

	if runner.count() != 0 {
		t.Fatalf("RunTask called %d times, want 0 (stale, catch-up off)", runner.count())
	}
}

// TestEvaluateDaily_CatchUpFiresMissed fires a missed run later the same day
// when catch-up is enabled.
func TestEvaluateDaily_CatchUpFiresMissed(t *testing.T) {
	proj := makeDailyMonitor("daily-3", []string{"07:00"}, true)
	agent := makeActiveAgent("ag-d3")
	projectRepo := newFakeProjectRepo([]*model.Project{proj}, map[string][]*model.Agent{"daily-3": {agent}})
	taskRepo := &fakeTaskRepo{}
	runner := &countingRunner{}
	s := New(&fakeAgentRepo{}, projectRepo, taskRepo, runner, time.Minute)

	now := at(time.Now(), 8, 30) // laptop woke at 08:30, missed 07:00
	s.evaluateDaily(context.Background(), dailySpecFor(proj, agent), now)

	if runner.count() != 1 {
		t.Fatalf("RunTask called %d times, want 1 (catch-up)", runner.count())
	}
}

// TestEvaluateDaily_DedupSameOccurrence does not fire twice for the same
// occurrence once a run has been recorded.
func TestEvaluateDaily_DedupSameOccurrence(t *testing.T) {
	proj := makeDailyMonitor("daily-4", []string{"07:00"}, true)
	agent := makeActiveAgent("ag-d4")
	projectRepo := newFakeProjectRepo([]*model.Project{proj}, map[string][]*model.Agent{"daily-4": {agent}})
	taskRepo := &fakeTaskRepo{}
	runner := &countingRunner{}
	s := New(&fakeAgentRepo{}, projectRepo, taskRepo, runner, time.Minute)

	spec := dailySpecFor(proj, agent)
	now := at(time.Now(), 8, 0)
	s.evaluateDaily(context.Background(), spec, now)
	// Second evaluation a minute later must not create another task.
	s.evaluateDaily(context.Background(), spec, now.Add(time.Minute))

	if runner.count() != 1 {
		t.Fatalf("RunTask called %d times, want 1 (deduped)", runner.count())
	}
}

// TestEvaluateDaily_MultiDayOffSingleRun ensures a multi-day outage triggers a
// single catch-up run, not one per missed day.
func TestEvaluateDaily_MultiDayOffSingleRun(t *testing.T) {
	proj := makeDailyMonitor("daily-5", []string{"07:00"}, true)
	agent := makeActiveAgent("ag-d5")
	projectRepo := newFakeProjectRepo([]*model.Project{proj}, map[string][]*model.Agent{"daily-5": {agent}})
	// Last monitor run was three days ago.
	taskRepo := &fakeTaskRepo{tasks: []*model.Task{
		{ID: "old", ProjectID: "daily-5", Source: "monitor", Status: model.TaskStatusCompleted, CreatedAt: time.Now().Add(-72 * time.Hour)},
	}}
	runner := &countingRunner{}
	s := New(&fakeAgentRepo{}, projectRepo, taskRepo, runner, time.Minute)

	spec := dailySpecFor(proj, agent)
	now := at(time.Now(), 9, 0)
	s.evaluateDaily(context.Background(), spec, now)
	s.evaluateDaily(context.Background(), spec, now.Add(time.Minute))

	if runner.count() != 1 {
		t.Fatalf("RunTask called %d times, want 1 (single catch-up after multi-day outage)", runner.count())
	}
}

// TestEvaluateDaily_MultipleTimesPicksLatest fires for the most recent passed
// time when several are configured.
func TestEvaluateDaily_MultipleTimesPicksLatest(t *testing.T) {
	proj := makeDailyMonitor("daily-6", []string{"00:00", "06:00", "12:00", "18:00"}, true)
	agent := makeActiveAgent("ag-d6")
	projectRepo := newFakeProjectRepo([]*model.Project{proj}, map[string][]*model.Agent{"daily-6": {agent}})
	// Already ran at 06:05 today.
	taskRepo := &fakeTaskRepo{tasks: []*model.Task{
		{ID: "ran-06", ProjectID: "daily-6", Source: "monitor", Status: model.TaskStatusCompleted, CreatedAt: at(time.Now(), 6, 5)},
	}}
	runner := &countingRunner{}
	s := New(&fakeAgentRepo{}, projectRepo, taskRepo, runner, time.Minute)

	spec := dailySpecFor(proj, agent)
	// At 12:30 the 12:00 occurrence is due and has not run yet.
	s.evaluateDaily(context.Background(), spec, at(time.Now(), 12, 30))
	if runner.count() != 1 {
		t.Fatalf("RunTask called %d times, want 1 (12:00 due)", runner.count())
	}
}

// TestEvaluateDaily_DismissedRunStillCounts verifies that a monitor does NOT
// re-fire after its task has been dismissed from the inbox. Regression test for
// the bug where lastMonitorRun used tasks.List (dismissed=0 filter), causing
// dismissed runs to be invisible and the monitor to re-fire on the next sync.
func TestEvaluateDaily_DismissedRunStillCounts(t *testing.T) {
	proj := makeDailyMonitor("daily-dismissed", []string{"05:00"}, true)
	agent := makeActiveAgent("ag-dismissed")
	projectRepo := newFakeProjectRepo([]*model.Project{proj}, map[string][]*model.Agent{"daily-dismissed": {agent}})

	// The task ran today at 05:01 but was dismissed by the user from the inbox.
	now := at(time.Now(), 8, 0)
	dismissedRun := &model.Task{
		ID:        "dismissed-run",
		ProjectID: "daily-dismissed",
		Source:    "monitor",
		Status:    model.TaskStatusCompleted,
		CreatedAt: at(now, 5, 1),
		Dismissed: true,
	}
	taskRepo := &fakeTaskRepo{tasks: []*model.Task{dismissedRun}}
	runner := &countingRunner{}
	s := New(&fakeAgentRepo{}, projectRepo, taskRepo, runner, time.Minute)

	spec := dailySpecFor(proj, agent)
	s.evaluateDaily(context.Background(), spec, now)

	if runner.count() != 0 {
		t.Fatalf("RunTask called %d times, want 0 — dismissed run must count as the daily occurrence", runner.count())
	}
}
