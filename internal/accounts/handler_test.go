package accounts

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// fakeService implements the handler's Service interface with programmable
// outcomes.
type fakeService struct {
	loginUser    User
	loginErr     error
	registerErr  error
	resetErr     error
	verifyErr    error
	forgotErr    error
	registerSeen *RegisterInput
	forgotEmail  string
}

func (f *fakeService) Register(_ context.Context, in RegisterInput) (User, error) {
	f.registerSeen = &in
	return User{ID: uuid.New(), Email: in.Email}, f.registerErr
}

func (f *fakeService) Login(_ context.Context, _ LoginInput) (User, error) {
	return f.loginUser, f.loginErr
}

func (f *fakeService) RequestPasswordReset(_ context.Context, email string) error {
	f.forgotEmail = email
	return f.forgotErr
}
func (f *fakeService) ResetPassword(_ context.Context, _, _ string) error { return f.resetErr }
func (f *fakeService) VerifyEmail(_ context.Context, _ string) error      { return f.verifyErr }

type fakeSessionLogin struct {
	loggedIn  uuid.UUID
	loggedOut bool
}

func (f *fakeSessionLogin) Login(_ context.Context, id uuid.UUID) error {
	f.loggedIn = id
	return nil
}
func (f *fakeSessionLogin) Logout(_ context.Context) error { f.loggedOut = true; return nil }

func newTestHandler(svc Service, sess SessionLogin) *Handler {
	return NewHandler(svc, sess, func(*http.Request) string { return "csrf-token" }, NewValidator())
}

func postForm(values url.Values) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func TestSubmitLoginSuccessRedirectsAndStartsSession(t *testing.T) {
	id := uuid.New()
	svc := &fakeService{loginUser: User{ID: id}}
	sess := &fakeSessionLogin{}
	h := newTestHandler(svc, sess)

	rec := httptest.NewRecorder()
	h.SubmitLogin(rec, postForm(url.Values{"identifier": {"a@b.com"}, "password": {"pw"}}))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("code = %d, want 303", rec.Code)
	}
	if rec.Header().Get("Location") != "/admin" {
		t.Errorf("redirect = %q, want /admin", rec.Header().Get("Location"))
	}
	if sess.loggedIn != id {
		t.Error("session login not called with user id")
	}
}

func TestSubmitLoginInvalidShowsGenericError(t *testing.T) {
	svc := &fakeService{loginErr: ErrInvalidCredentials}
	h := newTestHandler(svc, &fakeSessionLogin{})

	rec := httptest.NewRecorder()
	h.SubmitLogin(rec, postForm(url.Values{"identifier": {"a@b.com"}, "password": {"wrong"}}))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Invalid email/username or password") {
		t.Error("expected generic credential error in body")
	}
	if !strings.Contains(body, `data-testid="form-error"`) {
		t.Error("expected error banner")
	}
}

func TestSubmitSignupValidationErrors(t *testing.T) {
	svc := &fakeService{}
	h := newTestHandler(svc, &fakeSessionLogin{})

	rec := httptest.NewRecorder()
	h.SubmitSignup(rec, postForm(url.Values{"name": {""}, "email": {"bad"}, "password": {"short"}}))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("code = %d, want 422", rec.Code)
	}
	if svc.registerSeen != nil {
		t.Error("service should not be called when validation fails")
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-testid="error-summary"`) {
		t.Error("expected error summary on invalid signup")
	}
}

func TestSubmitSignupSuccessShowsNotice(t *testing.T) {
	svc := &fakeService{}
	h := newTestHandler(svc, &fakeSessionLogin{})

	rec := httptest.NewRecorder()
	h.SubmitSignup(rec, postForm(url.Values{
		"name": {"Alice"}, "email": {"alice@example.com"}, "password": {"long-enough-pw"},
	}))

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if svc.registerSeen == nil || svc.registerSeen.Email != "alice@example.com" {
		t.Error("service.Register not called with the form values")
	}
	if !strings.Contains(rec.Body.String(), "Check your email") {
		t.Error("expected verification notice")
	}
}

func TestSubmitForgotAlwaysSucceedsNoEnumeration(t *testing.T) {
	svc := &fakeService{} // RequestPasswordReset returns nil even for unknown
	h := newTestHandler(svc, &fakeSessionLogin{})

	rec := httptest.NewRecorder()
	h.SubmitForgotPassword(rec, postForm(url.Values{"email": {"ghost@example.com"}}))

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "If an account exists") {
		t.Error("expected non-committal anti-enumeration notice")
	}
	if svc.forgotEmail != "ghost@example.com" {
		t.Error("service should still be invoked")
	}
}

func TestVerifyEmailInvalidTokenShowsFailure(t *testing.T) {
	svc := &fakeService{verifyErr: ErrInvalidToken}
	h := newTestHandler(svc, &fakeSessionLogin{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/verify-email?token=bad", nil)
	h.VerifyEmail(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Verification failed") {
		t.Error("expected failure message")
	}
}

func TestVerifyEmailSuccess(t *testing.T) {
	svc := &fakeService{}
	h := newTestHandler(svc, &fakeSessionLogin{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/verify-email?token=good", nil)
	h.VerifyEmail(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Email verified") {
		t.Error("expected success message")
	}
}

func TestSubmitLogoutEndsSession(t *testing.T) {
	sess := &fakeSessionLogin{}
	h := newTestHandler(&fakeService{}, sess)

	rec := httptest.NewRecorder()
	h.SubmitLogout(rec, httptest.NewRequest(http.MethodPost, "/logout", nil))

	if !sess.loggedOut {
		t.Error("logout not called")
	}
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Errorf("expected redirect to /login, got %d %q", rec.Code, rec.Header().Get("Location"))
	}
}
