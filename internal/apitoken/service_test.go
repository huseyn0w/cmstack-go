package apitoken

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// fakeRepo is an in-memory Repo enforcing the same validity rules as the pg
// query (not revoked, not expired) so Generate/Verify can be exercised without a
// database.
type fakeRepo struct {
	byHash  map[string]Token
	touched map[uuid.UUID]int
	now     func() time.Time
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{byHash: map[string]Token{}, touched: map[uuid.UUID]int{}, now: time.Now}
}

func (f *fakeRepo) Create(_ context.Context, in CreateParams) (Token, error) {
	t := Token{
		ID:        uuid.New(),
		UserID:    in.UserID,
		Name:      in.Name,
		LastFour:  in.LastFour,
		ExpiresAt: in.ExpiresAt,
		CreatedAt: f.now(),
	}
	f.byHash[in.TokenHash] = t
	return t, nil
}

func (f *fakeRepo) GetByHash(_ context.Context, hash string) (Token, error) {
	t, ok := f.byHash[hash]
	if !ok {
		return Token{}, ErrNotFound
	}
	if t.RevokedAt != nil {
		return Token{}, ErrNotFound
	}
	if t.ExpiresAt != nil && !t.ExpiresAt.After(f.now()) {
		return Token{}, ErrNotFound
	}
	return t, nil
}

func (f *fakeRepo) Touch(_ context.Context, id uuid.UUID) error {
	f.touched[id]++
	return nil
}

func (f *fakeRepo) ListByUser(_ context.Context, userID uuid.UUID) ([]Token, error) {
	var out []Token
	for _, t := range f.byHash {
		if t.UserID == userID {
			out = append(out, t)
		}
	}
	return out, nil
}

func (f *fakeRepo) Revoke(_ context.Context, id, userID uuid.UUID) error {
	for h, t := range f.byHash {
		if t.ID == id && t.UserID == userID {
			now := f.now()
			t.RevokedAt = &now
			f.byHash[h] = t
		}
	}
	return nil
}

func TestGenerateVerifyRoundTrip(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)
	userID := uuid.New()

	plaintext, tok, err := svc.Generate(context.Background(), userID, "ci", nil)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.HasPrefix(plaintext, tokenPrefix) {
		t.Errorf("plaintext missing %q prefix: %q", tokenPrefix, plaintext)
	}
	if tok.LastFour != plaintext[len(plaintext)-4:] {
		t.Errorf("last_four=%q not the trailing 4 of plaintext", tok.LastFour)
	}
	// The plaintext must never be persisted: only its hash is a map key.
	if _, stored := repo.byHash[plaintext]; stored {
		t.Error("plaintext was stored as a key (must store hash only)")
	}
	if _, stored := repo.byHash[hashToken(plaintext)]; !stored {
		t.Error("hash not stored")
	}

	gotID, ok, err := svc.Verify(context.Background(), plaintext)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !ok || gotID != userID {
		t.Errorf("Verify = (%v, %v), want (%v, true)", gotID, ok, userID)
	}
	if repo.touched[tok.ID] != 1 {
		t.Errorf("expected token touched once on successful verify, got %d", repo.touched[tok.ID])
	}
}

func TestVerifyRejects(t *testing.T) {
	repo := newFakeRepo()
	fixed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	repo.now = func() time.Time { return fixed }
	svc := NewService(repo)
	userID := uuid.New()

	valid, _, err := svc.Generate(context.Background(), userID, "valid", nil)
	if err != nil {
		t.Fatalf("Generate valid: %v", err)
	}

	// Expired token.
	past := fixed.Add(-time.Hour)
	expired, _, err := svc.Generate(context.Background(), userID, "expired", &past)
	if err != nil {
		t.Fatalf("Generate expired: %v", err)
	}

	// Revoked token.
	revokedPlain, revokedTok, err := svc.Generate(context.Background(), userID, "revoked", nil)
	if err != nil {
		t.Fatalf("Generate revoked: %v", err)
	}
	if err := svc.Revoke(context.Background(), revokedTok.ID, userID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	cases := []struct {
		name      string
		plaintext string
	}{
		{"empty", ""},
		{"whitespace", "   "},
		{"malformed (no prefix)", "not-a-token"},
		{"unknown", tokenPrefix + "deadbeef"},
		{"expired", expired},
		{"revoked", revokedPlain},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, ok, err := svc.Verify(context.Background(), tc.plaintext)
			if err != nil {
				t.Fatalf("Verify error: %v", err)
			}
			if ok || id != uuid.Nil {
				t.Errorf("Verify(%q) = (%v, %v), want (Nil, false)", tc.plaintext, id, ok)
			}
		})
	}

	// The valid token still verifies after the negatives.
	if _, ok, _ := svc.Verify(context.Background(), valid); !ok {
		t.Error("valid token should still verify")
	}
}

func TestGenerateUniqueTokens(t *testing.T) {
	svc := NewService(newFakeRepo())
	userID := uuid.New()
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		p, _, err := svc.Generate(context.Background(), userID, "x", nil)
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		if seen[p] {
			t.Fatalf("duplicate token generated: %q", p)
		}
		seen[p] = true
	}
}
