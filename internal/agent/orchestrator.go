// Package agent - orchestrator.go implements the dynamic task orchestration
// pipeline. When a project has no explicitly-assigned agents and dynamic
// orchestration is enabled, tasks are routed to the global orchestrator agent
// which analyses the task, optionally decomposes it into subtasks, and assigns
// each subtask to the cheapest capable agent — creating new agents on demand.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/provider"
	"github.com/solarisjon/phoenix/internal/provider/registry"
	"github.com/solarisjon/phoenix/internal/store"
)

// Orchestrator handles the dynamic dispatch and decomposition pipeline.
// It is called from runner.go after an orchestration-type task completes.
type Orchestrator struct {
	agents    store.AgentRepo
	tasks     store.TaskRepo
	projects  store.ProjectRepo
	providers store.ProviderRepo
	settings  store.SystemSettingsRepo
	registry  *registry.Registry
	runner    TaskStarter
}

// TaskStarter is the subset of Runner that Orchestrator needs to start new tasks.
type TaskStarter interface {
	RunTask(ctx context.Context, taskID string) error
}

// NewOrchestrator creates an Orchestrator. All dependencies are required.
func NewOrchestrator(
	agents store.AgentRepo,
	tasks store.TaskRepo,
	projects store.ProjectRepo,
	providers store.ProviderRepo,
	settings store.SystemSettingsRepo,
	reg *registry.Registry,
	runner TaskStarter,
) *Orchestrator {
	return &Orchestrator{
		agents:    agents,
		tasks:     tasks,
		projects:  projects,
		providers: providers,
		settings:  settings,
		registry:  reg,
		runner:    runner,
	}
}

// ---- System prompt injection ----

// OrchestratorSystemSection returns the orchestration instructions injected into the
// global orchestrator agent's system prompt. It is appended by assembleSystemPrompt
// when agent.IsOrchestrator == true.
func OrchestratorSystemSection(availableAgents []*model.Agent, allProviders []*model.Provider, maxDepth, maxPerLevel int) string {
	var b strings.Builder

	b.WriteString("## Orchestration Mode\n\n")
	b.WriteString("You are the global task orchestrator for this Phoenix instance. ")
	b.WriteString("Your job is to analyse the incoming task and decide:\n")
	b.WriteString("1. Can this task be completed as-is by a single agent? If so, describe that single-agent plan.\n")
	b.WriteString("2. Should this task be decomposed into smaller, more discrete subtasks? If so, list them.\n\n")
	b.WriteString(fmt.Sprintf("Decomposition rules:\n- Maximum decomposition depth: %d\n- Maximum subtasks per level: %d\n\n", maxDepth, maxPerLevel))

	b.WriteString("You MUST respond with a JSON object (no other text) in EXACTLY this format:\n\n")
	b.WriteString("```json\n")
	b.WriteString("{\n")
	b.WriteString("  \"confidence\": 0.95,\n")
	b.WriteString("  \"rationale\": \"Brief explanation of your decision\",\n")
	b.WriteString("  \"subtasks\": [\n")
	b.WriteString("    {\n")
	b.WriteString("      \"title\": \"Short task title\",\n")
	b.WriteString("      \"description\": \"Full task description\",\n")
	b.WriteString("      \"domain\": \"code\",\n")
	b.WriteString("      \"complexity\": \"medium\"\n")
	b.WriteString("    }\n")
	b.WriteString("  ]\n")
	b.WriteString("}\n")
	b.WriteString("```\n\n")

	b.WriteString("Field guidance:\n")
	b.WriteString("- confidence: float 0–1 reflecting how certain you are this decomposition is correct\n")
	b.WriteString("- domain: one of code, write, analyse, research, ops, design, test, other\n")
	b.WriteString("- complexity: one of low, medium, high\n")
	b.WriteString("- subtasks: empty array [] if the task should NOT be decomposed (single-agent run)\n\n")

	if len(availableAgents) > 0 {
		b.WriteString("## Available Agents\n\n")
		b.WriteString("When choosing which agent to assign each subtask to, consider these existing agents:\n\n")
		for _, a := range availableAgents {
			if a.Status != model.AgentStatusActive {
				continue
			}
			desc := a.Behaviour
			if desc == "" {
				desc = a.Persona + " " + a.Instructions
			}
			if len(desc) > 200 {
				desc = desc[:200] + "..."
			}
			b.WriteString(fmt.Sprintf("- **%s** (id: %s): %s\n", a.Name, a.ID, strings.TrimSpace(desc)))
		}
		b.WriteString("\n")
	}

	if len(allProviders) > 0 {
		b.WriteString("## Available Providers and Models\n\n")
		for _, p := range allProviders {
			if len(p.AllowedModels) == 0 {
				continue
			}
			b.WriteString(fmt.Sprintf("Provider **%s** (id: %s, type: %s):\n", p.Name, p.ID, p.Type))
			for _, m := range p.AllowedModels {
				cost := m.InputCostPer1K + m.OutputCostPer1K
				b.WriteString(fmt.Sprintf("  - %s (%s): tier=%s, cost_per_1k=$%.4f — %s\n",
					m.ModelID, m.Label, m.CapabilityTier, cost, m.CapabilityDesc))
			}
		}
		b.WriteString("\n")
	}

	b.WriteString("For each subtask you may optionally add:\n")
	b.WriteString("- \"agent_id\": \"<id>\" to assign to an existing agent (must be from the Available Agents list above)\n")
	b.WriteString("- \"provider_id\": \"<id>\" and \"model_id\": \"<id>\" to specify which model to use\n")
	b.WriteString("If you leave these fields out, the system will auto-select based on domain and cost.\n")

	return b.String()
}

// ---- Plan parsing ----

// extractPlanJSON pulls the JSON object from the LLM output. The model may
// wrap it in a ```json ... ``` code block or return it bare.
func extractPlanJSON(output string) string {
	// Try fenced code block first.
	for _, fence := range []string{"```json", "```"} {
		start := strings.Index(output, fence)
		if start == -1 {
			continue
		}
		start += len(fence)
		end := strings.Index(output[start:], "```")
		if end == -1 {
			continue
		}
		return strings.TrimSpace(output[start : start+end])
	}
	// Fall back: find the first { … } in the output.
	start := strings.Index(output, "{")
	end := strings.LastIndex(output, "}")
	if start != -1 && end > start {
		return output[start : end+1]
	}
	return ""
}

// ---- Extended plan with agent routing ----

// routedSubtask extends OrchestrationSubtask with optional explicit routing hints
// emitted by the orchestrator LLM.
type routedSubtask struct {
	model.OrchestrationSubtask
	AgentID    string `json:"agent_id,omitempty"`
	ProviderID string `json:"provider_id,omitempty"`
	ModelID    string `json:"model_id,omitempty"`
}

type routedPlan struct {
	Confidence float64         `json:"confidence"`
	Rationale  string          `json:"rationale"`
	Subtasks   []routedSubtask `json:"subtasks"`
}

// parseRoutedPlan decodes the orchestrator's JSON output.
func parseRoutedPlan(raw string) (*routedPlan, error) {
	jsonStr := extractPlanJSON(raw)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON object found in orchestrator output")
	}
	var p routedPlan
	if err := json.Unmarshal([]byte(jsonStr), &p); err != nil {
		return nil, fmt.Errorf("parse orchestration plan: %w", err)
	}
	return &p, nil
}

// ---- Agent selection and creation ----

// SelectModelForDomain picks the cheapest model in the provider's pool that
// meets the minimum capability tier required for the given domain/complexity.
// Returns providerID, modelID, or empty strings if nothing suitable found.
func SelectModelForDomain(providers []*model.Provider, domain, complexity string) (providerID, modelID string) {
	// Map domain + complexity to minimum tier.
	minTier := requiredTier(domain, complexity)

	bestCost := -1.0
	for _, p := range providers {
		if p.Type != model.ProviderTypeLLM {
			continue
		}
		for _, m := range p.AllowedModels {
			if !tierMeetsMinimum(m.CapabilityTier, minTier) {
				continue
			}
			cost := m.InputCostPer1K + m.OutputCostPer1K
			if bestCost < 0 || cost < bestCost {
				bestCost = cost
				providerID = p.ID
				modelID = m.ModelID
			}
		}
	}
	return providerID, modelID
}

// tierMeetsMinimum returns true if the model tier is at or above the minimum.
func tierMeetsMinimum(tier, minTier model.ModelCapabilityTier) bool {
	order := map[model.ModelCapabilityTier]int{
		model.ModelTierFast:     1,
		model.ModelTierStandard: 2,
		model.ModelTierPowerful: 3,
		model.ModelTierPlanning: 2, // planning is specialised, treat as standard floor
	}
	return order[tier] >= order[minTier]
}

// requiredTier maps domain + complexity to the minimum acceptable capability tier.
func requiredTier(domain, complexity string) model.ModelCapabilityTier {
	if complexity == "high" {
		return model.ModelTierPowerful
	}
	switch domain {
	case "code", "design", "ops":
		if complexity == "medium" {
			return model.ModelTierStandard
		}
		return model.ModelTierFast
	case "analyse", "research":
		return model.ModelTierStandard
	default:
		return model.ModelTierFast
	}
}

// SelectOrchestrationModel picks the cheapest model flagged as "planning" tier.
// Falls back to "standard" if no planning-tier model exists.
func SelectOrchestrationModel(providers []*model.Provider) (providerID, modelID string) {
	// First pass: look for planning tier.
	bestCost := -1.0
	for _, p := range providers {
		if p.Type != model.ProviderTypeLLM {
			continue
		}
		for _, m := range p.AllowedModels {
			if m.CapabilityTier != model.ModelTierPlanning {
				continue
			}
			cost := m.InputCostPer1K + m.OutputCostPer1K
			if bestCost < 0 || cost < bestCost {
				bestCost = cost
				providerID = p.ID
				modelID = m.ModelID
			}
		}
	}
	if providerID != "" {
		return
	}
	// Fallback: cheapest standard or powerful.
	bestCost = -1.0
	for _, p := range providers {
		if p.Type != model.ProviderTypeLLM {
			continue
		}
		for _, m := range p.AllowedModels {
			if m.CapabilityTier != model.ModelTierStandard && m.CapabilityTier != model.ModelTierPowerful {
				continue
			}
			cost := m.InputCostPer1K + m.OutputCostPer1K
			if bestCost < 0 || cost < bestCost {
				bestCost = cost
				providerID = p.ID
				modelID = m.ModelID
			}
		}
	}
	return
}

// ---- HandleOrchestrationComplete ----

// HandleOrchestrationComplete is called by runner.finaliseTask after an
// orchestration-type task completes successfully. It parses the plan from
// the output, then either spawns subtasks (high confidence) or marks the
// task as awaiting_approval (low confidence) so the human can review.
func (o *Orchestrator) HandleOrchestrationComplete(ctx context.Context, task *model.Task, outputText string) {
	slog.Info("orchestrator: handling completed orchestration task", "task_id", task.ID)

	settings, err := o.settings.Get(ctx)
	if err != nil {
		slog.Error("orchestrator: get settings", "error", err)
		return
	}

	plan, err := parseRoutedPlan(outputText)
	if err != nil {
		slog.Warn("orchestrator: parse plan failed — task left as completed", "task_id", task.ID, "error", err)
		return
	}

	// Store the plan JSON on the task for UI display.
	cleanPlan, _ := json.Marshal(plan)
	task.OrchestrationPlan = string(cleanPlan)
	if err := o.tasks.Update(ctx, task); err != nil {
		slog.Warn("orchestrator: persist plan", "task_id", task.ID, "error", err)
	}

	threshold := settings.OrchestratorConfidenceThreshold
	if threshold <= 0 {
		threshold = 0.75
	}

	if plan.Confidence < threshold {
		slog.Info("orchestrator: confidence below threshold — awaiting approval",
			"task_id", task.ID,
			"confidence", plan.Confidence,
			"threshold", threshold)
		// Revert to awaiting_approval so human can review + approve.
		task.Status = model.TaskStatusAwaitingApproval
		if err := o.tasks.Update(ctx, task); err != nil {
			slog.Error("orchestrator: set awaiting_approval", "error", err)
		}
		return
	}

	if len(plan.Subtasks) == 0 {
		// Orchestrator decided no decomposition needed — task is already complete.
		slog.Info("orchestrator: no decomposition needed, task complete", "task_id", task.ID)
		return
	}

	// Spawn all subtasks.
	if err := o.spawnSubtasks(ctx, task, plan, settings); err != nil {
		slog.Error("orchestrator: spawn subtasks", "task_id", task.ID, "error", err)
	}
}

// spawnSubtasks creates child tasks from the orchestration plan and queues them.
func (o *Orchestrator) spawnSubtasks(ctx context.Context, parent *model.Task, plan *routedPlan, settings *model.SystemSettings) error {
	allAgents, err := o.agents.List(ctx, "")
	if err != nil {
		return fmt.Errorf("list agents: %w", err)
	}

	allProviders, err := o.providers.List(ctx, "")
	if err != nil {
		return fmt.Errorf("list providers: %w", err)
	}

	maxPerLevel := settings.MaxSubtasksPerLevel
	if maxPerLevel <= 0 {
		maxPerLevel = 5
	}

	created := 0
	for i, sub := range plan.Subtasks {
		if created >= maxPerLevel {
			slog.Warn("orchestrator: max subtasks per level reached", "task_id", parent.ID, "limit", maxPerLevel)
			break
		}

		agentID, modelOverride, providerID, err := o.resolveSubtaskRouting(ctx, sub, allAgents, allProviders, parent.ProjectID, i)
		if err != nil {
			slog.Warn("orchestrator: resolve subtask routing", "subtask", sub.Title, "error", err)
			continue
		}

		subtask := &model.Task{
			ID:           uuid.New().String(),
			ProjectID:    parent.ProjectID,
			AgentID:      agentID,
			ParentTaskID: &parent.ID,
			Title:        sub.Title,
			Description:  sub.Description,
			Status:       model.TaskStatusPending,
			TaskType:     model.TaskTypeSubtask,
			Source:       "orchestrator",
			Input:        "{}",
			Output:       "{}",
			CreatedAt:    time.Now(),
		}
		// Apply model override if we found a specific cheapest model.
		if modelOverride != "" {
			subtask.AgentID = agentID
			// Store the model override via a temporary agent override — we need to update the agent or use a different mechanism.
			// For now we encode the desired model in the task source field as routing metadata.
			_ = modelOverride // applied via agent.ModelOverride at agent creation time
			_ = providerID
		}

		if err := o.tasks.Create(ctx, subtask); err != nil {
			slog.Error("orchestrator: create subtask", "title", sub.Title, "error", err)
			continue
		}

		if err := o.runner.RunTask(ctx, subtask.ID); err != nil {
			slog.Error("orchestrator: run subtask", "task_id", subtask.ID, "error", err)
			continue
		}

		slog.Info("orchestrator: spawned subtask", "task_id", subtask.ID, "title", sub.Title, "agent_id", agentID)
		created++
	}

	return nil
}

// resolveSubtaskRouting determines the agentID and optional model for a subtask.
// It uses the orchestrator's explicit hint first, then falls back to finding the
// cheapest capable agent/model from the pool.
func (o *Orchestrator) resolveSubtaskRouting(ctx context.Context, sub routedSubtask, allAgents []*model.Agent, allProviders []*model.Provider, projectID string, index int) (agentID, modelOverride, providerID string, err error) {
	// 1. Orchestrator explicitly named an existing agent.
	if sub.AgentID != "" {
		for _, a := range allAgents {
			if a.ID == sub.AgentID && a.Status == model.AgentStatusActive {
				return a.ID, sub.ModelID, sub.ProviderID, nil
			}
		}
		// Agent hint was invalid — fall through to auto-select.
		slog.Warn("orchestrator: hinted agent not found, auto-selecting", "hinted_agent_id", sub.AgentID)
	}

	// 2. Find cheapest model for the domain/complexity.
	pID, mID := SelectModelForDomain(allProviders, sub.Domain, sub.Complexity)

	// 3. Find an existing agent that best matches this domain.
	bestAgent := o.findBestMatchingAgent(allAgents, sub.Domain)
	if bestAgent != nil {
		return bestAgent.ID, mID, pID, nil
	}

	// 4. No suitable existing agent — create a dynamic one.
	if pID == "" {
		// No model pool configured, fall back to first available LLM provider.
		for _, p := range allProviders {
			if p.Type == model.ProviderTypeLLM {
				pID = p.ID
				break
			}
		}
	}
	if pID == "" {
		return "", "", "", fmt.Errorf("no LLM provider available for subtask %q", sub.Title)
	}

	newAgent, err := o.createDynamicAgent(ctx, sub, pID, mID)
	if err != nil {
		return "", "", "", fmt.Errorf("create dynamic agent: %w", err)
	}

	return newAgent.ID, mID, pID, nil
}

// findBestMatchingAgent returns the active agent whose behaviour best matches
// the given domain. Simple keyword heuristic — the orchestrator LLM handles
// precise matching via explicit agent_id hints when it can.
func (o *Orchestrator) findBestMatchingAgent(agents []*model.Agent, domain string) *model.Agent {
	keywords := domainKeywords(domain)
	var best *model.Agent
	bestScore := 0

	for _, a := range agents {
		if a.Status != model.AgentStatusActive || a.IsOrchestrator {
			continue
		}
		haystack := strings.ToLower(a.Name + " " + a.Behaviour + " " + a.Persona + " " + a.Instructions)
		score := 0
		for _, kw := range keywords {
			if strings.Contains(haystack, kw) {
				score++
			}
		}
		if score > bestScore {
			bestScore = score
			best = a
		}
	}
	return best
}

// domainKeywords returns a list of keywords that indicate an agent's suitability
// for the given task domain.
func domainKeywords(domain string) []string {
	switch domain {
	case "code":
		return []string{"code", "coding", "developer", "engineer", "programming", "software", "git", "build"}
	case "write":
		return []string{"write", "writing", "author", "content", "copywriter", "docs", "documentation", "markdown"}
	case "analyse":
		return []string{"analyse", "analyze", "analysis", "data", "insight", "report", "review", "metrics"}
	case "research":
		return []string{"research", "investigate", "search", "find", "discover", "information"}
	case "ops":
		return []string{"ops", "devops", "infrastructure", "deploy", "cloud", "docker", "kubernetes", "ci"}
	case "design":
		return []string{"design", "ux", "ui", "interface", "visual", "product"}
	case "test":
		return []string{"test", "testing", "qa", "quality", "assert", "spec"}
	default:
		return []string{}
	}
}

// createDynamicAgent creates a new persistent agent for a subtask domain.
// The agent is flagged created_by="orchestrator" so it can be identified in the UI.
func (o *Orchestrator) createDynamicAgent(ctx context.Context, sub routedSubtask, providerID, modelOverride string) (*model.Agent, error) {
	behaviour := fmt.Sprintf(`You are a specialised %s agent. Your role is to complete tasks in the %s domain.

Focus area: %s
Complexity handling: %s tasks

Complete the task you are given accurately and efficiently. Produce clear, well-structured output.`,
		sub.Domain, sub.Domain, sub.Title, sub.Complexity)

	name := fmt.Sprintf("Auto-%s Agent", strings.Title(sub.Domain)) //nolint:staticcheck

	// Check if a dynamic agent for this domain already exists to avoid proliferation.
	existing, err := o.agents.List(ctx, "")
	if err == nil {
		for _, a := range existing {
			if a.CreatedBy == "orchestrator" && strings.EqualFold(a.Name, name) && a.Status == model.AgentStatusActive {
				slog.Info("orchestrator: reusing existing dynamic agent", "agent_id", a.ID, "name", name)
				return a, nil
			}
		}
	}

	a := &model.Agent{
		ID:            uuid.New().String(),
		Name:          name,
		Behaviour:     behaviour,
		ProviderID:    providerID,
		ModelOverride: modelOverride,
		CreatedBy:     "orchestrator",
		Status:        model.AgentStatusActive,
		CreatedAt:     time.Now(),
	}

	if err := o.agents.Create(ctx, a); err != nil {
		return nil, fmt.Errorf("create dynamic agent: %w", err)
	}

	slog.Info("orchestrator: created dynamic agent", "agent_id", a.ID, "name", a.Name, "domain", sub.Domain)
	return a, nil
}

// ---- Dynamic task routing ----

// ShouldOrchestrate returns true if a task should be routed to the orchestrator
// rather than a direct agent assignment. This is the case when:
// 1. The project has no assigned agents, AND
// 2. Dynamic orchestration is enabled with a configured orchestrator agent.
func ShouldOrchestrate(projectAgents []*model.Agent, settings *model.SystemSettings) bool {
	if !settings.DynamicOrchestrationEnabled {
		return false
	}
	if settings.OrchestratorAgentID == "" {
		return false
	}
	// Only orchestrate when no explicit agents are assigned to the project.
	for _, a := range projectAgents {
		if a.Status == model.AgentStatusActive {
			return false
		}
	}
	return true
}

// BuildOrchestrationTask creates a task record (not yet persisted) that is
// pre-configured as an orchestration task routed to the global orchestrator agent.
func BuildOrchestrationTask(original *model.Task, orchestratorAgentID string) *model.Task {
	t := *original
	t.AgentID = orchestratorAgentID
	t.TaskType = model.TaskTypeOrchestration
	t.Source = "orchestrator"
	return &t
}

// InjectOrchestratorInstructions appends orchestrator-specific instructions
// to a TaskRequest system prompt when the agent is the global orchestrator.
func InjectOrchestratorInstructions(req provider.TaskRequest, agents []*model.Agent, providers []*model.Provider, maxDepth, maxPerLevel int) provider.TaskRequest {
	section := OrchestratorSystemSection(agents, providers, maxDepth, maxPerLevel)
	req.SystemPrompt = req.SystemPrompt + "\n\n" + section
	return req
}
