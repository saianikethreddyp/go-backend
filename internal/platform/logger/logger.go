// Package logger builds the process-wide structured logger.
package logger

import (
	"log/slog"
	"os"
)

// New returns a JSON logger in production and a readable text logger elsewhere.
func New(production bool) *slog.Logger {
	if production {
		return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
}
