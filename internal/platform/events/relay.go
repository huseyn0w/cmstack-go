package events

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/huseyn0w/agentic-cms-go/internal/platform/db"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/db/sqlcgen"
)

// Dispatcher invokes async handlers for a persisted event. *Bus satisfies it.
type Dispatcher interface {
	Dispatch(ctx context.Context, eventName string, payload []byte) error
}

// defaultMaxAttempts bounds how many times a single outbox row may fail dispatch
// before it is dead-lettered (failed_at stamped) so a poison event cannot loop
// forever.
const defaultMaxAttempts = 5

// Relay drains unprocessed rows from the outbox table after commit and
// dispatches them to the registered async handlers, marking each row processed
// on success. It is constructed honestly in cmd/worker with a real pool, the
// querier, and the wired bus as the dispatcher.
type Relay struct {
	pool        db.Beginner
	q           *sqlcgen.Queries
	dispatcher  Dispatcher
	batchSize   int32
	maxAttempts int32
	log         *slog.Logger
}

// NewRelay constructs an outbox Relay over the given transaction-capable pool
// and dispatcher (the bus). A nil dispatcher disables dispatch (rows are
// observed only); pass the bus for real delivery.
func NewRelay(pool db.Beginner, dispatcher Dispatcher, batchSize int32, log *slog.Logger) *Relay {
	return NewRelayWithMaxAttempts(pool, dispatcher, batchSize, defaultMaxAttempts, log)
}

// NewRelayWithMaxAttempts is NewRelay with an explicit dead-letter threshold. A
// non-positive maxAttempts falls back to defaultMaxAttempts.
func NewRelayWithMaxAttempts(pool db.Beginner, dispatcher Dispatcher, batchSize, maxAttempts int32, log *slog.Logger) *Relay {
	if batchSize <= 0 {
		batchSize = 100
	}
	if maxAttempts <= 0 {
		maxAttempts = defaultMaxAttempts
	}
	return &Relay{
		pool:        pool,
		q:           sqlcgen.New(nil),
		dispatcher:  dispatcher,
		batchSize:   batchSize,
		maxAttempts: maxAttempts,
		log:         log,
	}
}

// Drain runs one relay pass inside a transaction: it claims a batch of
// unprocessed outbox rows (FOR UPDATE SKIP LOCKED) and dispatches each to its
// async handlers. Successful rows are marked processed in the same transaction
// so claim + dispatch + mark are atomic.
//
// Dispatch errors are ISOLATED PER ROW: a failing event records an attempt (and
// last_error) and the relay advances to the next row, so one poison event can
// never block its siblings (no head-of-line blocking). A row that exhausts
// maxAttempts is dead-lettered (failed_at stamped) so it can no longer be
// fetched and cannot loop forever; failures below the threshold leave the row
// for a later retry pass. The number of rows successfully processed is returned.
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
				if derr := r.dispatcher.Dispatch(ctx, row.EventName, row.Payload); derr != nil {
					if herr := r.handleDispatchFailure(ctx, q, row, derr); herr != nil {
						return herr
					}
					continue
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

// handleDispatchFailure records a failed dispatch for a single row without
// failing the batch. When the row's attempts reach maxAttempts it is
// dead-lettered (failed_at stamped) and dropped from future fetches; otherwise
// the attempt/last_error is recorded and the row is left for a retry pass. The
// returned error is only non-nil on a DB write failure (which must roll the
// batch back), never on the dispatch error itself.
func (r *Relay) handleDispatchFailure(ctx context.Context, q *sqlcgen.Queries, row sqlcgen.Outbox, derr error) error {
	msg := derr.Error()
	if row.Attempts+1 >= r.maxAttempts {
		r.log.Error("outbox relay dead-lettering poison event",
			"id", row.ID, "event", row.EventName, "attempts", row.Attempts+1, "error", msg)
		if err := q.DeadLetterOutbox(ctx, sqlcgen.DeadLetterOutboxParams{ID: row.ID, LastError: &msg}); err != nil {
			return fmt.Errorf("dead-letter outbox row %d: %w", row.ID, err)
		}
		return nil
	}
	r.log.Warn("outbox relay dispatch failed; will retry",
		"id", row.ID, "event", row.EventName, "attempts", row.Attempts+1, "error", msg)
	if err := q.RecordOutboxFailure(ctx, sqlcgen.RecordOutboxFailureParams{ID: row.ID, LastError: &msg}); err != nil {
		return fmt.Errorf("record outbox row %d failure: %w", row.ID, err)
	}
	return nil
}
