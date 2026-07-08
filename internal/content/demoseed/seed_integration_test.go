package demoseed_test

import (
	"context"
	"database/sql"
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

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/content/demoseed"
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

// TestIntegration_DemoSeed verifies the demo content seeder inserts published
// posts + pages with de/ru overlays, is idempotent, and attributes posts to the
// seeded admin.
func TestIntegration_DemoSeed(t *testing.T) {
	pool := startPostgres(t)
	ctx := context.Background()
	queries := sqlcgen.New(pool)

	// Seed the auth baseline so the admin author exists.
	users := accounts.NewUserRepoPG(queries)
	roles := accounts.NewRoleRepoPG(queries)
	authSeeder := accounts.NewSeeder(pool, queries, users, roles, security.NewPasswordHasher())
	if err := authSeeder.Seed(ctx, accounts.AdminSeed{Email: "admin@test.local", Password: "password123"}); err != nil {
		t.Fatalf("auth seed: %v", err)
	}
	admin, err := queries.GetUserByEmail(ctx, "admin@test.local")
	if err != nil {
		t.Fatalf("get admin: %v", err)
	}

	seeder := demoseed.NewSeeder(pool, queries)

	// First run: everything is created.
	res, err := seeder.Seed(ctx, admin.ID)
	if err != nil {
		t.Fatalf("demo seed: %v", err)
	}
	if res.PostsCreated != 6 || res.PagesCreated != 2 {
		t.Fatalf("first run created posts=%d pages=%d, want 6/2", res.PostsCreated, res.PagesCreated)
	}

	// Base rows are published + attributed to the admin.
	var (
		postCount, pubPostCount, adminPostCount int
		pageCount                               int
	)
	if err := pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE status='PUBLISHED'), count(*) FILTER (WHERE author_id=$1) FROM posts`, admin.ID).
		Scan(&postCount, &pubPostCount, &adminPostCount); err != nil {
		t.Fatalf("count posts: %v", err)
	}
	if postCount != 6 || pubPostCount != 6 || adminPostCount != 6 {
		t.Errorf("posts total=%d published=%d byAdmin=%d, want 6/6/6", postCount, pubPostCount, adminPostCount)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pages WHERE status='PUBLISHED'`).Scan(&pageCount); err != nil {
		t.Fatalf("count pages: %v", err)
	}
	if pageCount != 2 {
		t.Errorf("published pages=%d, want 2", pageCount)
	}

	// de + ru overlays exist for every post and page (6*2 + 2*2).
	var postTr, pageTr int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM post_translations WHERE locale IN ('de','ru')`).Scan(&postTr); err != nil {
		t.Fatalf("count post_translations: %v", err)
	}
	if postTr != 12 {
		t.Errorf("post_translations=%d, want 12 (6 posts * de+ru)", postTr)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM page_translations WHERE locale IN ('de','ru')`).Scan(&pageTr); err != nil {
		t.Fatalf("count page_translations: %v", err)
	}
	if pageTr != 4 {
		t.Errorf("page_translations=%d, want 4 (2 pages * de+ru)", pageTr)
	}

	// Spot-check a ru overlay title.
	var ruTitle string
	if err := pool.QueryRow(ctx,
		`SELECT t.title FROM post_translations t JOIN posts p ON p.id=t.post_id
		 WHERE p.slug='introducing-the-cms' AND t.locale='ru'`).Scan(&ruTitle); err != nil {
		t.Fatalf("ru overlay lookup: %v", err)
	}
	if ruTitle == "" {
		t.Error("expected a non-empty ru title overlay for introducing-the-cms")
	}

	// Second run: idempotent — no new rows, everything updated in place.
	res2, err := seeder.Seed(ctx, admin.ID)
	if err != nil {
		t.Fatalf("demo seed re-run: %v", err)
	}
	if res2.PostsCreated != 0 || res2.PagesCreated != 0 {
		t.Errorf("re-run created posts=%d pages=%d, want 0/0", res2.PostsCreated, res2.PagesCreated)
	}
	if res2.PostsUpdated != 6 || res2.PagesUpdated != 2 {
		t.Errorf("re-run updated posts=%d pages=%d, want 6/2", res2.PostsUpdated, res2.PagesUpdated)
	}

	var postCountAfter int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM posts`).Scan(&postCountAfter); err != nil {
		t.Fatalf("recount posts: %v", err)
	}
	if postCountAfter != 6 {
		t.Errorf("posts after re-run=%d, want 6 (idempotent)", postCountAfter)
	}
}
