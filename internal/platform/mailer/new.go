package mailer

import (
	"context"
	"fmt"
	"log/slog"
)

// Mailer is the union of every transactional-email method the app sends. It is
// the concrete return type of the factory: LogMailer, SMTPMailer and Noop all
// satisfy it. It is assignable to the narrower ports (accounts.Mailer — the
// verification+reset subset — and the comment/contact notifier adapter shapes),
// so a single selected instance wires every listener in both processes.
type Mailer interface {
	SendVerificationEmail(ctx context.Context, to, name, verifyURL string) error
	SendPasswordResetEmail(ctx context.Context, to, resetURL string) error
	SendCommentNotification(ctx context.Context, to []string, postTitle, authorName, excerpt, moderateURL string) error
	SendContactNotification(ctx context.Context, to, fromEmail, fromName, subject, message string) error
}

// Compile-time proof that every backend satisfies the union.
var (
	_ Mailer = (*LogMailer)(nil)
	_ Mailer = (*SMTPMailer)(nil)
	_ Mailer = (*Noop)(nil)
)

// Config selects and configures the mailer backend. It is a small local struct
// rather than a dependency on internal/platform/config, keeping this package
// import-cycle free; the caller in cmd/* maps its config.Config onto it.
type Config struct {
	// Driver selects the backend: "log" (default), "smtp" or "noop".
	Driver string
	// SMTP holds the SMTP settings, used only when Driver is "smtp".
	SMTP SMTPConfig
}

// New constructs the Mailer selected by cfg.Driver:
//
//   - "" or "log" -> *LogMailer (dev: logs the links)
//   - "noop"      -> *Noop (silent)
//   - "smtp"      -> *SMTPMailer over a go-mail client (errors if host/from missing)
//
// An unknown driver returns an error. The caller decides fallback policy (the
// cmd/* wiring logs and falls back to LogMailer on an smtp error so the app still
// boots).
func New(cfg Config, log *slog.Logger) (Mailer, error) {
	if log == nil {
		log = slog.Default()
	}
	switch cfg.Driver {
	case "", "log":
		return NewLogMailer(log), nil
	case "noop":
		return NewNoop(), nil
	case "smtp":
		return NewSMTP(cfg.SMTP, log)
	default:
		return nil, fmt.Errorf("mailer: unknown driver %q", cfg.Driver)
	}
}
