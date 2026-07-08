package web

import (
	"context"
	"net/http"
	"strings"

	"github.com/huseyn0w/cmstack-go/internal/platform/i18n"
)

// adminLocaleCookie is the cookie that carries the operator-chosen UI language
// for the admin surfaces (dashboard, /account, ...). Public pages ignore it —
// their locale comes only from the URL prefix ("as-needed" routing).
const adminLocaleCookie = "admin_locale"

// isAdminSurface reports whether the (locale-stripped) path belongs to an admin
// surface, i.e. one that is served UNPREFIXED but should still honor the
// operator's cookie-selected language. These are the authenticated back-office
// routes: the admin area itself and the self-service account editor.
func isAdminSurface(path string) bool {
	return path == "/admin" || strings.HasPrefix(path, "/admin/") ||
		path == "/account" || strings.HasPrefix(path, "/account/")
}

// LocaleResolver builds the public-locale middleware. It carries the shared
// message catalog so the middleware can attach a locale-bound Translator to the
// request context alongside the resolved locale.
type LocaleResolver struct {
	catalog *i18n.Catalog
}

// NewLocaleResolver constructs a resolver over cat. A nil catalog is tolerated:
// the attached translator then echoes keys, so a reduced-Deps render never
// panics.
func NewLocaleResolver(cat *i18n.Catalog) *LocaleResolver {
	return &LocaleResolver{catalog: cat}
}

// Middleware resolves the active UI locale from the URL prefix using "as-needed"
// routing and makes it available to downstream handlers and templ:
//
//   - /de/... selects de, /ru/... selects ru; everything else is the default
//     (en) on its unchanged, unprefixed path.
//   - The locale prefix is STRIPPED from the request URL (r.URL.Path) so existing
//     unprefixed chi routes ("/blog", "/services") match unchanged — the router
//     never learns about locales.
//   - The active locale, the stripped path/query, and a locale-bound Translator
//     are stored in the request context (LocaleFromContext / TranslatorFromContext)
//     so the layout can set <html lang>, render UI strings, and the switcher can
//     rebuild the current page in any locale.
//
// Unknown prefixes (e.g. /fr) are deliberately NOT treated as locales: the path
// is left intact and resolved as the default locale, so it flows to the normal
// router and 404s there like any other unknown path. This keeps a typo'd or
// hostile prefix from silently masquerading as a supported locale, and avoids a
// second not-found surface.
//
// It wraps the whole chi router as an OUTER handler (see Router in router.go for
// why chi's own middleware chain is too late for a prefix strip). Admin routes
// are never prefixed, so they resolve to the default (en) from the URL alone;
// for those unprefixed admin surfaces the middleware additionally honors the
// operator-selected `admin_locale` cookie (see isAdminSurface below), so the
// back-office UI can be switched to de/ru at runtime without prefixing its URLs.
func (lr *LocaleResolver) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		loc, rest := i18n.SplitLocalePrefix(r.URL.Path)

		// Rewrite the request so downstream routing sees the unprefixed path.
		r.URL.Path = rest
		if r.URL.RawPath != "" {
			// RawPath, when set, must stay consistent with Path; drop it so the
			// stdlib recomputes escaping from the (now unprefixed) Path.
			r.URL.RawPath = ""
		}

		// Admin surfaces are served UNPREFIXED (SplitLocalePrefix left the path
		// intact and resolved en). Unlike the public site, the admin honors a
		// cookie-selected language so an operator can switch the back-office UI at
		// runtime. We only override when the path is an admin surface AND no URL
		// prefix was present (loc is default) — a stray cookie must never bleed
		// into public, unprefixed pages (those stay en).
		if loc == i18n.Default() && isAdminSurface(rest) {
			if c, err := r.Cookie(adminLocaleCookie); err == nil {
				if parsed, ok := i18n.Parse(c.Value); ok {
					loc = parsed
				}
			}
		}

		ctx := withLocale(r.Context(), localeState{
			locale:     loc,
			rest:       rest,
			rawQuery:   r.URL.RawQuery,
			translator: i18n.NewTranslator(lr.catalog, loc),
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// TranslatorFromContext returns the locale-bound Translator for the request. It
// falls back to a default-locale, nil-catalog translator (which echoes keys)
// when the locale middleware has not run — so admin/reduced-Deps renders that
// happen to call it never panic.
func TranslatorFromContext(ctx context.Context) i18n.Translator {
	if s, ok := localeStateFromContext(ctx); ok {
		return s.translator
	}
	return i18n.NewTranslator(nil, i18n.Default())
}
