package accounts_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"

	"github.com/huseyn0w/agentic-cms-go/internal/accounts"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/events"
)

func slogDiscard() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// asyncCapture records the plaintext tokens carried by async account events as
// they are dispatched by the relay, standing in for the email listener.
type asyncCapture struct {
	mu        sync.Mutex
	verifyTok string
	resetTokV string
}

// captureAsync registers capturing async handlers on the bus so a relay Drain
// delivers events here (the same mechanism the real EmailListener uses).
func captureAsync(bus *events.Bus) *asyncCapture {
	c := &asyncCapture{}
	bus.SubscribeAsyncHandler(accounts.EventAccountRegistered, func(_ context.Context, payload []byte) error {
		var ev accounts.AccountRegisteredEvent
		if err := json.Unmarshal(payload, &ev); err != nil {
			return err
		}
		c.mu.Lock()
		c.verifyTok = ev.VerificationToken
		c.mu.Unlock()
		return nil
	})
	bus.SubscribeAsyncHandler(accounts.EventPasswordResetRequested, func(_ context.Context, payload []byte) error {
		var ev accounts.PasswordResetRequestedEvent
		if err := json.Unmarshal(payload, &ev); err != nil {
			return err
		}
		c.mu.Lock()
		c.resetTokV = ev.ResetToken
		c.mu.Unlock()
		return nil
	})
	return c
}

func (c *asyncCapture) verificationToken() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.verifyTok
}

func (c *asyncCapture) resetToken() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.resetTokV
}
