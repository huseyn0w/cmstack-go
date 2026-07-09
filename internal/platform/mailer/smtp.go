package mailer

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"strings"

	"github.com/wneessen/go-mail"
)

// SMTPConfig configures the SMTP backend. It is a small local struct rather than
// a dependency on internal/platform/config, keeping this package import-cycle
// free; the caller in cmd/* maps its config.Config onto it.
type SMTPConfig struct {
	// Host / Port address the SMTP server (e.g. "smtp.example.com", 587).
	Host string
	Port int
	// Username / Password authenticate with the server. When Username is empty no
	// SMTP auth is configured (open relay / local MTA).
	Username string
	Password string
	// From is the envelope + header From address (required). FromName is the
	// optional display name shown to recipients.
	From     string
	FromName string
	// TLS selects the transport security: "starttls" (default), "tls" (implicit
	// TLS) or "none" (unencrypted). An unknown value falls back to starttls.
	TLS string
}

// sender is the injectable seam through which a composed message is delivered.
// Production uses a sender that dials the configured SMTP client and sends; tests
// swap in a capturing sender so no network is required.
type sender func(ctx context.Context, msg *mail.Msg) error

// SMTPMailer sends transactional email over SMTP via github.com/wneessen/go-mail.
// It implements the Mailer union interface, so it drop-in satisfies
// accounts.Mailer and the comment/contact notifier adapter shapes. Each method
// composes a multipart/alternative message (text/plain + text/html) and delivers
// it through the injectable sender seam.
type SMTPMailer struct {
	cfg  SMTPConfig
	log  *slog.Logger
	send sender
}

// NewSMTP builds an SMTPMailer over a go-mail client configured from cfg
// (host/port/auth/TLS). It returns an error when Host or From is missing, or when
// the client cannot be constructed. The default sender dials the client and sends
// on each call; tests may override it via withSender.
func NewSMTP(cfg SMTPConfig, log *slog.Logger) (*SMTPMailer, error) {
	if log == nil {
		log = slog.Default()
	}
	if strings.TrimSpace(cfg.Host) == "" {
		return nil, fmt.Errorf("mailer: SMTP host is required")
	}
	if strings.TrimSpace(cfg.From) == "" {
		return nil, fmt.Errorf("mailer: MAIL_FROM (sender address) is required")
	}

	opts := []mail.Option{
		mail.WithPort(cfg.Port),
		mail.WithTLSPolicy(tlsPolicy(cfg.TLS)),
	}
	if strings.EqualFold(cfg.TLS, "tls") {
		opts = append(opts, mail.WithSSL())
	}
	if cfg.Username != "" {
		opts = append(
			opts,
			mail.WithSMTPAuth(mail.SMTPAuthPlain),
			mail.WithUsername(cfg.Username),
			mail.WithPassword(cfg.Password),
		)
	}

	client, err := mail.NewClient(cfg.Host, opts...)
	if err != nil {
		return nil, fmt.Errorf("mailer: build SMTP client: %w", err)
	}

	m := &SMTPMailer{cfg: cfg, log: log}
	m.send = func(ctx context.Context, msg *mail.Msg) error {
		return client.DialAndSendWithContext(ctx, msg)
	}
	return m, nil
}

// withSender overrides the delivery seam. Test-only: it lets a capturing sender
// stand in for the network so message composition can be asserted offline.
func (m *SMTPMailer) withSender(s sender) *SMTPMailer {
	m.send = s
	return m
}

// tlsPolicy maps the config string onto a go-mail TLSPolicy. Unknown values fall
// back to the safe default (mandatory STARTTLS).
func tlsPolicy(v string) mail.TLSPolicy {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "none":
		return mail.NoTLS
	case "tls":
		// Implicit TLS is enabled via WithSSL; the negotiated policy is still
		// mandatory for the wrapped connection.
		return mail.TLSMandatory
	case "starttls", "":
		return mail.TLSMandatory
	default:
		return mail.TLSMandatory
	}
}

// newMessage builds a base message with From/To/Subject set. Callers add the
// text + html bodies (and optionally Reply-To). Returned errors surface a
// malformed From/To address.
func (m *SMTPMailer) newMessage(subject string, to ...string) (*mail.Msg, error) {
	msg := mail.NewMsg()
	if m.cfg.FromName != "" {
		if err := msg.FromFormat(m.cfg.FromName, m.cfg.From); err != nil {
			return nil, fmt.Errorf("mailer: set from: %w", err)
		}
	} else if err := msg.From(m.cfg.From); err != nil {
		return nil, fmt.Errorf("mailer: set from: %w", err)
	}
	if err := msg.To(to...); err != nil {
		return nil, fmt.Errorf("mailer: set recipients: %w", err)
	}
	msg.Subject(subject)
	return msg, nil
}

// setBodies attaches the plain-text body and the HTML alternative to msg.
func setBodies(msg *mail.Msg, text, htmlBody string) {
	msg.SetBodyString(mail.TypeTextPlain, text)
	msg.AddAlternativeString(mail.TypeTextHTML, htmlBody)
}

// htmlDoc wraps an HTML fragment in a minimal, on-brand document shell.
func htmlDoc(inner string) string {
	return `<!doctype html><html><body style="font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;line-height:1.5;color:#1a1a1a">` +
		inner +
		`</body></html>`
}

// button renders a simple on-brand HTML link/button. label is static (caller
// controlled); href is escaped as an attribute value.
func button(href, label string) string {
	return `<p><a href="` + html.EscapeString(href) +
		`" style="display:inline-block;padding:10px 18px;background:#1a1a1a;color:#fff;text-decoration:none;border-radius:6px">` +
		label + `</a></p>`
}

// SendVerificationEmail sends the email-confirmation link.
func (m *SMTPMailer) SendVerificationEmail(ctx context.Context, to, name, verifyURL string) error {
	msg, err := m.newMessage("Confirm your email", to)
	if err != nil {
		return err
	}
	greeting := "Hello"
	if strings.TrimSpace(name) != "" {
		greeting = "Hello " + name
	}
	text := greeting + ",\n\nConfirm your email address to finish setting up your account:\n" +
		verifyURL + "\n\nIf you did not create this account, you can ignore this message.\n"
	htmlBody := htmlDoc(
		"<h2>Confirm your email</h2>" +
			"<p>" + html.EscapeString(greeting) + ",</p>" +
			"<p>Confirm your email address to finish setting up your account.</p>" +
			button(verifyURL, "Confirm email") +
			`<p style="color:#666;font-size:13px">If you did not create this account, you can ignore this message.</p>`,
	)
	setBodies(msg, text, htmlBody)
	return m.deliver(ctx, msg)
}

// SendPasswordResetEmail sends the password-reset link.
func (m *SMTPMailer) SendPasswordResetEmail(ctx context.Context, to, resetURL string) error {
	msg, err := m.newMessage("Reset your password", to)
	if err != nil {
		return err
	}
	text := "We received a request to reset your password. Use the link below to choose a new one:\n" +
		resetURL + "\n\nIf you did not request this, you can safely ignore this message.\n"
	htmlBody := htmlDoc(
		"<h2>Reset your password</h2>" +
			"<p>We received a request to reset your password. Use the button below to choose a new one.</p>" +
			button(resetURL, "Reset password") +
			`<p style="color:#666;font-size:13px">If you did not request this, you can safely ignore this message.</p>`,
	)
	setBodies(msg, text, htmlBody)
	return m.deliver(ctx, msg)
}

// SendCommentNotification sends the comment-moderation notification to moderators.
func (m *SMTPMailer) SendCommentNotification(ctx context.Context, to []string, postTitle, authorName, excerpt, moderateURL string) error {
	msg, err := m.newMessage("New comment on "+postTitle, to...)
	if err != nil {
		return err
	}
	text := fmt.Sprintf(
		"A new comment was posted on %q by %s:\n\n%s\n\nModerate it here:\n%s\n",
		postTitle, authorName, excerpt, moderateURL,
	)
	htmlBody := htmlDoc(
		"<h2>New comment on " + html.EscapeString(postTitle) + "</h2>" +
			"<p>Posted by <strong>" + html.EscapeString(authorName) + "</strong>:</p>" +
			`<blockquote style="border-left:3px solid #ddd;margin:0;padding:4px 12px;color:#333">` +
			html.EscapeString(excerpt) + "</blockquote>" +
			button(moderateURL, "Moderate comment"),
	)
	setBodies(msg, text, htmlBody)
	return m.deliver(ctx, msg)
}

// SendContactNotification sends the public contact-form notification. The
// submitter's email is set as Reply-To so a reply reaches them directly. All
// interpolated submitter data is HTML-escaped in the HTML part (attacker
// controlled), while the plain-text part carries the raw values.
func (m *SMTPMailer) SendContactNotification(ctx context.Context, to, fromEmail, fromName, subject, message string) error {
	msg, err := m.newMessage(fmt.Sprintf("New contact message from %s <%s>", fromName, fromEmail), to)
	if err != nil {
		return err
	}
	if strings.TrimSpace(fromEmail) != "" {
		if err := msg.ReplyTo(fromEmail); err != nil {
			// A malformed submitter address must not drop the notification; log and
			// proceed without Reply-To.
			m.log.Warn("mailer: contact reply-to invalid; sending without it", "from_email", fromEmail, "err", err)
		}
	}
	text := fmt.Sprintf(
		"New contact message from %s <%s>\n\nSubject: %s\n\n%s\n",
		fromName, fromEmail, subject, message,
	)
	htmlBody := htmlDoc(
		"<h2>New contact message</h2>" +
			"<p>From <strong>" + html.EscapeString(fromName) + "</strong> &lt;" +
			html.EscapeString(fromEmail) + "&gt;</p>" +
			"<p><strong>Subject:</strong> " + html.EscapeString(subject) + "</p>" +
			`<div style="border-left:3px solid #ddd;padding:4px 12px;color:#333;white-space:pre-wrap">` +
			html.EscapeString(message) + "</div>",
	)
	setBodies(msg, text, htmlBody)
	return m.deliver(ctx, msg)
}

// deliver hands msg to the configured sender.
func (m *SMTPMailer) deliver(ctx context.Context, msg *mail.Msg) error {
	if err := m.send(ctx, msg); err != nil {
		return fmt.Errorf("mailer: send: %w", err)
	}
	return nil
}
