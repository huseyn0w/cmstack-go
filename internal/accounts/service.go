package accounts

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/huseyn0w/cmstack-go/internal/platform/db"
	"github.com/huseyn0w/cmstack-go/internal/platform/security"
)

// Token lifetimes.
const (
	emailVerificationTTL = 24 * time.Hour
	passwordResetTTL     = 1 * time.Hour
)

// dummyHasher derives the package-level dummyHash. It uses the same argon2id
// parameters as the production PasswordHasher so the dummy Verify on the
// unknown-user path performs identical work to a real comparison.
var dummyHasher = security.NewPasswordHasher()

// dummyHash is a REAL argon2id encoded hash computed once at process start. It
// keeps Login timing constant when the account does not exist, defeating user
// enumeration via response-time analysis. Computing it from the actual hasher
// (rather than hardcoding a string) guarantees it is always a parseable hash so
// Verify runs the full argon2 work and returns (false, nil) — never the fast
// ErrInvalidHash path that would re-open the timing oracle.
var dummyHash = mustHash(dummyHasher, "cmstack-dummy-password")

// mustHash computes an argon2id hash at init time, panicking only if hashing
// fails (which would indicate a broken crypto runtime, not a recoverable error).
func mustHash(h *security.PasswordHasher, password string) string {
	encoded, err := h.Hash(password)
	if err != nil {
		panic(fmt.Sprintf("accounts: cannot precompute dummy hash: %v", err))
	}
	return encoded
}

// Domain errors returned by the service. Handlers map these to user-facing
// outcomes; the credential-related ones are intentionally generic.
var (
	// ErrInvalidCredentials is returned by Login for ANY failure (unknown user,
	// wrong password) so the caller cannot distinguish them — anti-enumeration.
	ErrInvalidCredentials = errors.New("accounts: invalid credentials")
	// ErrEmailNotVerified is returned by Login when verification is required and
	// the account is unverified.
	ErrEmailNotVerified = errors.New("accounts: email not verified")
	// ErrSignupDisabled is returned by Register when signups are turned off.
	ErrSignupDisabled = errors.New("accounts: signup disabled")
	// ErrInvalidToken is returned when a verification/reset token is unknown,
	// expired, or already consumed.
	ErrInvalidToken = errors.New("accounts: invalid or expired token")
)

// AuthService holds all authentication/registration logic. It accesses data
// only through repositories and fires side effects only by emitting events.
type AuthService struct {
	pool     db.Beginner
	users    UserRepository
	roles    RoleRepository
	tokens   TokenRepository
	hasher   Hasher
	bus      Publisher
	settings SettingsProvider
	now      Clock
}

// NewAuthService constructs an AuthService with explicit dependencies.
func NewAuthService(
	pool db.Beginner,
	users UserRepository,
	roles RoleRepository,
	tokens TokenRepository,
	hasher Hasher,
	bus Publisher,
	settings SettingsProvider,
	now Clock,
) *AuthService {
	if now == nil {
		now = time.Now
	}
	return &AuthService{
		pool:     pool,
		users:    users,
		roles:    roles,
		tokens:   tokens,
		hasher:   hasher,
		bus:      bus,
		settings: settings,
		now:      now,
	}
}

// RegisterInput is the validated registration request.
type RegisterInput struct {
	Email    string
	Username string
	Name     string
	Password string
}

// Register creates a new account with the default Member role, issues an email
// verification token, and emits account.registered (async -> verification
// email). The user write, token persist, and event enqueue all commit in one
// transaction.
func (s *AuthService) Register(ctx context.Context, in RegisterInput) (User, error) {
	if !s.settings.SignupEnabled(ctx) {
		return User{}, ErrSignupDisabled
	}

	email := normalizeEmail(in.Email)

	if _, err := s.users.GetByEmail(ctx, email); err == nil {
		return User{}, ErrEmailTaken
	} else if !errors.Is(err, ErrNotFound) {
		return User{}, fmt.Errorf("lookup existing email: %w", err)
	}

	role, err := s.roles.GetByKey(ctx, RoleMember)
	if err != nil {
		return User{}, fmt.Errorf("resolve member role: %w", err)
	}

	passwordHash, err := s.hasher.Hash(in.Password)
	if err != nil {
		return User{}, fmt.Errorf("hash password: %w", err)
	}

	plaintext, tokenHash, err := generateToken()
	if err != nil {
		return User{}, err
	}

	var created User
	err = db.RunInTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		u, err := s.users.CreateTx(ctx, tx, CreateUserInput{
			Email:        email,
			Username:     strings.TrimSpace(in.Username),
			PasswordHash: passwordHash,
			Name:         strings.TrimSpace(in.Name),
			RoleID:       role.ID,
		})
		if err != nil {
			return fmt.Errorf("create user: %w", err)
		}
		created = u

		if err := s.tokens.CreateEmailVerificationTx(ctx, tx, u.ID, tokenHash, s.now().Add(emailVerificationTTL)); err != nil {
			return fmt.Errorf("create verification token: %w", err)
		}

		return s.bus.Publish(ctx, tx, AccountRegisteredEvent{
			UserID:            u.ID,
			Email:             u.Email,
			DisplayName:       u.Name,
			VerificationToken: plaintext,
		})
	})
	if err != nil {
		return User{}, err
	}
	return created, nil
}

// LoginInput is a login request: Identifier is an email or username.
type LoginInput struct {
	Identifier string
	Password   string
}

// Login verifies credentials and returns the user on success. It always returns
// ErrInvalidCredentials for unknown user OR wrong password (no enumeration), and
// performs a dummy hash verification when the user is absent so the response
// timing does not reveal account existence.
func (s *AuthService) Login(ctx context.Context, in LoginInput) (User, error) {
	id := strings.TrimSpace(in.Identifier)

	var (
		user User
		err  error
	)
	if strings.Contains(id, "@") {
		user, err = s.users.GetByEmail(ctx, normalizeEmail(id))
	} else {
		user, err = s.users.GetByUsername(ctx, id)
	}

	if errors.Is(err, ErrNotFound) {
		// Burn equivalent time so timing does not leak existence.
		_, _ = s.hasher.Verify(in.Password, dummyHash)
		return User{}, ErrInvalidCredentials
	}
	if err != nil {
		return User{}, fmt.Errorf("lookup user: %w", err)
	}

	ok, err := s.hasher.Verify(in.Password, user.PasswordHash)
	if err != nil || !ok {
		return User{}, ErrInvalidCredentials
	}

	if s.settings.EmailVerificationRequired(ctx) && !user.EmailVerified() {
		return User{}, ErrEmailNotVerified
	}

	return user, nil
}

// ChangePassword verifies the current password then sets a new hash. It is used
// by an authenticated user.
func (s *AuthService) ChangePassword(ctx context.Context, userID uuid.UUID, current, next string) error {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("load user: %w", err)
	}
	ok, err := s.hasher.Verify(current, user.PasswordHash)
	if err != nil || !ok {
		return ErrInvalidCredentials
	}
	hash, err := s.hasher.Hash(next)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	return db.RunInTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return s.users.SetPasswordTx(ctx, tx, userID, hash)
	})
}

// RequestPasswordReset issues a reset token for an existing account and emits
// account.password_reset_requested (async -> reset email). It NEVER reveals
// whether the email exists: callers always observe success.
func (s *AuthService) RequestPasswordReset(ctx context.Context, email string) error {
	email = normalizeEmail(email)

	user, err := s.users.GetByEmail(ctx, email)
	if errors.Is(err, ErrNotFound) {
		return nil // outwardly succeed; no enumeration
	}
	if err != nil {
		return fmt.Errorf("lookup user: %w", err)
	}

	plaintext, tokenHash, err := generateToken()
	if err != nil {
		return err
	}

	return db.RunInTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.tokens.CreatePasswordResetTx(ctx, tx, user.ID, tokenHash, s.now().Add(passwordResetTTL)); err != nil {
			return fmt.Errorf("create reset token: %w", err)
		}
		return s.bus.Publish(ctx, tx, PasswordResetRequestedEvent{
			UserID:     user.ID,
			Email:      user.Email,
			ResetToken: plaintext,
		})
	})
}

// ResetPassword validates the plaintext token (hash match, unexpired,
// unconsumed), sets the new password, and consumes the token — all atomically.
func (s *AuthService) ResetPassword(ctx context.Context, plaintextToken, newPassword string) error {
	tok, err := s.tokens.GetPasswordReset(ctx, hashToken(plaintextToken))
	if errors.Is(err, ErrNotFound) {
		return ErrInvalidToken
	}
	if err != nil {
		return fmt.Errorf("lookup reset token: %w", err)
	}
	if !tok.Usable(s.now()) {
		return ErrInvalidToken
	}

	hash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	return db.RunInTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		// Consume is the atomic single-use gate: it only matches a still-unconsumed
		// row, so under concurrent double-use exactly one tx wins and the loser sees
		// ErrNotFound. We MUST consume before changing the password so a losing
		// request cannot apply its new password.
		if err := s.tokens.ConsumePasswordResetTx(ctx, tx, tok.ID); err != nil {
			if errors.Is(err, ErrNotFound) {
				return ErrInvalidToken
			}
			return fmt.Errorf("consume reset token: %w", err)
		}
		if err := s.users.SetPasswordTx(ctx, tx, tok.UserID, hash); err != nil {
			return fmt.Errorf("set password: %w", err)
		}
		return nil
	})
}

// VerifyEmail validates the plaintext verification token, stamps
// email_verified_at, and consumes the token — atomically.
func (s *AuthService) VerifyEmail(ctx context.Context, plaintextToken string) error {
	tok, err := s.tokens.GetEmailVerification(ctx, hashToken(plaintextToken))
	if errors.Is(err, ErrNotFound) {
		return ErrInvalidToken
	}
	if err != nil {
		return fmt.Errorf("lookup verification token: %w", err)
	}
	if !tok.Usable(s.now()) {
		return ErrInvalidToken
	}

	return db.RunInTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		// Consume is the atomic single-use gate (see ResetPassword): consume first
		// so a concurrent double-use cannot verify the email twice.
		if err := s.tokens.ConsumeEmailVerificationTx(ctx, tx, tok.ID); err != nil {
			if errors.Is(err, ErrNotFound) {
				return ErrInvalidToken
			}
			return fmt.Errorf("consume verification token: %w", err)
		}
		if err := s.users.MarkEmailVerifiedTx(ctx, tx, tok.UserID); err != nil {
			return fmt.Errorf("mark verified: %w", err)
		}
		return nil
	})
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
