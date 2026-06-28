package events

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/huseyn0w/cmstack-go/internal/platform/db"
	"github.com/huseyn0w/cmstack-go/internal/platform/db/sqlcgen"
)

// Dispatcher invokes async handlers for a persisted event. *Bus satisfies it.
type Dispatcher interface {
	Dispatch(ctx context.Context, eventName string, payload []byte) error
}

// Relay drains unprocessed rows from the outbox table after commit and
// dispatches them to the registered async handlers, marking each row processed
// on success. It is constructed honestly in cmd/worker with a real pool, the
// querier, and the wired bus as the dispatcher.
type Relay struct {
	pool       db.Beginner
	q          *sqlcgen.Queries
	dispatcher Dispatcher
	batchSize  int32
	log        *slog.Logger
}

// NewRelay constructs an outbox Relay over the given transaction-capable pool
// and dispatcher (the bus). A nil dispatcher disables dispatch (rows are
// observed only); pass the bus for real delivery.
func NewRelay(pool db.Beginner, dispatcher Dispatcher, batchSize int32, log *slog.Logger) *Relay {
	if batchSize <= 0 {
		batchSize = 100
	}
	return &Relay{
		pool:       pool,
		q:          sqlcgen.New(nil),
		dispatcher: dispatcher,
		batchSize:  batchSize,
		log:        log,
	}
}

// Drain runs one relay pass inside a transaction: it claims a batch of
// unprocessed outbox rows (FOR UPDATE SKIP LOCKED), dispatches each to its async
// handlers, and marks delivered rows processed within the same transaction so
// claim + dispatch + mark are atomic. The number of rows successfully processed
// is returned.
func (r *Relay) Drain(ctx context.Context) (int, error) {
	var processed int
	err := db.RunInTx(ctx, r.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		rows, err := q.FetchUnprocessedOutbox(ctx, r.batchSize)
		if err != nil {
			return fmt.Errorf("fetch unprocessed outbox: %w", err)
		}
		for _, row := range rows {
			if r.dispatcher != nil {
				if err := r.dispatcher.Dispatch(ctx, row.EventName, row.Payload); err != nil {
					// Leave this row (and the rest of the batch) for the next pass;
					// the row's lock releases on rollback so it is retried.
					return fmt.Errorf("dispatch outbox row %d (%s): %w", row.ID, row.EventName, err)
				}
			}
			if err := q.MarkOutboxProcessed(ctx, row.ID); err != nil {
				return fmt.Errorf("mark outbox row %d processed: %w", row.ID, err)
			}
			processed++
		}
		if processed > 0 {
			r.log.Debug("outbox relay processed rows", "count", processed)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return processed, nil
}
