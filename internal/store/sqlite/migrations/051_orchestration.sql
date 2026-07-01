-- Migration 051: orchestration support.
-- is_orchestrator: marks an agent as the global task orchestrator.
ALTER TABLE agents ADD COLUMN is_orchestrator INTEGER NOT NULL DEFAULT 0;

-- task_type distinguishes orchestration tasks from regular tasks and subtasks.
ALTER TABLE tasks ADD COLUMN task_type TEXT NOT NULL DEFAULT 'standard';

-- orchestration_plan holds the JSON plan produced by the orchestrator
-- (confidence score, rationale, proposed subtask list).
ALTER TABLE tasks ADD COLUMN orchestration_plan TEXT NOT NULL DEFAULT '';
