package mailer

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/wneessen/go-mail"
)

// capture returns a sender that records every delivered message and the rendered
// wire bytes, so composition can be asserted offline (no network).
type capture struct {
	msgs []*mail.Msg
	raw  []string
}

func (c *capture) sender() sender {
	return func(_ context.Context, msg *mail.Msg) error {
		var buf bytes.Buffer
		if _, err := msg.WriteTo(&buf); err != nil {
			return err
		}
		c.msgs = append(c.msgs, msg)
		c.raw = append(c.raw, buf.String())
		return nil
	}
}

func newTestMailer(t *testing.T) (*SMTPMailer, *capture) {
	t.Helper()
	m, err := NewSMTP(SMTPConfig{
		Host:     "smtp.example.com",
		Port:     587,
		From:     "no-reply@cmstack.local",
		FromName: "CMStack",
		TLS:      "starttls",
	}, nil)
	if err != nil {
		t.Fatalf("NewSMTP: %v", err)
	}
	cap := &capture{}
	m.withSender(cap.sender())
	return m, cap
}

func lastRaw(t *testing.T, c *capture) string {
	t.Helper()
	if len(c.raw) == 0 {
		t.Fatal("no message captured")
	}
	// Normalize quoted-printable encoding so assertions can match plain content:
	// drop soft line breaks and decode "=3D" back to "=". This is enough for the
	// ASCII payloads used here (URLs, subjects) without a full QP decoder.
	return strings.NewReplacer("=\r\n", "", "=\n", "", "=3D", "=", "=3d", "=").Replace(c.raw[len(c.raw)-1])
}

func TestSendVerificationEmail(t *testing.T) {
	m, cap := newTestMailer(t)
	if err := m.SendVerificationEmail(context.Background(), "user@example.com", "Ada", "https://x.test/verify?t=abc"); err != nil {
		t.Fatalf("send: %v", err)
	}
	raw := lastRaw(t, cap)
	for _, want := range []string{
		"To: <user@example.com>",
		"From: \"CMStack\" <no-reply@cmstack.local>",
		"Subject: Confirm your email",
		"https://x.test/verify?t=abc",
		"Ada",
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("verification message missing %q\n---\n%s", want, raw)
		}
	}
}

func TestSendPasswordResetEmail(t *testing.T) {
	m, cap := newTestMailer(t)
	if err := m.SendPasswordResetEmail(context.Background(), "user@example.com", "https://x.test/reset?t=zzz"); err != nil {
		t.Fatalf("send: %v", err)
	}
	raw := lastRaw(t, cap)
	for _, want := range []string{
		"Subject: Reset your password",
		"https://x.test/reset?t=zzz",
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("reset message missing %q\n---\n%s", want, raw)
		}
	}
}

func TestSendCommentNotification(t *testing.T) {
	m, cap := newTestMailer(t)
	err := m.SendCommentNotification(context.Background(),
		[]string{"mod1@example.com", "mod2@example.com"},
		"Hello World", "Grace", "great post!", "https://x.test/admin/comments/1")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	raw := lastRaw(t, cap)
	for _, want := range []string{
		"Subject: New comment on Hello World",
		"mod1@example.com",
		"mod2@example.com",
		"Grace",
		"great post!",
		"https://x.test/admin/comments/1",
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("comment message missing %q\n---\n%s", want, raw)
		}
	}
}

func TestSendContactNotification(t *testing.T) {
	m, cap := newTestMailer(t)
	err := m.SendContactNotification(context.Background(),
		"owner@example.com", "sender@visitor.test", "Sam Visitor",
		"Question about pricing", "Do you offer annual plans?")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	raw := lastRaw(t, cap)
	for _, want := range []string{
		"To: <owner@example.com>",
		"Reply-To: <sender@visitor.test>",
		"Question about pricing",
		"Do you offer annual plans?",
		"Sam Visitor",
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("contact message missing %q\n---\n%s", want, raw)
		}
	}
}

func TestSendContactNotification_HTMLEscaped(t *testing.T) {
	m, cap := newTestMailer(t)
	const payload = `<script>alert('xss')</script>`
	err := m.SendContactNotification(context.Background(),
		"owner@example.com", "attacker@evil.test", payload,
		"hi", "body "+payload)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	raw := lastRaw(t, cap)

	// Isolate the text/html part. The HTML body carries attacker-controlled data
	// and MUST escape it: the raw script tag must be absent and the escaped entity
	// form present. (The plain-text part and the Subject header intentionally carry
	// the raw submitter values; only the HTML body is an injection surface.)
	htmlPart := raw
	if i := strings.Index(raw, "text/html"); i >= 0 {
		htmlPart = raw[i:]
	}
	if strings.Contains(htmlPart, payload) {
		t.Errorf("raw <script> payload leaked into HTML part:\n%s", htmlPart)
	}
	if !strings.Contains(htmlPart, "&lt;script&gt;") {
		t.Errorf("expected escaped &lt;script&gt; in HTML part\n---\n%s", htmlPart)
	}
}

func TestNewSMTP_RequiresHostAndFrom(t *testing.T) {
	if _, err := NewSMTP(SMTPConfig{From: "a@b.test"}, nil); err == nil {
		t.Error("expected error when host is missing")
	}
	if _, err := NewSMTP(SMTPConfig{Host: "smtp.test"}, nil); err == nil {
		t.Error("expected error when from is missing")
	}
}
