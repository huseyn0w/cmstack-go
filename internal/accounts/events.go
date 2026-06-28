package accounts

import "github.com/google/uuid"

// Event names. These are the canonical strings used for bus subscriptions and
// persisted as the outbox event_name. See BUILD_PLAN §5.
const (
	EventAccountRegistered      = "account.registered"
	EventPasswordResetRequested = "account.password_reset_requested"
)

// AccountRegisteredEvent is emitted (async) after a user registers. The async
// email listener consumes it to send the verification link. Only the data the
// listener needs is carried; the plaintext token is included so the listener
// can build the verification URL without re-deriving it (the DB stores only the
// hash).
type AccountRegisteredEvent struct {
	UserID            uuid.UUID `json:"user_id"`
	Email             string    `json:"email"`
	DisplayName       string    `json:"display_name"`
	VerificationToken string    `json:"verification_token"`
}

// Name implements events.Event.
func (AccountRegisteredEvent) Name() string { return EventAccountRegistered }

// PasswordResetRequestedEvent is emitted (async) when a reset is requested for
// an existing account. It carries the plaintext reset token so the listener can
// build the reset URL.
type PasswordResetRequestedEvent struct {
	UserID     uuid.UUID `json:"user_id"`
	Email      string    `json:"email"`
	ResetToken string    `json:"reset_token"`
}

// Name implements events.Event.
func (PasswordResetRequestedEvent) Name() string { return EventPasswordResetRequested }
