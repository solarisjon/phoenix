// Package config loads Phoenix runtime configuration from environment variables.
// All values have sensible defaults so no configuration is required to run locally.
package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds all runtime-configurable values for the Phoenix server.
type Config struct {
	// DBPath is the absolute path to the SQLite database file.
	// Defaults to the platform data directory resolved by paths.Init().
	DBPath string

	// Port is the TCP port to listen on.
	Port string

	// TaskTimeout is the maximum duration a single agent task may run.
	TaskTimeout time.Duration

	// SchedulerInterval is how often the monitor scheduler polls for due tasks.
	SchedulerInterval time.Duration

	// HTTPTimeout is the per-request handler timeout.
	HTTPTimeout time.Duration

	// CORSOrigin is an optional comma-separated list of allowed CORS origins
	// beyond the default localhost/loopback set.
	CORSOrigin string

	// HealthCheckInterval is how often the background checker probes each provider.
	HealthCheckInterval time.Duration
}

// Load reads environment variables and returns a populated Config.
// defaultDBPath is used as the fallback for PHOENIX_DB_PATH and must be
// computed by the caller after paths.Init().
// Unset or unparseable values fall back to their stated defaults.
func Load(defaultDBPath string) Config {
	return Config{
		DBPath:            envString("PHOENIX_DB_PATH", defaultDBPath),
		Port:              envString("PHOENIX_PORT", "8080"),
		TaskTimeout:       envDuration("PHOENIX_TASK_TIMEOUT", 30*time.Minute),
		SchedulerInterval: envDuration("PHOENIX_SCHEDULER_INTERVAL", 60*time.Second),
		HTTPTimeout:         envDuration("PHOENIX_HTTP_TIMEOUT", 60*time.Second),
		CORSOrigin:          os.Getenv("PHOENIX_CORS_ORIGIN"),
		HealthCheckInterval: envDuration("PHOENIX_HEALTH_CHECK_INTERVAL", 10*time.Minute),
	}
}

func envString(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// envDuration parses a duration env var. It accepts either a plain integer
// (interpreted as seconds) or any valid Go duration string (e.g. "5m", "1h30m").
// Unset or unparseable values return def.
func envDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	if secs, err := strconv.Atoi(v); err == nil {
		return time.Duration(secs) * time.Second
	}
	if d, err := time.ParseDuration(v); err == nil {
		return d
	}
	return def
}
