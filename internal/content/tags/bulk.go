package tags

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
)

// BulkResult is re-exported from kernel so handlers depend on the tags package
// without reaching into kernel for the shape.
type BulkResult = kernel.BulkResult

// classifyBulk maps a single-item error to the bulk skip buckets.
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

// BulkDelete hard-deletes every authorized id, reusing the single-item Delete.
func (s *Service) BulkDelete(ctx context.Context, actorID uuid.UUID, ids []uuid.UUID) (BulkResult, error) {
	return kernel.RunBulk(ids, func(id uuid.UUID) error {
		return s.Delete(ctx, actorID, id)
	}, classifyBulk)
}
