-- Heartbeat reactive wiring: per-monitor action on health signal + linked project
ALTER TABLE projects ADD COLUMN heartbeat_on_attention TEXT NOT NULL DEFAULT ''; -- '' | 'spawn' | 'notify' | 'escalate'
ALTER TABLE projects ADD COLUMN heartbeat_on_failed    TEXT NOT NULL DEFAULT ''; -- same options
ALTER TABLE projects ADD COLUMN linked_project_id      TEXT;                     -- project to spawn remediation tasks in
ALTER TABLE projects ADD COLUMN heartbeat_consecutive_bad INTEGER NOT NULL DEFAULT 0; -- consecutive non-clear signal count
ALTER TABLE projects ADD COLUMN heartbeat_last_signal  TEXT NOT NULL DEFAULT ''; -- last signal value ('all_clear' | 'needs_attention' | 'failed')
