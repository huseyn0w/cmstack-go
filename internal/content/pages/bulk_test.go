package pages

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestBulkTrash_AppliesToAllAuthorized verifies a permitted actor trashes every
// submitted page id (pages are permission-gated, no per-author ownership).
func TestBulkTrash_AppliesToAllAuthorized(t *testing.T) {
	actor := uuid.New()
	repo := newMemRepo()
	svc := newTestService(repo, newMemRevisions(), allow(actor), nullBus{}, time.Now())

	p1, _ := svc.Create(context.Background(), actor, CreateInput{Title: "A", Body: "<p>a</p>"})
	p2, _ := svc.Create(context.Background(), actor, CreateInput{Title: "B", Body: "<p>b</p>"})

	res, err := svc.BulkTrash(context.Background(), actor, []uuid.UUID{p1.ID, p2.ID})
	if err != nil {
		t.Fatalf("bulk trash: %v", err)
	}
	if res.AppliedCount() != 2 {
		t.Fatalf("applied=%d, want 2", res.AppliedCount())
	}
	for _, id := range []uuid.UUID{p1.ID, p2.ID} {
		got, _ := repo.GetByID(context.Background(), id)
		if !got.Trashed() {
			t.Errorf("page %s not trashed", id)
		}
	}
}

// TestBulkTrash_DeniedWithoutPermissionSkips verifies an actor without the page
// grant has every id skipped-unauthorized (none applied).
func TestBulkTrash_DeniedWithoutPermissionSkips(t *testing.T) {
	owner := uuid.New()
	stranger := uuid.New()
	repo := newMemRepo()
	svc := newTestService(repo, newMemRevisions(), allow(owner), nullBus{}, time.Now())

	p, _ := svc.Create(context.Background(), owner, CreateInput{Title: "A", Body: "<p>a</p>"})

	res, err := svc.BulkTrash(context.Background(), stranger, []uuid.UUID{p.ID})
	if err != nil {
		t.Fatalf("bulk trash: %v", err)
	}
	if res.AppliedCount() != 0 || res.SkippedCount() != 1 {
		t.Fatalf("applied=%d skipped=%d, want 0/1", res.AppliedCount(), res.SkippedCount())
	}
	got, _ := repo.GetByID(context.Background(), p.ID)
	if got.Trashed() {
		t.Error("page trashed by an actor without permission")
	}
}

// TestBulkPublishAndRestore exercises the publish/restore/delete bulk methods.
func TestBulkPublishAndRestore(t *testing.T) {
	actor := uuid.New()
	repo := newMemRepo()
	svc := newTestService(repo, newMemRevisions(), allow(actor), nullBus{}, time.Now())

	p, _ := svc.Create(context.Background(), actor, CreateInput{Title: "P", Body: "<p>x</p>"})

	if res, err := svc.BulkPublish(context.Background(), actor, []uuid.UUID{p.ID}); err != nil || res.AppliedCount() != 1 {
		t.Fatalf("bulk publish res=%+v err=%v", res, err)
	}
	got, _ := repo.GetByID(context.Background(), p.ID)
	if !got.Published() {
		t.Error("page not published after bulk publish")
	}

	_ = svc.Trash(context.Background(), actor, p.ID)
	if res, err := svc.BulkRestore(context.Background(), actor, []uuid.UUID{p.ID}); err != nil || res.AppliedCount() != 1 {
		t.Fatalf("bulk restore res=%+v err=%v", res, err)
	}
}

// TestBulkTrash_ReportsNotFound verifies a missing id lands in NotFound.
func TestBulkTrash_ReportsNotFound(t *testing.T) {
	actor := uuid.New()
	repo := newMemRepo()
	svc := newTestService(repo, newMemRevisions(), allow(actor), nullBus{}, time.Now())

	p, _ := svc.Create(context.Background(), actor, CreateInput{Title: "Real", Body: "<p>x</p>"})
	ghost := uuid.New()

	res, err := svc.BulkTrash(context.Background(), actor, []uuid.UUID{p.ID, ghost})
	if err != nil {
		t.Fatalf("bulk trash: %v", err)
	}
	if res.AppliedCount() != 1 || res.NotFoundCount() != 1 || res.NotFound[0] != ghost {
		t.Fatalf("applied=%v notfound=%v", res.Applied, res.NotFound)
	}
}
