package events_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/huseyn0w/cmstack-go/internal/platform/db"
	"github.com/huseyn0w/cmstack-go/internal/platform/events"
)

func slogDiscard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// outboxState reads the bookkeeping columns for a single event_name.
func outboxState(t *testing.T, pool *pgxpool.Pool, eventName string) (attempts int, processed bool, failed bool) {
	t.Helper()
	var (
		att         int32
		processedAt pgtype.Timestamptz
		failedAt    pgtype.Timestamptz
	)
	err := pool.QueryRow(context.Background(),
		`SELECT attempts, processed_at, failed_at FROM outbox WHERE event_name = $1 ORDER BY id DESC LIMIT 1`,
		eventName,
	).Scan(&att, &processedAt, &failedAt)
	if err != nil {
		t.Fatalf("read outbox state for %s: %v", eventName, err)
	}
	return int(att), processedAt.Valid, failedAt.Valid
}

// TestRelayIsolatesPoisonEvent guards Fix 5: a batch containing one
// always-failing ("poison") event must still process the good events instead of
// stalling the whole batch (no head-of-line blocking), and the poison event must
// stop being retried once it exhausts maxAttempts (dead-lettered).
func TestRelayIsolatesPoisonEvent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testcontainers integration test in -short mode")
	}
	pool := startPostgres(t)
	ctx := context.Background()

	const (
		goodA  = "probe.good.a"
		poison = "probe.poison"
		goodB  = "probe.good.b"
		maxAtt = 3
	)

	bus := events.NewBus(events.NewOutboxRepository())
	bus.SubscribeAsync(goodA)
	bus.SubscribeAsync(poison)
	bus.SubscribeAsync(goodB)

	var goodADelivered, goodBDelivered int
	bus.SubscribeAsyncHandler(goodA, func(context.Context, []byte) error { goodADelivered++; return nil })
	bus.SubscribeAsyncHandler(goodB, func(context.Context, []byte) error { goodBDelivered++; return nil })
	bus.SubscribeAsyncHandler(poison, func(context.Context, []byte) error {
		return errors.New("poison: always fails")
	})

	// Enqueue good, poison, good in order so the poison sits between siblings.
	if err := db.RunInTx(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
		if err := bus.Publish(ctx, tx, probeEvent{EventName: goodA, Value: "a"}); err != nil {
			return err
		}
		if err := bus.Publish(ctx, tx, probeEvent{EventName: poison, Value: "p"}); err != nil {
			return err
		}
		return bus.Publish(ctx, tx, probeEvent{EventName: goodB, Value: "b"})
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	relay := events.NewRelayWithMaxAttempts(pool, bus, 100, maxAtt, slogDiscard())

	// First pass: both good events process despite the poison sibling failing.
	processed, err := relay.Drain(ctx)
	if err != nil {
		t.Fatalf("Drain pass 1: %v", err)
	}
	if processed != 2 {
		t.Fatalf("pass 1 processed = %d, want 2 (both good events, poison isolated)", processed)
	}
	if goodADelivered != 1 || goodBDelivered != 1 {
		t.Fatalf("good events not delivered: a=%d b=%d", goodADelivered, goodBDelivered)
	}

	// The good events are marked processed; the poison event recorded one attempt
	// and is NOT yet dead-lettered (below threshold).
	if att, proc, _ := outboxState(t, pool, goodA); !proc {
		t.Errorf("goodA should be processed (attempts=%d)", att)
	}
	if att, proc, failed := outboxState(t, pool, poison); proc || failed || att != 1 {
		t.Fatalf("poison after pass 1: attempts=%d processed=%v failed=%v, want attempts=1 not processed/failed", att, proc, failed)
	}

	// Subsequent passes only re-fetch the still-unprocessed, not-failed poison row.
	for pass := 2; ; pass++ {
		n, err := relay.Drain(ctx)
		if err != nil {
			t.Fatalf("Drain pass %d: %v", pass, err)
		}
		if n != 0 {
			t.Fatalf("pass %d processed = %d, want 0 (only the poison row remains)", pass, n)
		}
		att, _, failed := outboxState(t, pool, poison)
		if failed {
			if att != maxAtt {
				t.Fatalf("dead-lettered poison attempts = %d, want %d", att, maxAtt)
			}
			break
		}
		if pass > maxAtt+2 {
			t.Fatalf("poison event never dead-lettered after %d passes (attempts=%d)", pass, att)
		}
	}

	// Once dead-lettered the poison row is no longer fetched: the relay is stable.
	n, err := relay.Drain(ctx)
	if err != nil {
		t.Fatalf("final Drain: %v", err)
	}
	if n != 0 {
		t.Fatalf("final pass processed = %d, want 0 (poison dead-lettered, nothing left)", n)
	}
}
