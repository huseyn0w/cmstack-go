package events

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/huseyn0w/agentic-cms-go/internal/platform/db/sqlcgen"
)

// OutboxRepository persists async events to the outbox table inside a
// transaction. The async relay (cmd/worker) drains unprocessed rows after
// commit. For M0 the relay itself is a documented stub.
//
// It delegates the actual INSERT to the sqlc-generated querier so the outbox
// table has a single source of truth (db/queries/outbox.sql).
type OutboxRepository struct {
	q *sqlcgen.Queries
}

// NewOutboxRepository constructs an OutboxRepository backed by the sqlc querier.
func NewOutboxRepository() *OutboxRepository {
	// The querier is bound to a transaction per Enqueue call via WithTx, so the
	// base Queries here carries no live DBTX. Constructing it with nil is safe
	// because every code path rebinds it through WithTx before issuing SQL.
	return &OutboxRepository{q: sqlcgen.New(nil)}
}

// Enqueue inserts the event into the outbox within tx via the sqlc querier. The
// payload is the JSON-marshalled event so the relay can reconstruct it.
func (r *OutboxRepository) Enqueue(ctx context.Context, tx pgx.Tx, event Event) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event %q: %w", event.Name(), err)
	}
	if err := r.q.WithTx(tx).EnqueueOutbox(ctx, sqlcgen.EnqueueOutboxParams{
		EventName: event.Name(),
		Payload:   payload,
	}); err != nil {
		return fmt.Errorf("insert outbox row: %w", err)
	}
	return nil
}
