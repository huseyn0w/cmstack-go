package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/huseyn0w/cmstack-go/internal/platform/i18n"
)

// captureHandler records the request path/context the middleware forwards.
type captureHandler struct {
	gotPath    string
	gotLocale  i18n.Locale
	gotTransOK bool
}

func (c *captureHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.gotPath = r.URL.Path
	c.gotLocale = LocaleFromContext(r.Context())
	tr := TranslatorFromContext(r.Context())
	c.gotTransOK = tr.Locale() == c.gotLocale
	w.WriteHeader(http.StatusOK)
}

func newResolver(t *testing.T) *LocaleResolver {
	t.Helper()
	cat, err := i18n.LoadCatalog()
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	return NewLocaleResolver(cat)
}

func TestLocaleMiddleware_AsNeededRouting(t *testing.T) {
	cases := []struct {
		name       string
		url        string
		wantLocale i18n.Locale
		wantPath   string // downstream (stripped) path
	}{
		{"en unprefixed root", "/", i18n.LocaleEN, "/"},
		{"en unprefixed blog", "/blog", i18n.LocaleEN, "/blog"},
		{"de prefixed blog", "/de/blog", i18n.LocaleDE, "/blog"},
		{"ru prefixed services", "/ru/services", i18n.LocaleRU, "/services"},
		{"de bare prefix", "/de", i18n.LocaleDE, "/"},
		{"unknown prefix stays path", "/fr/blog", i18n.LocaleEN, "/fr/blog"},
		{"admin stays en", "/admin", i18n.LocaleEN, "/admin"},
	}
	lr := newResolver(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cap := &captureHandler{}
			h := lr.Middleware(cap)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.url, nil))

			if cap.gotLocale != c.wantLocale {
				t.Errorf("locale = %q, want %q", cap.gotLocale, c.wantLocale)
			}
			if cap.gotPath != c.wantPath {
				t.Errorf("downstream path = %q, want %q", cap.gotPath, c.wantPath)
			}
			if !cap.gotTransOK {
				t.Error("translator locale did not match context locale")
			}
		})
	}
}

func TestLocaleMiddleware_AdminCookieLocale(t *testing.T) {
	cases := []struct {
		name       string
		url        string
		cookie     string // "" = no admin_locale cookie
		wantLocale i18n.Locale
	}{
		{"admin no cookie -> en", "/admin", "", i18n.LocaleEN},
		{"admin de cookie", "/admin", "de", i18n.LocaleDE},
		{"admin ru cookie", "/admin", "ru", i18n.LocaleRU},
		{"admin subpath de cookie", "/admin/posts", "de", i18n.LocaleDE},
		{"account de cookie", "/account", "de", i18n.LocaleDE},
		{"account subpath ru cookie", "/account/password", "ru", i18n.LocaleRU},
		{"invalid cookie falls back to en", "/admin", "fr", i18n.LocaleEN},
		{"empty cookie falls back to en", "/admin", "", i18n.LocaleEN},
		// The cookie must NOT bleed into public, unprefixed pages: they stay en.
		{"public page ignores admin cookie", "/blog", "de", i18n.LocaleEN},
		{"public root ignores admin cookie", "/", "ru", i18n.LocaleEN},
		// An explicit URL prefix still wins over the admin cookie mechanism
		// (there is no /de/admin route, but the prefix path must resolve de and
		// not consult the cookie); admin cookie only applies to unprefixed admin.
		{"de-prefixed public wins over cookie", "/de/blog", "ru", i18n.LocaleDE},
	}
	lr := newResolver(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cap := &captureHandler{}
			h := lr.Middleware(cap)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, c.url, nil)
			if c.cookie != "" {
				req.AddCookie(&http.Cookie{Name: "admin_locale", Value: c.cookie})
			}
			h.ServeHTTP(rec, req)

			if cap.gotLocale != c.wantLocale {
				t.Errorf("locale = %q, want %q", cap.gotLocale, c.wantLocale)
			}
			if !cap.gotTransOK {
				t.Error("translator locale did not match context locale")
			}
		})
	}
}

func TestLocaleMiddleware_ContextPropagatesQuery(t *testing.T) {
	lr := newResolver(t)
	var alts []i18n.Alternate
	h := lr.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		alts = AlternatesFromContext(r.Context())
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/de/blog?tag=x", nil))

	// Alternates rebuild the CURRENT page (path + query) in every locale.
	want := map[i18n.Locale]string{
		i18n.LocaleEN: "/blog?tag=x",
		i18n.LocaleDE: "/de/blog?tag=x",
		i18n.LocaleRU: "/ru/blog?tag=x",
	}
	if len(alts) != 3 {
		t.Fatalf("alternates len = %d, want 3", len(alts))
	}
	for _, a := range alts {
		if a.URL != want[a.Locale] {
			t.Errorf("alt %q URL = %q, want %q", a.Locale, a.URL, want[a.Locale])
		}
	}
}

func TestLocaleFromContext_DefaultsWhenAbsent(t *testing.T) {
	if got := LocaleFromContext(httptest.NewRequest(http.MethodGet, "/", nil).Context()); got != i18n.Default() {
		t.Errorf("absent locale = %q, want default", got)
	}
}
