// Package logging provides structured logging setup for Phoenix using log/slog.
package logging

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

type contextKey struct{}

// Init sets up the global slog logger based on environment variables and TTY
// detection. Call this once at program startup before any logging occurs.
//
// PHOENIX_LOG_LEVEL controls the minimum level (debug/info/warn/error).
// Defaults to "info".
// Output format: JSON when stdout is not a TTY (e.g. systemd, Docker);
// human-readable text otherwise.
func Init() {
	level := parseLevel(os.Getenv("PHOENIX_LOG_LEVEL"))

	var handler slog.Handler
	opts := &slog.HandlerOptions{Level: level}

	if isTTY(os.Stderr) {
		handler = slog.NewTextHandler(os.Stderr, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	}

	slog.SetDefault(slog.New(handler))
}

// FromContext returns the logger stored in ctx by Middleware, or the global
// default logger if none was stored.
func FromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(contextKey{}).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.Default()
}

// WithLogger returns a new context carrying the given logger.
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, contextKey{}, l)
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// isTTY returns true if the given file is connected to a terminal.
func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
