package settings_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/huseyn0w/agentic-cms-go/internal/platform/db"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/db/sqlcgen"
	"github.com/huseyn0w/agentic-cms-go/internal/settings"
)

func migrationsDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve caller for migrations path")
	}
	root := filepath.Join(filepath.Dir(thisFile), "..", "..")
	return filepath.Join(root, "db", "migrations")
}

func startPostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	pgC, err := postgres.Run(
		ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("agentic_cms_test"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForListeningPort("5432/tcp").WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(pgC); err != nil {
			t.Logf("terminate container: %v", err)
		}
	})

	dsn, err := pgC.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("goose dialect: %v", err)
	}
	if err := goose.Up(sqlDB, migrationsDir(t)); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	_ = sqlDB.Close()

	pool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// TestRepoPG_UpsertGetAll exercises the sqlc-backed GetSetting/UpsertSetting/
// ListSettings against a real Postgres: an absent key maps to ErrNotFound, an
// upsert inserts then overwrites, and All() returns the full map.
func TestRepoPG_UpsertGetAll(t *testing.T) {
	pool := startPostgres(t)
	repo := settings.NewRepoPG(sqlcgen.New(pool))
	ctx := context.Background()

	// Absent key -> ErrNotFound.
	if _, err := repo.Get(ctx, "active_theme"); !errors.Is(err, settings.ErrNotFound) {
		t.Fatalf("Get(absent) err = %v, want ErrNotFound", err)
	}

	// Insert.
	if err := repo.Set(ctx, "active_theme", "sepia"); err != nil {
		t.Fatalf("Set insert: %v", err)
	}
	if v, err := repo.Get(ctx, "active_theme"); err != nil || v != "sepia" {
		t.Fatalf("Get after insert = (%q, %v), want (sepia, nil)", v, err)
	}

	// Overwrite (ON CONFLICT DO UPDATE).
	if err := repo.Set(ctx, "active_theme", "noir"); err != nil {
		t.Fatalf("Set overwrite: %v", err)
	}
	if v, _ := repo.Get(ctx, "active_theme"); v != "noir" {
		t.Fatalf("Get after overwrite = %q, want noir", v)
	}

	// A second key + All().
	if err := repo.Set(ctx, "site_tagline", "Quiet luxury"); err != nil {
		t.Fatalf("Set second: %v", err)
	}
	all, err := repo.All(ctx)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if all["active_theme"] != "noir" || all["site_tagline"] != "Quiet luxury" || len(all) != 2 {
		t.Fatalf("All() = %v, want {active_theme:noir, site_tagline:Quiet luxury}", all)
	}
}
