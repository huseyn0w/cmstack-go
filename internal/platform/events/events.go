// Package events provides an in-memory domain event bus with two listener
// kinds: synchronous in-transaction listeners (run inside the caller's pgx.Tx,
// an error rolls the transaction back) and asynchronous listeners (persisted to
// an outbox table within the same transaction and drained later by a relay).
package events

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Event is the contract every domain event satisfies. Name groups listeners
// and is persisted as the outbox event_name.
type Event interface {
	Name() string
}

// SyncHandler runs inside the publishing transaction. Returning an error aborts
// the publish and signals the caller to roll the transaction back.
type SyncHandler func(ctx context.Context, tx pgx.Tx, event Event) error

// AsyncHandler runs AFTER commit, invoked by the relay draining the outbox. It
// receives the JSON payload persisted at publish time and must be idempotent
// (the relay may redeliver). Returning an error leaves the outbox row
// unprocessed for retry.
type AsyncHandler func(ctx context.Context, payload []byte) error

// OutboxEnqueuer persists asynchronous events transactionally so a relay can
// deliver them after commit. The concrete implementation lives alongside the
// repository layer; the bus only depends on this narrow interface.
type OutboxEnqueuer interface {
	Enqueue(ctx context.Context, tx pgx.Tx, event Event) error
}

// Bus dispatches events to registered listeners. It is safe to assemble at
// startup and is not mutated after Subscribe* calls during wiring.
type Bus struct {
	syncListeners  map[string][]SyncHandler
	asyncListeners map[string]bool
	asyncHandlers  map[string][]AsyncHandler
	outbox         OutboxEnqueuer
}

// NewBus constructs a Bus. The outbox enqueuer may be nil when no asynchronous
// listeners are registered (e.g. early M0 wiring without a live DB).
func NewBus(outbox OutboxEnqueuer) *Bus {
	return &Bus{
		syncListeners:  make(map[string][]SyncHandler),
		asyncListeners: make(map[string]bool),
		asyncHandlers:  make(map[string][]AsyncHandler),
		outbox:         outbox,
	}
}

// SubscribeSync registers a synchronous in-transaction listener for an event
// name.
func (b *Bus) SubscribeSync(name string, h SyncHandler) {
	b.syncListeners[name] = append(b.syncListeners[name], h)
}

// SubscribeAsync marks an event name for asynchronous delivery via the outbox.
// Use SubscribeAsyncHandler to additionally register a post-commit handler the
// relay invokes when draining the outbox.
func (b *Bus) SubscribeAsync(name string) {
	b.asyncListeners[name] = true
}

// SubscribeAsyncHandler marks an event for async delivery AND registers a
// handler the relay invokes after commit with the persisted payload.
func (b *Bus) SubscribeAsyncHandler(name string, h AsyncHandler) {
	b.asyncListeners[name] = true
	b.asyncHandlers[name] = append(b.asyncHandlers[name], h)
}

// Dispatch invokes every async handler registered for eventName with the
// persisted payload. It is called by the outbox relay after commit. An error
// from any handler is returned so the relay can leave the row for retry. When no
// handler is registered the event is treated as delivered (returns nil) so the
// relay can mark it processed rather than looping forever.
func (b *Bus) Dispatch(ctx context.Context, eventName string, payload []byte) error {
	for _, h := range b.asyncHandlers[eventName] {
		if err := h(ctx, payload); err != nil {
			return fmt.Errorf("async handler for %q: %w", eventName, err)
		}
	}
	return nil
}

// ErrNoOutbox is returned when an event has an async listener but no outbox
// enqueuer was configured.
var ErrNoOutbox = errors.New("events: async listener registered but no outbox configured")

// Publish dispatches event within the caller's transaction tx. Synchronous
// listeners run immediately; an error from any of them is returned so the
// caller can roll back. If the event has async listeners, it is enqueued to the
// outbox inside the same transaction.
func (b *Bus) Publish(ctx context.Context, tx pgx.Tx, event Event) error {
	name := event.Name()

	for _, h := range b.syncListeners[name] {
		if err := h(ctx, tx, event); err != nil {
			return fmt.Errorf("sync listener for %q: %w", name, err)
		}
	}

	if b.asyncListeners[name] {
		if b.outbox == nil {
			return ErrNoOutbox
		}
		if err := b.outbox.Enqueue(ctx, tx, event); err != nil {
			return fmt.Errorf("enqueue %q: %w", name, err)
		}
	}

	return nil
}
