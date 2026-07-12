package accounts

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/huseyn0w/agentic-cms-go/internal/platform/db"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/security"
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
var dummyHash = mustHash(dummyHasher, "agentic-cms-dummy-password")

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
	// ErrOAuthSignupDisabled is returned by LoginWithOAuth when no local account
	// matches the provider identity AND public signup is disabled: social login
	// may link to an existing account but may not create a new one.
	ErrOAuthSignupDisabled = errors.New("accounts: signup disabled; no existing account to link")
	// ErrOAuthNoEmail is returned when the provider supplies no email, so the
	// link-by-email and create paths cannot proceed safely.
	ErrOAuthNoEmail = errors.New("accounts: oauth provider returned no email")
	// ErrUsernameTaken is returned by Register when the chosen username exists.
	ErrUsernameTaken = errors.New("accounts: username already taken")
	// ErrInvalidUsername is returned by Register when the username is malformed.
	ErrInvalidUsername = errors.New("accounts: invalid username")
)

// usernamePattern allows 3–30 chars of lowercase letters, digits, underscore and
// hyphen. The CITEXT column is case-insensitive, so we normalize to lowercase
// before storing/comparing to keep "Alice" and "alice" the same handle.
var usernamePattern = regexp.MustCompile(`^[a-z0-9_-]{3,30}$`)

// validUsername reports whether s is an acceptable username after lowercasing.
func validUsername(s string) bool {
	return usernamePattern.MatchString(strings.ToLower(s))
}

// AuthService holds all authentication/registration logic. It accesses data
// only through repositories and fires side effects only by emitting events.
type AuthService struct {
	pool     db.Beginner
	users    UserRepository
	roles    RoleRepository
	tokens   TokenRepository
	oauth    OAuthRepository
	hasher   Hasher
	bus      Publisher
	settings SettingsProvider
	now      Clock
}

// NewAuthService constructs an AuthService with explicit dependencies. oauth may
// be nil when social login is not wired (no providers configured); the OAuth
// path is the only caller that dereferences it.
func NewAuthService(
	pool db.Beginner,
	users UserRepository,
	roles RoleRepository,
	tokens TokenRepository,
	oauth OAuthRepository,
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
		oauth:    oauth,
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

	// Username is OPTIONAL: empty means email stays the only login identifier.
	// When supplied it must be a valid handle and globally unique.
	username := strings.ToLower(strings.TrimSpace(in.Username))
	if username != "" {
		if !validUsername(username) {
			return User{}, ErrInvalidUsername
		}
		n, err := s.users.CountByUsername(ctx, username)
		if err != nil {
			return User{}, fmt.Errorf("check username: %w", err)
		}
		if n > 0 {
			return User{}, ErrUsernameTaken
		}
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
			Username:     username,
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

// OAuthIdentity is the normalized identity returned by a social provider after a
// successful callback. The handler builds it from goth's user; the service is
// provider-agnostic.
type OAuthIdentity struct {
	Provider       string // e.g. "google", "github"
	ProviderUserID string // the provider's stable user id
	Email          string // provider-verified email (may be empty for some providers)
	Name           string
	AvatarURL      string
}

// LoginWithOAuth resolves a provider identity to a local user, following three
// branches:
//
//  1. an oauth_accounts(provider, provider_user_id) link exists -> log in that
//     user (no writes);
//  2. else a user with the identity's email exists -> link a new oauth_accounts
//     row to them and log in (one tx);
//  3. else create a new Member user (email_verified_at = now(), the provider
//     verified it) and link the identity (one tx), then log in.
//
// Signup gating: when public signup is disabled, branch 3 is denied with
// ErrOAuthSignupDisabled — social login may LINK to an existing account but may
// not CREATE one. A missing email blocks branches 2 and 3 (ErrOAuthNoEmail);
// branch 1 still works because it keys off the provider id, not the email.
//
// All create+link writes for a single call commit in one RunInTx; there are no
// inline side effects.
func (s *AuthService) LoginWithOAuth(ctx context.Context, id OAuthIdentity) (User, error) {
	// Branch 1: existing link -> log in, no writes.
	link, err := s.oauth.GetByProvider(ctx, id.Provider, id.ProviderUserID)
	if err == nil {
		user, gErr := s.users.GetByID(ctx, link.UserID)
		if gErr != nil {
			return User{}, fmt.Errorf("load linked user: %w", gErr)
		}
		return user, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return User{}, fmt.Errorf("lookup oauth link: %w", err)
	}

	email := normalizeEmail(id.Email)
	if email == "" {
		// No link and no email: we cannot safely match or create an account.
		return User{}, ErrOAuthNoEmail
	}

	// Branch 2: a user with this email exists -> link and log in.
	existing, err := s.users.GetByEmail(ctx, email)
	if err == nil {
		if linkErr := db.RunInTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
			return s.oauth.LinkTx(ctx, tx, existing.ID, id.Provider, id.ProviderUserID)
		}); linkErr != nil {
			return User{}, fmt.Errorf("link oauth to existing user: %w", linkErr)
		}
		return existing, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return User{}, fmt.Errorf("lookup user by email: %w", err)
	}

	// Branch 3: no user -> create one (Member, verified) and link. Gated by signup.
	if !s.settings.SignupEnabled(ctx) {
		return User{}, ErrOAuthSignupDisabled
	}

	role, err := s.roles.GetByKey(ctx, RoleMember)
	if err != nil {
		return User{}, fmt.Errorf("resolve member role: %w", err)
	}

	// Social accounts never authenticate by password. We still must satisfy the
	// NOT NULL password_hash column, so we store an unusable random hash: it is a
	// real argon2id hash of high-entropy bytes the user can never reproduce, so
	// the password path can never succeed for a social-only account.
	unusableHash, err := s.unusablePasswordHash()
	if err != nil {
		return User{}, err
	}

	verifiedAt := s.now()
	var created User
	err = db.RunInTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		u, cErr := s.users.CreateTx(ctx, tx, CreateUserInput{
			Email:           email,
			Name:            strings.TrimSpace(id.Name),
			PasswordHash:    unusableHash,
			RoleID:          role.ID,
			EmailVerifiedAt: &verifiedAt,
			AvatarURL:       strings.TrimSpace(id.AvatarURL),
		})
		if cErr != nil {
			return fmt.Errorf("create user: %w", cErr)
		}
		created = u
		if lErr := s.oauth.LinkTx(ctx, tx, u.ID, id.Provider, id.ProviderUserID); lErr != nil {
			return fmt.Errorf("link oauth: %w", lErr)
		}
		return nil
	})
	if err != nil {
		return User{}, err
	}
	return created, nil
}

// unusablePasswordHash returns a real argon2id hash of fresh random bytes. It
// satisfies the NOT NULL password_hash column for social-only accounts while
// guaranteeing the password login path can never succeed for them.
func (s *AuthService) unusablePasswordHash() (string, error) {
	plaintext, _, err := generateToken()
	if err != nil {
		return "", fmt.Errorf("generate unusable secret: %w", err)
	}
	hash, err := s.hasher.Hash(plaintext)
	if err != nil {
		return "", fmt.Errorf("hash unusable secret: %w", err)
	}
	return hash, nil
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
