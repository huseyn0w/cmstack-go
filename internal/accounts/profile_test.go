package accounts

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/huseyn0w/agentic-cms-go/internal/platform/storage"
)

// fakeAvatarStore is an in-memory AvatarStore for service tests.
type fakeAvatarStore struct {
	saved   map[string][]byte
	deleted []string
}

func newFakeAvatarStore() *fakeAvatarStore {
	return &fakeAvatarStore{saved: map[string][]byte{}}
}

func (f *fakeAvatarStore) Save(_ context.Context, key string, r io.Reader, _ string) (string, error) {
	b, _ := io.ReadAll(r)
	f.saved[key] = b
	return key, nil
}

func (f *fakeAvatarStore) Delete(_ context.Context, key string) error {
	f.deleted = append(f.deleted, key)
	delete(f.saved, key)
	return nil
}

func (f *fakeAvatarStore) URL(key string) string {
	if key == "" {
		return ""
	}
	return "/uploads/" + key
}

func newProfileFixture(t *testing.T) (*ProfileService, *fakeUserRepo, *fakeAvatarStore, User) {
	t.Helper()
	users := newFakeUserRepo()
	roles := fakeRoleRepo{member: Role{ID: uuid.New(), Key: RoleAuthor, Label: "Author"}}
	store := newFakeAvatarStore()
	u := User{ID: uuid.New(), Email: "secret@example.com", Name: "Ada Lovelace", RoleID: roles.member.ID}
	users.byID[u.ID] = u
	svc := NewProfileService(fakePool{}, users, roles, store)
	return svc, users, store, u
}

func TestUpdateProfile_NormalizesAndPersists(t *testing.T) {
	svc, users, _, u := newProfileFixture(t)
	got, err := svc.UpdateProfile(context.Background(), u.ID, UpdateProfileInput{
		Name:    "  Grace Hopper ",
		Bio:     " Pioneer ",
		Website: "grace.dev",
		SocialLinks: map[string]string{
			"github":  "github.com/grace",
			"twitter": "https://twitter.com/grace",
			"unknown": "https://evil.example/x", // dropped, not stored
		},
	})
	if err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	if got.Name != "Grace Hopper" {
		t.Errorf("name = %q", got.Name)
	}
	if got.Website != "https://grace.dev" {
		t.Errorf("website not normalized: %q", got.Website)
	}
	if got.SocialLinks["github"] != "https://github.com/grace" {
		t.Errorf("github not normalized: %q", got.SocialLinks["github"])
	}
	if _, ok := got.SocialLinks["unknown"]; ok {
		t.Error("unknown social network should be dropped")
	}
	stored := users.byID[u.ID]
	if stored.Bio != "Pioneer" {
		t.Errorf("bio not persisted/trimmed: %q", stored.Bio)
	}
}

func TestUpdateProfile_RejectsBadURLAndEmptyName(t *testing.T) {
	svc, _, _, u := newProfileFixture(t)
	_, err := svc.UpdateProfile(context.Background(), u.ID, UpdateProfileInput{
		Name:        "",
		Website:     "javascript:alert(1)",
		SocialLinks: map[string]string{"github": "not a url with spaces and no host"},
	})
	var verr ProfileValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("expected ProfileValidationError, got %v", err)
	}
	if verr.Fields["name"] == "" {
		t.Error("expected name error")
	}
	if verr.Fields["website"] == "" {
		t.Error("expected website error for javascript: scheme")
	}
}

func TestUpdateAvatar_StoresSwitchesDeletesOld(t *testing.T) {
	svc, users, store, u := newProfileFixture(t)
	// Seed an existing avatar so we can assert the old one is deleted.
	u.AvatarPath = "avatars/old/old.png"
	users.byID[u.ID] = u
	store.saved[u.AvatarPath] = []byte("old")

	up := storage.ValidatedAvatar{Data: []byte("new-bytes"), ContentType: "image/png", Ext: ".png"}
	got, err := svc.UpdateAvatar(context.Background(), u.ID, up)
	if err != nil {
		t.Fatalf("UpdateAvatar: %v", err)
	}
	if got.AvatarPath == "" || got.AvatarPath == "avatars/old/old.png" {
		t.Errorf("avatar path not switched: %q", got.AvatarPath)
	}
	if _, ok := store.saved[got.AvatarPath]; !ok {
		t.Error("new avatar not stored")
	}
	found := false
	for _, d := range store.deleted {
		if d == "avatars/old/old.png" {
			found = true
		}
	}
	if !found {
		t.Error("old avatar not deleted")
	}
}

func TestPublicAuthor_NeverLeaksEmail(t *testing.T) {
	svc, users, _, u := newProfileFixture(t)
	u.Bio = "Author bio"
	u.Website = "https://grace.dev"
	u.SocialLinks = map[string]string{"github": "https://github.com/grace"}
	users.byID[u.ID] = u

	author, err := svc.PublicAuthor(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("PublicAuthor: %v", err)
	}
	if author.Name != "Ada Lovelace" {
		t.Errorf("name = %q", author.Name)
	}
	if author.RoleLabel != "Author" {
		t.Errorf("role label = %q", author.RoleLabel)
	}
	if author.Posts == nil {
		t.Error("Posts should be empty slice, not nil")
	}
	if len(author.Posts) != 0 {
		t.Error("Posts should be empty until M2")
	}
	// The PublicAuthor struct has NO Email field by design; assert the email does
	// not appear in any of its string fields.
	blob := author.Name + author.Bio + author.Website + author.AvatarURL + author.RoleLabel
	for _, v := range author.SocialLinks {
		blob += v
	}
	if strings.Contains(strings.ToLower(blob), "secret@example.com") {
		t.Error("email leaked into public author payload")
	}
}

func TestPublicAuthor_NotFound(t *testing.T) {
	svc, _, _, _ := newProfileFixture(t)
	_, err := svc.PublicAuthor(context.Background(), uuid.New())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
