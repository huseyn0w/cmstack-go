package posts

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
)

// BulkResult is re-exported from kernel so handlers depend on the posts package
// without reaching into kernel for the shape.
type BulkResult = kernel.BulkResult

// classifyBulk maps a single-item error to the bulk skip buckets. ErrForbidden
// (coarse permission OR per-author ownership) is "unauthorized" and is skipped;
// ErrNotFound is "not found" and is skipped. Any other error is unrecognized
// and aborts the batch (it is a genuine failure, not a per-item skip). This is
// the security seam: an Author bulk-acting on another user's post id hits
// ErrForbidden inside the reused single-item method and is recorded as skipped,
// never applied.
func classifyBulk(err error) (unauthorized, notFound, recognized bool) {
	switch {
	case errors.Is(err, ErrForbidden):
		return true, false, true
	case errors.Is(err, ErrNotFound):
		return false, true, true
	default:
		return false, false, false
	}
}

// BulkTrash trashes every authorized id, skipping ids the actor may not act on
// (others' posts for an Author) and ids that no longer exist. Each id reuses the
// single-item Trash, so ownership/permission/events stay correct per id and a
// per-item failure never aborts the batch.
func (s *Service) BulkTrash(ctx context.Context, actorID uuid.UUID, ids []uuid.UUID) (BulkResult, error) {
	return kernel.RunBulk(ids, func(id uuid.UUID) error {
		return s.Trash(ctx, actorID, id)
	}, classifyBulk)
}

// BulkRestore restores every authorized trashed id.
func (s *Service) BulkRestore(ctx context.Context, actorID uuid.UUID, ids []uuid.UUID) (BulkResult, error) {
	return kernel.RunBulk(ids, func(id uuid.UUID) error {
		return s.Restore(ctx, actorID, id)
	}, classifyBulk)
}

// BulkPermanentDelete hard-deletes every authorized trashed id (irreversible).
func (s *Service) BulkPermanentDelete(ctx context.Context, actorID uuid.UUID, ids []uuid.UUID) (BulkResult, error) {
	return kernel.RunBulk(ids, func(id uuid.UUID) error {
		return s.PermanentDelete(ctx, actorID, id)
	}, classifyBulk)
}

// BulkPublish publishes every authorized id, emitting the per-post publish event
// for each newly-published post (the reused single-item Publish does that).
func (s *Service) BulkPublish(ctx context.Context, actorID uuid.UUID, ids []uuid.UUID) (BulkResult, error) {
	return kernel.RunBulk(ids, func(id uuid.UUID) error {
		_, err := s.Publish(ctx, actorID, id)
		return err
	}, classifyBulk)
}

// BulkUnpublish returns every authorized id to draft.
func (s *Service) BulkUnpublish(ctx context.Context, actorID uuid.UUID, ids []uuid.UUID) (BulkResult, error) {
	return kernel.RunBulk(ids, func(id uuid.UUID) error {
		_, err := s.Unpublish(ctx, actorID, id)
		return err
	}, classifyBulk)
}
