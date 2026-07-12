package categories_test

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

	"github.com/huseyn0w/agentic-cms-go/internal/accounts"
	"github.com/huseyn0w/agentic-cms-go/internal/content/categories"
	"github.com/huseyn0w/agentic-cms-go/internal/content/kernel"
	"github.com/huseyn0w/agentic-cms-go/internal/content/posts"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/db"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/db/sqlcgen"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/security"
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

type wiring struct {
	pool     *pgxpool.Pool
	queries  *sqlcgen.Queries
	catSvc   *categories.Service
	catRepo  *categories.RepoPG
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

	authz := accounts.NewAuthorizer(users, roles)
	catRepo := categories.NewRepoPG(queries)
	catSvc := categories.NewService(pool, catRepo, authz)
	postRepo := posts.NewRepoPG(queries)

	return wiring{
		pool:     pool,
		queries:  queries,
		catSvc:   catSvc,
		catRepo:  catRepo,
		postRepo: postRepo,
		adminID:  adminUUID(t, queries),
	}
}

func adminUUID(t *testing.T, q *sqlcgen.Queries) uuid.UUID {
	t.Helper()
	u, err := q.GetUserByEmail(context.Background(), "admin@test.local")
	if err != nil {
		t.Fatalf("admin uuid: %v", err)
	}
	return u.ID.Bytes
}

// insertPost creates a published post directly via the repo for M2M tests.
func insertPost(t *testing.T, w wiring, title, slug string) uuid.UUID {
	t.Helper()
	now := time.Now()
	var created posts.Post
	err := db.RunInTx(context.Background(), w.pool, func(ctx context.Context, tx pgx.Tx) error {
		p, err := w.postRepo.CreateTx(ctx, tx, posts.CreatePostData{
			Title:       title,
			Slug:        slug,
			Status:      kernel.StatusPublished,
			PublishedAt: &now,
			AuthorID:    w.adminID,
		})
		created = p
		return err
	})
	if err != nil {
		t.Fatalf("insert post: %v", err)
	}
	return created.ID
}

func TestCategoryCRUDAndTree(t *testing.T) {
	w := newWiring(t)
	ctx := context.Background()

	root, err := w.catSvc.Create(ctx, w.adminID, categories.CreateInput{Name: "Engineering"})
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	child, err := w.catSvc.Create(ctx, w.adminID, categories.CreateInput{Name: "Backend", ParentID: &root.ID})
	if err != nil {
		t.Fatalf("create child: %v", err)
	}

	// GetBySlug round-trips.
	got, err := w.catRepo.GetBySlug(ctx, "engineering")
	if err != nil || got.ID != root.ID {
		t.Fatalf("getBySlug: %v / %v", got, err)
	}

	// Children query returns the child.
	kids, err := w.catRepo.ListChildren(ctx, root.ID)
	if err != nil {
		t.Fatalf("children: %v", err)
	}
	if len(kids) != 1 || kids[0].ID != child.ID {
		t.Fatalf("children = %v, want [backend]", kids)
	}

	// Tree is depth-ordered.
	tree, err := w.catSvc.Tree(ctx)
	if err != nil {
		t.Fatalf("tree: %v", err)
	}
	if len(tree) != 2 || tree[0].Depth != 0 || tree[1].Depth != 1 {
		t.Fatalf("tree depths = %+v", tree)
	}

	// Delete root: child must be detached (parent set NULL), not deleted.
	if err := w.catSvc.Delete(ctx, w.adminID, root.ID); err != nil {
		t.Fatalf("delete root: %v", err)
	}
	c2, err := w.catRepo.GetByID(ctx, child.ID)
	if err != nil {
		t.Fatalf("child after root delete: %v", err)
	}
	if c2.ParentID != nil {
		t.Fatalf("child parent = %v, want nil (detached)", c2.ParentID)
	}
}

func TestCategoryCyclePreventionInDB(t *testing.T) {
	w := newWiring(t)
	ctx := context.Background()
	root, _ := w.catSvc.Create(ctx, w.adminID, categories.CreateInput{Name: "A"})
	child, _ := w.catSvc.Create(ctx, w.adminID, categories.CreateInput{Name: "B", ParentID: &root.ID})

	_, err := w.catSvc.Update(ctx, w.adminID, root.ID, categories.UpdateInput{SetParent: true, ParentID: &child.ID})
	if err != categories.ErrParentCycle {
		t.Fatalf("err = %v, want ErrParentCycle", err)
	}
}

func TestCategoryM2MAttachDetachAndPostsIn(t *testing.T) {
	w := newWiring(t)
	ctx := context.Background()

	cat, _ := w.catSvc.Create(ctx, w.adminID, categories.CreateInput{Name: "News"})
	postID := insertPost(t, w, "Hello", "hello")

	// Attach via the M2M assign seam (own tx).
	err := db.RunInTx(ctx, w.pool, func(ctx context.Context, tx pgx.Tx) error {
		return w.catSvc.AssignTx(ctx, tx, postID, []uuid.UUID{cat.ID})
	})
	if err != nil {
		t.Fatalf("assign: %v", err)
	}

	cats, err := w.catRepo.ListForPost(ctx, postID)
	if err != nil || len(cats) != 1 || cats[0].ID != cat.ID {
		t.Fatalf("listForPost = %v / %v", cats, err)
	}

	ids, total, err := w.catSvc.PublishedPostIDs(ctx, cat.ID, 10, 0)
	if err != nil {
		t.Fatalf("postsIn: %v", err)
	}
	if total != 1 || len(ids) != 1 || ids[0] != postID {
		t.Fatalf("postsIn ids=%v total=%d, want [post] 1", ids, total)
	}

	// Detach (assign empty) removes the association.
	err = db.RunInTx(ctx, w.pool, func(ctx context.Context, tx pgx.Tx) error {
		return w.catSvc.AssignTx(ctx, tx, postID, nil)
	})
	if err != nil {
		t.Fatalf("detach: %v", err)
	}
	cats, _ = w.catRepo.ListForPost(ctx, postID)
	if len(cats) != 0 {
		t.Fatalf("after detach len = %d, want 0", len(cats))
	}
}
