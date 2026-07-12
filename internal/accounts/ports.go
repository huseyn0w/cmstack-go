package accounts

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/huseyn0w/agentic-cms-go/internal/platform/events"
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
	CountByUsername(ctx context.Context, username string) (int, error)
	CreateTx(ctx context.Context, tx pgx.Tx, in CreateUserInput) (User, error)
	SetPasswordTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, passwordHash string) error
	MarkEmailVerifiedTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error
	// UpdateProfileTx persists the editable profile fields, returning the updated
	// user. SetAvatarPathTx updates only avatar_path (empty clears it).
	UpdateProfileTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, in ProfileFields) (User, error)
	SetAvatarPathTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, path string) (User, error)
}

// ProfileFields are the editable profile attributes persisted by UpdateProfileTx.
// They are already validated/normalized by the service before reaching the repo.
type ProfileFields struct {
	Name        string
	Bio         string
	Website     string
	SocialLinks map[string]string
}

// CreateUserInput carries the fields needed to insert a user.
type CreateUserInput struct {
	Email           string
	Username        string // empty -> NULL
	PasswordHash    string
	Name            string
	RoleID          uuid.UUID
	EmailVerifiedAt *time.Time
	AvatarURL       string // empty -> NULL; set by social login
}

// OAuthRepository is the data-access contract for linked third-party identities
// (oauth_accounts). It is the ONLY layer touching sqlc/pgx for that table.
// The link write is transactional so creating a user and linking the identity
// commit atomically.
type OAuthRepository interface {
	// GetByProvider resolves a linked identity by (provider, providerUserID),
	// returning ErrNotFound when no link exists.
	GetByProvider(ctx context.Context, provider, providerUserID string) (OAuthAccount, error)
	// LinkTx inserts a new oauth_accounts row binding userID to a provider
	// identity within tx.
	LinkTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, provider, providerUserID string) error
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
// TokenRepository's Consume*Tx methods are the atomic single-use gate: the
// UPDATE only matches a still-unconsumed row, so a concurrent double-use returns
// ErrNotFound for the loser. Callers MUST treat ErrNotFound as "already
// consumed" and abort without applying the side effect.
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

// AvatarStore is the narrow subset of platform/storage.Storage the profile
// service needs: save an avatar blob, delete an old one, and resolve its public
// URL. Declaring it here keeps accounts decoupled from the storage package and
// trivially fakeable in tests.
type AvatarStore interface {
	Save(ctx context.Context, key string, r io.Reader, contentType string) (string, error)
	Delete(ctx context.Context, key string) error
	URL(key string) string
}
