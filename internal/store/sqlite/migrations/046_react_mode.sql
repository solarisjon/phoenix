-- ReAct autonomous iteration loop: per-project mode + per-task iteration counter
ALTER TABLE projects ADD COLUMN react_mode     INTEGER NOT NULL DEFAULT 0;  -- 0=off, 1=on
ALTER TABLE projects ADD COLUMN max_iterations INTEGER NOT NULL DEFAULT 10; -- safety cap; 0 = use default (10)
ALTER TABLE tasks    ADD COLUMN loop_iteration INTEGER NOT NULL DEFAULT 0;  -- iteration index within a ReAct loop (0=first)
