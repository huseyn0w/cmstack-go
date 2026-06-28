package web

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/health"
	"github.com/huseyn0w/cmstack-go/internal/platform/config"
	"github.com/huseyn0w/cmstack-go/internal/platform/security"
	"github.com/huseyn0w/cmstack-go/internal/platform/session"
)

// fakeProfileService is a controllable ProfileService + PasswordChanger for the
// account handler tests.
type fakeProfileService struct {
	updateErr  error
	avatarErr  error
	pwdErr     error
	updated    accounts.UpdateProfileInput
	avatarUp   accounts.AvatarUpload
	pwdCalled  bool
	lastUserID uuid.UUID
}

func (f *fakeProfileService) UpdateProfile(_ context.Context, id uuid.UUID, in accounts.UpdateProfileInput) (accounts.User, error) {
	f.lastUserID = id
	f.updated = in
	if f.updateErr != nil {
		return accounts.User{}, f.updateErr
	}
	return accounts.User{ID: id, Name: in.Name, Bio: in.Bio, Website: in.Website, SocialLinks: in.SocialLinks}, nil
}

func (f *fakeProfileService) UpdateAvatar(_ context.Context, id uuid.UUID, up accounts.AvatarUpload) (accounts.User, error) {
	f.avatarUp = up
	if f.avatarErr != nil {
		return accounts.User{}, f.avatarErr
	}
	return accounts.User{ID: id, AvatarPath: "avatars/" + id.String() + "/new" + up.Ext}, nil
}

func (f *fakeProfileService) AvatarURL(u accounts.User) string {
	if u.AvatarPath != "" {
		return "/uploads/" + u.AvatarPath
	}
	return ""
}

func (f *fakeProfileService) ChangePassword(_ context.Context, id uuid.UUID, _ string, _ string) error {
	f.pwdCalled = true
	f.lastUserID = id
	return f.pwdErr
}

func buildAccountEnv(t *testing.T, user accounts.User, svc *fakeProfileService) (http.Handler, *scs.SessionManager, *AuthMiddleware) {
	t.Helper()
	sess := session.NewManager(false)
	users := fakeUsers{users: map[uuid.UUID]accounts.User{user.ID: user}}
	authz := allowAllAuthz{}
	mw := NewAuthMiddleware(sess, users, authz)
	auth := accounts.NewHandler(stubAuthService{}, mw, security.Token, accounts.NewValidator())
	account := NewAccountHandler(svc, svc, fakeRoles{role: accounts.Role{Key: "author", Label: "Author"}}, authz, security.Token, "https://site.test")
	r := Router(Deps{
		Config:   config.Config{AppEnv: "test", BaseURL: "https://site.test"},
		Health:   health.NewHandler(health.NewService(nil)),
		Session:  sess,
		Auth:     auth,
		AuthMW:   mw,
		CSRFFunc: security.Token,
		Authz:    authz,
		Roles:    fakeRoles{role: accounts.Role{Key: "author", Label: "Author"}},
		Account:  account,
	})
	return r, sess, mw
}

func authedUser(t *testing.T) accounts.User {
	t.Helper()
	return accounts.User{ID: uuid.New(), Email: "ada@example.com", Name: "Ada", PasswordChangedAt: time.Now()}
}

func TestAccountShow_RequiresAuth(t *testing.T) {
	r, _, _ := buildAccountEnv(t, authedUser(t), &fakeProfileService{})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/account", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("unauthenticated /account = %d, want 303", rec.Code)
	}
}

func TestAccountShow_RendersEditorInsideShell(t *testing.T) {
	u := authedUser(t)
	r, sess, mw := buildAccountEnv(t, u, &fakeProfileService{})
	cookie := mintSession(t, sess, mw, u)
	req := httptest.NewRequest(http.MethodGet, "/account", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/account = %d\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`data-testid="account-page"`, `data-testid="profile-form"`, `data-testid="avatar-form"`, `data-testid="password-form"`, `data-testid="admin-main"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestAccountSubmitProfile_RouteIsCSRFProtected(t *testing.T) {
	// The POST /account route lives inside the CSRF-guarded group: a request
	// without a valid token is rejected (400) before reaching the service. The
	// handler's delegation logic is proven directly in the tests below.
	u := authedUser(t)
	svc := &fakeProfileService{}
	r, sess, mw := buildAccountEnv(t, u, svc)
	cookie := mintSession(t, sess, mw, u)

	form := strings.NewReader("name=Grace&bio=Pioneer&website=grace.dev")
	req := httptest.NewRequest(http.MethodPost, "/account", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("CSRF-less POST = %d, want 400", rec.Code)
	}
	if svc.updated.Name != "" {
		t.Error("service must not be reached when CSRF check fails")
	}
}

func TestAccountSubmitProfile_ShowsValidationErrors(t *testing.T) {
	u := authedUser(t)
	svc := &fakeProfileService{updateErr: accounts.ProfileValidationError{Fields: map[string]string{"website": "Enter a valid http(s) URL."}}}
	// Drive the handler directly to bypass CSRF and assert error rendering.
	h := NewAccountHandler(svc, svc, fakeRoles{role: accounts.Role{Label: "Author"}}, allowAllAuthz{}, func(*http.Request) string { return "x" }, "https://site.test")
	req := httptest.NewRequest(http.MethodPost, "/account", strings.NewReader("name=Grace&website=javascript:alert(1)"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(withUser(req.Context(), u))
	rec := httptest.NewRecorder()
	h.SubmitProfile(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("code = %d, want 422", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Enter a valid http(s) URL.") {
		t.Error("validation error not rendered")
	}
}

func TestAccountSubmitAvatar_RejectsNonImage(t *testing.T) {
	u := authedUser(t)
	svc := &fakeProfileService{}
	h := NewAccountHandler(svc, svc, fakeRoles{role: accounts.Role{Label: "Author"}}, allowAllAuthz{}, func(*http.Request) string { return "x" }, "https://site.test")

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("avatar", "evil.png")
	fw.Write([]byte("this is plain text, not an image at all"))
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/account/avatar", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req = req.WithContext(withUser(req.Context(), u))
	rec := httptest.NewRecorder()
	h.SubmitAvatar(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("non-image avatar code = %d, want 422\n%s", rec.Code, rec.Body.String())
	}
	if svc.avatarUp.Ext != "" {
		t.Error("service should not be called for invalid avatar")
	}
	if !strings.Contains(rec.Body.String(), "PNG, JPEG, WebP or GIF") {
		t.Error("expected avatar type error message")
	}
}

func TestAccountSubmitPassword_MismatchRejected(t *testing.T) {
	u := authedUser(t)
	svc := &fakeProfileService{}
	h := NewAccountHandler(svc, svc, fakeRoles{role: accounts.Role{Label: "Author"}}, allowAllAuthz{}, func(*http.Request) string { return "x" }, "https://site.test")
	req := httptest.NewRequest(http.MethodPost, "/account/password", strings.NewReader("current_password=old12345&new_password=newpassword&confirm_password=different"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(withUser(req.Context(), u))
	rec := httptest.NewRecorder()
	h.SubmitPassword(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("code = %d, want 422", rec.Code)
	}
	if svc.pwdCalled {
		t.Error("ChangePassword should not be called when confirmation mismatches")
	}
}

func TestAccountSubmitPassword_DelegatesOnValid(t *testing.T) {
	u := authedUser(t)
	svc := &fakeProfileService{}
	h := NewAccountHandler(svc, svc, fakeRoles{role: accounts.Role{Label: "Author"}}, allowAllAuthz{}, func(*http.Request) string { return "x" }, "https://site.test")
	req := httptest.NewRequest(http.MethodPost, "/account/password", strings.NewReader("current_password=old12345&new_password=newpassword&confirm_password=newpassword"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(withUser(req.Context(), u))
	rec := httptest.NewRecorder()
	h.SubmitPassword(rec, req)
	if !svc.pwdCalled {
		t.Error("ChangePassword should be called on valid input")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
}
