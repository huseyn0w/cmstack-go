// Command server is the CMStack-Go HTTP entrypoint. It loads config, builds the
// pgx pool, wires services explicitly, and runs an http.Server with graceful
// shutdown on SIGINT/SIGTERM.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/health"
	"github.com/huseyn0w/cmstack-go/internal/platform/config"
	"github.com/huseyn0w/cmstack-go/internal/platform/db"
	"github.com/huseyn0w/cmstack-go/internal/platform/db/sqlcgen"
	"github.com/huseyn0w/cmstack-go/internal/platform/events"
	"github.com/huseyn0w/cmstack-go/internal/platform/logging"
	"github.com/huseyn0w/cmstack-go/internal/platform/mailer"
	"github.com/huseyn0w/cmstack-go/internal/platform/security"
	"github.com/huseyn0w/cmstack-go/internal/platform/session"
	"github.com/huseyn0w/cmstack-go/internal/web"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server exited with error", "err", err)
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

	// Explicit dependency wiring — assembled here, nowhere else.
	healthSvc := health.NewService(pool)
	healthHandler := health.NewHandler(healthSvc)
	sess := session.NewManager(cfg.IsProduction())

	// Event bus + outbox. The outbox enqueue path is the sqlc-backed repository.
	// The async email listener is registered so account events are marked async
	// (enqueued in-tx); the worker process drains and dispatches them.
	outbox := events.NewOutboxRepository()
	bus := events.NewBus(outbox)
	emailListener := accounts.NewEmailListener(mailer.NewLogMailer(logger), cfg.BaseURL)
	emailListener.Register(bus)

	// Accounts (auth) wiring.
	queries := sqlcgen.New(pool)
	hasher := security.NewPasswordHasher()
	userRepo := accounts.NewUserRepoPG(queries)
	roleRepo := accounts.NewRoleRepoPG(queries)
	tokenRepo := accounts.NewTokenRepoPG(queries)
	settings := accounts.NewStaticSettings(cfg.SignupEnabled, cfg.EmailVerificationRequired)
	authz := accounts.NewAuthorizer(userRepo, roleRepo)
	authSvc := accounts.NewAuthService(pool, userRepo, roleRepo, tokenRepo, hasher, bus, settings, nil)

	// Idempotent seed: roles, permissions, mappings, default administrator.
	seeder := accounts.NewSeeder(pool, queries, userRepo, roleRepo, hasher)
	if err := seeder.Seed(ctx, accounts.AdminSeed{Email: cfg.AdminEmail, Password: cfg.AdminPassword}); err != nil {
		return err
	}

	authMW := web.NewAuthMiddleware(sess, userRepo, authz)
	authHandler := accounts.NewHandler(authSvc, authMW, security.Token, accounts.NewValidator())

	handler := web.Router(web.Deps{
		Config:        cfg,
		Health:        healthHandler,
		Bus:           bus,
		Session:       sess,
		StaticDir:     web.StaticDirDefault(),
		LoggerHandler: logging.RequestLogger(logger),
		Auth:          authHandler,
		AuthMW:        authMW,
		CSRFFunc:      security.Token,
	})

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: cfg.ReadTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", cfg.HTTPAddr, "env", cfg.AppEnv)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	logger.Info("server stopped cleanly")
	return nil
}
