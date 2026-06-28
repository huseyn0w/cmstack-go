package accounts_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/platform/events"
)

// TestResetPasswordDoubleConsumeRace guards Fix 2: firing two concurrent
// ResetPassword calls with the SAME valid token must result in exactly one
// success and one ErrInvalidToken, and the stored password must equal the
// winner's. Before the fix both succeeded (token consume was not the gate).
func TestResetPasswordDoubleConsumeRace(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testcontainers integration test in -short mode")
	}
	w := newWiring(t, accounts.NewStaticSettings(true, false))
	ctx := context.Background()
	captured := captureAsync(w.bus)

	if _, err := w.svc.Register(ctx, accounts.RegisterInput{
		Name: "Race", Email: "race@example.com", Password: "first-password-1",
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := w.svc.RequestPasswordReset(ctx, "race@example.com"); err != nil {
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

	const (
		pwA = "winner-password-AAAA"
		pwB = "winner-password-BBBB"
	)

	var (
		wg          sync.WaitGroup
		mu          sync.Mutex
		successes   int
		invalidErrs int
		winnerPw    string
	)
	start := make(chan struct{})
	fire := func(pw string) {
		defer wg.Done()
		<-start
		err := w.svc.ResetPassword(ctx, resetTok, pw)
		mu.Lock()
		defer mu.Unlock()
		switch {
		case err == nil:
			successes++
			winnerPw = pw
		case errors.Is(err, accounts.ErrInvalidToken):
			invalidErrs++
		default:
			t.Errorf("unexpected error from ResetPassword: %v", err)
		}
	}
	wg.Add(2)
	go fire(pwA)
	go fire(pwB)
	close(start)
	wg.Wait()

	if successes != 1 || invalidErrs != 1 {
		t.Fatalf("expected exactly 1 success and 1 ErrInvalidToken, got successes=%d invalid=%d", successes, invalidErrs)
	}

	// The stored password must be the winner's; the loser's must be rejected.
	if _, err := w.svc.Login(ctx, accounts.LoginInput{Identifier: "race@example.com", Password: winnerPw}); err != nil {
		t.Fatalf("login with winner password %q: %v", winnerPw, err)
	}
	loserPw := pwA
	if winnerPw == pwA {
		loserPw = pwB
	}
	if _, err := w.svc.Login(ctx, accounts.LoginInput{Identifier: "race@example.com", Password: loserPw}); !errors.Is(err, accounts.ErrInvalidCredentials) {
		t.Fatalf("loser password %q must be rejected, got %v", loserPw, err)
	}
}

// TestVerifyEmailDoubleConsumeRace guards Fix 2 for email verification: two
// concurrent VerifyEmail with the same token must yield exactly one success and
// one ErrInvalidToken.
func TestVerifyEmailDoubleConsumeRace(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testcontainers integration test in -short mode")
	}
	w := newWiring(t, accounts.NewStaticSettings(true, true)) // verification required
	ctx := context.Background()
	captured := captureAsync(w.bus)

	if _, err := w.svc.Register(ctx, accounts.RegisterInput{
		Name: "Vera", Email: "vera@example.com", Password: "long-enough-password",
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	relay := events.NewRelay(w.pool, w.bus, 100, slogDiscard())
	if _, err := relay.Drain(ctx); err != nil {
		t.Fatalf("drain: %v", err)
	}
	token := captured.verificationToken()
	if token == "" {
		t.Fatal("no verification token captured")
	}

	var (
		wg          sync.WaitGroup
		mu          sync.Mutex
		successes   int
		invalidErrs int
	)
	start := make(chan struct{})
	fire := func() {
		defer wg.Done()
		<-start
		err := w.svc.VerifyEmail(ctx, token)
		mu.Lock()
		defer mu.Unlock()
		switch {
		case err == nil:
			successes++
		case errors.Is(err, accounts.ErrInvalidToken):
			invalidErrs++
		default:
			t.Errorf("unexpected error from VerifyEmail: %v", err)
		}
	}
	wg.Add(2)
	go fire()
	go fire()
	close(start)
	wg.Wait()

	if successes != 1 || invalidErrs != 1 {
		t.Fatalf("expected exactly 1 success and 1 ErrInvalidToken, got successes=%d invalid=%d", successes, invalidErrs)
	}

	// Email is verified, so login now succeeds.
	if _, err := w.svc.Login(ctx, accounts.LoginInput{Identifier: "vera@example.com", Password: "long-enough-password"}); err != nil {
		t.Fatalf("login after verification: %v", err)
	}
}
