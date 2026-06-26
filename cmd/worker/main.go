// Command worker is the async background process for CMStack-Go. In M0 it hosts
// the outbox relay loop, honestly constructed over a real pgx pool and the sqlc
// querier. Each tick claims unprocessed outbox rows (FOR UPDATE SKIP LOCKED)
// inside a transaction; dispatch to async listeners is a documented TODO(M1),
// so rows are observed but not yet marked processed.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/huseyn0w/cmstack-go/internal/platform/config"
	"github.com/huseyn0w/cmstack-go/internal/platform/db"
	"github.com/huseyn0w/cmstack-go/internal/platform/events"
	"github.com/huseyn0w/cmstack-go/internal/platform/logging"
)

func main() {
	if err := run(); err != nil {
		slog.Error("worker exited with error", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := logging.New(cfg)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	// Honest relay wiring: real pool + sqlc querier. Dispatch to async listeners
	// is TODO(M1); until then Drain claims and observes rows without marking them
	// processed, so no events are lost before dispatch exists.
	relay := events.NewRelay(pool, 100, logger)

	logger.Info("worker started", "env", cfg.AppEnv)

	const interval = 5 * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("worker stopped cleanly")
			return nil
		case <-ticker.C:
			n, err := relay.Drain(ctx)
			if err != nil {
				logger.Error("outbox relay drain failed", "err", err)
				continue
			}
			logger.Debug("outbox relay tick", "observed", n)
		}
	}
}
