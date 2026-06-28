-- Monitor cache TTL: bypass prompt-hash cache when cached output is older than N seconds
ALTER TABLE projects ADD COLUMN monitor_cache_ttl INTEGER NOT NULL DEFAULT 0; -- 0 = cache indefinitely (original behaviour)
