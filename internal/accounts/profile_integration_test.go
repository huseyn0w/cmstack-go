package accounts_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/huseyn0w/agentic-cms-go/internal/accounts"
)

// memAvatarStore is an in-memory AvatarStore for the profile integration test.
type memAvatarStore struct {
	saved   map[string][]byte
	deleted []string
}

func (m *memAvatarStore) Save(_ context.Context, key string, r io.Reader, _ string) (string, error) {
	if m.saved == nil {
		m.saved = map[string][]byte{}
	}
	b, _ := io.ReadAll(r)
	m.saved[key] = b
	return key, nil
}

func (m *memAvatarStore) Delete(_ context.Context, key string) error {
	m.deleted = append(m.deleted, key)
	delete(m.saved, key)
	return nil
}
func (m *memAvatarStore) URL(key string) string { return "/uploads/" + key }

func TestProfileRepoRoundTrip_RealDB(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testcontainers integration test in -short mode")
	}
	w := newWiring(t, accounts.NewStaticSettings(true, false))
	ctx := context.Background()
	store := &memAvatarStore{}
	profiles := accounts.NewProfileService(w.pool, w.users, w.roles, store)

	// Register a user, then update its profile and avatar against the real DB.
	u, err := w.svc.Register(ctx, accounts.RegisterInput{Name: "Grace", Username: "grace_h", Email: "grace@example.com", Password: "password-123"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	updated, err := profiles.UpdateProfile(ctx, u.ID, accounts.UpdateProfileInput{
		Name:        "Grace Hopper",
		Bio:         "Pioneer",
		Website:     "grace.dev",
		SocialLinks: map[string]string{"github": "github.com/grace", "bogus": "nope"},
	})
	if err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	if updated.Website != "https://grace.dev" {
		t.Errorf("website not normalized/persisted: %q", updated.Website)
	}
	if updated.SocialLinks["github"] != "https://github.com/grace" {
		t.Errorf("github not persisted: %v", updated.SocialLinks)
	}
	if _, ok := updated.SocialLinks["bogus"]; ok {
		t.Error("unknown social network should not be stored")
	}

	// Re-read from DB through GetByID to confirm jsonb round-trips.
	reloaded, err := w.users.GetByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Bio != "Pioneer" || reloaded.SocialLinks["github"] != "https://github.com/grace" {
		t.Errorf("profile did not persist to DB: %+v", reloaded)
	}

	// Avatar upload sets avatar_path and AvatarURL resolves via the store.
	withAvatar, err := profiles.UpdateAvatar(ctx, u.ID, accounts.AvatarUpload{Data: []byte("png-bytes"), ContentType: "image/png", Ext: ".png"})
	if err != nil {
		t.Fatalf("UpdateAvatar: %v", err)
	}
	if withAvatar.AvatarPath == "" {
		t.Fatal("avatar path not set")
	}
	if got := profiles.AvatarURL(withAvatar); !strings.HasPrefix(got, "/uploads/avatars/") {
		t.Errorf("AvatarURL = %q", got)
	}

	// PublicAuthor returns the public view with role label and NO email.
	author, err := profiles.PublicAuthor(ctx, u.ID)
	if err != nil {
		t.Fatalf("PublicAuthor: %v", err)
	}
	if author.Name != "Grace Hopper" {
		t.Errorf("public name = %q", author.Name)
	}
	if author.RoleLabel == "" {
		t.Error("expected a role label")
	}
}

func TestUsernameUniqueness_RealDB(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testcontainers integration test in -short mode")
	}
	w := newWiring(t, accounts.NewStaticSettings(true, false))
	ctx := context.Background()

	if _, err := w.svc.Register(ctx, accounts.RegisterInput{Name: "A", Username: "shared", Email: "a@example.com", Password: "password-123"}); err != nil {
		t.Fatalf("first register: %v", err)
	}
	// Same username (different case) must conflict via the CITEXT unique column.
	_, err := w.svc.Register(ctx, accounts.RegisterInput{Name: "B", Username: "Shared", Email: "b@example.com", Password: "password-123"})
	if err != accounts.ErrUsernameTaken {
		t.Fatalf("expected ErrUsernameTaken, got %v", err)
	}

	// Empty username is allowed and stored as NULL (no conflict between two such).
	if _, err := w.svc.Register(ctx, accounts.RegisterInput{Name: "C", Email: "c@example.com", Password: "password-123"}); err != nil {
		t.Fatalf("register without username: %v", err)
	}
	if _, err := w.svc.Register(ctx, accounts.RegisterInput{Name: "D", Email: "d@example.com", Password: "password-123"}); err != nil {
		t.Fatalf("second register without username should not conflict: %v", err)
	}
}
