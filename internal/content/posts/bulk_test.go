package posts

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/huseyn0w/agentic-cms-go/internal/accounts"
	"github.com/huseyn0w/agentic-cms-go/internal/content/kernel"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/events"
)

// TestBulkTrash_AppliesToAllAuthorized verifies a privileged actor trashes every
// submitted id.
func TestBulkTrash_AppliesToAllAuthorized(t *testing.T) {
	editor := uuid.New()
	author := uuid.New()
	repo := newMemRepo()
	svc := newTestService(repo, newMemRevisions(),
		fakeAuthz{allowed: map[uuid.UUID]bool{editor: true, author: true}},
		fakeRoles{byUser: map[uuid.UUID]string{editor: accounts.RoleEditor, author: accounts.RoleAuthor}},
		nullBus{}, time.Now())

	p1, _ := svc.Create(context.Background(), author, CreateInput{Title: "A", Body: "<p>a</p>"})
	p2, _ := svc.Create(context.Background(), author, CreateInput{Title: "B", Body: "<p>b</p>"})

	res, err := svc.BulkTrash(context.Background(), editor, []uuid.UUID{p1.ID, p2.ID})
	if err != nil {
		t.Fatalf("bulk trash: %v", err)
	}
	if res.AppliedCount() != 2 || res.SkippedCount() != 0 || res.NotFoundCount() != 0 {
		t.Fatalf("applied=%d skipped=%d notfound=%d, want 2/0/0", res.AppliedCount(), res.SkippedCount(), res.NotFoundCount())
	}
	for _, id := range []uuid.UUID{p1.ID, p2.ID} {
		got, _ := repo.GetByID(context.Background(), id)
		if !got.Trashed() {
			t.Errorf("post %s not trashed", id)
		}
	}
}

// TestBulkTrash_AuthorCannotTouchOthersPosts is the SECURITY-CRITICAL case: an
// Author submitting a mixed-ownership set may only trash their OWN posts; the
// others are reported skipped-unauthorized and remain untouched.
func TestBulkTrash_AuthorCannotTouchOthersPosts(t *testing.T) {
	author := uuid.New()
	other := uuid.New()
	repo := newMemRepo()
	svc := newTestService(repo, newMemRevisions(),
		fakeAuthz{allowed: map[uuid.UUID]bool{author: true, other: true}},
		fakeRoles{byUser: map[uuid.UUID]string{author: accounts.RoleAuthor, other: accounts.RoleAuthor}},
		nullBus{}, time.Now())

	mine, _ := svc.Create(context.Background(), author, CreateInput{Title: "Mine", Body: "<p>x</p>"})
	theirs, _ := svc.Create(context.Background(), other, CreateInput{Title: "Theirs", Body: "<p>y</p>"})

	res, err := svc.BulkTrash(context.Background(), author, []uuid.UUID{mine.ID, theirs.ID})
	if err != nil {
		t.Fatalf("bulk trash: %v", err)
	}
	if res.AppliedCount() != 1 || len(res.Applied) != 1 || res.Applied[0] != mine.ID {
		t.Fatalf("expected only own post applied, got applied=%v", res.Applied)
	}
	if res.SkippedCount() != 1 || res.SkippedUnauthorized[0] != theirs.ID {
		t.Fatalf("expected other's post skipped-unauthorized, got %v", res.SkippedUnauthorized)
	}
	// The other author's post MUST remain untouched.
	got, _ := repo.GetByID(context.Background(), theirs.ID)
	if got.Trashed() {
		t.Fatal("SECURITY: author bulk-trashed another author's post")
	}
}

// TestBulkPublish_AuthorCannotPublishOthers mirrors the ownership gate for the
// publish action: an Author cannot bulk-publish someone else's draft.
func TestBulkPublish_AuthorCannotPublishOthers(t *testing.T) {
	author := uuid.New()
	other := uuid.New()
	repo := newMemRepo()
	svc := newTestService(repo, newMemRevisions(),
		fakeAuthz{allowed: map[uuid.UUID]bool{author: true, other: true}},
		fakeRoles{byUser: map[uuid.UUID]string{author: accounts.RoleAuthor, other: accounts.RoleAuthor}},
		nullBus{}, time.Now())

	mine, _ := svc.Create(context.Background(), author, CreateInput{Title: "Mine", Body: "<p>x</p>"})
	theirs, _ := svc.Create(context.Background(), other, CreateInput{Title: "Theirs", Body: "<p>y</p>"})

	res, err := svc.BulkPublish(context.Background(), author, []uuid.UUID{mine.ID, theirs.ID})
	if err != nil {
		t.Fatalf("bulk publish: %v", err)
	}
	if res.AppliedCount() != 1 || res.Applied[0] != mine.ID {
		t.Fatalf("expected only own post published, applied=%v", res.Applied)
	}
	got, _ := repo.GetByID(context.Background(), theirs.ID)
	if got.Published() {
		t.Fatal("SECURITY: author bulk-published another author's post")
	}
}

// TestBulkTrash_ReportsNotFound verifies an id with no row lands in NotFound and
// does not abort the batch.
func TestBulkTrash_ReportsNotFound(t *testing.T) {
	author := uuid.New()
	repo := newMemRepo()
	svc := newTestService(repo, newMemRevisions(),
		fakeAuthz{allowed: map[uuid.UUID]bool{author: true}},
		fakeRoles{byUser: map[uuid.UUID]string{author: accounts.RoleAuthor}},
		nullBus{}, time.Now())

	p, _ := svc.Create(context.Background(), author, CreateInput{Title: "Real", Body: "<p>x</p>"})
	ghost := uuid.New()

	res, err := svc.BulkTrash(context.Background(), author, []uuid.UUID{p.ID, ghost})
	if err != nil {
		t.Fatalf("bulk trash: %v", err)
	}
	if res.AppliedCount() != 1 || res.NotFoundCount() != 1 || res.NotFound[0] != ghost {
		t.Fatalf("applied=%v notfound=%v, want 1 applied + ghost not-found", res.Applied, res.NotFound)
	}
}

// countingBus records every event published in-tx, so a unit test can assert
// the service emits the publish event once per applied post (the same event a
// single Publish emits). It satisfies Publisher.
type countingBus struct{ byName map[string]int }

func (b *countingBus) Publish(_ context.Context, _ pgx.Tx, ev events.Event) error {
	if b.byName == nil {
		b.byName = map[string]int{}
	}
	b.byName[ev.Name()]++
	return nil
}

// TestBulkPublish_EmitsEventPerAppliedItem asserts the per-post publish event
// fires once for each applied post, exactly like a single publish.
func TestBulkPublish_EmitsEventPerAppliedItem(t *testing.T) {
	author := uuid.New()
	repo := newMemRepo()
	bus := &countingBus{}
	svc := newTestService(repo, newMemRevisions(),
		fakeAuthz{allowed: map[uuid.UUID]bool{author: true}},
		fakeRoles{byUser: map[uuid.UUID]string{author: accounts.RoleAuthor}},
		bus, time.Now())

	p1, _ := svc.Create(context.Background(), author, CreateInput{Title: "A", Body: "<p>a</p>"})
	p2, _ := svc.Create(context.Background(), author, CreateInput{Title: "B", Body: "<p>b</p>"})

	res, err := svc.BulkPublish(context.Background(), author, []uuid.UUID{p1.ID, p2.ID})
	if err != nil {
		t.Fatalf("bulk publish: %v", err)
	}
	if res.AppliedCount() != 2 {
		t.Fatalf("applied=%d, want 2", res.AppliedCount())
	}
	if got := bus.byName[EventContentPublished]; got != 2 {
		t.Errorf("publish events = %d, want 2 (one per applied post)", got)
	}
}

// TestBulkRestore_RestoresTrashed verifies bulk restore un-trashes each id.
func TestBulkRestore_RestoresTrashed(t *testing.T) {
	author := uuid.New()
	repo := newMemRepo()
	svc := newTestService(repo, newMemRevisions(),
		fakeAuthz{allowed: map[uuid.UUID]bool{author: true}},
		fakeRoles{byUser: map[uuid.UUID]string{author: accounts.RoleAuthor}},
		nullBus{}, time.Now())

	p, _ := svc.Create(context.Background(), author, CreateInput{Title: "R", Body: "<p>x</p>"})
	if err := svc.Trash(context.Background(), author, p.ID); err != nil {
		t.Fatalf("trash: %v", err)
	}
	res, err := svc.BulkRestore(context.Background(), author, []uuid.UUID{p.ID})
	if err != nil {
		t.Fatalf("bulk restore: %v", err)
	}
	if res.AppliedCount() != 1 {
		t.Fatalf("applied=%d, want 1", res.AppliedCount())
	}
	got, _ := repo.GetByID(context.Background(), p.ID)
	if got.Trashed() {
		t.Error("post still trashed after bulk restore")
	}
}

// TestBulkPermanentDelete_HardDeletes verifies each id is removed.
func TestBulkPermanentDelete_HardDeletes(t *testing.T) {
	author := uuid.New()
	repo := newMemRepo()
	svc := newTestService(repo, newMemRevisions(),
		fakeAuthz{allowed: map[uuid.UUID]bool{author: true}},
		fakeRoles{byUser: map[uuid.UUID]string{author: accounts.RoleAuthor}},
		nullBus{}, time.Now())

	p, _ := svc.Create(context.Background(), author, CreateInput{Title: "D", Body: "<p>x</p>"})
	_ = svc.Trash(context.Background(), author, p.ID)

	res, err := svc.BulkPermanentDelete(context.Background(), author, []uuid.UUID{p.ID})
	if err != nil {
		t.Fatalf("bulk delete: %v", err)
	}
	if res.AppliedCount() != 1 {
		t.Fatalf("applied=%d, want 1", res.AppliedCount())
	}
	if _, err := repo.GetByID(context.Background(), p.ID); err != ErrNotFound {
		t.Errorf("post not hard-deleted: %v", err)
	}
}

// TestBulkTrash_PartialFailureDoesNotAbort proves an unrecognized infra error on
// one id surfaces to the caller WITHOUT having silently swallowed it, while the
// recognized skips before it are still applied. Here the failing repo errors on
// a specific id; the bulk driver returns that error (genuine failure) but the
// earlier applied id is recorded.
func TestBulkTrash_PartialFailureDoesNotAbort(t *testing.T) {
	author := uuid.New()
	boom := uuid.New()
	repo := &flakyRepo{memRepo: newMemRepo(), failTrashID: boom}
	svc := newTestService(repo, newMemRevisions(),
		fakeAuthz{allowed: map[uuid.UUID]bool{author: true}},
		fakeRoles{byUser: map[uuid.UUID]string{author: accounts.RoleAuthor}},
		nullBus{}, time.Now())

	good, _ := svc.Create(context.Background(), author, CreateInput{Title: "Good", Body: "<p>x</p>"})
	// Seed the boom id directly so it resolves (so we reach TrashTx).
	repo.put(Post{ID: boom, Title: "Boom", AuthorID: author, Status: kernel.StatusDraft, CreatedAt: time.Now(), UpdatedAt: time.Now()})

	// good first (applied), then boom (infra error aborts and is returned).
	res, err := svc.BulkTrash(context.Background(), author, []uuid.UUID{good.ID, boom})
	if err == nil {
		t.Fatal("expected the infra error to surface, got nil")
	}
	if res.AppliedCount() != 1 || res.Applied[0] != good.ID {
		t.Fatalf("the id before the failure must still be applied, got %v", res.Applied)
	}
}

// flakyRepo wraps memRepo and fails TrashTx for one specific id, simulating an
// infrastructure error that is NOT a per-item authorization skip.
type flakyRepo struct {
	*memRepo
	failTrashID uuid.UUID
}

func (r *flakyRepo) TrashTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	if id == r.failTrashID {
		return errBoom
	}
	return r.memRepo.TrashTx(ctx, tx, id)
}

var errBoom = &boomError{}

type boomError struct{}

func (*boomError) Error() string { return "boom: simulated infra failure" }
