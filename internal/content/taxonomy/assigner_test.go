package taxonomy

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// fakeAssigner records the arguments of the last AssignTx call and returns a
// canned error, so the delegation can be verified without a real transaction.
type fakeAssigner struct {
	gotPostID uuid.UUID
	gotIDs    []uuid.UUID
	called    bool
	err       error
}

func (f *fakeAssigner) AssignTx(_ context.Context, _ pgx.Tx, postID uuid.UUID, ids []uuid.UUID) error {
	f.called = true
	f.gotPostID = postID
	f.gotIDs = ids
	return f.err
}

func TestAssignCategoriesTx_DelegatesToCategoryService(t *testing.T) {
	cats := &fakeAssigner{}
	tags := &fakeAssigner{}
	a := NewAssigner(cats, tags)

	postID := uuid.New()
	ids := []uuid.UUID{uuid.New(), uuid.New()}
	if err := a.AssignCategoriesTx(context.Background(), nil, postID, ids); err != nil {
		t.Fatalf("AssignCategoriesTx: unexpected error %v", err)
	}
	if !cats.called || cats.gotPostID != postID || len(cats.gotIDs) != 2 {
		t.Errorf("category assign not delegated correctly: %+v", cats)
	}
	if tags.called {
		t.Error("tag assigner must not be touched by a category assign")
	}
}

func TestAssignTagsTx_DelegatesAndPropagatesError(t *testing.T) {
	sentinel := errors.New("boom")
	cats := &fakeAssigner{}
	tags := &fakeAssigner{err: sentinel}
	a := NewAssigner(cats, tags)

	postID := uuid.New()
	err := a.AssignTagsTx(context.Background(), nil, postID, []uuid.UUID{uuid.New()})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want sentinel", err)
	}
	if !tags.called || tags.gotPostID != postID {
		t.Errorf("tag assign not delegated correctly: %+v", tags)
	}
	if cats.called {
		t.Error("category assigner must not be touched by a tag assign")
	}
}
