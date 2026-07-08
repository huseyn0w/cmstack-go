// Command seedcontent idempotently seeds DEMO CONTENT: the canonical demo posts
// and pages (en base rows + de/ru translation overlays), published and
// attributed to the default administrator (ADMIN_EMAIL). It is safe to run
// repeatedly — content is keyed by slug, so a re-run updates in place.
//
// It expects the auth baseline to already exist (run `seed` first, or start the
// server which seeds at boot) so the admin author is present.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/huseyn0w/cmstack-go/internal/content/demoseed"
	"github.com/huseyn0w/cmstack-go/internal/platform/config"
	"github.com/huseyn0w/cmstack-go/internal/platform/db"
	"github.com/huseyn0w/cmstack-go/internal/platform/db/sqlcgen"
	"github.com/huseyn0w/cmstack-go/internal/platform/logging"
)

func main() {
	if err := run(); err != nil {
		slog.Error("seed content failed", "err", err)
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

	// Resolve the administrator to attribute posts to.
	admin, err := queries.GetUserByEmail(ctx, cfg.AdminEmail)
	if err != nil {
		return fmt.Errorf("resolve admin author %q (run `seed` first?): %w", cfg.AdminEmail, err)
	}

	seeder := demoseed.NewSeeder(pool, queries)
	res, err := seeder.Seed(ctx, admin.ID)
	if err != nil {
		return err
	}

	logger.Info("demo content seed complete",
		"posts_created", res.PostsCreated,
		"posts_updated", res.PostsUpdated,
		"pages_created", res.PagesCreated,
		"pages_updated", res.PagesUpdated,
		"locales", res.Locales,
	)
	return nil
}
