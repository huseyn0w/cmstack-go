// Package logging builds the application's structured slog logger.
package logging

import (
	"io"
	"log/slog"
	"os"

	"github.com/huseyn0w/agentic-cms-go/internal/platform/config"
)

// New returns a JSON slog.Logger whose level is derived from the app
// environment: debug in development, info in production, warn in tests.
func New(cfg config.Config) *slog.Logger {
	return NewWithWriter(cfg, os.Stdout)
}

// NewWithWriter is New with an explicit destination, useful in tests.
func NewWithWriter(cfg config.Config, w io.Writer) *slog.Logger {
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: levelFor(cfg.AppEnv),
	})
	return slog.New(handler)
}

func levelFor(appEnv string) slog.Level {
	switch appEnv {
	case "production":
		return slog.LevelInfo
	case "test":
		return slog.LevelWarn
	default:
		return slog.LevelDebug
	}
}
