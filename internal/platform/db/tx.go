package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Beginner is the minimal contract RunInTx needs to start a transaction. The
// pgxpool.Pool satisfies it; tests can supply a fake.
type Beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// RunInTx is the single blessed path for executing a unit of work inside a pgx
// transaction. It begins a transaction, runs fn, and commits when fn returns
// nil. If fn returns an error or panics, the transaction is rolled back and the
// error (or panic) is propagated. Event publishing that must be transactional
// (sync listeners + outbox enqueue) MUST go through here so the unit of work
// and its events commit or roll back atomically.
func RunInTx(ctx context.Context, pool Beginner, fn func(ctx context.Context, tx pgx.Tx) error) (err error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			// Best-effort rollback, then re-panic so callers observe the panic.
			_ = tx.Rollback(ctx)
			panic(p)
		}
		if err != nil {
			if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
				err = fmt.Errorf("%w (rollback failed: %v)", err, rbErr)
			}
		}
	}()

	if err = fn(ctx, tx); err != nil {
		return err
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// compile-time assertion that the concrete pool satisfies Beginner.
var _ Beginner = (*pgxpool.Pool)(nil)
