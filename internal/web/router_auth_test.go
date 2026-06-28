package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/health"
	"github.com/huseyn0w/cmstack-go/internal/platform/config"
	"github.com/huseyn0w/cmstack-go/internal/platform/security"
	"github.com/huseyn0w/cmstack-go/internal/platform/session"
)

// stubAuthService satisfies accounts.Service for router wiring tests.
type stubAuthService struct {
	loginUser accounts.User
	loginErr  error
}

func (s stubAuthService) Register(context.Context, accounts.RegisterInput) (accounts.User, error) {
	return accounts.User{}, nil
}

func (s stubAuthService) Login(context.Context, accounts.LoginInput) (accounts.User, error) {
	return s.loginUser, s.loginErr
}
func (s stubAuthService) RequestPasswordReset(context.Context, string) error  { return nil }
func (s stubAuthService) ResetPassword(context.Context, string, string) error { return nil }
func (s stubAuthService) VerifyEmail(context.Context, string) error           { return nil }

func newAuthRouter(t *testing.T, svc accounts.Service, users UserLoader, authz PermissionChecker) (http.Handler, *AuthMiddleware) {
	t.Helper()
	sess := session.NewManager(false)
	mw := NewAuthMiddleware(sess, users, authz)
	h := accounts.NewHandler(svc, mw, security.Token, accounts.NewValidator())
	r := Router(Deps{
		Config:   config.Config{AppEnv: "test"},
		Health:   health.NewHandler(health.NewService(nil)),
		Session:  sess,
		Auth:     h,
		AuthMW:   mw,
		CSRFFunc: security.Token,
	})
	return r, mw
}

func TestLoginPageIsReachable(t *testing.T) {
	r, _ := newAuthRouter(t, stubAuthService{}, fakeUsers{}, fakeAuthz{})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/login", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/login code = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `data-testid="login-form"`) {
		t.Error("login page did not render the form")
	}
}

func TestAdminRedirectsUnauthenticated(t *testing.T) {
	r, _ := newAuthRouter(t, stubAuthService{}, fakeUsers{}, fakeAuthz{})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("/admin code = %d, want 303 redirect", rec.Code)
	}
	if rec.Header().Get("Location") != "/login" {
		t.Errorf("redirect = %q, want /login", rec.Header().Get("Location"))
	}
}

func TestVerifyEmailRouteReachable(t *testing.T) {
	r, _ := newAuthRouter(t, stubAuthService{}, fakeUsers{}, fakeAuthz{})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/verify-email?token=anything", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/verify-email code = %d, want 200 (stub verifies ok)", rec.Code)
	}
}

func TestUnusedMiddlewareReference(t *testing.T) {
	// Touch the returned middleware so the helper's second value is exercised.
	_, mw := newAuthRouter(t, stubAuthService{}, fakeUsers{users: map[uuid.UUID]accounts.User{}}, fakeAuthz{})
	if mw == nil {
		t.Fatal("expected non-nil middleware")
	}
}
