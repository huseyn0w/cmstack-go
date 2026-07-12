package menus_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/huseyn0w/agentic-cms-go/internal/content/menus"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/db"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/db/sqlcgen"
)

func migrationsDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve caller for migrations path")
	}
	root := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
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

func newRepo(t *testing.T) (*menus.RepoPG, *pgxpool.Pool) {
	t.Helper()
	pool := startPostgres(t)
	repo := menus.NewRepoPG(pool, sqlcgen.New(pool))
	return repo, pool
}

// TestRepoPG_LocationUnique proves the partial-unique index rejects a second
// menu at the same assigned location (mapped to ErrLocationTaken), while any
// number of unassigned menus is allowed.
func TestRepoPG_LocationUnique(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()

	if _, err := repo.CreateMenu(ctx, "Primary", "header"); err != nil {
		t.Fatalf("create primary: %v", err)
	}
	if _, err := repo.CreateMenu(ctx, "Secondary", "header"); err != menus.ErrLocationTaken {
		t.Fatalf("want ErrLocationTaken on duplicate location, got %v", err)
	}
	// Unassigned menus never collide.
	if _, err := repo.CreateMenu(ctx, "A", ""); err != nil {
		t.Fatalf("create unassigned A: %v", err)
	}
	if _, err := repo.CreateMenu(ctx, "B", ""); err != nil {
		t.Fatalf("create unassigned B: %v", err)
	}

	got, err := repo.MenuByLocation(ctx, "header")
	if err != nil {
		t.Fatalf("menu by location: %v", err)
	}
	if got.Name != "Primary" {
		t.Fatalf("want Primary, got %q", got.Name)
	}
}

// TestRepoPG_SetPositionsAndOverlay proves SetPositions reassigns position by
// index in a single tx and ListItemsInLocale overlays the label with per-locale
// fallback to the base label, ordered by position.
func TestRepoPG_SetPositionsAndOverlay(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()

	menu, err := repo.CreateMenu(ctx, "Primary", "header")
	if err != nil {
		t.Fatalf("create menu: %v", err)
	}

	a, err := repo.AddItem(ctx, menus.CreateItemData{MenuID: menu.ID, Position: 0, Type: menus.ItemPage, URL: "/", Label: "Home"})
	if err != nil {
		t.Fatalf("add a: %v", err)
	}
	b, err := repo.AddItem(ctx, menus.CreateItemData{MenuID: menu.ID, Position: 1, Type: menus.ItemPage, URL: "/about", Label: "About"})
	if err != nil {
		t.Fatalf("add b: %v", err)
	}

	// Reorder to b, a.
	if err := repo.SetPositions(ctx, menu.ID, []uuid.UUID{b.ID, a.ID}); err != nil {
		t.Fatalf("set positions: %v", err)
	}
	items, err := repo.ListItems(ctx, menu.ID)
	if err != nil {
		t.Fatalf("list items: %v", err)
	}
	if len(items) != 2 || items[0].ID != b.ID || items[1].ID != a.ID {
		t.Fatalf("reorder failed: %+v", items)
	}
	if items[0].Position != 0 || items[1].Position != 1 {
		t.Fatalf("positions not reassigned: %+v", items)
	}

	// German label for "a" only; overlay read returns it, "b" falls back to base.
	if err := repo.UpsertItemTranslation(ctx, a.ID, "de", "Startseite"); err != nil {
		t.Fatalf("upsert translation: %v", err)
	}
	overlaid, err := repo.ListItemsInLocale(ctx, menu.ID, "de")
	if err != nil {
		t.Fatalf("overlay read: %v", err)
	}
	// Ordered by position: b (About, no de) then a (Startseite).
	if overlaid[0].Label != "About" {
		t.Fatalf("base fallback want About, got %q", overlaid[0].Label)
	}
	if overlaid[1].Label != "Startseite" {
		t.Fatalf("overlay want Startseite, got %q", overlaid[1].Label)
	}

	locales, err := repo.ItemTranslatedLocales(ctx, a.ID)
	if err != nil {
		t.Fatalf("translated locales: %v", err)
	}
	if len(locales) != 1 || locales[0] != "de" {
		t.Fatalf("want [de], got %v", locales)
	}
}
