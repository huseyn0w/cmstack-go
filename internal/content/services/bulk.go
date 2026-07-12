package services

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/huseyn0w/agentic-cms-go/internal/content/kernel"
)

// BulkResult is re-exported from kernel so handlers depend on the services
// package without reaching into kernel for the shape.
type BulkResult = kernel.BulkResult

// classifyBulk maps a single-item error to the bulk skip buckets. Services have
// no per-author ownership: ErrForbidden means the actor lacks the coarse service
// grant. ErrNotFound is skipped; any other error aborts the batch.
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
func (m *Manager) BulkTrash(ctx context.Context, actorID uuid.UUID, ids []uuid.UUID) (BulkResult, error) {
	return kernel.RunBulk(ids, func(id uuid.UUID) error {
		return m.Trash(ctx, actorID, id)
	}, classifyBulk)
}

// BulkRestore restores every authorized trashed id.
func (m *Manager) BulkRestore(ctx context.Context, actorID uuid.UUID, ids []uuid.UUID) (BulkResult, error) {
	return kernel.RunBulk(ids, func(id uuid.UUID) error {
		return m.Restore(ctx, actorID, id)
	}, classifyBulk)
}

// BulkPermanentDelete hard-deletes every authorized trashed id (irreversible).
func (m *Manager) BulkPermanentDelete(ctx context.Context, actorID uuid.UUID, ids []uuid.UUID) (BulkResult, error) {
	return kernel.RunBulk(ids, func(id uuid.UUID) error {
		return m.PermanentDelete(ctx, actorID, id)
	}, classifyBulk)
}

// BulkPublish publishes every authorized id, emitting the per-service publish event.
func (m *Manager) BulkPublish(ctx context.Context, actorID uuid.UUID, ids []uuid.UUID) (BulkResult, error) {
	return kernel.RunBulk(ids, func(id uuid.UUID) error {
		_, err := m.Publish(ctx, actorID, id)
		return err
	}, classifyBulk)
}

// BulkUnpublish returns every authorized id to draft.
func (m *Manager) BulkUnpublish(ctx context.Context, actorID uuid.UUID, ids []uuid.UUID) (BulkResult, error) {
	return kernel.RunBulk(ids, func(id uuid.UUID) error {
		_, err := m.Unpublish(ctx, actorID, id)
		return err
	}, classifyBulk)
}
