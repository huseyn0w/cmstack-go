package web

import (
	"context"

	"github.com/huseyn0w/cmstack-go/internal/content/menus"
	"github.com/huseyn0w/cmstack-go/internal/platform/i18n"
	webtempl "github.com/huseyn0w/cmstack-go/web/templ"
)

// MenuPublicService is the narrow read the public layout needs to render managed
// menus: resolve the menu assigned to a location for a locale (labels overlaid,
// URLs localized). *menus.Service satisfies it.
type MenuPublicService interface {
	ResolveForLocation(ctx context.Context, location string, locale i18n.Locale) ([]menus.ResolvedItem, error)
}

// menuPublicSource adapts a MenuPublicService to the templ menu source: it reads
// the active locale from the request context and maps the resolved domain items
// onto the templ MenuLink view shape. A resolve error yields an empty menu so
// the layout degrades gracefully (the header/footer simply omit the nav).
type menuPublicSource struct{ svc MenuPublicService }

// MenuForLocation resolves the location's menu for the request locale and maps
// it to templ MenuLinks. Returns nil on error or when no menu is assigned.
func (s menuPublicSource) MenuForLocation(ctx context.Context, location string) []webtempl.MenuLink {
	items, err := s.svc.ResolveForLocation(ctx, location, LocaleFromContext(ctx))
	if err != nil {
		return nil
	}
	return toMenuLinks(items)
}

// toMenuLinks recursively maps resolved menu items onto the templ view shape.
func toMenuLinks(items []menus.ResolvedItem) []webtempl.MenuLink {
	if len(items) == 0 {
		return nil
	}
	out := make([]webtempl.MenuLink, 0, len(items))
	for _, it := range items {
		out = append(out, webtempl.MenuLink{
			Label:    it.Label,
			URL:      it.URL,
			Children: toMenuLinks(it.Children),
		})
	}
	return out
}
