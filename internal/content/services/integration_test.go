package services_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
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

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
	"github.com/huseyn0w/cmstack-go/internal/content/posts"
	"github.com/huseyn0w/cmstack-go/internal/content/services"
	"github.com/huseyn0w/cmstack-go/internal/platform/db"
	"github.com/huseyn0w/cmstack-go/internal/platform/db/sqlcgen"
	"github.com/huseyn0w/cmstack-go/internal/platform/events"
	"github.com/huseyn0w/cmstack-go/internal/platform/security"
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
		postgres.WithDatabase("cmstack_test"),
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

type servicesWiring struct {
	pool *pgxpool.Pool
	repo *services.RepoPG
	mgr  *services.Manager
}

func newServicesWiring(t *testing.T, now func() time.Time) (servicesWiring, uuid.UUID) {
	t.Helper()
	pool := startPostgres(t)
	queries := sqlcgen.New(pool)

	users := accounts.NewUserRepoPG(queries)
	roles := accounts.NewRoleRepoPG(queries)
	hasher := security.NewPasswordHasher()
	seeder := accounts.NewSeeder(pool, queries, users, roles, hasher)
	if err := seeder.Seed(context.Background(), accounts.AdminSeed{Email: "admin@test.local", Password: "password123"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	admin, err := users.GetByEmail(context.Background(), "admin@test.local")
	if err != nil {
		t.Fatalf("get admin: %v", err)
	}

	authz := accounts.NewAuthorizer(users, roles)
	repo := services.NewRepoPG(queries)
	revisions := services.NewRevisionRepoPG(queries)
	bus := events.NewBus(events.NewOutboxRepository())
	posts.NewPublishListener(slogDiscard(), nil, nil).Register(bus)
	mgr := services.NewManager(pool, repo, revisions, authz, bus, now)

	return servicesWiring{pool: pool, repo: repo, mgr: mgr}, admin.ID
}

func TestIntegration_ServiceCRUD_FAQOrdering(t *testing.T) {
	w, actor := newServicesWiring(t, time.Now)
	ctx := context.Background()

	svc, err := w.mgr.Create(ctx, actor, services.CreateInput{
		Title:   "SEO Audit",
		Summary: "We audit <b>your</b> site.",
		Body:    "<p>Details</p><script>x</script>",
		Price:   "From $499",
		FAQs: []services.FAQInput{
			{Question: "Q1", Answer: "<p>A1</p>"},
			{Question: "Q2", Answer: "<p>A2</p><script>y</script>"},
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if svc.Slug != "seo-audit" || svc.Body != "<p>Details</p>" {
		t.Errorf("service not sanitized/slugged: slug=%q body=%q", svc.Slug, svc.Body)
	}
	if svc.Summary != "We audit your site." {
		t.Errorf("summary not plain-sanitized: %q", svc.Summary)
	}

	faqs, err := w.repo.ListFAQs(ctx, svc.ID)
	if err != nil {
		t.Fatalf("list faqs: %v", err)
	}
	if len(faqs) != 2 || faqs[0].Question != "Q1" || faqs[1].Question != "Q2" {
		t.Fatalf("FAQ order wrong: %+v", faqs)
	}
	if faqs[1].Answer != "<p>A2</p>" {
		t.Errorf("FAQ answer not sanitized: %q", faqs[1].Answer)
	}

	// Reorder + drop one via update.
	updated, err := w.mgr.Update(ctx, actor, svc.ID, services.UpdateInput{
		SetFAQs: true,
		FAQs: []services.FAQInput{
			{Question: "Q2", Answer: "<p>A2</p>"},
			{Question: "Q1", Answer: "<p>A1</p>"},
		},
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if len(updated.FAQs) != 2 || updated.FAQs[0].Question != "Q2" || updated.FAQs[0].Position != 0 {
		t.Fatalf("reorder wrong: %+v", updated.FAQs)
	}
}

func TestIntegration_ServiceTrashRestoreAndPublic(t *testing.T) {
	t0 := time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)
	clock := &mutableClock{t: t0}
	w, actor := newServicesWiring(t, clock.now)
	ctx := context.Background()

	svc, _ := w.mgr.Create(ctx, actor, services.CreateInput{
		Title: "Consulting", Status: kernel.StatusPublished,
		FAQs: []services.FAQInput{{Question: "Q", Answer: "<p>A</p>"}},
	})
	// Public read returns it with FAQs.
	got, err := w.mgr.PublicBySlug(ctx, "consulting")
	if err != nil {
		t.Fatalf("public: %v", err)
	}
	if len(got.FAQs) != 1 {
		t.Errorf("public service missing FAQs: %d", len(got.FAQs))
	}

	// Trash + restore round-trip.
	if err := w.mgr.Trash(ctx, actor, svc.ID); err != nil {
		t.Fatalf("trash: %v", err)
	}
	if _, err := w.mgr.PublicBySlug(ctx, "consulting"); err != services.ErrNotFound {
		t.Errorf("trashed service should be unpublishable, got %v", err)
	}
	if err := w.mgr.Restore(ctx, actor, svc.ID); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if _, err := w.repo.GetActiveByID(ctx, svc.ID); err != nil {
		t.Errorf("restored service should be active, got %v", err)
	}

	// Permanent delete cascades FAQs (no orphan rows).
	if err := w.mgr.PermanentDelete(ctx, actor, svc.ID); err != nil {
		t.Fatalf("permanent delete: %v", err)
	}
	if _, err := w.repo.GetByID(ctx, svc.ID); err != services.ErrNotFound {
		t.Errorf("hard-deleted service should be gone, got %v", err)
	}
}

type mutableClock struct{ t time.Time }

func (c *mutableClock) now() time.Time { return c.t }

func slogDiscard() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
