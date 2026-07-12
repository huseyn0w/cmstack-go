package pages

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/huseyn0w/agentic-cms-go/internal/content/kernel"
)

// BulkResult is re-exported from kernel so handlers depend on the pages package
// without reaching into kernel for the shape.
type BulkResult = kernel.BulkResult

// classifyBulk maps a single-item error to the bulk skip buckets. Pages have no
// per-author ownership: ErrForbidden here means the actor lacks the coarse page
// grant. It is still treated as a per-item skip (unauthorized) for symmetry,
// though in practice the route gate already requires the grant. ErrNotFound is
// skipped; any other error aborts the batch.
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

// BulkTrash trashes every id the actor is permitted to delete.
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

// BulkPublish publishes every authorized id, emitting the per-page publish event.
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
