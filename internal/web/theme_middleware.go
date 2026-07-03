package web

import (
	"context"
	"net/http"

	"github.com/huseyn0w/cmstack-go/internal/theme"
)

// ThemeReader is the narrow settings dependency the theme middleware needs: the
// current active-theme id. *settings.Service satisfies it. Declaring it here
// keeps web decoupled from the settings package and trivially fakeable in tests.
type ThemeReader interface {
	// ActiveTheme returns the stored active theme id, or "" when unset/errored.
	ActiveTheme(ctx context.Context) string
}

// ThemeResolver builds the public-theme middleware (M9-1). It reads the active
// theme id from the settings store on each public request, validates it against
// the in-code theme registry, and stores the resolved id in the request context
// so the layout can re-scope the color tokens via a `.theme-<id>` <html> class.
type ThemeResolver struct {
	reader ThemeReader
}

// NewThemeResolver constructs a resolver over the settings reader. A nil reader
// is tolerated: the middleware then always resolves to the default theme, so a
// reduced-Deps wiring renders on the base palette without panicking.
func NewThemeResolver(reader ThemeReader) *ThemeResolver {
	return &ThemeResolver{reader: reader}
}

// Middleware resolves the active public theme and stores its id in the request
// context. It runs on the PUBLIC route group only; admin routes never run it, so
// ActiveThemeFromContext returns "" there and the base palette applies (theme
// isolation). The stored id is always a REGISTERED theme (theme.Resolve maps an
// empty/unknown/stale stored id to the default), so the layout can trust it.
func (tr *ThemeResolver) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var stored string
		if tr.reader != nil {
			stored = tr.reader.ActiveTheme(r.Context())
		}
		resolved := theme.Resolve(stored)
		ctx := withActiveTheme(r.Context(), resolved.ID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
