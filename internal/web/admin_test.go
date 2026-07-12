package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"

	"github.com/huseyn0w/agentic-cms-go/internal/accounts"
	"github.com/huseyn0w/agentic-cms-go/internal/health"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/config"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/security"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/session"
)

// fakeRoles resolves a role label for the shell badge.
type fakeRoles struct{ role accounts.Role }

func (f fakeRoles) GetByID(context.Context, uuid.UUID) (accounts.Role, error) {
	return f.role, nil
}

// allowAllAuthz grants every permission (Administrator), so the shell renders
// every nav group.
type allowAllAuthz struct{}

func (allowAllAuthz) Can(context.Context, uuid.UUID, string, string) bool { return true }

// buildAdminEnv builds a router with the full admin shell wired, sharing ONE scs
// manager so a session cookie minted by mintSession is recognized on /admin.
func buildAdminEnv(t *testing.T, user accounts.User, authz PermissionChecker) (http.Handler, *scs.SessionManager) {
	t.Helper()
	sess := session.NewManager(false)
	users := fakeUsers{users: map[uuid.UUID]accounts.User{user.ID: user}}
	mw := NewAuthMiddleware(sess, users, authz)
	h := accounts.NewHandler(stubAuthService{}, mw, security.Token, accounts.NewValidator())
	r := Router(Deps{
		Config:   config.Config{AppEnv: "test", BaseURL: "https://site.test"},
		Health:   health.NewHandler(health.NewService(nil)),
		Session:  sess,
		Auth:     h,
		AuthMW:   mw,
		CSRFFunc: security.Token,
		Authz:    authz,
		Roles:    fakeRoles{role: accounts.Role{Key: "administrator", Label: "Administrator"}},
	})
	return r, sess
}

// mintSession logs the user in through the shared scs manager and returns the
// resulting session cookie.
func mintSession(t *testing.T, sess *scs.SessionManager, mw *AuthMiddleware, user accounts.User) *http.Cookie {
	t.Helper()
	login := session.Middleware(sess)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := mw.Login(r.Context(), user.ID, user.PasswordChangedAt); err != nil {
			t.Fatalf("login: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	login.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/__login", nil))
	for _, c := range rec.Result().Cookies() {
		if c.Name == "agentic_cms_session" {
			return c
		}
	}
	t.Fatal("no session cookie minted")
	return nil
}

func TestAdminDashboardRendersForAuthenticatedUser(t *testing.T) {
	id := uuid.New()
	user := accounts.User{ID: id, Email: "ada@example.com", Name: "Ada Lovelace", PasswordChangedAt: time.Now()}
	authz := allowAllAuthz{}

	sess := session.NewManager(false)
	users := fakeUsers{users: map[uuid.UUID]accounts.User{id: user}}
	mw := NewAuthMiddleware(sess, users, authz)
	h := accounts.NewHandler(stubAuthService{}, mw, security.Token, accounts.NewValidator())
	r := Router(Deps{
		Config:   config.Config{AppEnv: "test", BaseURL: "https://site.test"},
		Health:   health.NewHandler(health.NewService(nil)),
		Session:  sess,
		Auth:     h,
		AuthMW:   mw,
		CSRFFunc: security.Token,
		Authz:    authz,
		Roles:    fakeRoles{role: accounts.Role{Key: "administrator", Label: "Administrator"}},
	})

	cookie := mintSession(t, sess, mw, user)
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("/admin code = %d, want 200\nbody: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-testid="admin-dashboard"`,
		`data-testid="admin-sidebar"`,
		"nav-group-Content",
		"nav-group-Settings",
		"Administrator",
		`data-testid="theme-toggle"`,
		`data-testid="menu-logout"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("admin dashboard missing %q", want)
		}
	}
}

func TestAdminRequiresAuthInShell(t *testing.T) {
	r, _ := buildAdminEnv(t, accounts.User{ID: uuid.New()}, allowAllAuthz{})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("unauthenticated /admin = %d, want 303 redirect", rec.Code)
	}
	if rec.Header().Get("Location") != "/login" {
		t.Errorf("redirect = %q, want /login", rec.Header().Get("Location"))
	}
}
