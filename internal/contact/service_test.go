package contact

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/huseyn0w/agentic-cms-go/internal/platform/events"
)

// --- test doubles ------------------------------------------------------------

// fakeTx is a no-op pgx.Tx; the recording bus ignores it.
type fakeTx struct{ pgx.Tx }

func (fakeTx) Commit(context.Context) error   { return nil }
func (fakeTx) Rollback(context.Context) error { return nil }

// fakeBeginner runs RunInTx against a no-op tx.
type fakeBeginner struct{}

func (fakeBeginner) Begin(context.Context) (pgx.Tx, error) { return fakeTx{}, nil }

// recordingBus captures published events.
type recordingBus struct {
	events []events.Event
	err    error
}

func (b *recordingBus) Publish(_ context.Context, _ pgx.Tx, e events.Event) error {
	if b.err != nil {
		return b.err
	}
	b.events = append(b.events, e)
	return nil
}

// fakeVerifier returns a fixed verification outcome.
type fakeVerifier struct {
	ok  bool
	err error
}

func (v fakeVerifier) Verify(context.Context, string) (bool, error) { return v.ok, v.err }

// fakeLimiter allows or denies every request.
type fakeLimiter struct{ allow bool }

func (l fakeLimiter) Allow(string) bool { return l.allow }

func validInput() Input {
	return Input{
		Name:           "Ada Lovelace",
		Email:          "ada@example.com",
		Subject:        "Hello",
		Message:        "I would like to get in touch.",
		RecaptchaToken: "tok",
		RemoteIP:       "203.0.113.7",
	}
}

// --- tests -------------------------------------------------------------------

func TestSubmit_RateLimited(t *testing.T) {
	bus := &recordingBus{}
	svc := NewService(fakeBeginner{}, fakeVerifier{ok: true}, fakeLimiter{allow: false}, bus)
	err := svc.Submit(context.Background(), validInput())
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
	if len(bus.events) != 0 {
		t.Fatalf("rate-limited submit must not publish; got %d", len(bus.events))
	}
}

func TestSubmit_Invalid(t *testing.T) {
	cases := []struct {
		name  string
		in    Input
		field string
	}{
		{"missing name", Input{Email: "a@b.com", Message: "hi"}, "name"},
		{"missing email", Input{Name: "A", Message: "hi"}, "email"},
		{"bad email", Input{Name: "A", Email: "not-an-email", Message: "hi"}, "email"},
		{"missing message", Input{Name: "A", Email: "a@b.com"}, "message"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bus := &recordingBus{}
			svc := NewService(fakeBeginner{}, fakeVerifier{ok: true}, fakeLimiter{allow: true}, bus)
			err := svc.Submit(context.Background(), tc.in)
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("err = %v, want ErrInvalid", err)
			}
			var ve ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("err = %v, want ValidationError", err)
			}
			if ve.Field != tc.field {
				t.Fatalf("field = %q, want %q", ve.Field, tc.field)
			}
			if len(bus.events) != 0 {
				t.Fatalf("invalid submit must not publish")
			}
		})
	}
}

func TestSubmit_RecaptchaFalse(t *testing.T) {
	bus := &recordingBus{}
	svc := NewService(fakeBeginner{}, fakeVerifier{ok: false}, fakeLimiter{allow: true}, bus)
	err := svc.Submit(context.Background(), validInput())
	if !errors.Is(err, ErrRecaptcha) {
		t.Fatalf("err = %v, want ErrRecaptcha", err)
	}
	if len(bus.events) != 0 {
		t.Fatalf("recaptcha failure must not publish")
	}
}

func TestSubmit_RecaptchaError(t *testing.T) {
	bus := &recordingBus{}
	svc := NewService(fakeBeginner{}, fakeVerifier{err: errors.New("timeout")}, fakeLimiter{allow: true}, bus)
	if err := svc.Submit(context.Background(), validInput()); !errors.Is(err, ErrRecaptcha) {
		t.Fatalf("err = %v, want ErrRecaptcha", err)
	}
}

func TestSubmit_HappyPublishesEvent(t *testing.T) {
	bus := &recordingBus{}
	fixed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	svc := NewService(fakeBeginner{}, fakeVerifier{ok: true}, fakeLimiter{allow: true}, bus).
		WithClock(func() time.Time { return fixed })

	if err := svc.Submit(context.Background(), validInput()); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if len(bus.events) != 1 {
		t.Fatalf("published %d events, want 1", len(bus.events))
	}
	ev, ok := bus.events[0].(SubmittedEvent)
	if !ok {
		t.Fatalf("event type = %T, want SubmittedEvent", bus.events[0])
	}
	if ev.Name() != EventContactSubmitted {
		t.Fatalf("event name = %q, want %q", ev.Name(), EventContactSubmitted)
	}
	if ev.FromName != "Ada Lovelace" || ev.FromEmail != "ada@example.com" || ev.Subject != "Hello" {
		t.Fatalf("event fields not composed from input: %+v", ev)
	}
	if !ev.SubmittedAt.Equal(fixed) {
		t.Fatalf("SubmittedAt = %v, want %v", ev.SubmittedAt, fixed)
	}
}

func TestSubmit_NoVerifierNoLimiter(t *testing.T) {
	// With nil verifier + nil limiter, a valid submission still publishes.
	bus := &recordingBus{}
	svc := NewService(fakeBeginner{}, nil, nil, bus)
	if err := svc.Submit(context.Background(), validInput()); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if len(bus.events) != 1 {
		t.Fatalf("published %d events, want 1", len(bus.events))
	}
}
