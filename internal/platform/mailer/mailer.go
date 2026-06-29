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

// CommentNotification is the dev-mailer view of a comment-moderation email. It
// mirrors comments.CommentNotification structurally so the LogMailer can satisfy
// the comments.CommentNotifier interface without importing the comments package
// (avoiding an import cycle): the comments package passes its own struct whose
// fields are read here via the adapter in the web wiring. To keep the LogMailer
// self-contained, it accepts the already-composed fields.

// SendCommentNotification logs the comment-moderation notification (dev). The
// recipients + composed message are visible in development without SMTP. The
// signature matches comments.CommentNotifier via a thin wrapper in wiring, but
// is also directly usable.
func (m *LogMailer) SendCommentNotification(_ context.Context, to []string, postTitle, authorName, excerpt, moderateURL string) error {
	m.log.Info("dev mailer: comment notification email",
		"to", to,
		"post_title", postTitle,
		"author", authorName,
		"excerpt", excerpt,
		"moderate_url", moderateURL,
	)
	return nil
}
