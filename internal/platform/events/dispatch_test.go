package events

import (
	"context"
	"errors"
	"testing"
)

func TestDispatchInvokesRegisteredAsyncHandler(t *testing.T) {
	bus := NewBus(nil)
	var got []byte
	bus.SubscribeAsyncHandler("email.queued", func(_ context.Context, payload []byte) error {
		got = payload
		return nil
	})

	if err := bus.Dispatch(context.Background(), "email.queued", []byte(`{"x":1}`)); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if string(got) != `{"x":1}` {
		t.Errorf("handler received %q, want payload", string(got))
	}
}

func TestDispatchNoHandlerIsNoop(t *testing.T) {
	bus := NewBus(nil)
	if err := bus.Dispatch(context.Background(), "unhandled", []byte(`{}`)); err != nil {
		t.Fatalf("expected nil for unhandled event, got %v", err)
	}
}

func TestDispatchPropagatesHandlerError(t *testing.T) {
	bus := NewBus(nil)
	boom := errors.New("boom")
	bus.SubscribeAsyncHandler("email.queued", func(context.Context, []byte) error { return boom })

	err := bus.Dispatch(context.Background(), "email.queued", nil)
	if !errors.Is(err, boom) {
		t.Fatalf("expected boom, got %v", err)
	}
}
