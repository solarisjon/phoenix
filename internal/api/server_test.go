package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/solarisjon/phoenix/internal/agent"
	"github.com/solarisjon/phoenix/internal/config"
	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/plugin"
	"github.com/solarisjon/phoenix/internal/pricing"
	"github.com/solarisjon/phoenix/internal/provider"
	"github.com/solarisjon/phoenix/internal/provider/registry"
	"github.com/solarisjon/phoenix/internal/store"
	sqllite "github.com/solarisjon/phoenix/internal/store/sqlite"
)

// ---- in-memory test DB ----

func testServer(t *testing.T) *Server {
	t.Helper()
	db, err := sqllite.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Seed(context.Background()); err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	provRepo := sqllite.NewProviderRepo(db)
	agentRepo := sqllite.NewAgentRepo(db)
	projRepo := sqllite.NewProjectRepo(db)
	taskRepo := sqllite.NewTaskRepo(db)
	statsRepo := sqllite.NewStatsRepo(db)
	userRepo := sqllite.NewUserRepo(db)
	sessionRepo := sqllite.NewSessionRepo(db)
	teamRepo := sqllite.NewTeamRepo(db)

	reg := registry.NewRegistry(provRepo)
	reg.InjectForTest("prov-test", &mockProv{})

	memoRepo := sqllite.NewMemoRepo(db)
	runner := agent.New(agentRepo, taskRepo, projRepo, nil, memoRepo, reg, nil)
	t.Cleanup(runner.Shutdown)

	agentDraftRepo := sqllite.NewAgentDraftRepo(db)
	systemSettingsRepo := sqllite.NewSystemSettingsRepo(db)
	adminRepo := sqllite.NewAdminRepo(db)
	pluginRepo := sqllite.NewPluginRepo(db)
	ruleRepo := sqllite.NewNotificationRuleRepo(db)
	pm := plugin.NewManager(pluginRepo, ruleRepo, systemSettingsRepo, plugin.ManagerOpts{NoPlugins: true})
	obsidianVaultRepo := sqllite.NewObsidianVaultRepo(db)
	taskTemplateRepo := sqllite.NewTaskTemplateRepo(db)
	return New(provRepo, agentRepo, projRepo, taskRepo, statsRepo, userRepo, sessionRepo, teamRepo, agentDraftRepo, systemSettingsRepo, memoRepo, pluginRepo, ruleRepo, obsidianVaultRepo, taskTemplateRepo, pm, runner, reg, pricing.New(), adminRepo, 0, config.Config{})
}

type mockProv struct{}

func (m *mockProv) Execute(_ context.Context, _ provider.TaskRequest) (provider.TaskResponse, error) {
	return provider.TaskResponse{Output: "mock output", CostUSD: 0.001}, nil
}
func (m *mockProv) StreamExecute(_ context.Context, _ provider.TaskRequest) (<-chan provider.StreamChunk, error) {
	ch := make(chan provider.StreamChunk, 2)
	ch <- provider.StreamChunk{Content: "mock output"}
	ch <- provider.StreamChunk{Done: true}
	close(ch)
	return ch, nil
}
func (m *mockProv) EstimateCost(_ provider.TaskRequest) provider.CostEstimate {
	return provider.CostEstimate{}
}

func postJSON(t *testing.T, srv *Server, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

func getJSON(t *testing.T, srv *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

func deleteReq(t *testing.T, srv *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, path, nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

// ---- seed helpers ----

func seedProvider(t *testing.T, srv *Server) string {
	t.Helper()
	w := postJSON(t, srv, "/api/providers", map[string]string{
		"name": "Test LLM", "type": "llm",
		"config": `{"endpoint":"http://mock.local","model":"test"}`,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create provider: %d %s", w.Code, w.Body.String())
	}
	var p model.Provider
	json.NewDecoder(w.Body).Decode(&p)
	return p.ID
}

func seedAgent(t *testing.T, srv *Server, provID string) string {
	t.Helper()
	w := postJSON(t, srv, "/api/agents", map[string]interface{}{
		"name": "Test Agent", "persona": "Expert",
		"instructions": "Do the task.", "guardrails": "Be safe.",
		"provider_id": provID,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create agent: %d %s", w.Code, w.Body.String())
	}
	var a model.Agent
	json.NewDecoder(w.Body).Decode(&a)
	return a.ID
}

func seedProject(t *testing.T, srv *Server) string {
	t.Helper()
	w := postJSON(t, srv, "/api/projects", map[string]string{
		"name": "Test Project", "description": "A project",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create project: %d %s", w.Code, w.Body.String())
	}
	var p model.Project
	json.NewDecoder(w.Body).Decode(&p)
	return p.ID
}

// ---- Provider tests ----

func TestProviderCRUD(t *testing.T) {
	srv := testServer(t)

	// Create
	id := seedProvider(t, srv)
	if id == "" {
		t.Fatal("expected provider id")
	}

	// List
	w := getJSON(t, srv, "/api/providers")
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d", w.Code)
	}
	var list []model.Provider
	json.NewDecoder(w.Body).Decode(&list)
	if len(list) != 1 {
		t.Errorf("list len = %d, want 1", len(list))
	}

	// Get
	w = getJSON(t, srv, "/api/providers/"+id)
	if w.Code != http.StatusOK {
		t.Fatalf("get: %d", w.Code)
	}

	// Get 404
	w = getJSON(t, srv, "/api/providers/nonexistent")
	if w.Code != http.StatusNotFound {
		t.Errorf("get missing: want 404 got %d", w.Code)
	}

	// Delete
	w = deleteReq(t, srv, "/api/providers/"+id)
	if w.Code != http.StatusNoContent {
		t.Errorf("delete: want 204 got %d", w.Code)
	}
}

func TestProviderCreate_Validation(t *testing.T) {
	srv := testServer(t)

	// Missing name
	w := postJSON(t, srv, "/api/providers", map[string]string{"type": "llm"})
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}

	// Invalid type
	w = postJSON(t, srv, "/api/providers", map[string]string{"name": "X", "type": "invalid"})
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

// ---- Agent tests ----

func TestAgentCRUD(t *testing.T) {
	srv := testServer(t)
	provID := seedProvider(t, srv)
	agentID := seedAgent(t, srv, provID)

	// List
	w := getJSON(t, srv, "/api/agents")
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d", w.Code)
	}

	// Get
	w = getJSON(t, srv, "/api/agents/"+agentID)
	if w.Code != http.StatusOK {
		t.Fatalf("get: %d", w.Code)
	}
	var a model.Agent
	json.NewDecoder(w.Body).Decode(&a)
	if a.Name != "Test Agent" {
		t.Errorf("name = %q", a.Name)
	}

	// Delete
	w = deleteReq(t, srv, "/api/agents/"+agentID)
	if w.Code != http.StatusNoContent {
		t.Errorf("delete: want 204 got %d", w.Code)
	}
}

func TestAgentCreate_InvalidProvider(t *testing.T) {
	srv := testServer(t)
	w := postJSON(t, srv, "/api/agents", map[string]string{
		"name": "Agent", "provider_id": "nonexistent",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

// ---- Project tests ----

func TestProjectCRUD(t *testing.T) {
	srv := testServer(t)
	projID := seedProject(t, srv)

	w := getJSON(t, srv, "/api/projects/"+projID)
	if w.Code != http.StatusOK {
		t.Fatalf("get: %d", w.Code)
	}

	// Delete
	w = deleteReq(t, srv, "/api/projects/"+projID)
	if w.Code != http.StatusNoContent {
		t.Errorf("delete: want 204 got %d", w.Code)
	}
}

func TestProjectAgentAssignment(t *testing.T) {
	srv := testServer(t)
	provID := seedProvider(t, srv)
	agentID := seedAgent(t, srv, provID)
	projID := seedProject(t, srv)

	// Assign
	w := postJSON(t, srv, "/api/projects/"+projID+"/agents", map[string]string{"agent_id": agentID})
	if w.Code != http.StatusNoContent {
		t.Fatalf("assign: %d %s", w.Code, w.Body.String())
	}

	// List agents on project
	w = getJSON(t, srv, "/api/projects/"+projID+"/agents")
	if w.Code != http.StatusOK {
		t.Fatalf("list agents: %d", w.Code)
	}
	var agents []model.Agent
	json.NewDecoder(w.Body).Decode(&agents)
	if len(agents) != 1 {
		t.Errorf("want 1 agent, got %d", len(agents))
	}

	// Remove
	req := httptest.NewRequest(http.MethodDelete, "/api/projects/"+projID+"/agents/"+agentID, nil)
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, req)
	if w2.Code != http.StatusNoContent {
		t.Errorf("remove: want 204 got %d", w2.Code)
	}
}

// ---- Task tests ----

func TestTaskCreate_MissingFields(t *testing.T) {
	srv := testServer(t)
	w := postJSON(t, srv, "/api/tasks", map[string]string{"title": "Test"})
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

func TestTaskCreate_InvalidProject(t *testing.T) {
	srv := testServer(t)
	w := postJSON(t, srv, "/api/tasks", map[string]string{
		"project_id": "nonexistent", "agent_id": "nonexistent", "title": "Task",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

func TestTaskCreate_DependsOnUnmet_StaysQueued(t *testing.T) {
	srv := testServer(t)
	provID := seedProvider(t, srv)
	agentID := seedAgent(t, srv, provID)
	projID := seedProject(t, srv)
	postJSON(t, srv, "/api/projects/"+projID+"/agents", map[string]string{"agent_id": agentID})

	w := postJSON(t, srv, "/api/tasks", map[string]interface{}{
		"project_id": projID, "agent_id": agentID, "title": "Blocked task",
		"depends_on": []string{"some-not-yet-completed-task"},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var got model.Task
	json.NewDecoder(w.Body).Decode(&got)
	if got.Status != model.TaskStatusQueued {
		t.Errorf("status = %q, want queued", got.Status)
	}
}

func TestTaskCreate_DependsOnAlreadySatisfied_RunsImmediately(t *testing.T) {
	srv := testServer(t)
	provID := seedProvider(t, srv)
	// Route this provider through the fast, deterministic mock instead of a real
	// HTTP call, so the task can actually reach "completed" in this test.
	srv.registry.InjectForTest(provID, &mockProv{})
	agentID := seedAgent(t, srv, provID)
	projID := seedProject(t, srv)
	postJSON(t, srv, "/api/projects/"+projID+"/agents", map[string]string{"agent_id": agentID})

	w := postJSON(t, srv, "/api/tasks", map[string]interface{}{
		"project_id": projID, "agent_id": agentID, "title": "Prereq",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create prereq: %d %s", w.Code, w.Body.String())
	}
	var prereq model.Task
	json.NewDecoder(w.Body).Decode(&prereq)
	waitForTaskStatus(t, srv, prereq.ID, model.TaskStatusCompleted)

	// Now that the prereq has already completed, a task created depending on it
	// must not get stuck queued forever — it should run (and complete) too.
	w = postJSON(t, srv, "/api/tasks", map[string]interface{}{
		"project_id": projID, "agent_id": agentID, "title": "Follow-on",
		"depends_on": []string{prereq.ID},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create dependent: %d %s", w.Code, w.Body.String())
	}
	var got model.Task
	json.NewDecoder(w.Body).Decode(&got)
	waitForTaskStatus(t, srv, got.ID, model.TaskStatusCompleted)
}

func waitForTaskStatus(t *testing.T, srv *Server, taskID string, want model.TaskStatus) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, err := srv.tasks.Get(context.Background(), taskID)
		if err == nil && task != nil && task.Status == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("task %s did not reach status %q within deadline", taskID, want)
}

// ---- Task estimate tests ----

func seedProviderWithModel(t *testing.T, srv *Server, modelName string) string {
	t.Helper()
	w := postJSON(t, srv, "/api/providers", map[string]string{
		"name": "Test LLM", "type": "llm",
		"config": `{"endpoint":"http://mock.local","model":"` + modelName + `"}`,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create provider: %d %s", w.Code, w.Body.String())
	}
	var p model.Provider
	json.NewDecoder(w.Body).Decode(&p)
	return p.ID
}

type estimateResponse struct {
	Supported             bool               `json:"supported"`
	PromptTokens          int                `json:"prompt_tokens"`
	EstimatedOutputTokens map[string]int     `json:"estimated_output_tokens"`
	EstimatedCostUSD      map[string]float64 `json:"estimated_cost_usd"`
	Provider              map[string]string  `json:"provider"`
}

func TestEstimateTask_KnownModel_ReturnsCostRange(t *testing.T) {
	srv := testServer(t)
	provID := seedProviderWithModel(t, srv, "claude-3-5-sonnet")
	agentID := seedAgent(t, srv, provID)

	w := postJSON(t, srv, "/api/tasks/estimate", map[string]string{
		"agent_id": agentID, "title": "Summarize the quarterly report", "description": "Focus on revenue trends.",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("estimate: %d %s", w.Code, w.Body.String())
	}
	var got estimateResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Supported {
		t.Fatalf("supported = false, want true for known model")
	}
	if got.PromptTokens <= 0 {
		t.Errorf("prompt_tokens = %d, want > 0", got.PromptTokens)
	}
	if got.EstimatedCostUSD["low"] <= 0 || got.EstimatedCostUSD["high"] <= 0 {
		t.Errorf("estimated_cost_usd = %v, want positive low/high", got.EstimatedCostUSD)
	}
	if got.EstimatedCostUSD["low"] > got.EstimatedCostUSD["high"] {
		t.Errorf("cost low (%v) > high (%v)", got.EstimatedCostUSD["low"], got.EstimatedCostUSD["high"])
	}
	if got.Provider["model"] != "claude-3-5-sonnet" {
		t.Errorf("provider.model = %q, want claude-3-5-sonnet", got.Provider["model"])
	}
}

func TestEstimateTask_UnknownModel_NotSupported(t *testing.T) {
	srv := testServer(t)
	provID := seedProviderWithModel(t, srv, "some-unpriced-model")
	agentID := seedAgent(t, srv, provID)

	w := postJSON(t, srv, "/api/tasks/estimate", map[string]string{
		"agent_id": agentID, "title": "Task title",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("estimate: %d %s", w.Code, w.Body.String())
	}
	var got estimateResponse
	json.NewDecoder(w.Body).Decode(&got)
	if got.Supported {
		t.Errorf("supported = true, want false for unpriced model")
	}
	if got.EstimatedCostUSD["low"] != 0 || got.EstimatedCostUSD["high"] != 0 {
		t.Errorf("estimated_cost_usd = %v, want zero when unsupported", got.EstimatedCostUSD)
	}
}

func TestEstimateTask_AgentNotFound(t *testing.T) {
	srv := testServer(t)
	w := postJSON(t, srv, "/api/tasks/estimate", map[string]string{
		"agent_id": "nonexistent", "title": "Task title",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d %s", w.Code, w.Body.String())
	}
}

// ---- Stats tests ----

func TestGetCosts(t *testing.T) {
	srv := testServer(t)
	w := getJSON(t, srv, "/api/stats/costs")
	if w.Code != http.StatusOK {
		t.Fatalf("costs: %d %s", w.Code, w.Body.String())
	}
	var resp costsResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Total != 0 {
		t.Errorf("total = %v, want 0", resp.Total)
	}
}

// ---- Inbox tests ----

func TestInboxEmpty(t *testing.T) {
	srv := testServer(t)
	w := getJSON(t, srv, "/api/inbox")
	if w.Code != http.StatusOK {
		t.Fatalf("inbox: %d", w.Code)
	}
	var tasks []model.Task
	json.NewDecoder(w.Body).Decode(&tasks)
	if len(tasks) != 0 {
		t.Errorf("want 0, got %d", len(tasks))
	}
}

func TestInboxApproveRejectMissing(t *testing.T) {
	srv := testServer(t)

	// Approve non-existent task
	w := postJSON(t, srv, "/api/inbox/nonexistent/approve", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("approve missing: want 404 got %d", w.Code)
	}

	// Reject non-existent task
	w = postJSON(t, srv, "/api/inbox/nonexistent/reject", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("reject missing: want 404 got %d", w.Code)
	}
}

// Ensure the store interfaces are satisfied (compile-time check)
var _ store.ProviderRepo = (*sqllite.ProviderRepo)(nil)
var _ store.AgentRepo = (*sqllite.AgentRepo)(nil)
var _ store.ProjectRepo = (*sqllite.ProjectRepo)(nil)
var _ store.TaskRepo = (*sqllite.TaskRepo)(nil)
var _ store.TeamRepo = (*sqllite.TeamRepo)(nil)

// Suppress unused import
var _ = time.Now
