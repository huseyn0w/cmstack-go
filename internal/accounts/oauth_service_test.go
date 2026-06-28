package accounts

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// seedExistingUser inserts a user directly into the fake repos so branch tests
// can target the existing-email path.
func seedExistingUser(h *harness, email string) User {
	id := uuid.New()
	u := User{ID: id, Email: normalizeEmail(email), Name: "Existing"}
	h.users.byEmail[u.Email] = u
	h.users.byID[id] = u
	return u
}

func TestLoginWithOAuth(t *testing.T) {
	provider := "google"
	providerUID := "google-123"

	t.Run("existing oauth link logs in without writes", func(t *testing.T) {
		h := newHarness(t, fakeSettings{signup: true})
		existing := seedExistingUser(h, "linked@example.com")
		h.oauth.links[oauthKey(provider, providerUID)] = OAuthAccount{
			ID: uuid.New(), UserID: existing.ID, Provider: provider, ProviderUserID: providerUID,
		}

		got, err := h.svc.LoginWithOAuth(context.Background(), OAuthIdentity{
			Provider: provider, ProviderUserID: providerUID, Email: "different@example.com",
		})
		if err != nil {
			t.Fatalf("LoginWithOAuth: %v", err)
		}
		if got.ID != existing.ID {
			t.Errorf("logged in %s, want existing %s", got.ID, existing.ID)
		}
		// No new user created.
		if len(h.users.created) != 0 {
			t.Error("existing-link path must not create a user")
		}
	})

	t.Run("existing email links new identity and logs in", func(t *testing.T) {
		h := newHarness(t, fakeSettings{signup: true})
		existing := seedExistingUser(h, "match@example.com")

		got, err := h.svc.LoginWithOAuth(context.Background(), OAuthIdentity{
			Provider: provider, ProviderUserID: providerUID, Email: "Match@example.com",
		})
		if err != nil {
			t.Fatalf("LoginWithOAuth: %v", err)
		}
		if got.ID != existing.ID {
			t.Errorf("logged in %s, want existing %s", got.ID, existing.ID)
		}
		if _, ok := h.oauth.links[oauthKey(provider, providerUID)]; !ok {
			t.Error("expected a new oauth link to the existing user")
		}
		if h.oauth.links[oauthKey(provider, providerUID)].UserID != existing.ID {
			t.Error("link must point at the existing user")
		}
		if len(h.users.created) != 0 {
			t.Error("link path must not create a user")
		}
	})

	t.Run("new identity creates verified member and links", func(t *testing.T) {
		h := newHarness(t, fakeSettings{signup: true})

		got, err := h.svc.LoginWithOAuth(context.Background(), OAuthIdentity{
			Provider: provider, ProviderUserID: providerUID,
			Email: "New@example.com", Name: "New User", AvatarURL: "https://img/a.png",
		})
		if err != nil {
			t.Fatalf("LoginWithOAuth: %v", err)
		}
		if len(h.users.created) != 1 {
			t.Fatalf("expected 1 user created, got %d", len(h.users.created))
		}
		c := h.users.created[0]
		if c.Email != "new@example.com" {
			t.Errorf("email not normalized: %q", c.Email)
		}
		if c.EmailVerifiedAt == nil {
			t.Error("provider-verified account must be created with email_verified_at set")
		}
		if c.AvatarURL != "https://img/a.png" {
			t.Errorf("avatar not stored: %q", c.AvatarURL)
		}
		if c.PasswordHash == "" {
			t.Error("password_hash must be a non-empty unusable hash")
		}
		if _, ok := h.oauth.links[oauthKey(provider, providerUID)]; !ok {
			t.Error("expected the new user to be linked")
		}
		if got.Email != "new@example.com" {
			t.Errorf("returned user email = %q", got.Email)
		}
	})

	t.Run("signup disabled + new identity is denied", func(t *testing.T) {
		h := newHarness(t, fakeSettings{signup: false})

		_, err := h.svc.LoginWithOAuth(context.Background(), OAuthIdentity{
			Provider: provider, ProviderUserID: providerUID, Email: "new@example.com",
		})
		if !errors.Is(err, ErrOAuthSignupDisabled) {
			t.Fatalf("expected ErrOAuthSignupDisabled, got %v", err)
		}
		if len(h.users.created) != 0 {
			t.Error("denied signup must not create a user")
		}
	})

	t.Run("signup disabled still links to existing user", func(t *testing.T) {
		h := newHarness(t, fakeSettings{signup: false})
		existing := seedExistingUser(h, "match@example.com")

		got, err := h.svc.LoginWithOAuth(context.Background(), OAuthIdentity{
			Provider: provider, ProviderUserID: providerUID, Email: "match@example.com",
		})
		if err != nil {
			t.Fatalf("LoginWithOAuth (link with signup off): %v", err)
		}
		if got.ID != existing.ID {
			t.Error("should link to and log in the existing user even when signup is off")
		}
	})

	t.Run("missing email with no link is denied", func(t *testing.T) {
		h := newHarness(t, fakeSettings{signup: true})

		_, err := h.svc.LoginWithOAuth(context.Background(), OAuthIdentity{
			Provider: provider, ProviderUserID: providerUID, Email: "",
		})
		if !errors.Is(err, ErrOAuthNoEmail) {
			t.Fatalf("expected ErrOAuthNoEmail, got %v", err)
		}
	})

	t.Run("missing email still works when an oauth link exists", func(t *testing.T) {
		h := newHarness(t, fakeSettings{signup: true})
		existing := seedExistingUser(h, "linked@example.com")
		h.oauth.links[oauthKey(provider, providerUID)] = OAuthAccount{
			ID: uuid.New(), UserID: existing.ID, Provider: provider, ProviderUserID: providerUID,
		}
		got, err := h.svc.LoginWithOAuth(context.Background(), OAuthIdentity{
			Provider: provider, ProviderUserID: providerUID, Email: "",
		})
		if err != nil {
			t.Fatalf("LoginWithOAuth: %v", err)
		}
		if got.ID != existing.ID {
			t.Error("link path keys off provider id, not email")
		}
	})
}

// TestUnusablePasswordHashIsUnique guards that two social account creations get
// distinct, non-empty password hashes (random source), so the password path can
// never be guessed.
func TestUnusablePasswordHashIsUnique(t *testing.T) {
	h := newHarness(t, fakeSettings{})
	a, err := h.svc.unusablePasswordHash()
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	b, err := h.svc.unusablePasswordHash()
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if a == "" || b == "" {
		t.Fatal("unusable hash must be non-empty")
	}
	if a == b {
		t.Error("unusable hashes must differ (fresh random source)")
	}
	_ = time.Now
}
