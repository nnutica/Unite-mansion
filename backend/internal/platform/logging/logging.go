// Package logging configures the process-wide slog logger.
package logging

import (
	"log/slog"
	"os"
)

// New returns a text logger for local and a JSON logger for other environments.
func New(appEnv string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	if appEnv == "local" {
		opts.Level = slog.LevelDebug
		return slog.New(slog.NewTextHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, opts))
}
