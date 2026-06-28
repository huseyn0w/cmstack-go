package accounts_test

import (
	"context"
	"testing"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/platform/events"
)

// TestPasswordChangeBumpsPasswordChangedAt guards Fix 3 at the DB level: both
// ResetPassword and ChangePassword must advance the user's PasswordChangedAt so
// the session-epoch check in the middleware can globally invalidate older
// sessions.
func TestPasswordChangeBumpsPasswordChangedAt(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testcontainers integration test in -short mode")
	}
	w := newWiring(t, accounts.NewStaticSettings(true, false))
	ctx := context.Background()
	captured := captureAsync(w.bus)

	created, err := w.svc.Register(ctx, accounts.RegisterInput{
		Name: "Pat", Email: "pat@example.com", Password: "first-password-1",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	before, err := w.users.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("load user before: %v", err)
	}
	if before.PasswordChangedAt.IsZero() {
		t.Fatal("password_changed_at should default to a non-zero timestamp")
	}

	// ChangePassword must bump the epoch.
	if err := w.svc.ChangePassword(ctx, created.ID, "first-password-1", "second-password-2"); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	afterChange, err := w.users.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("load user after change: %v", err)
	}
	if !afterChange.PasswordChangedAt.After(before.PasswordChangedAt) {
		t.Fatalf("ChangePassword must advance PasswordChangedAt: before=%v after=%v",
			before.PasswordChangedAt, afterChange.PasswordChangedAt)
	}

	// ResetPassword must bump it too.
	if err := w.svc.RequestPasswordReset(ctx, "pat@example.com"); err != nil {
		t.Fatalf("RequestPasswordReset: %v", err)
	}
	relay := events.NewRelay(w.pool, w.bus, 100, slogDiscard())
	if _, err := relay.Drain(ctx); err != nil {
		t.Fatalf("drain: %v", err)
	}
	resetTok := captured.resetToken()
	if resetTok == "" {
		t.Fatal("no reset token captured")
	}
	if err := w.svc.ResetPassword(ctx, resetTok, "third-password-3"); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	afterReset, err := w.users.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("load user after reset: %v", err)
	}
	if !afterReset.PasswordChangedAt.After(afterChange.PasswordChangedAt) {
		t.Fatalf("ResetPassword must advance PasswordChangedAt: change=%v reset=%v",
			afterChange.PasswordChangedAt, afterReset.PasswordChangedAt)
	}
}
