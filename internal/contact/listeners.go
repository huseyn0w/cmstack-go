package contact

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/huseyn0w/cmstack-go/internal/platform/events"
)

// Notifier sends the contact-notification email. The platform LogMailer
// (dev) satisfies it via a thin wrapper in the web wiring; SMTP arrives later.
// Declaring the narrow interface here keeps contact decoupled from the mailer
// package and trivially fakeable.
type Notifier interface {
	// SendContactNotification delivers the composed contact email to `to`,
	// carrying the submitter's from-email/from-name and the subject/message.
	SendContactNotification(ctx context.Context, to, fromEmail, fromName, subject, message string) error
}

// RecipientResolver resolves the recipient the contact email is sent to. The web
// wiring composes it over the settings store + config default (settings key
// `contact_recipient` → ContactRecipient → AdminEmail). An empty result means
// "no recipient configured" and the listener no-ops.
type RecipientResolver interface {
	Recipient(ctx context.Context) string
}

// NotifyListener consumes the async contact.submitted events drained from the
// outbox and sends the contact-notification email to the resolved recipient. It
// is fault-tolerant by the relay's retry semantics; a send failure leaves the
// outbox row for retry. An empty recipient is a benign no-op (it does NOT error
// the drain).
type NotifyListener struct {
	log       *slog.Logger
	recipient RecipientResolver
	notifier  Notifier
}

// NewNotifyListener constructs the listener. A nil logger falls back to
// slog.Default.
func NewNotifyListener(log *slog.Logger, recipient RecipientResolver, notifier Notifier) *NotifyListener {
	if log == nil {
		log = slog.Default()
	}
	return &NotifyListener{log: log, recipient: recipient, notifier: notifier}
}

// Register subscribes the async contact.submitted handler. Call in BOTH the
// server (so the event is marked async + enqueued in-tx) and the worker (so the
// relay dispatches it after commit).
func (l *NotifyListener) Register(bus *events.Bus) {
	bus.SubscribeAsyncHandler(EventContactSubmitted, l.onContactSubmitted)
}

func (l *NotifyListener) onContactSubmitted(ctx context.Context, payload []byte) error {
	var ev SubmittedEvent
	if err := json.Unmarshal(payload, &ev); err != nil {
		return fmt.Errorf("unmarshal %s: %w", EventContactSubmitted, err)
	}

	to := l.recipient.Recipient(ctx)
	if to == "" {
		l.log.Warn("contact notification: no recipient configured; dropping", "from", ev.FromEmail)
		return nil
	}

	if err := l.notifier.SendContactNotification(ctx, to, ev.FromEmail, ev.FromName, ev.Subject, ev.Message); err != nil {
		return fmt.Errorf("send contact notification: %w", err)
	}
	return nil
}
