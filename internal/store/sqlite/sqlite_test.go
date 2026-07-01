package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/solarisjon/phoenix/internal/model"
)

// testDB opens a fresh in-memory SQLite database for testing.
func testDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	ctx := context.Background()
	if err := db.Seed(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

const defaultUserID = "00000000-0000-0000-0000-000000000001"

// ---- User ----

func TestUserGetDefault(t *testing.T) {
	db := testDB(t)
	repo := NewUserRepo(db)
	u, err := repo.GetDefault(context.Background())
	if err != nil {
		t.Fatalf("GetDefault: %v", err)
	}
	if u == nil {
		t.Fatal("expected default user, got nil")
	}
	if u.ID != defaultUserID {
		t.Errorf("user id = %q, want %q", u.ID, defaultUserID)
	}
}

// ---- Provider ----

func seedProvider(t *testing.T, db *DB) *model.Provider {
	t.Helper()
	p := &model.Provider{
		ID:        "prov-1",
		Name:      "LLM Proxy",
		Type:      model.ProviderTypeLLM,
		Config:    `{"endpoint":"http://llm.local"}`,
		CreatedBy: defaultUserID,
	}
	if err := NewProviderRepo(db).Create(context.Background(), p); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	return p
}

func TestProviderCRUD(t *testing.T) {
	db := testDB(t)
	repo := NewProviderRepo(db)
	ctx := context.Background()

	p := seedProvider(t, db)

	// List
	list, err := repo.List(ctx, "")
	if err != nil || len(list) != 1 {
		t.Fatalf("List: err=%v len=%d", err, len(list))
	}

	// Get
	got, err := repo.Get(ctx, p.ID)
	if err != nil || got == nil {
		t.Fatalf("Get: err=%v got=%v", err, got)
	}
	if got.Name != p.Name {
		t.Errorf("Name = %q, want %q", got.Name, p.Name)
	}

	// Update
	got.Name = "Updated LLM"
	if err := repo.Update(ctx, got); err != nil {
		t.Fatalf("Update: %v", err)
	}
	updated, _ := repo.Get(ctx, p.ID)
	if updated.Name != "Updated LLM" {
		t.Error("Update did not persist")
	}

	// Delete
	if err := repo.Delete(ctx, p.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	list, _ = repo.List(ctx, "")
	if len(list) != 0 {
		t.Error("expected empty list after delete")
	}
}

// ---- Agent ----

func seedAgent(t *testing.T, db *DB) *model.Agent {
	t.Helper()
	seedProvider(t, db)
	a := &model.Agent{
		ID:           "agent-1",
		Name:         "Ops Manager",
		Persona:      "Senior operations expert",
		Instructions: "Always delegate research.",
		Guardrails:   "Never approve without user review.",
		ProviderID:   "prov-1",
		CreatedBy:    defaultUserID,
		Status:       model.AgentStatusActive,
	}
	if err := NewAgentRepo(db).Create(context.Background(), a); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	return a
}

func TestAgentCRUD(t *testing.T) {
	db := testDB(t)
	repo := NewAgentRepo(db)
	ctx := context.Background()

	a := seedAgent(t, db)

	list, err := repo.List(ctx, "")
	if err != nil || len(list) != 1 {
		t.Fatalf("List: err=%v len=%d", err, len(list))
	}

	got, err := repo.Get(ctx, a.ID)
	if err != nil || got == nil {
		t.Fatalf("Get: err=%v got=%v", err, got)
	}

	got.Name = "Updated Agent"
	if err := repo.Update(ctx, got); err != nil {
		t.Fatalf("Update: %v", err)
	}
	updated, _ := repo.Get(ctx, a.ID)
	if updated.Name != "Updated Agent" {
		t.Error("name not updated")
	}

	if err := repo.Delete(ctx, a.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

// ---- Project ----

func seedProject(t *testing.T, db *DB) *model.Project {
	t.Helper()
	p := &model.Project{
		ID:          "proj-1",
		Name:        "Build OKRs",
		Description: "Org-wide OKR generation",
		Owner:       defaultUserID,
		Status:      model.ProjectStatusActive,
	}
	if err := NewProjectRepo(db).Create(context.Background(), p); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	return p
}

func TestProjectCRUD(t *testing.T) {
	db := testDB(t)
	repo := NewProjectRepo(db)
	ctx := context.Background()

	proj := seedProject(t, db)

	list, err := repo.List(ctx, "")
	if err != nil || len(list) != 1 {
		t.Fatalf("List: err=%v len=%d", err, len(list))
	}

	got, err := repo.Get(ctx, proj.ID)
	if err != nil || got == nil {
		t.Fatalf("Get: err=%v", err)
	}

	got.Name = "Updated Project"
	if err := repo.Update(ctx, got); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if err := repo.Delete(ctx, proj.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func TestProjectAgentAssignment(t *testing.T) {
	db := testDB(t)
	repo := NewProjectRepo(db)
	ctx := context.Background()

	seedProject(t, db)
	seedAgent(t, db)

	if added, err := repo.AssignAgent(ctx, "proj-1", "agent-1"); err != nil {
		t.Fatalf("AssignAgent: %v", err)
	} else if !added {
		t.Error("expected added=true for first assign")
	}

	agents, err := repo.ListAgents(ctx, "proj-1")
	if err != nil || len(agents) != 1 {
		t.Fatalf("ListAgents: err=%v len=%d", err, len(agents))
	}

	// Idempotent re-assign — should return added=false, no error.
	if added, err := repo.AssignAgent(ctx, "proj-1", "agent-1"); err != nil {
		t.Fatalf("re-assign: %v", err)
	} else if added {
		t.Error("expected added=false for duplicate assign")
	}
	agents, _ = repo.ListAgents(ctx, "proj-1")
	if len(agents) != 1 {
		t.Error("expected 1 agent after idempotent assign")
	}

	if err := repo.RemoveAgent(ctx, "proj-1", "agent-1"); err != nil {
		t.Fatalf("RemoveAgent: %v", err)
	}
	agents, _ = repo.ListAgents(ctx, "proj-1")
	if len(agents) != 0 {
		t.Error("expected 0 agents after remove")
	}
}

// ---- Task ----

func TestTaskCRUD(t *testing.T) {
	db := testDB(t)
	repo := NewTaskRepo(db)
	ctx := context.Background()

	seedAgent(t, db)
	seedProject(t, db)

	task := &model.Task{
		ID:          "task-1",
		ProjectID:   "proj-1",
		AgentID:     "agent-1",
		Title:       "Research OKRs",
		Description: "Research best practices",
		Status:      model.TaskStatusPending,
		Input:       `{"query":"OKR best practices"}`,
		Output:      `{}`,
	}
	if err := repo.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	list, err := repo.List(ctx, "proj-1")
	if err != nil || len(list) != 1 {
		t.Fatalf("List: err=%v len=%d", err, len(list))
	}

	got, err := repo.Get(ctx, task.ID)
	if err != nil || got == nil {
		t.Fatalf("Get: err=%v", err)
	}
	if got.ParentTaskID != nil {
		t.Error("expected nil parent task id")
	}

	now := time.Now()
	got.Status = model.TaskStatusRunning
	got.StartedAt = &now
	if err := repo.Update(ctx, got); err != nil {
		t.Fatalf("Update: %v", err)
	}

	byStatus, err := repo.ListByStatus(ctx, model.TaskStatusRunning)
	if err != nil || len(byStatus) != 1 {
		t.Fatalf("ListByStatus: err=%v len=%d", err, len(byStatus))
	}

	if err := repo.Delete(ctx, task.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func TestTaskDependsOnRoundTrip(t *testing.T) {
	db := testDB(t)
	repo := NewTaskRepo(db)
	ctx := context.Background()

	seedAgent(t, db)
	seedProject(t, db)

	task := &model.Task{
		ID:        "task-deps",
		ProjectID: "proj-1",
		AgentID:   "agent-1",
		Title:     "Depends on others",
		Status:    model.TaskStatusQueued,
		Input:     `{}`,
		Output:    `{}`,
		DependsOn: []string{"task-a", "task-b"},
	}
	if err := repo.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.Get(ctx, task.ID)
	if err != nil || got == nil {
		t.Fatalf("Get: err=%v", err)
	}
	if len(got.DependsOn) != 2 || got.DependsOn[0] != "task-a" || got.DependsOn[1] != "task-b" {
		t.Errorf("DependsOn = %v, want [task-a task-b]", got.DependsOn)
	}
}

func TestDependenciesSatisfied(t *testing.T) {
	db := testDB(t)
	repo := NewTaskRepo(db)
	ctx := context.Background()

	seedAgent(t, db)
	seedProject(t, db)

	mk := func(id string, status model.TaskStatus) {
		if err := repo.Create(ctx, &model.Task{
			ID: id, ProjectID: "proj-1", AgentID: "agent-1",
			Title: id, Status: status, Input: `{}`, Output: `{}`,
		}); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	mk("dep-done", model.TaskStatusCompleted)
	mk("dep-pending", model.TaskStatusRunning)

	if ok, err := repo.DependenciesSatisfied(ctx, []string{"dep-done"}); err != nil || !ok {
		t.Errorf("single completed dep: ok=%v err=%v, want true", ok, err)
	}
	if ok, err := repo.DependenciesSatisfied(ctx, []string{"dep-done", "dep-pending"}); err != nil || ok {
		t.Errorf("one pending dep: ok=%v err=%v, want false", ok, err)
	}
	if ok, err := repo.DependenciesSatisfied(ctx, []string{"does-not-exist"}); err != nil || ok {
		t.Errorf("missing dep: ok=%v err=%v, want false, no error", ok, err)
	}
}

func TestUnlockDependents(t *testing.T) {
	db := testDB(t)
	repo := NewTaskRepo(db)
	ctx := context.Background()

	seedAgent(t, db)
	seedProject(t, db)

	mk := func(id string, status model.TaskStatus, deps []string) {
		if err := repo.Create(ctx, &model.Task{
			ID: id, ProjectID: "proj-1", AgentID: "agent-1",
			Title: id, Status: status, Input: `{}`, Output: `{}`, DependsOn: deps,
		}); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}

	// Linear chain: b depends on a.
	mk("task-a", model.TaskStatusRunning, nil)
	mk("task-b", model.TaskStatusQueued, []string{"task-a"})
	// Diamond: d depends on both b and c.
	mk("task-c", model.TaskStatusRunning, nil)
	mk("task-d", model.TaskStatusQueued, []string{"task-b", "task-c"})

	// Completing task-a should unlock task-b but not task-d (still waiting on task-c).
	setCompleted(t, ctx, repo, "task-a")
	agentIDs, err := repo.UnlockDependents(ctx, "task-a")
	if err != nil {
		t.Fatalf("UnlockDependents(task-a): %v", err)
	}
	if len(agentIDs) != 1 || agentIDs[0] != "agent-1" {
		t.Errorf("agentIDs = %v, want [agent-1]", agentIDs)
	}
	b, _ := repo.Get(ctx, "task-b")
	if len(b.DependsOn) != 0 {
		t.Errorf("task-b DependsOn = %v, want cleared", b.DependsOn)
	}
	d, _ := repo.Get(ctx, "task-d")
	if len(d.DependsOn) != 2 {
		t.Errorf("task-d DependsOn = %v, want still [task-b task-c] (unmet)", d.DependsOn)
	}

	// task-d still depends on task-b (not yet completed, just unblocked-to-run) and
	// task-c. Completing task-c alone must not unlock task-d.
	setCompleted(t, ctx, repo, "task-c")
	agentIDs, err = repo.UnlockDependents(ctx, "task-c")
	if err != nil {
		t.Fatalf("UnlockDependents(task-c): %v", err)
	}
	if len(agentIDs) != 0 {
		t.Errorf("agentIDs = %v, want none (task-b not completed yet)", agentIDs)
	}

	// Now complete task-b too; task-d's remaining deps are all satisfied.
	setCompleted(t, ctx, repo, "task-b")
	agentIDs, err = repo.UnlockDependents(ctx, "task-b")
	if err != nil {
		t.Fatalf("UnlockDependents(task-b): %v", err)
	}
	if len(agentIDs) != 1 || agentIDs[0] != "agent-1" {
		t.Errorf("agentIDs = %v, want [agent-1]", agentIDs)
	}
	d, _ = repo.Get(ctx, "task-d")
	if len(d.DependsOn) != 0 {
		t.Errorf("task-d DependsOn = %v, want cleared", d.DependsOn)
	}
}

func setCompleted(t *testing.T, ctx context.Context, repo *TaskRepo, id string) {
	t.Helper()
	task, err := repo.Get(ctx, id)
	if err != nil || task == nil {
		t.Fatalf("get %s: %v", id, err)
	}
	task.Status = model.TaskStatusCompleted
	if err := repo.Update(ctx, task); err != nil {
		t.Fatalf("update %s: %v", id, err)
	}
}

// ---- Stats ----

func TestStats(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	stats := NewStatsRepo(db)

	seedAgent(t, db)
	seedProject(t, db)

	task := &model.Task{
		ID: "task-cost", ProjectID: "proj-1", AgentID: "agent-1",
		Title: "Cost task", Status: model.TaskStatusCompleted,
		Input: "{}", Output: "{}", CostUSD: 0.042,
	}
	NewTaskRepo(db).Create(ctx, task)

	usage, err := stats.TotalUsage(ctx)
	if err != nil || usage.CostUSD != 0.042 {
		t.Errorf("TotalUsage: err=%v cost=%v", err, usage.CostUSD)
	}

	byAgent, err := stats.CostByAgent(ctx)
	if err != nil || len(byAgent) != 1 {
		t.Fatalf("CostByAgent: err=%v len=%d", err, len(byAgent))
	}
	if byAgent[0].Total != 0.042 {
		t.Errorf("CostByAgent total = %v, want 0.042", byAgent[0].Total)
	}

	byProj, err := stats.CostByProject(ctx)
	if err != nil || len(byProj) != 1 {
		t.Fatalf("CostByProject: err=%v len=%d", err, len(byProj))
	}
}
