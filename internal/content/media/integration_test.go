package media_test

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
	"github.com/huseyn0w/cmstack-go/internal/content/media"
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
		ctx, "postgres:16-alpine",
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

type wiring struct {
	pool    *pgxpool.Pool
	repo    *media.RepoPG
	adminID uuid.UUID
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
		t.Fatalf("admin uuid: %v", err)
	}
	return wiring{pool: pool, repo: media.NewRepoPG(queries), adminID: admin.ID.Bytes}
}

func ptr[T any](v T) *T { return &v }

// createMedia inserts a media row (with optional thumbnails) directly via the
// repo in a tx, the path the service uses.
func createMedia(t *testing.T, w wiring, key string, width, height *int, variants ...media.CreateThumbnailData) media.Media {
	t.Helper()
	var created media.Media
	err := db.RunInTx(context.Background(), w.pool, func(ctx context.Context, tx pgx.Tx) error {
		m, err := w.repo.CreateTx(ctx, tx, media.CreateMediaData{
			StorageKey:       key,
			OriginalFilename: "photo.png",
			MIME:             "image/png",
			SizeBytes:        1234,
			Width:            width,
			Height:           height,
			Alt:              "",
			UploadedBy:       w.adminID,
		})
		if err != nil {
			return err
		}
		created = m
		for _, v := range variants {
			v.MediaID = m.ID
			th, err := w.repo.CreateThumbnailTx(ctx, tx, v)
			if err != nil {
				return err
			}
			created.Thumbnails = append(created.Thumbnails, th)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("create media: %v", err)
	}
	return created
}

func TestMediaCRUD(t *testing.T) {
	w := newWiring(t)
	ctx := context.Background()

	m := createMedia(
		t, w, "media/2026/06/abc.png", ptr(800), ptr(600),
		media.CreateThumbnailData{Variant: "thumb", StorageKey: "media/2026/06/thumb-abc.png", Width: 320, Height: 240},
		media.CreateThumbnailData{Variant: "medium", StorageKey: "media/2026/06/medium-abc.png", Width: 1024, Height: 768},
	)

	got, err := w.repo.GetByID(ctx, m.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Width == nil || *got.Width != 800 {
		t.Errorf("width = %v", got.Width)
	}
	if len(got.Thumbnails) != 2 {
		t.Fatalf("thumbnails = %d, want 2", len(got.Thumbnails))
	}
	if got.ThumbnailKey("thumb") != "media/2026/06/thumb-abc.png" {
		t.Errorf("thumb key = %q", got.ThumbnailKey("thumb"))
	}

	// Metadata update.
	upd, err := w.repo.UpdateMetadata(ctx, m.ID, "alt text", "My Title", "A caption")
	if err != nil {
		t.Fatalf("UpdateMetadata: %v", err)
	}
	if upd.Alt != "alt text" || upd.Title != "My Title" || upd.Caption != "A caption" {
		t.Errorf("metadata not persisted: %+v", upd)
	}
	// Thumbnails preserved across a metadata update.
	if len(upd.Thumbnails) != 2 {
		t.Errorf("thumbnails after update = %d, want 2", len(upd.Thumbnails))
	}
}

func TestMediaListPaginationNewestFirst(t *testing.T) {
	w := newWiring(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		createMedia(t, w, "media/k"+uuid.NewString()+".png", ptr(10), ptr(10))
		time.Sleep(2 * time.Millisecond) // distinct created_at ordering
	}

	total, err := w.repo.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if total != 5 {
		t.Fatalf("count = %d, want 5", total)
	}

	page1, err := w.repo.List(ctx, 2, 0)
	if err != nil {
		t.Fatalf("List p1: %v", err)
	}
	if len(page1) != 2 {
		t.Fatalf("page1 = %d, want 2", len(page1))
	}
	// Newest-first: page1[0].CreatedAt >= page1[1].CreatedAt.
	if page1[0].CreatedAt.Before(page1[1].CreatedAt) {
		t.Error("list is not newest-first")
	}

	page3, err := w.repo.List(ctx, 2, 4)
	if err != nil {
		t.Fatalf("List p3: %v", err)
	}
	if len(page3) != 1 {
		t.Errorf("page3 = %d, want 1 (the remainder)", len(page3))
	}
}

func TestMediaDeleteCascadesThumbnails(t *testing.T) {
	w := newWiring(t)
	ctx := context.Background()

	m := createMedia(
		t, w, "media/del.png", ptr(100), ptr(100),
		media.CreateThumbnailData{Variant: "thumb", StorageKey: "media/del-thumb.png", Width: 32, Height: 32},
	)
	// Thumbnails exist.
	if ts, _ := w.repo.ThumbnailsForMedia(ctx, m.ID); len(ts) != 1 {
		t.Fatalf("expected 1 thumbnail before delete, got %d", len(ts))
	}

	if err := w.repo.Delete(ctx, m.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := w.repo.GetByID(ctx, m.ID); err != media.ErrNotFound {
		t.Errorf("row should be gone, got %v", err)
	}
	// ON DELETE CASCADE removed the variant rows.
	if ts, _ := w.repo.ThumbnailsForMedia(ctx, m.ID); len(ts) != 0 {
		t.Errorf("thumbnails not cascaded: %d remain", len(ts))
	}
}

func TestThumbnailUpsert(t *testing.T) {
	w := newWiring(t)
	ctx := context.Background()
	m := createMedia(t, w, "media/up.png", ptr(50), ptr(50))

	// First insert.
	err := db.RunInTx(ctx, w.pool, func(ctx context.Context, tx pgx.Tx) error {
		_, e := w.repo.CreateThumbnailTx(ctx, tx, media.CreateThumbnailData{MediaID: m.ID, Variant: "thumb", StorageKey: "k1.png", Width: 1, Height: 1})
		return e
	})
	if err != nil {
		t.Fatalf("insert thumb: %v", err)
	}
	// Re-generate same variant -> upsert (not duplicate).
	err = db.RunInTx(ctx, w.pool, func(ctx context.Context, tx pgx.Tx) error {
		_, e := w.repo.CreateThumbnailTx(ctx, tx, media.CreateThumbnailData{MediaID: m.ID, Variant: "thumb", StorageKey: "k2.png", Width: 2, Height: 2})
		return e
	})
	if err != nil {
		t.Fatalf("upsert thumb: %v", err)
	}
	ts, _ := w.repo.ThumbnailsForMedia(ctx, m.ID)
	if len(ts) != 1 {
		t.Fatalf("expected 1 thumbnail after upsert, got %d", len(ts))
	}
	if ts[0].StorageKey != "k2.png" || ts[0].Width != 2 {
		t.Errorf("upsert did not replace: %+v", ts[0])
	}
}
