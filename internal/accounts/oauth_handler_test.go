package accounts

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/markbates/goth"
)

// fakeOAuthService is a programmable OAuthService for handler tests.
type fakeOAuthService struct {
	user User
	err  error
	seen OAuthIdentity
}

func (f *fakeOAuthService) LoginWithOAuth(_ context.Context, id OAuthIdentity) (User, error) {
	f.seen = id
	return f.user, f.err
}

func staticResolver(name string) ProviderResolver {
	return func(*http.Request) string { return name }
}

func TestOAuthCallbackSuccessLogsInAndRedirects(t *testing.T) {
	id := uuid.New()
	svc := &fakeOAuthService{user: User{ID: id, Email: "u@example.com"}}
	sess := &fakeSessionLogin{}
	h := NewOAuthHandler(svc, sess, staticResolver("google"))
	// Substitute goth completion with a canned provider user.
	h.complete = func(http.ResponseWriter, *http.Request) (goth.User, error) {
		return goth.User{UserID: "g-1", Email: "u@example.com", Name: "Ada", AvatarURL: "https://img"}, nil
	}

	rec := httptest.NewRecorder()
	h.Callback(rec, httptest.NewRequest(http.MethodGet, "/auth/google/callback", nil))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("code = %d, want 303", rec.Code)
	}
	if rec.Header().Get("Location") != "/admin" {
		t.Errorf("redirect = %q, want /admin", rec.Header().Get("Location"))
	}
	if sess.loggedIn != id {
		t.Error("session login not called with the user id")
	}
	// The identity passed to the service is mapped from the goth user.
	if svc.seen.Provider != "google" || svc.seen.ProviderUserID != "g-1" || svc.seen.Email != "u@example.com" {
		t.Errorf("identity mapped wrong: %+v", svc.seen)
	}
	if svc.seen.Name != "Ada" || svc.seen.AvatarURL != "https://img" {
		t.Errorf("name/avatar not mapped: %+v", svc.seen)
	}
}

func TestOAuthCallbackProviderErrorRendersLogin(t *testing.T) {
	h := NewOAuthHandler(&fakeOAuthService{}, &fakeSessionLogin{}, staticResolver("github"))
	h.complete = func(http.ResponseWriter, *http.Request) (goth.User, error) {
		return goth.User{}, errors.New("state mismatch")
	}

	rec := httptest.NewRecorder()
	h.Callback(rec, httptest.NewRequest(http.MethodGet, "/auth/github/callback", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "could not complete sign-in") {
		t.Error("expected provider failure message on the login page")
	}
}

func TestOAuthCallbackSignupDisabledShowsMessage(t *testing.T) {
	svc := &fakeOAuthService{err: ErrOAuthSignupDisabled}
	h := NewOAuthHandler(svc, &fakeSessionLogin{}, staticResolver("google"))
	h.complete = func(http.ResponseWriter, *http.Request) (goth.User, error) {
		return goth.User{UserID: "g-2", Email: "new@example.com"}, nil
	}

	rec := httptest.NewRecorder()
	h.Callback(rec, httptest.NewRequest(http.MethodGet, "/auth/google/callback", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Registration is disabled") {
		t.Error("expected signup-disabled message")
	}
}

func TestOAuthBeginInvokesGoth(t *testing.T) {
	called := false
	h := NewOAuthHandler(&fakeOAuthService{}, &fakeSessionLogin{}, staticResolver("google"))
	h.begin = func(http.ResponseWriter, *http.Request) { called = true }

	rec := httptest.NewRecorder()
	h.Begin(rec, httptest.NewRequest(http.MethodGet, "/auth/google", nil))
	if !called {
		t.Error("Begin should delegate to the goth begin handler")
	}
}
