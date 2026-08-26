// Package logger provides structured logging via slog.
package logger

import (
	"log/slog"
	"os"
)

// New returns a structured logger.
func New(env string) *slog.Logger {
	level := slog.LevelInfo
	if env == "dev" {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
}
