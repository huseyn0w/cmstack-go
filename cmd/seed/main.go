// Command seed idempotently seeds the auth baseline: the four roles, the full
// permission set, their mappings, and a default administrator from env
// (ADMIN_EMAIL / ADMIN_PASSWORD). It is safe to run repeatedly. The server also
// runs this seed at startup; this command exists for explicit ops/CI use.
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/platform/config"
	"github.com/huseyn0w/cmstack-go/internal/platform/db"
	"github.com/huseyn0w/cmstack-go/internal/platform/db/sqlcgen"
	"github.com/huseyn0w/cmstack-go/internal/platform/logging"
	"github.com/huseyn0w/cmstack-go/internal/platform/security"
)

func main() {
	if err := run(); err != nil {
		slog.Error("seed failed", "err", err)
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

	ctx := context.Background()
	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	queries := sqlcgen.New(pool)
	hasher := security.NewPasswordHasher()
	userRepo := accounts.NewUserRepoPG(queries)
	roleRepo := accounts.NewRoleRepoPG(queries)
	seeder := accounts.NewSeeder(pool, queries, userRepo, roleRepo, hasher)

	if err := seeder.Seed(ctx, accounts.AdminSeed{Email: cfg.AdminEmail, Password: cfg.AdminPassword}); err != nil {
		return err
	}
	logger.Info("seed complete", "admin_email", cfg.AdminEmail)
	return nil
}
