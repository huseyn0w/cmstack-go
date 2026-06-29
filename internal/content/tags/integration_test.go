package tags_test

import (
	"context"
	"database/sql"
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

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
	"github.com/huseyn0w/cmstack-go/internal/content/posts"
	"github.com/huseyn0w/cmstack-go/internal/content/tags"
	"github.com/huseyn0w/cmstack-go/internal/platform/db"
	"github.com/huseyn0w/cmstack-go/internal/platform/db/sqlcgen"
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
		testcontainers.WithWaitStrategy(wait.ForListeningPort("5432/tcp").WithStartupTimeout(60*time.Second)),
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

type wiring struct {
	pool     *pgxpool.Pool
	queries  *sqlcgen.Queries
	tagSvc   *tags.Service
	tagRepo  *tags.RepoPG
	postRepo *posts.RepoPG
	adminID  uuid.UUID
}

func newWiring(t *testing.T) wiring {
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
	admin, err := queries.GetUserByEmail(context.Background(), "admin@test.local")
	if err != nil {
		t.Fatalf("admin: %v", err)
	}
	authz := accounts.NewAuthorizer(users, roles)
	tagRepo := tags.NewRepoPG(queries)
	return wiring{
		pool:     pool,
		queries:  queries,
		tagSvc:   tags.NewService(pool, tagRepo, authz),
		tagRepo:  tagRepo,
		postRepo: posts.NewRepoPG(queries),
		adminID:  admin.ID.Bytes,
	}
}

func (w wiring) insertPost(t *testing.T, title, slug string, status kernel.Status) uuid.UUID {
	t.Helper()
	now := time.Now()
	var pubAt *time.Time
	if status == kernel.StatusPublished {
		pubAt = &now
	}
	var created posts.Post
	err := db.RunInTx(context.Background(), w.pool, func(ctx context.Context, tx pgx.Tx) error {
		p, err := w.postRepo.CreateTx(ctx, tx, posts.CreatePostData{
			Title: title, Slug: slug, Status: status, PublishedAt: pubAt, AuthorID: w.adminID,
		})
		created = p
		return err
	})
	if err != nil {
		t.Fatalf("insert post: %v", err)
	}
	return created.ID
}

func TestTagCRUDAndM2M(t *testing.T) {
	w := newWiring(t)
	ctx := context.Background()

	tag, err := w.tagSvc.Create(ctx, w.adminID, tags.CreateInput{Name: "Golang"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if got, err := w.tagRepo.GetBySlug(ctx, "golang"); err != nil || got.ID != tag.ID {
		t.Fatalf("getBySlug: %v / %v", got, err)
	}

	pubID := w.insertPost(t, "Pub", "pub", kernel.StatusPublished)
	draftID := w.insertPost(t, "Draft", "draft", kernel.StatusDraft)

	for _, pid := range []uuid.UUID{pubID, draftID} {
		err := db.RunInTx(ctx, w.pool, func(ctx context.Context, tx pgx.Tx) error {
			return w.tagSvc.AssignTx(ctx, tx, pid, []uuid.UUID{tag.ID})
		})
		if err != nil {
			t.Fatalf("assign %v: %v", pid, err)
		}
	}

	// posts-in-tag returns only the PUBLISHED post.
	ids, total, err := w.tagSvc.PublishedPostIDs(ctx, tag.ID, 10, 0)
	if err != nil {
		t.Fatalf("postsIn: %v", err)
	}
	if total != 1 || len(ids) != 1 || ids[0] != pubID {
		t.Fatalf("postsIn ids=%v total=%d, want [pub] 1 (draft excluded)", ids, total)
	}

	// Delete the tag: the join rows cascade.
	if err := w.tagSvc.Delete(ctx, w.adminID, tag.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if ts, _ := w.tagRepo.ListForPost(ctx, pubID); len(ts) != 0 {
		t.Fatalf("after tag delete listForPost = %d, want 0 (cascade)", len(ts))
	}
}
