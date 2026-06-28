package services

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestBulkTrash_AppliesToAllAuthorized verifies a permitted actor trashes every
// submitted service id (services are permission-gated, no per-author ownership).
func TestBulkTrash_AppliesToAllAuthorized(t *testing.T) {
	actor := uuid.New()
	repo := newMemRepo()
	mgr := newTestManager(repo, newMemRevisions(), allow(actor), nullBus{}, time.Now())

	s1, _ := mgr.Create(context.Background(), actor, CreateInput{Title: "A", Body: "<p>a</p>"})
	s2, _ := mgr.Create(context.Background(), actor, CreateInput{Title: "B", Body: "<p>b</p>"})

	res, err := mgr.BulkTrash(context.Background(), actor, []uuid.UUID{s1.ID, s2.ID})
	if err != nil {
		t.Fatalf("bulk trash: %v", err)
	}
	if res.AppliedCount() != 2 {
		t.Fatalf("applied=%d, want 2", res.AppliedCount())
	}
	for _, id := range []uuid.UUID{s1.ID, s2.ID} {
		got, _ := repo.GetByID(context.Background(), id)
		if !got.Trashed() {
			t.Errorf("service %s not trashed", id)
		}
	}
}

// TestBulkTrash_DeniedWithoutPermissionSkips verifies an actor without the
// service grant has every id skipped-unauthorized (none applied).
func TestBulkTrash_DeniedWithoutPermissionSkips(t *testing.T) {
	owner := uuid.New()
	stranger := uuid.New()
	repo := newMemRepo()
	mgr := newTestManager(repo, newMemRevisions(), allow(owner), nullBus{}, time.Now())

	s, _ := mgr.Create(context.Background(), owner, CreateInput{Title: "A", Body: "<p>a</p>"})

	res, err := mgr.BulkTrash(context.Background(), stranger, []uuid.UUID{s.ID})
	if err != nil {
		t.Fatalf("bulk trash: %v", err)
	}
	if res.AppliedCount() != 0 || res.SkippedCount() != 1 {
		t.Fatalf("applied=%d skipped=%d, want 0/1", res.AppliedCount(), res.SkippedCount())
	}
}

// TestBulkPublishRestoreDelete exercises publish/restore/permanent-delete.
func TestBulkPublishRestoreDelete(t *testing.T) {
	actor := uuid.New()
	repo := newMemRepo()
	mgr := newTestManager(repo, newMemRevisions(), allow(actor), nullBus{}, time.Now())

	s, _ := mgr.Create(context.Background(), actor, CreateInput{Title: "P", Body: "<p>x</p>"})

	if res, err := mgr.BulkPublish(context.Background(), actor, []uuid.UUID{s.ID}); err != nil || res.AppliedCount() != 1 {
		t.Fatalf("bulk publish res=%+v err=%v", res, err)
	}
	got, _ := repo.GetByID(context.Background(), s.ID)
	if !got.Published() {
		t.Error("service not published after bulk publish")
	}

	_ = mgr.Trash(context.Background(), actor, s.ID)
	if res, err := mgr.BulkRestore(context.Background(), actor, []uuid.UUID{s.ID}); err != nil || res.AppliedCount() != 1 {
		t.Fatalf("bulk restore res=%+v err=%v", res, err)
	}
	_ = mgr.Trash(context.Background(), actor, s.ID)
	if res, err := mgr.BulkPermanentDelete(context.Background(), actor, []uuid.UUID{s.ID}); err != nil || res.AppliedCount() != 1 {
		t.Fatalf("bulk delete res=%+v err=%v", res, err)
	}
	if _, err := repo.GetByID(context.Background(), s.ID); err != ErrNotFound {
		t.Errorf("service not hard-deleted: %v", err)
	}
}
