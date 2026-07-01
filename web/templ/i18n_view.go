package templ

import (
	"context"

	"github.com/huseyn0w/cmstack-go/internal/platform/i18n"
)

// Alternate re-exports i18n.Alternate so templ components can reference it
// without importing the i18n package directly.
type Alternate = i18n.Alternate

// LocaleViewData is the i18n payload the layout reads from the request context
// to render the <html lang> attribute, the localized UI strings, and the header
// language switcher. It is assembled by LocaleView so templates stay free of
// context-key plumbing.
type LocaleViewData struct {
	// Active is the resolved locale for the request (drives <html lang>).
	Active i18n.Locale
	// T translates a UI-string key for the active locale (with {name} interp).
	T i18n.Translator
	// Switch lists every locale with the URL to the CURRENT page in it, in
	// display order (default first). It drives the header language switcher and
	// carries the hreflang seam for M8.
	Switch []i18n.Alternate
}

// localeViewSource is satisfied by the web package's context accessors. It is an
// interface (rather than a direct import) so the templ package does not import
// the web package, avoiding an import cycle: web imports templ for rendering.
// The web package registers its resolver via SetLocaleViewSource at init.
type localeViewSource interface {
	Locale(ctx context.Context) i18n.Locale
	Translator(ctx context.Context) i18n.Translator
	Alternates(ctx context.Context) []i18n.Alternate
}

// localeSource is the registered accessor; nil until the web package wires it.
var localeSource localeViewSource

// SetLocaleViewSource registers the context accessors used by LocaleView. The
// web package calls this from an init function so the layout can read the active
// locale/translator/alternates without importing web (which would cycle).
func SetLocaleViewSource(s localeViewSource) { localeSource = s }

// LocaleView assembles the i18n view data from the request context. When the
// locale middleware has not run (admin routes, reduced-Deps tests) it falls back
// to the default locale with a key-echoing translator and switcher links that
// point at the root, so the layout always renders.
func LocaleView(ctx context.Context) LocaleViewData {
	if localeSource == nil {
		return LocaleViewData{
			Active: i18n.Default(),
			T:      i18n.NewTranslator(nil, i18n.Default()),
			Switch: i18n.Alternates("/", ""),
		}
	}
	return LocaleViewData{
		Active: localeSource.Locale(ctx),
		T:      localeSource.Translator(ctx),
		Switch: localeSource.Alternates(ctx),
	}
}
