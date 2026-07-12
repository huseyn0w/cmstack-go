package comments_test

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
	"github.com/huseyn0w/agentic-cms-go/internal/content/comments"
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
		ctx, "postgres:16-alpine",
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
	pool    *pgxpool.Pool
	repo    *comments.RepoPG
	adminID uuid.UUID
	postID  uuid.UUID
}

func newWiring(t *testing.T) wiring {
	t.Helper()
	ctx := context.Background()
	pool := startPostgres(t)
	queries := sqlcgen.New(pool)

	users := accounts.NewUserRepoPG(queries)
	roles := accounts.NewRoleRepoPG(queries)
	hasher := security.NewPasswordHasher()
	seeder := accounts.NewSeeder(pool, queries, users, roles, hasher)
	if err := seeder.Seed(ctx, accounts.AdminSeed{Email: "admin@test.local", Password: "password123"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	admin, err := queries.GetUserByEmail(ctx, "admin@test.local")
	if err != nil {
		t.Fatalf("admin uuid: %v", err)
	}
	adminID := uuid.UUID(admin.ID.Bytes)

	// Insert a published post directly (FK target for comments) to avoid wiring
	// the full posts service in this repo-focused test.
	postID := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO posts (id, title, slug, body, status, author_id, published_at)
		VALUES ($1, 'Test Post', 'test-post', '<p>b</p>', 'PUBLISHED', $2, now())`,
		postID, admin.ID)
	if err != nil {
		t.Fatalf("insert post: %v", err)
	}

	return wiring{pool: pool, repo: comments.NewRepoPG(queries), adminID: adminID, postID: postID}
}

// create inserts a comment through the repo in a tx, the path the service uses.
func (w wiring) create(t *testing.T, in comments.CreateCommentData) comments.Comment {
	t.Helper()
	var out comments.Comment
	err := db.RunInTx(context.Background(), w.pool, func(ctx context.Context, tx pgx.Tx) error {
		c, err := w.repo.CreateTx(ctx, tx, in)
		if err != nil {
			return err
		}
		out = c
		return nil
	})
	if err != nil {
		t.Fatalf("create comment: %v", err)
	}
	return out
}

func ptr[T any](v T) *T { return &v }

func TestIntegration_CreateAndGet(t *testing.T) {
	w := newWiring(t)
	ctx := context.Background()

	c := w.create(t, comments.CreateCommentData{
		PostID:      w.postID,
		AuthorName:  "Guest",
		AuthorEmail: "g@x.com",
		AuthorIP:    "1.2.3.4",
		Body:        "hello",
		Status:      comments.StatusPending,
	})
	if c.ID == uuid.Nil {
		t.Fatal("no id assigned")
	}
	got, err := w.repo.GetByID(ctx, c.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Body != "hello" || got.Status != comments.StatusPending {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
	if got.AuthorEmail != "g@x.com" || got.AuthorIP != "1.2.3.4" {
		t.Fatalf("PII not persisted: %+v", got)
	}
}

func TestIntegration_ApprovedOnlyTreeFetch(t *testing.T) {
	w := newWiring(t)
	ctx := context.Background()

	approved := w.create(t, comments.CreateCommentData{PostID: w.postID, AuthorName: "A", Body: "approved", Status: comments.StatusApproved})
	w.create(t, comments.CreateCommentData{PostID: w.postID, AuthorName: "P", Body: "pending", Status: comments.StatusPending})
	w.create(t, comments.CreateCommentData{PostID: w.postID, AuthorName: "S", Body: "spam", Status: comments.StatusSpam})

	list, err := w.repo.ListApprovedForPost(ctx, w.postID)
	if err != nil {
		t.Fatalf("list approved: %v", err)
	}
	if len(list) != 1 || list[0].ID != approved.ID {
		t.Fatalf("expected only the approved comment, got %d", len(list))
	}
}

func TestIntegration_GetApprovedByID_Threading(t *testing.T) {
	w := newWiring(t)
	ctx := context.Background()

	approved := w.create(t, comments.CreateCommentData{PostID: w.postID, AuthorName: "A", Body: "ok", Status: comments.StatusApproved})
	pending := w.create(t, comments.CreateCommentData{PostID: w.postID, AuthorName: "P", Body: "no", Status: comments.StatusPending})

	if _, err := w.repo.GetApprovedByID(ctx, approved.ID, w.postID); err != nil {
		t.Fatalf("approved parent should resolve: %v", err)
	}
	if _, err := w.repo.GetApprovedByID(ctx, pending.ID, w.postID); err != comments.ErrNotFound {
		t.Fatalf("non-approved parent err = %v, want ErrNotFound", err)
	}
	if _, err := w.repo.GetApprovedByID(ctx, approved.ID, uuid.New()); err != comments.ErrNotFound {
		t.Fatalf("wrong-post parent err = %v, want ErrNotFound", err)
	}
}

func TestIntegration_ModerationListByStatus(t *testing.T) {
	w := newWiring(t)
	ctx := context.Background()

	w.create(t, comments.CreateCommentData{PostID: w.postID, AuthorName: "A", Body: "1", Status: comments.StatusPending})
	w.create(t, comments.CreateCommentData{PostID: w.postID, AuthorName: "B", Body: "2", Status: comments.StatusPending})
	w.create(t, comments.CreateCommentData{PostID: w.postID, AuthorName: "C", Body: "3", Status: comments.StatusApproved})

	pending := comments.StatusPending
	list, err := w.repo.ListForModeration(ctx, comments.ModerationFilter{Status: &pending, Limit: 50})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("pending list = %d, want 2", len(list))
	}
	total, err := w.repo.CountForModeration(ctx, &pending)
	if err != nil || total != 2 {
		t.Fatalf("count pending = %d err=%v", total, err)
	}
	all, err := w.repo.CountForModeration(ctx, nil)
	if err != nil || all != 3 {
		t.Fatalf("count all = %d err=%v", all, err)
	}
}

func TestIntegration_StatusUpdate(t *testing.T) {
	w := newWiring(t)
	ctx := context.Background()
	c := w.create(t, comments.CreateCommentData{PostID: w.postID, AuthorName: "A", Body: "x", Status: comments.StatusPending})

	err := db.RunInTx(ctx, w.pool, func(ctx context.Context, tx pgx.Tx) error {
		_, err := w.repo.UpdateStatusTx(ctx, tx, c.ID, comments.StatusApproved)
		return err
	})
	if err != nil {
		t.Fatalf("update status: %v", err)
	}
	got, _ := w.repo.GetByID(ctx, c.ID)
	if got.Status != comments.StatusApproved {
		t.Fatalf("status = %s, want APPROVED", got.Status)
	}
}

func TestIntegration_SelfEditSetsEditedAt(t *testing.T) {
	w := newWiring(t)
	ctx := context.Background()
	c := w.create(t, comments.CreateCommentData{PostID: w.postID, AuthorUserID: ptr(w.adminID), AuthorName: "A", Body: "old", Status: comments.StatusApproved})

	err := db.RunInTx(ctx, w.pool, func(ctx context.Context, tx pgx.Tx) error {
		_, err := w.repo.UpdateBodyTx(ctx, tx, c.ID, "new body", comments.StatusPending)
		return err
	})
	if err != nil {
		t.Fatalf("update body: %v", err)
	}
	got, _ := w.repo.GetByID(ctx, c.ID)
	if got.Body != "new body" {
		t.Fatalf("body = %q", got.Body)
	}
	if got.EditedAt == nil {
		t.Fatal("edited_at not set")
	}
	if got.Status != comments.StatusPending {
		t.Fatalf("status = %s, want PENDING", got.Status)
	}
}

func TestIntegration_CountsByStatus(t *testing.T) {
	w := newWiring(t)
	ctx := context.Background()
	w.create(t, comments.CreateCommentData{PostID: w.postID, AuthorName: "A", Body: "1", Status: comments.StatusPending})
	w.create(t, comments.CreateCommentData{PostID: w.postID, AuthorName: "B", Body: "2", Status: comments.StatusPending})
	w.create(t, comments.CreateCommentData{PostID: w.postID, AuthorName: "C", Body: "3", Status: comments.StatusApproved})
	w.create(t, comments.CreateCommentData{PostID: w.postID, AuthorName: "D", Body: "4", Status: comments.StatusSpam})

	counts, err := w.repo.CountsByStatus(ctx)
	if err != nil {
		t.Fatalf("counts: %v", err)
	}
	got := map[comments.Status]int{}
	for _, c := range counts {
		got[c.Status] = c.Count
	}
	if got[comments.StatusPending] != 2 || got[comments.StatusApproved] != 1 || got[comments.StatusSpam] != 1 {
		t.Fatalf("counts = %v", got)
	}
}

func TestIntegration_CascadeOnPostDelete(t *testing.T) {
	w := newWiring(t)
	ctx := context.Background()
	c := w.create(t, comments.CreateCommentData{PostID: w.postID, AuthorName: "A", Body: "x", Status: comments.StatusApproved})

	if _, err := w.pool.Exec(ctx, `DELETE FROM posts WHERE id = $1`, w.postID); err != nil {
		t.Fatalf("delete post: %v", err)
	}
	if _, err := w.repo.GetByID(ctx, c.ID); err != comments.ErrNotFound {
		t.Fatalf("comment should cascade-delete, got %v", err)
	}
}

func TestIntegration_ReplyCascadeOnParentDelete(t *testing.T) {
	w := newWiring(t)
	ctx := context.Background()
	parent := w.create(t, comments.CreateCommentData{PostID: w.postID, AuthorName: "P", Body: "root", Status: comments.StatusApproved})
	child := w.create(t, comments.CreateCommentData{PostID: w.postID, ParentID: ptr(parent.ID), AuthorName: "C", Body: "reply", Status: comments.StatusApproved})

	err := db.RunInTx(ctx, w.pool, func(ctx context.Context, tx pgx.Tx) error {
		return w.repo.DeleteTx(ctx, tx, parent.ID)
	})
	if err != nil {
		t.Fatalf("delete parent: %v", err)
	}
	if _, err := w.repo.GetByID(ctx, child.ID); err != comments.ErrNotFound {
		t.Fatalf("reply should cascade-delete, got %v", err)
	}
}
