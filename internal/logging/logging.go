// Package logging configures structured JSON logs, one object per line.
package logging

import (
	"log/slog"
	"os"
	"strings"
)

// New returns a JSON logger at the level named by HELIOSTAT_LOG_LEVEL (debug, info, warn, error).
func New(level string) *slog.Logger {
	var l slog.Level
	switch strings.ToLower(level) {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: l}))
}
