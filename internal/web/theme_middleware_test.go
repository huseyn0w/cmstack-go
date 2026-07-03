package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/health"
	"github.com/huseyn0w/cmstack-go/internal/platform/config"
	"github.com/huseyn0w/cmstack-go/internal/platform/i18n"
	"github.com/huseyn0w/cmstack-go/internal/platform/security"
	"github.com/huseyn0w/cmstack-go/internal/platform/session"
)

// fakeThemeReader is a static ThemeReader returning a fixed stored theme id.
type fakeThemeReader struct{ id string }

func (f fakeThemeReader) ActiveTheme(context.Context) string { return f.id }

// buildThemedPublicEnv wires the home route through the public group with a theme
// resolver reading a fixed stored id, so a GET / renders the base layout with the
// resolved theme class on <html>.
func buildThemedPublicEnv(t *testing.T, storedTheme string) http.Handler {
	t.Helper()
	sess := session.NewManager(false)
	cat, _ := i18n.LoadCatalog()
	return Router(Deps{
		Config:   config.Config{AppEnv: "test", BaseURL: "https://site.test"},
		Health:   health.NewHandler(health.NewService(nil)),
		Session:  sess,
		AuthMW:   NewAuthMiddleware(sess, fakeUsers{users: map[uuid.UUID]accounts.User{}}, allowAllAuthz{}),
		CSRFFunc: security.Token,
		SiteName: "CMStack",
		Locale:   NewLocaleResolver(cat),
		Theme:    NewThemeResolver(fakeThemeReader{id: storedTheme}),
	})
}

// htmlTag extracts the opening <html ...> tag from a rendered document.
func htmlTag(body string) string {
	i := strings.Index(body, "<html")
	if i < 0 {
		return ""
	}
	j := strings.Index(body[i:], ">")
	if j < 0 {
		return ""
	}
	return body[i : i+j+1]
}

// TestPublicPage_AppliesActiveTheme asserts a stored "sepia" theme surfaces as
// class="... theme-sepia" on the public <html>.
func TestPublicPage_AppliesActiveTheme(t *testing.T) {
	r := buildThemedPublicEnv(t, "sepia")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/ = %d\n%s", rec.Code, rec.Body.String())
	}
	tag := htmlTag(rec.Body.String())
	if !strings.Contains(tag, "theme-sepia") {
		t.Fatalf("public <html> missing theme-sepia class: %q", tag)
	}
	if !strings.Contains(tag, "h-full") {
		t.Fatalf("public <html> lost base h-full class: %q", tag)
	}
}

// TestPublicPage_InvalidThemeFallsBackToDefault asserts a stale/unknown stored
// theme resolves to default → NO theme- class (the ThemeResolver validates via
// the registry).
func TestPublicPage_InvalidThemeFallsBackToDefault(t *testing.T) {
	r := buildThemedPublicEnv(t, "does-not-exist")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/ = %d\n%s", rec.Code, rec.Body.String())
	}
	tag := htmlTag(rec.Body.String())
	if strings.Contains(tag, "theme-") {
		t.Fatalf("invalid stored theme should fall back to default (no theme- class): %q", tag)
	}
}

// TestPublicPage_DefaultThemeHasNoClass asserts the explicit "default" theme
// emits no theme- class (default IS :root).
func TestPublicPage_DefaultThemeHasNoClass(t *testing.T) {
	r := buildThemedPublicEnv(t, "default")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	tag := htmlTag(rec.Body.String())
	if strings.Contains(tag, "theme-") {
		t.Fatalf("default theme should emit no theme- class: %q", tag)
	}
}

// TestAdminPage_ThemeIsolation asserts admin pages carry NO theme- class even
// when a public theme is active — the admin routes never run the theme
// middleware, so ActiveThemeFromContext is "" there and the base palette applies.
func TestAdminPage_ThemeIsolation(t *testing.T) {
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
		// A public theme is active site-wide, but admin must stay isolated.
		Theme: NewThemeResolver(fakeThemeReader{id: "sepia"}),
	})

	cookie := mintSession(t, sess, mw, user)
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/admin = %d\n%s", rec.Code, rec.Body.String())
	}
	tag := htmlTag(rec.Body.String())
	if strings.Contains(tag, "theme-") {
		t.Fatalf("admin <html> must have NO theme- class (isolation): %q", tag)
	}
}
