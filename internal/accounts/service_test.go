package accounts

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/huseyn0w/cmstack-go/internal/platform/events"
	"github.com/huseyn0w/cmstack-go/internal/platform/security"
)

// --- fakes -------------------------------------------------------------------

// fakePool/fakeTx provide a no-op transaction so RunInTx executes fn without a
// real DB. The fakeTx satisfies pgx.Tx by embedding the interface (only Commit
// and Rollback are exercised).
type fakeTx struct{ pgx.Tx }

func (fakeTx) Commit(context.Context) error   { return nil }
func (fakeTx) Rollback(context.Context) error { return nil }

type fakePool struct{ beginErr error }

func (p fakePool) Begin(context.Context) (pgx.Tx, error) {
	if p.beginErr != nil {
		return nil, p.beginErr
	}
	return fakeTx{}, nil
}

type fakeUserRepo struct {
	byEmail    map[string]User
	byUsername map[string]User
	byID       map[uuid.UUID]User
	created    []CreateUserInput
	passwords  map[uuid.UUID]string
	verified   map[uuid.UUID]bool
}

func newFakeUserRepo() *fakeUserRepo {
	return &fakeUserRepo{
		byEmail:    map[string]User{},
		byUsername: map[string]User{},
		byID:       map[uuid.UUID]User{},
		passwords:  map[uuid.UUID]string{},
		verified:   map[uuid.UUID]bool{},
	}
}

func (r *fakeUserRepo) GetByID(_ context.Context, id uuid.UUID) (User, error) {
	if u, ok := r.byID[id]; ok {
		return u, nil
	}
	return User{}, ErrNotFound
}

func (r *fakeUserRepo) GetByEmail(_ context.Context, email string) (User, error) {
	if u, ok := r.byEmail[email]; ok {
		return u, nil
	}
	return User{}, ErrNotFound
}

func (r *fakeUserRepo) GetByUsername(_ context.Context, username string) (User, error) {
	if u, ok := r.byUsername[username]; ok {
		return u, nil
	}
	return User{}, ErrNotFound
}

func (r *fakeUserRepo) CreateTx(_ context.Context, _ pgx.Tx, in CreateUserInput) (User, error) {
	r.created = append(r.created, in)
	u := User{
		ID:           uuid.New(),
		Email:        in.Email,
		Username:     in.Username,
		PasswordHash: in.PasswordHash,
		Name:         in.Name,
		RoleID:       in.RoleID,
	}
	r.byEmail[in.Email] = u
	r.byID[u.ID] = u
	return u, nil
}

func (r *fakeUserRepo) SetPasswordTx(_ context.Context, _ pgx.Tx, id uuid.UUID, hash string) error {
	r.passwords[id] = hash
	if u, ok := r.byID[id]; ok {
		u.PasswordHash = hash
		r.byID[id] = u
	}
	return nil
}

func (r *fakeUserRepo) MarkEmailVerifiedTx(_ context.Context, _ pgx.Tx, id uuid.UUID) error {
	r.verified[id] = true
	return nil
}

type fakeRoleRepo struct{ member Role }

func (r fakeRoleRepo) GetByKey(_ context.Context, key string) (Role, error) {
	if key == RoleMember {
		return r.member, nil
	}
	return Role{}, ErrNotFound
}

func (r fakeRoleRepo) GetByID(_ context.Context, id uuid.UUID) (Role, error) {
	if id == r.member.ID {
		return r.member, nil
	}
	return Role{}, ErrNotFound
}

func (r fakeRoleRepo) AllRolePermissions(context.Context) (map[string][]Permission, error) {
	return map[string][]Permission{}, nil
}

type storedToken struct {
	tok      Token
	consumed bool
}

type fakeTokenRepo struct {
	emailByHash map[string]*storedToken
	resetByHash map[string]*storedToken
}

func newFakeTokenRepo() *fakeTokenRepo {
	return &fakeTokenRepo{
		emailByHash: map[string]*storedToken{},
		resetByHash: map[string]*storedToken{},
	}
}

func (r *fakeTokenRepo) CreateEmailVerificationTx(_ context.Context, _ pgx.Tx, userID uuid.UUID, hash string, exp time.Time) error {
	r.emailByHash[hash] = &storedToken{tok: Token{ID: uuid.New(), UserID: userID, TokenHash: hash, ExpiresAt: exp}}
	return nil
}

func (r *fakeTokenRepo) GetEmailVerification(_ context.Context, hash string) (Token, error) {
	st, ok := r.emailByHash[hash]
	if !ok {
		return Token{}, ErrNotFound
	}
	return st.tok, nil
}

func (r *fakeTokenRepo) ConsumeEmailVerificationTx(_ context.Context, _ pgx.Tx, id uuid.UUID) error {
	for _, st := range r.emailByHash {
		if st.tok.ID == id {
			// Atomic single-use gate: an already-consumed token does not match the
			// "WHERE consumed_at IS NULL" UPDATE, so the loser sees ErrNotFound.
			if st.consumed {
				return ErrNotFound
			}
			now := time.Now()
			st.tok.ConsumedAt = &now
			st.consumed = true
			return nil
		}
	}
	return ErrNotFound
}

func (r *fakeTokenRepo) CreatePasswordResetTx(_ context.Context, _ pgx.Tx, userID uuid.UUID, hash string, exp time.Time) error {
	r.resetByHash[hash] = &storedToken{tok: Token{ID: uuid.New(), UserID: userID, TokenHash: hash, ExpiresAt: exp}}
	return nil
}

func (r *fakeTokenRepo) GetPasswordReset(_ context.Context, hash string) (Token, error) {
	st, ok := r.resetByHash[hash]
	if !ok {
		return Token{}, ErrNotFound
	}
	return st.tok, nil
}

func (r *fakeTokenRepo) ConsumePasswordResetTx(_ context.Context, _ pgx.Tx, id uuid.UUID) error {
	for _, st := range r.resetByHash {
		if st.tok.ID == id {
			// Atomic single-use gate (see ConsumeEmailVerificationTx).
			if st.consumed {
				return ErrNotFound
			}
			now := time.Now()
			st.tok.ConsumedAt = &now
			st.consumed = true
			return nil
		}
	}
	return ErrNotFound
}

// recordingBus captures published events. It satisfies Publisher by wrapping a
// real *events.Bus so the structural contract is exercised end to end.
type recordingBus struct {
	bus    *events.Bus
	events []events.Event
}

func newRecordingBus() *recordingBus {
	b := &recordingBus{bus: events.NewBus(nil)}
	return b
}

func (b *recordingBus) Publish(ctx context.Context, tx pgx.Tx, ev events.Event) error {
	b.events = append(b.events, ev)
	return nil
}

type fakeSettings struct {
	signup       bool
	verifyNeeded bool
}

func (s fakeSettings) SignupEnabled(context.Context) bool             { return s.signup }
func (s fakeSettings) EmailVerificationRequired(context.Context) bool { return s.verifyNeeded }

// --- harness -----------------------------------------------------------------

type harness struct {
	svc      *AuthService
	users    *fakeUserRepo
	tokens   *fakeTokenRepo
	bus      *recordingBus
	settings *fakeSettings
	hasher   *security.PasswordHasher
}

func newHarness(t *testing.T, settings fakeSettings) *harness {
	t.Helper()
	users := newFakeUserRepo()
	tokens := newFakeTokenRepo()
	bus := newRecordingBus()
	hasher := security.NewPasswordHasher()
	s := &settings
	svc := NewAuthService(
		fakePool{},
		users,
		fakeRoleRepo{member: Role{ID: uuid.New(), Key: RoleMember, Label: "Member"}},
		tokens,
		hasher,
		bus,
		s,
		time.Now,
	)
	return &harness{svc: svc, users: users, tokens: tokens, bus: bus, settings: s, hasher: hasher}
}

// --- tests -------------------------------------------------------------------

func TestRegisterSuccessEmitsEventAndIssuesToken(t *testing.T) {
	h := newHarness(t, fakeSettings{signup: true})

	u, err := h.svc.Register(context.Background(), RegisterInput{
		Email: "Alice@Example.com", Name: "Alice", Password: "long-enough-password",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if u.Email != "alice@example.com" {
		t.Errorf("email not normalized: %q", u.Email)
	}
	if len(h.users.created) != 1 {
		t.Fatalf("expected 1 user created, got %d", len(h.users.created))
	}
	if h.users.created[0].PasswordHash == "long-enough-password" {
		t.Error("password stored in plaintext")
	}
	if len(h.tokens.emailByHash) != 1 {
		t.Errorf("expected 1 verification token, got %d", len(h.tokens.emailByHash))
	}
	if len(h.bus.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(h.bus.events))
	}
	ev, ok := h.bus.events[0].(AccountRegisteredEvent)
	if !ok {
		t.Fatalf("expected AccountRegisteredEvent, got %T", h.bus.events[0])
	}
	if ev.VerificationToken == "" {
		t.Error("event carries empty verification token")
	}
	// The plaintext token in the event must hash to a stored hash.
	if _, ok := h.tokens.emailByHash[hashToken(ev.VerificationToken)]; !ok {
		t.Error("event token does not match any stored hash")
	}
}

func TestRegisterRejectsWhenSignupDisabled(t *testing.T) {
	h := newHarness(t, fakeSettings{signup: false})
	_, err := h.svc.Register(context.Background(), RegisterInput{Email: "a@b.com", Password: "x"})
	if !errors.Is(err, ErrSignupDisabled) {
		t.Fatalf("expected ErrSignupDisabled, got %v", err)
	}
	if len(h.bus.events) != 0 {
		t.Error("no event should be emitted when signup is disabled")
	}
}

func TestRegisterRejectsDuplicateEmail(t *testing.T) {
	h := newHarness(t, fakeSettings{signup: true})
	h.users.byEmail["taken@example.com"] = User{ID: uuid.New(), Email: "taken@example.com"}
	_, err := h.svc.Register(context.Background(), RegisterInput{Email: "Taken@example.com", Password: "x"})
	if !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("expected ErrEmailTaken, got %v", err)
	}
}

func TestLoginSuccess(t *testing.T) {
	h := newHarness(t, fakeSettings{signup: true})
	hash, _ := h.hasher.Hash("super-secret")
	id := uuid.New()
	verified := time.Now()
	u := User{ID: id, Email: "bob@example.com", PasswordHash: hash, EmailVerifiedAt: &verified}
	h.users.byEmail["bob@example.com"] = u
	h.users.byID[id] = u

	got, err := h.svc.Login(context.Background(), LoginInput{Identifier: "bob@example.com", Password: "super-secret"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if got.ID != id {
		t.Error("wrong user returned")
	}
}

func TestLoginUnknownUserIsGenericError(t *testing.T) {
	h := newHarness(t, fakeSettings{})
	_, err := h.svc.Login(context.Background(), LoginInput{Identifier: "ghost@example.com", Password: "x"})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials for unknown user, got %v", err)
	}
}

func TestLoginWrongPasswordSameErrorAsUnknown(t *testing.T) {
	h := newHarness(t, fakeSettings{})
	hash, _ := h.hasher.Hash("real-password")
	h.users.byEmail["c@example.com"] = User{ID: uuid.New(), Email: "c@example.com", PasswordHash: hash}
	_, err := h.svc.Login(context.Background(), LoginInput{Identifier: "c@example.com", Password: "wrong"})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials for wrong password, got %v", err)
	}
}

func TestLoginRequiresVerifiedEmailWhenConfigured(t *testing.T) {
	h := newHarness(t, fakeSettings{verifyNeeded: true})
	hash, _ := h.hasher.Hash("pw12345678")
	id := uuid.New()
	u := User{ID: id, Email: "d@example.com", PasswordHash: hash} // unverified
	h.users.byEmail["d@example.com"] = u
	h.users.byID[id] = u
	_, err := h.svc.Login(context.Background(), LoginInput{Identifier: "d@example.com", Password: "pw12345678"})
	if !errors.Is(err, ErrEmailNotVerified) {
		t.Fatalf("expected ErrEmailNotVerified, got %v", err)
	}
}

func TestRequestPasswordResetNoEnumerationForUnknownEmail(t *testing.T) {
	h := newHarness(t, fakeSettings{})
	if err := h.svc.RequestPasswordReset(context.Background(), "nobody@example.com"); err != nil {
		t.Fatalf("expected nil (outward success), got %v", err)
	}
	if len(h.tokens.resetByHash) != 0 {
		t.Error("no token should be issued for unknown email")
	}
	if len(h.bus.events) != 0 {
		t.Error("no event should be emitted for unknown email")
	}
}

func TestRequestPasswordResetIssuesTokenAndEvent(t *testing.T) {
	h := newHarness(t, fakeSettings{})
	id := uuid.New()
	h.users.byEmail["e@example.com"] = User{ID: id, Email: "e@example.com"}
	if err := h.svc.RequestPasswordReset(context.Background(), "E@example.com"); err != nil {
		t.Fatalf("RequestPasswordReset: %v", err)
	}
	if len(h.tokens.resetByHash) != 1 {
		t.Fatalf("expected 1 reset token, got %d", len(h.tokens.resetByHash))
	}
	if len(h.bus.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(h.bus.events))
	}
	if _, ok := h.bus.events[0].(PasswordResetRequestedEvent); !ok {
		t.Fatalf("expected PasswordResetRequestedEvent, got %T", h.bus.events[0])
	}
}

func TestResetPasswordConsumesTokenAndIsSingleUse(t *testing.T) {
	h := newHarness(t, fakeSettings{})
	id := uuid.New()
	h.users.byEmail["f@example.com"] = User{ID: id, Email: "f@example.com"}
	h.users.byID[id] = User{ID: id, Email: "f@example.com"}
	_ = h.svc.RequestPasswordReset(context.Background(), "f@example.com")
	ev := h.bus.events[0].(PasswordResetRequestedEvent)

	if err := h.svc.ResetPassword(context.Background(), ev.ResetToken, "new-strong-password"); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	if h.users.passwords[id] == "" {
		t.Error("password was not updated")
	}
	// Second use must fail (single-use).
	err := h.svc.ResetPassword(context.Background(), ev.ResetToken, "another-password")
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken on reuse, got %v", err)
	}
}

func TestResetPasswordRejectsExpiredToken(t *testing.T) {
	past := func() time.Time { return time.Now().Add(2 * time.Hour) }
	h := newHarness(t, fakeSettings{})
	id := uuid.New()
	h.users.byEmail["g@example.com"] = User{ID: id, Email: "g@example.com"}
	_ = h.svc.RequestPasswordReset(context.Background(), "g@example.com")
	ev := h.bus.events[0].(PasswordResetRequestedEvent)
	// Advance the clock past the 1h TTL.
	h.svc.now = past
	if err := h.svc.ResetPassword(context.Background(), ev.ResetToken, "x"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken for expired token, got %v", err)
	}
}

func TestResetPasswordRejectsUnknownToken(t *testing.T) {
	h := newHarness(t, fakeSettings{})
	if err := h.svc.ResetPassword(context.Background(), "garbage-token", "x"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}
}

func TestVerifyEmailStampsAndConsumes(t *testing.T) {
	h := newHarness(t, fakeSettings{signup: true})
	u, err := h.svc.Register(context.Background(), RegisterInput{Email: "h@example.com", Password: "pw12345678"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	ev := h.bus.events[0].(AccountRegisteredEvent)
	if err := h.svc.VerifyEmail(context.Background(), ev.VerificationToken); err != nil {
		t.Fatalf("VerifyEmail: %v", err)
	}
	if !h.users.verified[u.ID] {
		t.Error("email_verified_at not stamped")
	}
	// Single-use.
	if err := h.svc.VerifyEmail(context.Background(), ev.VerificationToken); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken on reuse, got %v", err)
	}
}

func TestChangePasswordVerifiesCurrent(t *testing.T) {
	h := newHarness(t, fakeSettings{})
	hash, _ := h.hasher.Hash("current-password")
	id := uuid.New()
	h.users.byID[id] = User{ID: id, PasswordHash: hash}

	if err := h.svc.ChangePassword(context.Background(), id, "wrong", "new-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials for wrong current password, got %v", err)
	}
	if err := h.svc.ChangePassword(context.Background(), id, "current-password", "new-password"); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	if h.users.passwords[id] == "" {
		t.Error("password not changed")
	}
}
