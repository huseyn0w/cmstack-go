package accounts

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/huseyn0w/cmstack-go/internal/platform/events"
)

// EmailListener consumes the async account events drained from the outbox and
// calls the Mailer to deliver verification / reset links. It is the honest
// async side-effect handler: the service emits events, the relay drains them
// after commit and calls Dispatch, which routes here.
type EmailListener struct {
	mailer  Mailer
	baseURL string
}

// NewEmailListener constructs an EmailListener. baseURL is the externally
// reachable base used to build absolute links.
func NewEmailListener(mailer Mailer, baseURL string) *EmailListener {
	return &EmailListener{mailer: mailer, baseURL: baseURL}
}

// Register subscribes the async account events on the bus and binds their
// post-commit handlers. Call this during wiring in BOTH the server (so events
// are marked async and enqueued) and the worker (so the relay can dispatch).
func (l *EmailListener) Register(bus *events.Bus) {
	bus.SubscribeAsyncHandler(EventAccountRegistered, l.onAccountRegistered)
	bus.SubscribeAsyncHandler(EventPasswordResetRequested, l.onPasswordResetRequested)
}

func (l *EmailListener) onAccountRegistered(ctx context.Context, payload []byte) error {
	var ev AccountRegisteredEvent
	if err := json.Unmarshal(payload, &ev); err != nil {
		return fmt.Errorf("unmarshal %s: %w", EventAccountRegistered, err)
	}
	verifyURL := l.link("/verify-email", ev.VerificationToken)
	return l.mailer.SendVerificationEmail(ctx, ev.Email, ev.DisplayName, verifyURL)
}

func (l *EmailListener) onPasswordResetRequested(ctx context.Context, payload []byte) error {
	var ev PasswordResetRequestedEvent
	if err := json.Unmarshal(payload, &ev); err != nil {
		return fmt.Errorf("unmarshal %s: %w", EventPasswordResetRequested, err)
	}
	resetURL := l.link("/reset-password", ev.ResetToken)
	return l.mailer.SendPasswordResetEmail(ctx, ev.Email, resetURL)
}

// link builds an absolute URL to path with the token query parameter.
func (l *EmailListener) link(path, token string) string {
	q := url.Values{}
	q.Set("token", token)
	return l.baseURL + path + "?" + q.Encode()
}
