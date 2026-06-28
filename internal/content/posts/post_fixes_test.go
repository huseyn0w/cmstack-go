package posts

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
)

// --- LOW fix 1: concurrent same-slug create maps to retry/friendly error ------

// raceOnceRepo wraps memRepo and fails the FIRST CreateTx with a pg 23505
// unique-violation (as a concurrent same-slug create would), succeeding after.
// This exercises the service's bounded retry: it must re-derive a unique slug and
// succeed rather than surface a 500.
type raceOnceRepo struct {
	*memRepo
	mu       sync.Mutex
	failed   bool
	existing map[string]bool // slugs already "in the DB"
}

func newRaceOnceRepo() *raceOnceRepo {
	return &raceOnceRepo{memRepo: newMemRepo(), existing: map[string]bool{}}
}

func (r *raceOnceRepo) CreateTx(ctx context.Context, tx pgx.Tx, in CreatePostData) (Post, error) {
	r.mu.Lock()
	if !r.failed {
		r.failed = true
		// Simulate the row a concurrent create committed under our chosen slug.
		r.existing[in.Slug] = true
		r.mu.Unlock()
		return Post{}, &pgconn.PgError{Code: "23505", ConstraintName: "posts_slug_key"}
	}
	r.mu.Unlock()
	return r.memRepo.CreateTx(ctx, tx, in)
}

func (r *raceOnceRepo) SlugTaken(ctx context.Context, slug string, excludeID uuid.UUID) (bool, error) {
	r.mu.Lock()
	if r.existing[slug] {
		r.mu.Unlock()
		return true, nil
	}
	r.mu.Unlock()
	return r.memRepo.SlugTaken(ctx, slug, excludeID)
}

func TestCreate_SlugUniqueViolationRetriesNot500(t *testing.T) {
	author := uuid.New()
	repo := newRaceOnceRepo()
	svc := newTestService(repo, newMemRevisions(),
		fakeAuthz{allowed: map[uuid.UUID]bool{author: true}},
		fakeRoles{byUser: map[uuid.UUID]string{author: accounts.RoleAuthor}},
		nullBus{}, time.Now())

	p, err := svc.Create(context.Background(), author, CreateInput{Title: "Same Title", Body: "<p>b</p>"})
	if err != nil {
		t.Fatalf("expected retry to succeed, got error (would be a 500): %v", err)
	}
	// The first slug was taken by the simulated race; the retry must pick the
	// deduped slug.
	if p.Slug != "same-title-2" {
		t.Errorf("retry slug = %q, want same-title-2", p.Slug)
	}
}

// alwaysConflictRepo always returns 23505 so the retry budget is exhausted and
// the service maps to the friendly ErrSlugTaken (never a raw constraint error).
type alwaysConflictRepo struct{ *memRepo }

func (r alwaysConflictRepo) CreateTx(context.Context, pgx.Tx, CreatePostData) (Post, error) {
	return Post{}, &pgconn.PgError{Code: "23505"}
}

func TestCreate_SlugViolationExhaustedMapsToFriendlyError(t *testing.T) {
	author := uuid.New()
	svc := newTestService(alwaysConflictRepo{newMemRepo()}, newMemRevisions(),
		fakeAuthz{allowed: map[uuid.UUID]bool{author: true}},
		fakeRoles{byUser: map[uuid.UUID]string{author: accounts.RoleAuthor}},
		nullBus{}, time.Now())

	if _, err := svc.Create(context.Background(), author, CreateInput{Title: "T", Body: "b"}); err != ErrSlugTaken {
		t.Fatalf("exhausted retries = %v, want ErrSlugTaken", err)
	}
}

// --- LOW fix 2: likes rejected on trashed / unpublished posts in the SERVICE ---

func TestLike_RejectedOnTrashedPost(t *testing.T) {
	author := uuid.New()
	liker := uuid.New()
	repo := newMemRepo()
	svc := newTestService(repo, newMemRevisions(),
		fakeAuthz{allowed: map[uuid.UUID]bool{author: true}},
		fakeRoles{byUser: map[uuid.UUID]string{author: accounts.RoleAuthor}},
		nullBus{}, time.Now())

	p, _ := svc.Create(context.Background(), author, CreateInput{Title: "L", Body: "<p>b</p>", Status: kernel.StatusPublished})
	// Trash it, then a like must be rejected by the SERVICE (not just the handler).
	if err := svc.Trash(context.Background(), author, p.ID); err != nil {
		t.Fatalf("trash: %v", err)
	}
	if _, err := svc.Like(context.Background(), p.ID, liker); err != ErrNotLikeable {
		t.Fatalf("like on trashed = %v, want ErrNotLikeable", err)
	}
}

func TestLike_RejectedOnUnpublishedPost(t *testing.T) {
	author := uuid.New()
	liker := uuid.New()
	repo := newMemRepo()
	svc := newTestService(repo, newMemRevisions(),
		fakeAuthz{allowed: map[uuid.UUID]bool{author: true}},
		fakeRoles{byUser: map[uuid.UUID]string{author: accounts.RoleAuthor}},
		nullBus{}, time.Now())

	// A DRAFT post is not publicly available; liking it must be rejected.
	p, _ := svc.Create(context.Background(), author, CreateInput{Title: "Draft", Body: "<p>b</p>"})
	if _, err := svc.Like(context.Background(), p.ID, liker); err != ErrNotLikeable {
		t.Fatalf("like on draft = %v, want ErrNotLikeable", err)
	}
}

func TestUnlike_AllowedEvenWhenNotPublished(t *testing.T) {
	author := uuid.New()
	liker := uuid.New()
	repo := newMemRepo()
	svc := newTestService(repo, newMemRevisions(),
		fakeAuthz{allowed: map[uuid.UUID]bool{author: true}},
		fakeRoles{byUser: map[uuid.UUID]string{author: accounts.RoleAuthor}},
		nullBus{}, time.Now())

	p, _ := svc.Create(context.Background(), author, CreateInput{Title: "L", Body: "<p>b</p>", Status: kernel.StatusPublished})
	if _, err := svc.Like(context.Background(), p.ID, liker); err != nil {
		t.Fatalf("like: %v", err)
	}
	// Unpublish, then UNLIKE must still succeed so a user can retract.
	if _, err := svc.Unpublish(context.Background(), author, p.ID); err != nil {
		t.Fatalf("unpublish: %v", err)
	}
	got, err := svc.Unlike(context.Background(), p.ID, liker)
	if err != nil {
		t.Fatalf("unlike on unpublished = %v, want nil", err)
	}
	if got.LikeCount != 0 {
		t.Errorf("after unlike count = %d, want 0", got.LikeCount)
	}
}

// --- LOW fix 3: Excerpt sanitized write-time on create AND update -------------

func TestCreate_SanitizesExcerpt(t *testing.T) {
	author := uuid.New()
	repo := newMemRepo()
	svc := newTestService(repo, newMemRevisions(),
		fakeAuthz{allowed: map[uuid.UUID]bool{author: true}},
		fakeRoles{byUser: map[uuid.UUID]string{author: accounts.RoleAuthor}},
		nullBus{}, time.Now())

	p, err := svc.Create(context.Background(), author, CreateInput{
		Title:   "T",
		Body:    "<p>b</p>",
		Excerpt: `Hello <script>alert(1)</script><b>world</b>`,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if want := "Hello world"; p.Excerpt != want {
		t.Errorf("excerpt not sanitized on create: got %q, want %q", p.Excerpt, want)
	}
}

func TestUpdate_SanitizesExcerpt(t *testing.T) {
	author := uuid.New()
	repo := newMemRepo()
	svc := newTestService(repo, newMemRevisions(),
		fakeAuthz{allowed: map[uuid.UUID]bool{author: true}},
		fakeRoles{byUser: map[uuid.UUID]string{author: accounts.RoleAuthor}},
		nullBus{}, time.Now())

	p, _ := svc.Create(context.Background(), author, CreateInput{Title: "T", Body: "<p>b</p>"})
	evil := `Safe <img src=x onerror=alert(1)> tail`
	updated, err := svc.Update(context.Background(), author, p.ID, UpdateInput{Excerpt: &evil})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if want := "Safe  tail"; updated.Excerpt != want {
		t.Errorf("excerpt not sanitized on update: got %q, want %q", updated.Excerpt, want)
	}
}
