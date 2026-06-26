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
	outbox         OutboxEnqueuer
}

// NewBus constructs a Bus. The outbox enqueuer may be nil when no asynchronous
// listeners are registered (e.g. early M0 wiring without a live DB).
func NewBus(outbox OutboxEnqueuer) *Bus {
	return &Bus{
		syncListeners:  make(map[string][]SyncHandler),
		asyncListeners: make(map[string]bool),
		outbox:         outbox,
	}
}

// SubscribeSync registers a synchronous in-transaction listener for an event
// name.
func (b *Bus) SubscribeSync(name string, h SyncHandler) {
	b.syncListeners[name] = append(b.syncListeners[name], h)
}

// SubscribeAsync marks an event name for asynchronous delivery via the outbox.
func (b *Bus) SubscribeAsync(name string) {
	b.asyncListeners[name] = true
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
