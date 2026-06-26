package events

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

type testEvent struct {
	name string
}

func (e testEvent) Name() string { return e.name }

func TestPublishSyncListenerRuns(t *testing.T) {
	bus := NewBus(nil)
	called := false
	bus.SubscribeSync("user.created", func(_ context.Context, _ pgx.Tx, ev Event) error {
		called = true
		if ev.Name() != "user.created" {
			t.Errorf("unexpected event name %q", ev.Name())
		}
		return nil
	})

	if err := bus.Publish(context.Background(), nil, testEvent{name: "user.created"}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if !called {
		t.Fatal("sync listener was not invoked")
	}
}

func TestPublishSyncListenerErrorSurfaces(t *testing.T) {
	bus := NewBus(nil)
	boom := errors.New("boom")
	bus.SubscribeSync("user.created", func(_ context.Context, _ pgx.Tx, _ Event) error {
		return boom
	})

	err := bus.Publish(context.Background(), nil, testEvent{name: "user.created"})
	if err == nil {
		t.Fatal("expected error from failing sync listener")
	}
	if !errors.Is(err, boom) {
		t.Errorf("error %v does not wrap boom", err)
	}
}

func TestPublishNoListenersIsNoop(t *testing.T) {
	bus := NewBus(nil)
	if err := bus.Publish(context.Background(), nil, testEvent{name: "unhandled"}); err != nil {
		t.Fatalf("expected nil for unhandled event, got %v", err)
	}
}

func TestPublishAsyncWithoutOutboxFails(t *testing.T) {
	bus := NewBus(nil)
	bus.SubscribeAsync("email.queued")
	err := bus.Publish(context.Background(), nil, testEvent{name: "email.queued"})
	if !errors.Is(err, ErrNoOutbox) {
		t.Fatalf("expected ErrNoOutbox, got %v", err)
	}
}
