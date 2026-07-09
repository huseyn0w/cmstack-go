package mailer

import "context"

// Noop is a silent mailer: every method is a no-op returning nil. It is used in
// tests/CI where sending (or logging) transactional email is undesirable, and
// as the "noop" driver of the factory. It satisfies the Mailer union interface.
type Noop struct{}

// NewNoop constructs a Noop mailer.
func NewNoop() *Noop { return &Noop{} }

// SendVerificationEmail does nothing and returns nil.
func (Noop) SendVerificationEmail(_ context.Context, _, _, _ string) error { return nil }

// SendPasswordResetEmail does nothing and returns nil.
func (Noop) SendPasswordResetEmail(_ context.Context, _, _ string) error { return nil }

// SendCommentNotification does nothing and returns nil.
func (Noop) SendCommentNotification(_ context.Context, _ []string, _, _, _, _ string) error {
	return nil
}

// SendContactNotification does nothing and returns nil.
func (Noop) SendContactNotification(_ context.Context, _, _, _, _, _ string) error { return nil }
