-- Add daily time-of-day scheduling for monitors as an alternative to the
-- existing interval schedule. A monitor uses exactly one schedule kind:
--   'interval' — fire every schedule_interval seconds (existing behaviour)
--   'daily'    — fire at each HH:MM in schedule_times (server local time)
--
-- schedule_times is a JSON array of "HH:MM" strings, e.g. ["00:00","06:00"].
-- schedule_catch_up: when 1, a daily run missed because the host was offline
-- runs at the next opportunity the same calendar day (most recent missed time
-- only, once). When 0, daily runs only fire punctually at the scheduled minute.
--
-- Existing monitors default to 'interval' so behaviour is unchanged.
ALTER TABLE projects ADD COLUMN schedule_kind TEXT NOT NULL DEFAULT 'interval';
ALTER TABLE projects ADD COLUMN schedule_times TEXT NOT NULL DEFAULT '[]';
ALTER TABLE projects ADD COLUMN schedule_catch_up INTEGER NOT NULL DEFAULT 0;
