package mailer

import (
	"context"
	"testing"
)

func TestNew_DriverSelection(t *testing.T) {
	tests := []struct {
		driver string
		want   string // %T without pointer noise, checked via type switch below
	}{
		{"", "log"},
		{"log", "log"},
		{"noop", "noop"},
	}
	for _, tc := range tests {
		m, err := New(Config{Driver: tc.driver}, nil)
		if err != nil {
			t.Fatalf("driver %q: New: %v", tc.driver, err)
		}
		switch tc.want {
		case "log":
			if _, ok := m.(*LogMailer); !ok {
				t.Errorf("driver %q: want *LogMailer, got %T", tc.driver, m)
			}
		case "noop":
			if _, ok := m.(*Noop); !ok {
				t.Errorf("driver %q: want *Noop, got %T", tc.driver, m)
			}
		}
	}
}

func TestNew_SMTPDriver(t *testing.T) {
	m, err := New(Config{
		Driver: "smtp",
		SMTP:   SMTPConfig{Host: "smtp.test", Port: 587, From: "a@b.test"},
	}, nil)
	if err != nil {
		t.Fatalf("New smtp: %v", err)
	}
	if _, ok := m.(*SMTPMailer); !ok {
		t.Errorf("want *SMTPMailer, got %T", m)
	}
}

func TestNew_SMTPMissingHostOrFromErrors(t *testing.T) {
	if _, err := New(Config{Driver: "smtp", SMTP: SMTPConfig{From: "a@b.test"}}, nil); err == nil {
		t.Error("expected error when smtp host missing")
	}
	if _, err := New(Config{Driver: "smtp", SMTP: SMTPConfig{Host: "smtp.test"}}, nil); err == nil {
		t.Error("expected error when smtp from missing")
	}
}

func TestNew_UnknownDriverErrors(t *testing.T) {
	if _, err := New(Config{Driver: "carrier-pigeon"}, nil); err == nil {
		t.Error("expected error for unknown driver")
	}
}

func TestNoop_SendsNothing(t *testing.T) {
	n := NewNoop()
	ctx := context.Background()
	if err := n.SendVerificationEmail(ctx, "a@b.test", "n", "url"); err != nil {
		t.Errorf("verification: %v", err)
	}
	if err := n.SendPasswordResetEmail(ctx, "a@b.test", "url"); err != nil {
		t.Errorf("reset: %v", err)
	}
	if err := n.SendCommentNotification(ctx, []string{"a@b.test"}, "t", "a", "e", "url"); err != nil {
		t.Errorf("comment: %v", err)
	}
	if err := n.SendContactNotification(ctx, "a@b.test", "f@e.test", "f", "s", "m"); err != nil {
		t.Errorf("contact: %v", err)
	}
}
