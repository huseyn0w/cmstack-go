// Command worker is the async background process for CMStack-Go. It hosts the
// outbox relay loop, honestly constructed over a real pgx pool, the sqlc
// querier, and the wired event bus. Each tick claims unprocessed outbox rows
// (FOR UPDATE SKIP LOCKED) inside a transaction, dispatches each to its
// registered async handler (e.g. the email listener), and marks delivered rows
// processed atomically.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/platform/config"
	"github.com/huseyn0w/cmstack-go/internal/platform/db"
	"github.com/huseyn0w/cmstack-go/internal/platform/events"
	"github.com/huseyn0w/cmstack-go/internal/platform/logging"
	"github.com/huseyn0w/cmstack-go/internal/platform/mailer"
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

	// Honest relay wiring: real pool + sqlc querier + the bus as dispatcher. The
	// email listener is registered on the bus so the relay routes drained outbox
	// rows to it after commit. The bus needs no outbox enqueuer here (the worker
	// only dispatches; the server enqueues).
	bus := events.NewBus(nil)
	emailListener := accounts.NewEmailListener(mailer.NewLogMailer(logger), cfg.BaseURL)
	emailListener.Register(bus)

	relay := events.NewRelay(pool, bus, 100, logger)

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
