// Package mailer provides transactional email delivery. The dev LogMailer logs
// the links instead of sending; a real SMTP implementation arrives in M14.
package mailer

import (
	"context"
	"log/slog"
)

// LogMailer logs verification and reset links at info level so they are visible
// in development without an SMTP server. It implements accounts.Mailer.
type LogMailer struct {
	log *slog.Logger
}

// NewLogMailer constructs a LogMailer. A nil logger falls back to slog.Default.
func NewLogMailer(log *slog.Logger) *LogMailer {
	if log == nil {
		log = slog.Default()
	}
	return &LogMailer{log: log}
}

// SendVerificationEmail logs the verification link.
func (m *LogMailer) SendVerificationEmail(_ context.Context, to, name, verifyURL string) error {
	m.log.Info("dev mailer: verification email",
		"to", to,
		"name", name,
		"verify_url", verifyURL,
	)
	return nil
}

// SendPasswordResetEmail logs the password-reset link.
func (m *LogMailer) SendPasswordResetEmail(_ context.Context, to, resetURL string) error {
	m.log.Info("dev mailer: password reset email",
		"to", to,
		"reset_url", resetURL,
	)
	return nil
}
