package db

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

// fakeTx records commit/rollback calls for RunInTx unit tests.
type fakeTx struct {
	pgx.Tx
	committed  bool
	rolledBack bool
}

func (f *fakeTx) Commit(context.Context) error   { f.committed = true; return nil }
func (f *fakeTx) Rollback(context.Context) error { f.rolledBack = true; return nil }

type fakeBeginner struct {
	tx     *fakeTx
	begErr error
}

func (b *fakeBeginner) Begin(context.Context) (pgx.Tx, error) {
	if b.begErr != nil {
		return nil, b.begErr
	}
	return b.tx, nil
}

func TestRunInTxCommitsOnSuccess(t *testing.T) {
	tx := &fakeTx{}
	b := &fakeBeginner{tx: tx}
	err := RunInTx(context.Background(), b, func(context.Context, pgx.Tx) error { return nil })
	if err != nil {
		t.Fatalf("RunInTx: %v", err)
	}
	if !tx.committed {
		t.Error("expected commit")
	}
	if tx.rolledBack {
		t.Error("did not expect rollback")
	}
}

func TestRunInTxRollsBackOnError(t *testing.T) {
	tx := &fakeTx{}
	b := &fakeBeginner{tx: tx}
	boom := errors.New("boom")
	err := RunInTx(context.Background(), b, func(context.Context, pgx.Tx) error { return boom })
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if tx.committed {
		t.Error("did not expect commit")
	}
	if !tx.rolledBack {
		t.Error("expected rollback")
	}
}

func TestRunInTxRollsBackOnPanic(t *testing.T) {
	tx := &fakeTx{}
	b := &fakeBeginner{tx: tx}
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic to propagate")
		}
		if !tx.rolledBack {
			t.Error("expected rollback on panic")
		}
		if tx.committed {
			t.Error("did not expect commit on panic")
		}
	}()
	_ = RunInTx(context.Background(), b, func(context.Context, pgx.Tx) error {
		panic("kaboom")
	})
}

func TestRunInTxBeginError(t *testing.T) {
	b := &fakeBeginner{begErr: errors.New("no conn")}
	err := RunInTx(context.Background(), b, func(context.Context, pgx.Tx) error { return nil })
	if err == nil {
		t.Fatal("expected begin error")
	}
}
