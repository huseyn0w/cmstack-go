package posts_test

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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/huseyn0w/agentic-cms-go/internal/accounts"
	"github.com/huseyn0w/agentic-cms-go/internal/content/kernel"
	"github.com/huseyn0w/agentic-cms-go/internal/content/posts"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/db"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/db/sqlcgen"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/events"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/security"
)

func migrationsDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve caller for migrations path")
	}
	// internal/content/posts/integration_test.go -> repo root is 4 up.
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

// postsWiring is a real-DB stack for the posts integration tests.
type postsWiring struct {
	pool      *pgxpool.Pool
	queries   *sqlcgen.Queries
	repo      *posts.RepoPG
	revisions *posts.RevisionRepoPG
	svc       *posts.Service
	users     *accounts.UserRepoPG
	roles     *accounts.RoleRepoPG
}

func newPostsWiring(t *testing.T, now func() time.Time) postsWiring {
	t.Helper()
	pool := startPostgres(t)
	queries := sqlcgen.New(pool)

	users := accounts.NewUserRepoPG(queries)
	roles := accounts.NewRoleRepoPG(queries)
	hasher := security.NewPasswordHasher()

	// Seed roles/permissions + an administrator so the authorizer has grants.
	seeder := accounts.NewSeeder(pool, queries, users, roles, hasher)
	if err := seeder.Seed(context.Background(), accounts.AdminSeed{Email: "admin@test.local", Password: "password123"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	authz := accounts.NewAuthorizer(users, roles)
	repo := posts.NewRepoPG(queries)
	revisions := posts.NewRevisionRepoPG(queries)
	roleKeys := posts.NewRoleKeyResolver(users, roles)
	bus := events.NewBus(events.NewOutboxRepository())
	// Register the publish listener so content.published is marked async and
	// enqueued to the outbox inside the publishing transaction.
	posts.NewPublishListener(slogDiscard(), nil, nil).Register(bus)
	svc := posts.NewService(pool, repo, revisions, authz, roleKeys, bus, now)

	return postsWiring{pool: pool, queries: queries, repo: repo, revisions: revisions, svc: svc, users: users, roles: roles}
}

// createUser inserts a user with the given role key and returns its id.
func (w postsWiring) createUser(t *testing.T, email, roleKey string) uuid.UUID {
	t.Helper()
	role, err := w.roles.GetByKey(context.Background(), roleKey)
	if err != nil {
		t.Fatalf("get role %s: %v", roleKey, err)
	}
	hasher := security.NewPasswordHasher()
	hash, _ := hasher.Hash("password123")
	var id uuid.UUID
	err = db.RunInTx(context.Background(), w.pool, func(ctx context.Context, tx pgx.Tx) error {
		u, err := w.users.CreateTx(ctx, tx, accounts.CreateUserInput{
			Email: email, PasswordHash: hash, Name: email, RoleID: role.ID,
		})
		if err != nil {
			return err
		}
		id = u.ID
		return nil
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return id
}

func TestIntegration_PostCRUD_TrashRestore(t *testing.T) {
	w := newPostsWiring(t, time.Now)
	ctx := context.Background()
	author := w.createUser(t, "author1@test.local", accounts.RoleAuthor)

	p, err := w.svc.Create(ctx, author, posts.CreateInput{Title: "Hello World", Body: "<p>hi</p><script>x</script>"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if p.Slug != "hello-world" {
		t.Errorf("slug = %q", p.Slug)
	}
	if p.Body != "<p>hi</p>" {
		t.Errorf("body not sanitized in DB: %q", p.Body)
	}

	// Trash + restore round-trip.
	if err := w.svc.Trash(ctx, author, p.ID); err != nil {
		t.Fatalf("trash: %v", err)
	}
	if _, err := w.repo.GetActiveByID(ctx, p.ID); err != posts.ErrNotFound {
		t.Errorf("trashed post should not be active, got %v", err)
	}
	if err := w.svc.Restore(ctx, author, p.ID); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if _, err := w.repo.GetActiveByID(ctx, p.ID); err != nil {
		t.Errorf("restored post should be active, got %v", err)
	}
}

func TestIntegration_PublishOncePreserve(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := &mutableClock{t: t0}
	w := newPostsWiring(t, clock.now)
	ctx := context.Background()
	author := w.createUser(t, "author2@test.local", accounts.RoleAuthor)

	p, _ := w.svc.Create(ctx, author, posts.CreateInput{Title: "P", Body: "<p>b</p>"})
	pub, err := w.svc.Publish(ctx, author, p.ID)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if pub.PublishedAt == nil || !pub.PublishedAt.Equal(t0) {
		t.Fatalf("first publish stamp = %v, want %v", pub.PublishedAt, t0)
	}

	clock.t = t0.Add(72 * time.Hour)
	if _, err := w.svc.Unpublish(ctx, author, p.ID); err != nil {
		t.Fatalf("unpublish: %v", err)
	}
	re, err := w.svc.Publish(ctx, author, p.ID)
	if err != nil {
		t.Fatalf("republish: %v", err)
	}
	if re.PublishedAt == nil || !re.PublishedAt.Equal(t0) {
		t.Errorf("re-publish published_at = %v, want preserved %v", re.PublishedAt, t0)
	}
}

func TestIntegration_LikesAndCount(t *testing.T) {
	w := newPostsWiring(t, time.Now)
	ctx := context.Background()
	author := w.createUser(t, "author3@test.local", accounts.RoleAuthor)
	liker := w.createUser(t, "liker@test.local", accounts.RoleMember)

	p, _ := w.svc.Create(ctx, author, posts.CreateInput{Title: "Likeable", Body: "<p>b</p>", Status: kernel.StatusPublished})

	// Idempotent like.
	for i := 0; i < 2; i++ {
		got, err := w.svc.Like(ctx, p.ID, liker)
		if err != nil {
			t.Fatalf("like: %v", err)
		}
		if got.LikeCount != 1 {
			t.Fatalf("like count = %d, want 1", got.LikeCount)
		}
	}
	un, err := w.svc.Unlike(ctx, p.ID, liker)
	if err != nil {
		t.Fatalf("unlike: %v", err)
	}
	if un.LikeCount != 0 {
		t.Errorf("after unlike count = %d, want 0", un.LikeCount)
	}
}

func TestIntegration_RevisionSnapshotAndRestore(t *testing.T) {
	w := newPostsWiring(t, time.Now)
	ctx := context.Background()
	author := w.createUser(t, "author4@test.local", accounts.RoleAuthor)

	p, _ := w.svc.Create(ctx, author, posts.CreateInput{Title: "V1", Body: "<p>one</p>"})
	nt, nb := "V2", "<p>two</p>"
	if _, err := w.svc.Update(ctx, author, p.ID, posts.UpdateInput{Title: &nt, Body: &nb}); err != nil {
		t.Fatalf("update: %v", err)
	}

	revs, err := w.svc.Revisions(ctx, author, p.ID)
	if err != nil {
		t.Fatalf("revisions: %v", err)
	}
	if len(revs) != 1 {
		t.Fatalf("want 1 revision, got %d", len(revs))
	}

	restored, err := w.svc.RestoreRevision(ctx, author, p.ID, revs[0].ID)
	if err != nil {
		t.Fatalf("restore revision: %v", err)
	}
	if restored.Title != "V1" || restored.Body != "<p>one</p>" {
		t.Errorf("restored title=%q body=%q, want V1/<p>one</p>", restored.Title, restored.Body)
	}
	// Restore itself snapshots -> 2 revisions now.
	revs2, _ := w.svc.Revisions(ctx, author, p.ID)
	if len(revs2) != 2 {
		t.Errorf("want 2 revisions after restore, got %d", len(revs2))
	}
}

func TestIntegration_OwnershipDenial(t *testing.T) {
	w := newPostsWiring(t, time.Now)
	ctx := context.Background()
	owner := w.createUser(t, "owner@test.local", accounts.RoleAuthor)
	intruder := w.createUser(t, "intruder@test.local", accounts.RoleAuthor)
	editor := w.createUser(t, "editor@test.local", accounts.RoleEditor)

	p, _ := w.svc.Create(ctx, owner, posts.CreateInput{Title: "Owned", Body: "<p>b</p>"})

	nt := "hijack"
	if _, err := w.svc.Update(ctx, intruder, p.ID, posts.UpdateInput{Title: &nt}); err != posts.ErrForbidden {
		t.Fatalf("author editing another's post = %v, want ErrForbidden", err)
	}
	// Editor may edit any post.
	nt2 := "edited"
	if _, err := w.svc.Update(ctx, editor, p.ID, posts.UpdateInput{Title: &nt2}); err != nil {
		t.Fatalf("editor edit = %v, want nil", err)
	}
}

func TestIntegration_ScheduledDueQueryAndPublishDue(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	clock := &mutableClock{t: now}
	w := newPostsWiring(t, clock.now)
	ctx := context.Background()
	author := w.createUser(t, "author5@test.local", accounts.RoleAuthor)

	// One due (past), one future.
	due, _ := w.svc.Create(ctx, author, posts.CreateInput{Title: "Due", Body: "<p>b</p>"})
	if _, err := w.svc.Schedule(ctx, author, due.ID, now.Add(-time.Minute)); err != nil {
		t.Fatalf("schedule due: %v", err)
	}
	future, _ := w.svc.Create(ctx, author, posts.CreateInput{Title: "Future", Body: "<p>b</p>"})
	if _, err := w.svc.Schedule(ctx, author, future.ID, now.Add(time.Hour)); err != nil {
		t.Fatalf("schedule future: %v", err)
	}

	// The repo due-query returns only the due id.
	ids, err := w.repo.ListDueScheduledIDs(ctx, now)
	if err != nil {
		t.Fatalf("due query: %v", err)
	}
	if len(ids) != 1 || ids[0] != due.ID {
		t.Fatalf("due ids = %v, want [%s]", ids, due.ID)
	}

	// PublishDue auto-publishes the due post (scheduler integration).
	n, err := w.svc.PublishDue(ctx)
	if err != nil {
		t.Fatalf("publishDue: %v", err)
	}
	if n != 1 {
		t.Fatalf("published = %d, want 1", n)
	}
	gotDue, _ := w.repo.GetByID(ctx, due.ID)
	if !gotDue.Published() || gotDue.ScheduledAt != nil {
		t.Errorf("due post not auto-published cleanly: status=%s scheduledAt=%v", gotDue.Status, gotDue.ScheduledAt)
	}
	gotFuture, _ := w.repo.GetByID(ctx, future.ID)
	if gotFuture.Published() {
		t.Errorf("future post should not have published yet")
	}

	// The async content.published event was enqueued to the outbox in-tx.
	assertOutboxHas(t, w.pool, posts.EventContentPublished)
}

func assertOutboxHas(t *testing.T, pool *pgxpool.Pool, eventName string) {
	t.Helper()
	var count int
	row := pool.QueryRow(context.Background(),
		"SELECT count(*) FROM outbox WHERE event_name = $1", eventName)
	if err := row.Scan(&count); err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	if count < 1 {
		t.Errorf("expected at least one %q outbox row, got %d", eventName, count)
	}
}

// mutableClock is an adjustable clock for publish-once-preserve assertions.
type mutableClock struct{ t time.Time }

func (c *mutableClock) now() time.Time { return c.t }

func slogDiscard() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
