package accounts

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/huseyn0w/cmstack-go/internal/platform/events"
)

// ErrNotFound is the sentinel every repository returns when a row is absent. The
// service maps it to domain outcomes (e.g. anti-enumeration) and never leaks it
// to handlers.
var ErrNotFound = errors.New("accounts: not found")

// ErrEmailTaken is returned by Register when the email already exists.
var ErrEmailTaken = errors.New("accounts: email already registered")

// UserRepository is the data-access contract for users. It is the ONLY layer
// permitted to touch sqlc/pgx; the service depends solely on this interface.
// Mutations that must be transactional accept a pgx.Tx.
type UserRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (User, error)
	GetByEmail(ctx context.Context, email string) (User, error)
	GetByUsername(ctx context.Context, username string) (User, error)
	CreateTx(ctx context.Context, tx pgx.Tx, in CreateUserInput) (User, error)
	SetPasswordTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, passwordHash string) error
	MarkEmailVerifiedTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error
}

// CreateUserInput carries the fields needed to insert a user.
type CreateUserInput struct {
	Email           string
	Username        string // empty -> NULL
	PasswordHash    string
	Name            string
	RoleID          uuid.UUID
	EmailVerifiedAt *time.Time
}

// RoleRepository resolves roles and their permission sets.
type RoleRepository interface {
	GetByKey(ctx context.Context, key string) (Role, error)
	GetByID(ctx context.Context, id uuid.UUID) (Role, error)
	// AllRolePermissions returns every (role_key -> permissions) mapping. The
	// authorizer loads this once and caches it.
	AllRolePermissions(ctx context.Context) (map[string][]Permission, error)
}

// TokenRepository persists hashed verification/reset tokens transactionally and
// resolves them by hash for consumption.
type TokenRepository interface {
	CreateEmailVerificationTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, tokenHash string, expiresAt time.Time) error
	GetEmailVerification(ctx context.Context, tokenHash string) (Token, error)
	ConsumeEmailVerificationTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error

	CreatePasswordResetTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, tokenHash string, expiresAt time.Time) error
	GetPasswordReset(ctx context.Context, tokenHash string) (Token, error)
	ConsumePasswordResetTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error
}

// Hasher hashes and verifies passwords. security.PasswordHasher satisfies it.
type Hasher interface {
	Hash(password string) (string, error)
	Verify(password, encoded string) (bool, error)
}

// Publisher publishes a domain event inside a transaction. *events.Bus
// satisfies it; the service depends on this narrow interface, not the bus.
type Publisher interface {
	Publish(ctx context.Context, tx pgx.Tx, event events.Event) error
}

// SettingsProvider exposes the auth-relevant toggles. The config-backed impl is
// wired now; the admin-UI-backed impl arrives in M15.
type SettingsProvider interface {
	SignupEnabled(ctx context.Context) bool
	EmailVerificationRequired(ctx context.Context) bool
}

// Mailer delivers transactional emails. The dev LogMailer logs the links; real
// SMTP is M14. Listeners call the Mailer in response to emitted events.
type Mailer interface {
	SendVerificationEmail(ctx context.Context, to, name, verifyURL string) error
	SendPasswordResetEmail(ctx context.Context, to, resetURL string) error
}

// Clock returns the current time; injected so token expiry is testable.
type Clock func() time.Time
