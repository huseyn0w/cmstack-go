// Package contact implements the public, reCAPTCHA-protected contact form. A
// submission is validated, spam-checked, rate-limited, and then published as an
// async contact.submitted event to the outbox; the worker drains it and sends a
// notification email to the settings-driven recipient. There is NO contact table
// (the outbox provides durability) and NO custom form builder (out of scope v1).
package contact

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/huseyn0w/agentic-cms-go/internal/platform/db"
)

// Field-length caps for a contact submission (trimmed before checking).
const (
	maxNameLen    = 200
	maxSubjectLen = 300
	maxMessageLen = 5000
)

// rateLimitKeyPrefix namespaces the per-IP contact-submit bucket so it does not
// collide with other limiter keys when a shared limiter is ever used.
const rateLimitKeyPrefix = "contact-submit:"

// Domain errors. Handlers map them to HTTP outcomes + friendly messages.
var (
	// ErrRateLimited is returned when the per-IP submit rate limit is exceeded.
	ErrRateLimited = errors.New("contact: rate limit exceeded")
	// ErrRecaptcha is returned when the anti-spam (reCAPTCHA) check rejects the
	// submission (false or a verification error).
	ErrRecaptcha = errors.New("contact: recaptcha verification failed")
	// ErrInvalid is returned when the submission fails validation. It wraps a
	// ValidationError whose Field/Message identify the offending field; callers
	// inspect it with errors.As.
	ErrInvalid = errors.New("contact: validation failed")
)

// ValidationError identifies which field failed validation and why. It wraps
// ErrInvalid so callers can both errors.Is(err, ErrInvalid) and errors.As(err,
// &ValidationError{}) to surface the field.
type ValidationError struct {
	Field   string
	Message string
}

// Error implements error.
func (e ValidationError) Error() string {
	return fmt.Sprintf("contact: %s: %s", e.Field, e.Message)
}

// Unwrap makes errors.Is(err, ErrInvalid) succeed for any ValidationError.
func (e ValidationError) Unwrap() error { return ErrInvalid }

// Input is the raw public submission decoded from the form. The handler fills
// RemoteIP from the honest client IP.
type Input struct {
	Name           string
	Email          string
	Subject        string
	Message        string
	RecaptchaToken string
	RemoteIP       string
}

// Clock returns the current time; injected so SubmittedAt is testable.
type Clock func() time.Time

// Service holds all contact-form logic. It owns no globals: it rate-limits,
// validates, spam-checks, and publishes the async contact.submitted event
// through the injected bus (no other DB writes).
type Service struct {
	pool     db.Beginner
	verifier Verifier
	limiter  RateLimiter
	bus      Publisher
	now      Clock
}

// NewService constructs the contact Service. verifier/limiter may be nil
// (verification / rate-limit disabled). A nil clock defaults to time.Now.
func NewService(pool db.Beginner, verifier Verifier, limiter RateLimiter, bus Publisher) *Service {
	return &Service{
		pool:     pool,
		verifier: verifier,
		limiter:  limiter,
		bus:      bus,
		now:      time.Now,
	}
}

// WithClock overrides the clock (tests). Returns the receiver.
func (s *Service) WithClock(now Clock) *Service {
	if now != nil {
		s.now = now
	}
	return s
}

// Submit validates + spam-checks a contact submission and, on success, publishes
// an async contact.submitted event (outbox) inside a transaction. It performs no
// other DB writes. Order: rate-limit (cheap guard) → validate → reCAPTCHA →
// publish.
func (s *Service) Submit(ctx context.Context, in Input) error {
	if s.limiter != nil && in.RemoteIP != "" {
		if !s.limiter.Allow(rateLimitKeyPrefix + in.RemoteIP) {
			return ErrRateLimited
		}
	}

	name := strings.TrimSpace(in.Name)
	email := strings.TrimSpace(in.Email)
	subject := strings.TrimSpace(in.Subject)
	message := strings.TrimSpace(in.Message)

	if name == "" {
		return ValidationError{Field: "name", Message: "Name is required."}
	}
	if len(name) > maxNameLen {
		return ValidationError{Field: "name", Message: "Name is too long."}
	}
	if email == "" {
		return ValidationError{Field: "email", Message: "Email is required."}
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return ValidationError{Field: "email", Message: "Please enter a valid email address."}
	}
	if len(subject) > maxSubjectLen {
		return ValidationError{Field: "subject", Message: "Subject is too long."}
	}
	if message == "" {
		return ValidationError{Field: "message", Message: "Message is required."}
	}
	if len(message) > maxMessageLen {
		return ValidationError{Field: "message", Message: "Message is too long."}
	}

	if s.verifier != nil {
		ok, err := s.verifier.Verify(ctx, in.RecaptchaToken)
		if err != nil || !ok {
			return ErrRecaptcha
		}
	}

	evt := SubmittedEvent{
		FromName:    name,
		FromEmail:   email,
		Subject:     subject,
		Message:     message,
		SubmittedAt: s.now().UTC(),
	}

	return db.RunInTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.bus.Publish(ctx, tx, evt); err != nil {
			return fmt.Errorf("publish %s: %w", EventContactSubmitted, err)
		}
		return nil
	})
}
