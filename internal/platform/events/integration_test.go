package events_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // database/sql "pgx" driver for goose
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/huseyn0w/agentic-cms-go/internal/platform/db"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/events"
)

// probeEvent is a trivial async-capable event used by the integration test.
type probeEvent struct {
	EventName string `json:"-"`
	Value     string `json:"value"`
}

func (e probeEvent) Name() string { return e.EventName }

// migrationsDir resolves db/migrations relative to this test file, independent
// of the working directory.
func migrationsDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve caller for migrations path")
	}
	// this file: internal/platform/events/integration_test.go -> repo root is 3 up.
	root := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	return filepath.Join(root, "db", "migrations")
}

// startPostgres spins up a throwaway Postgres container, runs the goose
// migrations (so the outbox table exists), creates the tx_probe table, and
// returns a live pool. The container and pool are cleaned up via t.Cleanup.
func startPostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	pgC, err := postgres.Run(
		ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("agentic_cms_test"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForListeningPort("5432/tcp").WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(pgC); err != nil {
			t.Logf("terminate container: %v", err)
		}
	})

	dsn, err := pgC.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	// Run goose migrations via database/sql so the outbox table exists.
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("goose dialect: %v", err)
	}
	if err := goose.Up(sqlDB, migrationsDir(t)); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sql db: %v", err)
	}

	pool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(pool.Close)

	// Throwaway probe table representing the unit of work's own writes.
	if _, err := pool.Exec(ctx, `CREATE TABLE tx_probe (id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatalf("create tx_probe: %v", err)
	}
	return pool
}

func countRows(t *testing.T, pool *pgxpool.Pool, table string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// TestOutboxTransactionality proves the three guarantees of the events + outbox
// + RunInTx wiring against a real Postgres.
func TestOutboxTransactionality(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testcontainers integration test in -short mode")
	}

	pool := startPostgres(t)
	ctx := context.Background()

	// (a) sync in-tx listener commits with the unit of work.
	t.Run("sync listener writes commit with the unit of work", func(t *testing.T) {
		bus := events.NewBus(events.NewOutboxRepository())
		bus.SubscribeSync("probe.sync", func(ctx context.Context, tx pgx.Tx, ev events.Event) error {
			_, err := tx.Exec(ctx, `INSERT INTO tx_probe (value) VALUES ($1)`, "from-sync-listener")
			return err
		})

		before := countRows(t, pool, "tx_probe")
		err := db.RunInTx(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
			// The unit of work's own write.
			if _, err := tx.Exec(ctx, `INSERT INTO tx_probe (value) VALUES ($1)`, "unit-of-work"); err != nil {
				return err
			}
			return bus.Publish(ctx, tx, probeEvent{EventName: "probe.sync", Value: "x"})
		})
		if err != nil {
			t.Fatalf("RunInTx: %v", err)
		}
		if got := countRows(t, pool, "tx_probe"); got != before+2 {
			t.Fatalf("tx_probe rows = %d, want %d (uow + sync listener committed)", got, before+2)
		}
	})

	// (b) failing sync listener rolls the whole tx back: neither probe nor outbox row persists.
	t.Run("failing sync listener rolls back the whole unit of work", func(t *testing.T) {
		bus := events.NewBus(events.NewOutboxRepository())
		bus.SubscribeAsync("probe.fail")
		boom := errors.New("listener boom")
		bus.SubscribeSync("probe.fail", func(_ context.Context, _ pgx.Tx, _ events.Event) error {
			return boom
		})

		probeBefore := countRows(t, pool, "tx_probe")
		outboxBefore := countRows(t, pool, "outbox")

		err := db.RunInTx(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `INSERT INTO tx_probe (value) VALUES ($1)`, "should-roll-back"); err != nil {
				return err
			}
			return bus.Publish(ctx, tx, probeEvent{EventName: "probe.fail", Value: "y"})
		})
		if !errors.Is(err, boom) {
			t.Fatalf("expected boom error, got %v", err)
		}
		if got := countRows(t, pool, "tx_probe"); got != probeBefore {
			t.Errorf("tx_probe rows = %d, want %d (rolled back)", got, probeBefore)
		}
		if got := countRows(t, pool, "outbox"); got != outboxBefore {
			t.Errorf("outbox rows = %d, want %d (rolled back)", got, outboxBefore)
		}
	})

	// (c) async event is enqueued to outbox within the tx and visible after commit.
	t.Run("async event is written to outbox and visible after commit", func(t *testing.T) {
		bus := events.NewBus(events.NewOutboxRepository())
		bus.SubscribeAsync("probe.async")

		outboxBefore := countRows(t, pool, "outbox")
		err := db.RunInTx(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
			return bus.Publish(ctx, tx, probeEvent{EventName: "probe.async", Value: "z"})
		})
		if err != nil {
			t.Fatalf("RunInTx: %v", err)
		}
		if got := countRows(t, pool, "outbox"); got != outboxBefore+1 {
			t.Fatalf("outbox rows = %d, want %d (async enqueued + committed)", got, outboxBefore+1)
		}

		// Verify the persisted row contents.
		var name string
		var payload []byte
		if err := pool.QueryRow(
			ctx,
			`SELECT event_name, payload FROM outbox ORDER BY id DESC LIMIT 1`,
		).Scan(&name, &payload); err != nil {
			t.Fatalf("read outbox row: %v", err)
		}
		if name != "probe.async" {
			t.Errorf("event_name = %q, want probe.async", name)
		}
		if string(payload) == "" {
			t.Error("payload is empty")
		}
	})

	// (c, negative) async event absent if the tx rolls back.
	t.Run("async event absent when tx rolls back", func(t *testing.T) {
		bus := events.NewBus(events.NewOutboxRepository())
		bus.SubscribeAsync("probe.async.rollback")

		outboxBefore := countRows(t, pool, "outbox")
		wantErr := errors.New("uow failed after publish")
		err := db.RunInTx(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
			if pubErr := bus.Publish(ctx, tx, probeEvent{EventName: "probe.async.rollback", Value: "w"}); pubErr != nil {
				return pubErr
			}
			// Unit of work fails after the enqueue: the outbox insert must roll back too.
			return wantErr
		})
		if !errors.Is(err, wantErr) {
			t.Fatalf("expected wantErr, got %v", err)
		}
		if got := countRows(t, pool, "outbox"); got != outboxBefore {
			t.Errorf("outbox rows = %d, want %d (rolled back)", got, outboxBefore)
		}
	})
}
