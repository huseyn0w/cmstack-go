package templ

import "context"

// themeSource is satisfied by the web package's context accessor. It is an
// interface (rather than a direct import) so the templ package does not import
// the web package, avoiding an import cycle. The web package registers its
// accessor via SetThemeSource at init, mirroring SetLocaleViewSource.
type themeSource interface {
	ActiveTheme(ctx context.Context) string
}

// themeSrc is the registered accessor; nil until the web package wires it.
var themeSrc themeSource

// SetThemeSource registers the context accessor used by ActiveTheme. The web
// package calls this from an init function so the layout can read the resolved
// active theme id without importing web (which would cycle).
func SetThemeSource(s themeSource) { themeSrc = s }

// ActiveTheme returns the resolved active-theme id for the request, or "" when
// no theme source is registered or none was resolved (admin routes, reduced-Deps
// renders). The layout maps a non-default id to the `.theme-<id>` <html> class.
func ActiveTheme(ctx context.Context) string {
	if themeSrc == nil {
		return ""
	}
	return themeSrc.ActiveTheme(ctx)
}

// htmlClass builds the <html> class for the base layout: always "h-full", plus
// "theme-<id>" when a non-default public theme is active. An empty or "default"
// id yields just "h-full" so admin pages (which never set the context value) and
// the default theme both render on the base :root/.dark palette.
func htmlClass(themeID string) string {
	if themeID == "" || themeID == "default" {
		return "h-full"
	}
	return "h-full theme-" + themeID
}
