// Package logger wires structured logging (log/slog) for the whole backend.
// The default output is JSON on stdout so it plays well with Docker and
// log collectors.
package logger

import (
	"log/slog"
	"os"
	"strings"
)

// New returns a JSON slog.Logger writing to stdout, tagged with the service
// name. level is one of "debug", "info", "warn"/"warning", "error";
// anything else defaults to "info".
func New(level string) *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: ParseLevel(level),
	})).With("service", "kidcad-backend")
}

// ParseLevel maps a textual level onto slog.Level with a safe default.
func ParseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
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
