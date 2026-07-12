package search_test

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

	"github.com/huseyn0w/agentic-cms-go/internal/content/search"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/db"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/db/sqlcgen"
)

func migrationsDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve caller for migrations path")
	}
	// internal/content/search/integration_test.go -> repo root is 4 up.
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

// seed inserts a role, an author, and the content used by the search tests.
// Content is inserted via raw SQL so the GENERATED search_vector column is
// exercised end-to-end (Postgres populates it, not the app).
func seed(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	roleID := uuid.New()
	authorID := uuid.New()

	exec := func(q string, args ...any) {
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatalf("exec %q: %v", q, err)
		}
	}

	exec(`INSERT INTO roles (id, key, label) VALUES ($1,'admin','Admin')`, roleID)
	exec(`INSERT INTO users (id, email, password_hash, name, username, role_id, email_verified_at)
	      VALUES ($1,'a@t.local','x','Author','author',$2, now())`, authorID, roleID)

	// posts: one with the term in the TITLE, one with it only in the BODY, one DRAFT,
	// one TRASHED, plus a substring-only post ("PostgreSQL" -> substring "tgres").
	exec(`INSERT INTO posts (title, slug, excerpt, body, status, published_at, author_id) VALUES
	  ('PostgreSQL Tuning','pg-title','fast queries','a generic body', 'PUBLISHED', now() - interval '1 day', $1),
	  ('Generic Guide','pg-body','summary here','deep dive into postgresql internals', 'PUBLISHED', now() - interval '2 days', $1),
	  ('Draft About PostgreSQL','pg-draft','postgresql','postgresql draft body', 'DRAFT', NULL, $1)`, authorID)
	// trashed published post that matches:
	exec(`INSERT INTO posts (title, slug, excerpt, body, status, published_at, author_id, deleted_at) VALUES
	  ('Trashed PostgreSQL','pg-trashed','postgresql','postgresql body', 'PUBLISHED', now(), $1, now())`, authorID)

	// pages: a published match + a draft match.
	exec(`INSERT INTO pages (title, slug, body, status, published_at) VALUES
	  ('About PostgreSQL','about-pg','we love postgresql', 'PUBLISHED', now()),
	  ('Hidden PostgreSQL','hidden-pg','postgresql secret', 'DRAFT', NULL)`)

	// services: a published match.
	exec(`INSERT INTO services (title, slug, summary, body, status, published_at) VALUES
	  ('PostgreSQL Consulting','pg-consult','postgresql experts','deep body', 'PUBLISHED', now())`)

	return authorID
}

func newRepo(t *testing.T) *search.RepoPG {
	t.Helper()
	pool := startPostgres(t)
	seed(t, pool)
	return search.NewRepoPG(sqlcgen.New(pool))
}

func TestRepoFTS_RanksTitleAboveBody(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()

	hits, err := repo.FTS(ctx, "postgresql", 20, 0)
	if err != nil {
		t.Fatalf("FTS: %v", err)
	}
	// Locate the two posts.
	var titleRank, bodyRank float64
	var seenTitle, seenBody bool
	for _, h := range hits {
		switch h.Slug {
		case "pg-title":
			titleRank, seenTitle = h.Rank, true
		case "pg-body":
			bodyRank, seenBody = h.Rank, true
		}
	}
	if !seenTitle || !seenBody {
		t.Fatalf("expected both title+body posts in results; got %d hits", len(hits))
	}
	if !(titleRank > bodyRank) {
		t.Errorf("title-match rank (%v) should exceed body-only rank (%v)", titleRank, bodyRank)
	}
}

func TestRepoFTS_ExcludesDraftsAndTrashed(t *testing.T) {
	repo := newRepo(t)
	hits, err := repo.FTS(context.Background(), "postgresql", 50, 0)
	if err != nil {
		t.Fatalf("FTS: %v", err)
	}
	for _, h := range hits {
		switch h.Slug {
		case "pg-draft", "hidden-pg":
			t.Errorf("draft %q must not appear in search results", h.Slug)
		case "pg-trashed":
			t.Errorf("trashed %q must not appear in search results", h.Slug)
		}
	}
}

func TestRepoFTS_CrossTypeResults(t *testing.T) {
	repo := newRepo(t)
	hits, err := repo.FTS(context.Background(), "postgresql", 50, 0)
	if err != nil {
		t.Fatalf("FTS: %v", err)
	}
	seen := map[search.HitType]bool{}
	for _, h := range hits {
		seen[h.Type] = true
	}
	for _, typ := range []search.HitType{search.HitPost, search.HitPage, search.HitService} {
		if !seen[typ] {
			t.Errorf("expected a %s result in cross-type search", typ)
		}
	}
}

func TestRepoCountFTS_MatchesPublishedOnly(t *testing.T) {
	repo := newRepo(t)
	n, err := repo.CountFTS(context.Background(), "postgresql")
	if err != nil {
		t.Fatalf("CountFTS: %v", err)
	}
	// published matches: pg-title, pg-body, about-pg, pg-consult = 4 (drafts+trashed excluded).
	if n != 4 {
		t.Errorf("CountFTS = %d, want 4 (published, non-trashed only)", n)
	}
}

func TestRepoFTS_Pagination(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()
	first, err := repo.FTS(ctx, "postgresql", 2, 0)
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	second, err := repo.FTS(ctx, "postgresql", 2, 2)
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(first) != 2 {
		t.Fatalf("page1 len = %d, want 2", len(first))
	}
	// Pages must not overlap.
	for _, a := range first {
		for _, b := range second {
			if a.Slug == b.Slug {
				t.Errorf("slug %q appeared on both pages", a.Slug)
			}
		}
	}
}

func TestRepoFTS_EmptyQueryReturnsNothing(t *testing.T) {
	repo := newRepo(t)
	// websearch_to_tsquery('') matches nothing.
	hits, err := repo.FTS(context.Background(), "", 20, 0)
	if err != nil {
		t.Fatalf("FTS empty: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("empty query should match nothing, got %d hits", len(hits))
	}
}

func TestRepoILIKE_FindsSubstringTsqueryMisses(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()

	// "tgres" is a substring of "PostgreSQL" — tsquery lexemes won't match it.
	ftsCount, err := repo.CountFTS(ctx, "tgres")
	if err != nil {
		t.Fatalf("CountFTS: %v", err)
	}
	if ftsCount != 0 {
		t.Fatalf("expected FTS to MISS substring 'tgres', got %d", ftsCount)
	}

	ilikeCount, err := repo.CountILIKE(ctx, "tgres")
	if err != nil {
		t.Fatalf("CountILIKE: %v", err)
	}
	if ilikeCount == 0 {
		t.Fatal("ILIKE fallback should FIND the 'tgres' substring")
	}

	hits, err := repo.ILIKE(ctx, "tgres", 50, 0)
	if err != nil {
		t.Fatalf("ILIKE: %v", err)
	}
	// Fallback must also respect published/non-trashed scope.
	for _, h := range hits {
		switch h.Slug {
		case "pg-draft", "hidden-pg", "pg-trashed":
			t.Errorf("ILIKE fallback leaked non-public row %q", h.Slug)
		}
	}
	if len(hits) == 0 {
		t.Error("ILIKE should return substring matches")
	}
}

func TestRepoFTS_SnippetHighlights(t *testing.T) {
	repo := newRepo(t)
	hits, err := repo.FTS(context.Background(), "postgresql", 50, 0)
	if err != nil {
		t.Fatalf("FTS: %v", err)
	}
	// At least one snippet should carry the <mark> highlight from ts_headline.
	found := false
	for _, h := range hits {
		if len(h.Snippet) > 0 {
			found = true
		}
	}
	if !found {
		t.Error("expected non-empty snippets from ts_headline")
	}
}
