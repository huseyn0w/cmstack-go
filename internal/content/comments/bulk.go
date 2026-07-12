package comments

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/huseyn0w/agentic-cms-go/internal/content/kernel"
)

// BulkResult is re-exported from kernel so handlers depend on the comments
// package without reaching into kernel for the shape.
type BulkResult = kernel.BulkResult

// classifyBulk maps a single-item moderation error to the bulk skip buckets.
// ErrForbidden is "unauthorized" (skipped); ErrNotFound is "not found" (skipped).
// Any other error aborts the batch (a genuine infrastructure failure).
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

// BulkApprove approves every authorized id, reusing the single-item Approve so
// permission + events stay correct per id; an unauthorized/missing id is skipped.
func (s *Service) BulkApprove(ctx context.Context, actorID uuid.UUID, ids []uuid.UUID) (BulkResult, error) {
	return kernel.RunBulk(ids, func(id uuid.UUID) error {
		_, err := s.Approve(ctx, actorID, id)
		return err
	}, classifyBulk)
}

// BulkSpam marks every authorized id as spam.
func (s *Service) BulkSpam(ctx context.Context, actorID uuid.UUID, ids []uuid.UUID) (BulkResult, error) {
	return kernel.RunBulk(ids, func(id uuid.UUID) error {
		_, err := s.Spam(ctx, actorID, id)
		return err
	}, classifyBulk)
}

// BulkTrash trashes every authorized id.
func (s *Service) BulkTrash(ctx context.Context, actorID uuid.UUID, ids []uuid.UUID) (BulkResult, error) {
	return kernel.RunBulk(ids, func(id uuid.UUID) error {
		_, err := s.Trash(ctx, actorID, id)
		return err
	}, classifyBulk)
}

// BulkDelete hard-deletes every authorized id (irreversible).
func (s *Service) BulkDelete(ctx context.Context, actorID uuid.UUID, ids []uuid.UUID) (BulkResult, error) {
	return kernel.RunBulk(ids, func(id uuid.UUID) error {
		return s.Delete(ctx, actorID, id)
	}, classifyBulk)
}
