package pages_test

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
	"github.com/huseyn0w/cmstack-go/internal/content/pages"
	"github.com/huseyn0w/cmstack-go/internal/content/posts"
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

type pagesWiring struct {
	pool *pgxpool.Pool
	repo *pages.RepoPG
	svc  *pages.Service
}

func newPagesWiring(t *testing.T, now func() time.Time) (pagesWiring, uuid.UUID) {
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

	// Find the seeded administrator id (manage:all -> may manage pages).
	admin, err := users.GetByEmail(context.Background(), "admin@test.local")
	if err != nil {
		t.Fatalf("get admin: %v", err)
	}

	authz := accounts.NewAuthorizer(users, roles)
	repo := pages.NewRepoPG(queries)
	revisions := pages.NewRevisionRepoPG(queries)
	bus := events.NewBus(events.NewOutboxRepository())
	posts.NewPublishListener(slogDiscard(), nil, nil).Register(bus)
	svc := pages.NewService(pool, repo, revisions, authz, bus, now)

	return pagesWiring{pool: pool, repo: repo, svc: svc}, admin.ID
}

func TestIntegration_PageCRUD_HierarchyTrashRestore(t *testing.T) {
	w, actor := newPagesWiring(t, time.Now)
	ctx := context.Background()

	root, err := w.svc.Create(ctx, actor, pages.CreateInput{Title: "About", Body: "<p>hi</p><script>x</script>"})
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	if root.Slug != "about" || root.Body != "<p>hi</p>" {
		t.Errorf("root not as expected: slug=%q body=%q", root.Slug, root.Body)
	}

	child, err := w.svc.Create(ctx, actor, pages.CreateInput{Title: "Team", ParentID: &root.ID})
	if err != nil {
		t.Fatalf("create child: %v", err)
	}

	kids, err := w.repo.ListChildren(ctx, root.ID)
	if err != nil {
		t.Fatalf("list children: %v", err)
	}
	if len(kids) != 1 || kids[0].ID != child.ID {
		t.Fatalf("children = %v, want [%s]", kids, child.ID)
	}

	// Trash + restore round-trip.
	if err := w.svc.Trash(ctx, actor, child.ID); err != nil {
		t.Fatalf("trash: %v", err)
	}
	if _, err := w.repo.GetActiveByID(ctx, child.ID); err != pages.ErrNotFound {
		t.Errorf("trashed page should not be active, got %v", err)
	}
	if err := w.svc.Restore(ctx, actor, child.ID); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if _, err := w.repo.GetActiveByID(ctx, child.ID); err != nil {
		t.Errorf("restored page should be active, got %v", err)
	}
}

func TestIntegration_PageCyclePrevention(t *testing.T) {
	w, actor := newPagesWiring(t, time.Now)
	ctx := context.Background()

	root, _ := w.svc.Create(ctx, actor, pages.CreateInput{Title: "Root"})
	mid, _ := w.svc.Create(ctx, actor, pages.CreateInput{Title: "Mid", ParentID: &root.ID})
	leaf, _ := w.svc.Create(ctx, actor, pages.CreateInput{Title: "Leaf", ParentID: &mid.ID})

	// Root cannot become a child of its descendant leaf.
	if _, err := w.svc.Update(ctx, actor, root.ID, pages.UpdateInput{SetParent: true, ParentID: &leaf.ID}); err != pages.ErrParentCycle {
		t.Fatalf("descendant-as-parent = %v, want ErrParentCycle", err)
	}
	// Self-parent rejected.
	if _, err := w.svc.Update(ctx, actor, mid.ID, pages.UpdateInput{SetParent: true, ParentID: &mid.ID}); err != pages.ErrParentCycle {
		t.Fatalf("self-parent = %v, want ErrParentCycle", err)
	}
}

func TestIntegration_PagePublishAndRevisions(t *testing.T) {
	t0 := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	clock := &mutableClock{t: t0}
	w, actor := newPagesWiring(t, clock.now)
	ctx := context.Background()

	p, _ := w.svc.Create(ctx, actor, pages.CreateInput{Title: "Doc", Body: "<p>v1</p>"})
	pub, err := w.svc.Publish(ctx, actor, p.ID)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if pub.PublishedAt == nil || !pub.PublishedAt.Equal(t0) {
		t.Fatalf("publish stamp = %v, want %v", pub.PublishedAt, t0)
	}
	got, err := w.svc.PublicBySlug(ctx, "doc")
	if err != nil {
		t.Fatalf("public by slug: %v", err)
	}
	if got.ID != p.ID {
		t.Errorf("public page mismatch")
	}

	// Update body -> a revision is snapshotted. Publish above ALSO went through
	// Update (status change), so it snapshotted one too: 2 revisions total. The
	// newest (revs[0]) is the body-update snapshot, holding the pre-update body
	// "<p>v1</p>"; restoring it must reinstate v1.
	nb := "<p>v2</p>"
	if _, err := w.svc.Update(ctx, actor, p.ID, pages.UpdateInput{Body: &nb}); err != nil {
		t.Fatalf("update: %v", err)
	}
	revs, err := w.svc.Revisions(ctx, actor, p.ID)
	if err != nil {
		t.Fatalf("revisions: %v", err)
	}
	if len(revs) != 2 {
		t.Fatalf("want 2 revisions (publish + body update), got %d", len(revs))
	}
	restored, err := w.svc.RestoreRevision(ctx, actor, p.ID, revs[0].ID)
	if err != nil {
		t.Fatalf("restore revision: %v", err)
	}
	if restored.Body != "<p>v1</p>" {
		t.Errorf("restored body = %q, want <p>v1</p>", restored.Body)
	}
	if _, ok := kernelAssert(restored.Status); !ok {
		t.Errorf("restored status invalid: %v", restored.Status)
	}
}

func kernelAssert(s kernel.Status) (kernel.Status, bool) { return s, s.Valid() }

type mutableClock struct{ t time.Time }

func (c *mutableClock) now() time.Time { return c.t }

func slogDiscard() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
