package templ

import "context"

// MenuLink is one resolved navigation entry for public rendering: a display
// label, an already-localized href, and optional nested children (one level of
// dropdown). It is the view shape the web layer maps menus.ResolvedItem onto so
// the templ package stays decoupled from the menus domain.
type MenuLink struct {
	Label    string
	URL      string
	Children []MenuLink
}

// menuSource resolves the managed menu assigned to a named location (e.g.
// "header", "footer") for the current request, with labels/URLs already
// localized. The web package registers the real resolver via SetMenuSource; it
// is an interface so the templ package does not import the web/menus packages
// (avoiding an import cycle).
type menuSource interface {
	MenuForLocation(ctx context.Context, location string) []MenuLink
}

// menuLinkSource is the registered accessor; nil until the web package wires it.
var menuLinkSource menuSource

// SetMenuSource registers the public-menu resolver used by MenuForLocation. The
// web package calls this from Router when the menu service is wired; when unset
// every location resolves to an empty menu so the layout renders unchanged.
func SetMenuSource(s menuSource) { menuLinkSource = s }

// MenuForLocation returns the resolved links for a location, or nil when no menu
// source is registered or the location has no assigned menu. Safe to call from
// templates.
func MenuForLocation(ctx context.Context, location string) []MenuLink {
	if menuLinkSource == nil {
		return nil
	}
	return menuLinkSource.MenuForLocation(ctx, location)
}
