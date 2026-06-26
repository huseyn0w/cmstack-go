package events

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/huseyn0w/cmstack-go/internal/platform/db"
	"github.com/huseyn0w/cmstack-go/internal/platform/db/sqlcgen"
)

// Relay drains unprocessed rows from the outbox table after commit and would
// dispatch them to async listeners. It is constructed honestly in cmd/worker:
// it holds a real pool and querier and runs a real fetch transaction. Actual
// dispatch to registered async listeners is deferred to a later milestone, so
// Drain currently fetches a batch and logs it without marking rows processed.
type Relay struct {
	pool      db.Beginner
	q         *sqlcgen.Queries
	batchSize int32
	log       *slog.Logger
}

// NewRelay constructs an outbox Relay over the given transaction-capable pool.
func NewRelay(pool db.Beginner, batchSize int32, log *slog.Logger) *Relay {
	if batchSize <= 0 {
		batchSize = 100
	}
	return &Relay{
		pool:      pool,
		q:         sqlcgen.New(nil),
		batchSize: batchSize,
		log:       log,
	}
}

// Drain runs one relay pass inside a transaction: it claims a batch of
// unprocessed outbox rows (FOR UPDATE SKIP LOCKED) and returns them. Dispatch to
// async listeners is a TODO(M1); for now rows are observed but left unprocessed
// so no events are silently lost before dispatch exists.
func (r *Relay) Drain(ctx context.Context) (int, error) {
	var n int
	err := db.RunInTx(ctx, r.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := r.q.WithTx(tx).FetchUnprocessedOutbox(ctx, r.batchSize)
		if err != nil {
			return fmt.Errorf("fetch unprocessed outbox: %w", err)
		}
		n = len(rows)
		// TODO(M1): dispatch each row to its async listener(s) and call
		// MarkOutboxProcessed on success. Until dispatch exists we intentionally
		// do not mark rows processed.
		if n > 0 {
			r.log.Debug("outbox relay observed unprocessed rows", "count", n)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return n, nil
}
