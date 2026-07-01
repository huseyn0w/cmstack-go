package web

import (
	"context"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/platform/i18n"
	webtempl "github.com/huseyn0w/cmstack-go/web/templ"
)

// ctxKey is an unexported context key type to avoid collisions.
type ctxKey int

const (
	userCtxKey ctxKey = iota
	localeCtxKey
)

// withUser returns a copy of ctx carrying the authenticated user.
func withUser(ctx context.Context, u accounts.User) context.Context {
	return context.WithValue(ctx, userCtxKey, u)
}

// UserFromContext returns the authenticated user and whether one is present.
func UserFromContext(ctx context.Context) (accounts.User, bool) {
	u, ok := ctx.Value(userCtxKey).(accounts.User)
	return u, ok
}

// localeState is the per-request i18n context payload: the active locale plus
// the unprefixed (locale-stripped) path and raw query, so the language switcher
// and hreflang alternates can rebuild the current page in any locale.
type localeState struct {
	locale     i18n.Locale
	rest       string // locale-stripped, rooted path (e.g. "/blog")
	rawQuery   string
	translator i18n.Translator
}

// withLocale returns a copy of ctx carrying the resolved locale state.
func withLocale(ctx context.Context, s localeState) context.Context {
	return context.WithValue(ctx, localeCtxKey, s)
}

// LocaleFromContext returns the active locale for the request, defaulting to the
// i18n default when the locale middleware has not run (e.g. admin routes).
func LocaleFromContext(ctx context.Context) i18n.Locale {
	if s, ok := ctx.Value(localeCtxKey).(localeState); ok {
		return s.locale
	}
	return i18n.Default()
}

// localeStateFromContext returns the full locale state (locale + stripped path +
// query). The second return is false when the middleware did not run.
func localeStateFromContext(ctx context.Context) (localeState, bool) {
	s, ok := ctx.Value(localeCtxKey).(localeState)
	return s, ok
}

// AlternatesFromContext returns the per-locale alternate URLs for the current
// page (the language-switcher targets + M8 hreflang data). It uses the stripped
// path + query captured by the middleware; when the middleware has not run it
// yields the root alternates so the switcher still renders.
func AlternatesFromContext(ctx context.Context) []i18n.Alternate {
	if s, ok := localeStateFromContext(ctx); ok {
		return i18n.Alternates(s.rest, s.rawQuery)
	}
	return i18n.Alternates("/", "")
}

// localeViewSource adapts the web package's context accessors to the templ
// package's localeViewSource interface, so the public layout can read the active
// locale/translator/alternates without importing web (which would cycle).
type localeViewSource struct{}

func (localeViewSource) Locale(ctx context.Context) i18n.Locale { return LocaleFromContext(ctx) }
func (localeViewSource) Translator(ctx context.Context) i18n.Translator {
	return TranslatorFromContext(ctx)
}

func (localeViewSource) Alternates(ctx context.Context) []i18n.Alternate {
	return AlternatesFromContext(ctx)
}

func init() {
	webtempl.SetLocaleViewSource(localeViewSource{})
}
