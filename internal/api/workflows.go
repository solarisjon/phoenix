package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/solarisjon/phoenix/internal/agent"
	"github.com/solarisjon/phoenix/internal/model"
)

type createWorkflowRequest struct {
	Name             string   `json:"name"`
	Kind             string   `json:"kind"` // monitor | project
	SkillID          string   `json:"skill_id"`
	Objective        string   `json:"objective"`
	WorkingDir       string   `json:"working_dir"`
	ScheduleKind     string   `json:"schedule_kind"`
	ScheduleTimes    []string `json:"schedule_times"`
	ScheduleInterval int      `json:"schedule_interval"`
	AgentID          string   `json:"agent_id"`
	TestRun          bool     `json:"test_run"`
}

func (s *Server) createWorkflow(w http.ResponseWriter, r *http.Request) {
	var req createWorkflowRequest
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		respondErr(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.Kind != "monitor" && req.Kind != "project" {
		respondErr(w, http.StatusBadRequest, "kind must be monitor or project")
		return
	}
	if strings.TrimSpace(req.SkillID) == "" {
		respondErr(w, http.StatusBadRequest, "skill_id is required")
		return
	}

	skill, err := s.skills.Get(r.Context(), req.SkillID)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if skill == nil {
		respondErr(w, http.StatusBadRequest, "skill not found")
		return
	}

	user := userFromCtx(r.Context())
	settings, _ := s.systemSettings.Get(r.Context())

	agentID := strings.TrimSpace(req.AgentID)
	if agentID == "" && settings != nil && skill.ExecutionMode == model.SkillExecutionOrchestrate && settings.OrchestratorAgentID != "" {
		agentID = settings.OrchestratorAgentID
	}
	if agentID == "" && settings != nil && settings.DefaultWorkerAgentID != "" {
		agentID = settings.DefaultWorkerAgentID
	}
	if agentID == "" {
		respondErr(w, http.StatusBadRequest, "agent_id is required (or configure orchestrator/default worker in settings)")
		return
	}
	a, err := s.agents.Get(r.Context(), agentID)
	if err != nil || a == nil {
		respondErr(w, http.StatusBadRequest, "agent not found")
		return
	}

	objective := strings.TrimSpace(req.Objective)
	if objective == "" {
		objective = fmt.Sprintf("Run the skill called %s.", skill.Slug)
	}

	now := time.Now().UTC()
	skillID := skill.ID
	proj := &model.Project{
		ID:             uuid.New().String(),
		Name:           strings.TrimSpace(req.Name),
		Objective:      objective,
		Owner:          user.ID,
		Status:         model.ProjectStatusActive,
		Kind:           model.ProjectKind(req.Kind),
		WorkingDir:     strings.TrimSpace(req.WorkingDir),
		CreatedAt:      now,
		DefaultSkillID: &skillID,
	}
	if req.Kind == "monitor" {
		proj.ScheduleKind = req.ScheduleKind
		if proj.ScheduleKind == "" {
			proj.ScheduleKind = "daily"
		}
		if len(req.ScheduleTimes) > 0 {
			proj.ScheduleTimes = req.ScheduleTimes
		} else {
			proj.ScheduleTimes = []string{"07:00"}
		}
		if req.ScheduleInterval > 0 {
			proj.ScheduleInterval = &req.ScheduleInterval
		}
	}

	if err := s.projects.Create(r.Context(), proj); err != nil {
		respondInternalErr(w, err)
		return
	}
	if _, err := s.projects.AssignAgent(r.Context(), proj.ID, agentID); err != nil {
		respondInternalErr(w, err)
		return
	}

	var testTask *model.Task
	if req.TestRun {
		taskType := model.TaskTypeStandard
		importDirs := []string{}
		if settings != nil {
			importDirs = settings.SkillImportDirs
		}
		probe := &model.Task{Title: "Test run", Description: objective}
		if a.IsOrchestrator && agent.TaskShouldUseOrchestrationType(r.Context(), s.skills, importDirs, proj.WorkingDir, probe, proj) {
			taskType = model.TaskTypeOrchestration
		}
		testTask = &model.Task{
			ID:          uuid.New().String(),
			ProjectID:   proj.ID,
			AgentID:     agentID,
			Title:       fmt.Sprintf("Test run — %s", now.Format("2006-01-02 15:04")),
			Description: objective,
			Status:      model.TaskStatusPending,
			Source:      "workflow-wizard",
			TaskType:    taskType,
			Input:       "{}",
			Output:      "{}",
			CreatedAt:   now,
		}
		if err := s.tasks.Create(r.Context(), testTask); err != nil {
			respondInternalErr(w, err)
			return
		}
		if err := s.runner.RunTask(r.Context(), testTask.ID); err != nil {
			respondInternalErr(w, err)
			return
		}
	}

	respond(w, http.StatusCreated, map[string]any{
		"project":   proj,
		"skill":     skill,
		"test_task": testTask,
	})
}
