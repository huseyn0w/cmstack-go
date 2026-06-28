package accounts

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/platform/security"
)

// TestDummyHashIsAValidVerifiableHash guards Fix 1: the anti-enumeration dummy
// hash must be a REAL argon2id hash so Verify performs the full argon2 work and
// returns (false, nil) — never the fast (false, ErrInvalidHash) path that
// created a ~13,000x timing oracle distinguishing unknown users.
func TestDummyHashIsAValidVerifiableHash(t *testing.T) {
	hasher := security.NewPasswordHasher()
	ok, err := hasher.Verify("any-attempted-password", dummyHash)
	if err != nil {
		t.Fatalf("dummyHash must be a valid argon2id hash so Verify does real work; got error %v", err)
	}
	if ok {
		t.Error("dummyHash must not verify against an attacker-chosen password")
	}
}

// spyHasher counts Verify calls so the unknown-user login path can be asserted
// to still perform exactly one full verification (constant-time defense).
type spyHasher struct {
	inner       Hasher
	verifyCalls int
	lastEncoded string
}

func (s *spyHasher) Hash(p string) (string, error) { return s.inner.Hash(p) }

func (s *spyHasher) Verify(password, encoded string) (bool, error) {
	s.verifyCalls++
	s.lastEncoded = encoded
	return s.inner.Verify(password, encoded)
}

// TestLoginUnknownUserStillVerifiesOnce guards Fix 1 at the service level: an
// unknown-user login must invoke Verify exactly once (against dummyHash) before
// returning the generic error, so timing cannot leak account existence.
func TestLoginUnknownUserStillVerifiesOnce(t *testing.T) {
	users := newFakeUserRepo()
	tokens := newFakeTokenRepo()
	bus := newRecordingBus()
	spy := &spyHasher{inner: security.NewPasswordHasher()}
	settings := fakeSettings{}
	svc := NewAuthService(
		fakePool{},
		users,
		fakeRoleRepo{member: Role{ID: uuid.New(), Key: RoleMember, Label: "Member"}},
		tokens,
		newFakeOAuthRepo(),
		spy,
		bus,
		settings,
		time.Now,
	)

	_, err := svc.Login(context.Background(), LoginInput{Identifier: "ghost@example.com", Password: "x"})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials for unknown user, got %v", err)
	}
	if spy.verifyCalls != 1 {
		t.Fatalf("unknown-user login must call Verify exactly once, got %d", spy.verifyCalls)
	}
	if spy.lastEncoded != dummyHash {
		t.Errorf("unknown-user login must verify against dummyHash, got %q", spy.lastEncoded)
	}
}
