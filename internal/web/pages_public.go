package web

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/huseyn0w/cmstack-go/internal/content/pages"
	"github.com/huseyn0w/cmstack-go/internal/platform/i18n"
	"github.com/huseyn0w/cmstack-go/internal/platform/render"
	webtempl "github.com/huseyn0w/cmstack-go/web/templ"
)

// PagePublicService is the subset of *pages.Service the public handler calls.
type PagePublicService interface {
	PublicBySlug(ctx context.Context, slug string) (pages.Page, error)
	// PublicBySlugLocale overlays the active-locale translation with base (en)
	// fallback (M7b-2); the default locale resolves to the base row.
	PublicBySlugLocale(ctx context.Context, slug string, locale i18n.Locale) (pages.Page, error)
	Ancestors(ctx context.Context, p pages.Page) ([]pages.Page, error)
}

// PagePublicHandler is the thin HTTP boundary for public pages. It renders a
// published page at /p/{slug} using the page's selected template, with
// hierarchy-reflecting breadcrumbs.
type PagePublicHandler struct {
	svc      PagePublicService
	siteName string
	baseURL  string
}

// NewPagePublicHandler constructs the public pages handler.
func NewPagePublicHandler(svc PagePublicService, siteName, baseURL string) *PagePublicHandler {
	if siteName == "" {
		siteName = "CMStack"
	}
	return &PagePublicHandler{svc: svc, siteName: siteName, baseURL: baseURL}
}

// Show renders a single published page by slug.
func (h *PagePublicHandler) Show(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	locale := LocaleFromContext(r.Context())
	p, err := h.svc.PublicBySlugLocale(r.Context(), slug, locale)
	if errors.Is(err, pages.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}

	crumbs := h.breadcrumbs(r.Context(), p, locale)
	publishedAt := p.UpdatedAt
	if p.PublishedAt != nil {
		publishedAt = *p.PublishedAt
	}
	view := webtempl.PublicPageView{
		SiteName:     h.siteName,
		HomeURL:      i18n.LocalizePath(locale, "/"),
		Title:        p.Title,
		Slug:         p.Slug,
		BodyHTML:     p.Body,
		Template:     p.Template,
		Breadcrumbs:  crumbs,
		PublishedAt:  publishedAt,
		ReadingTime:  p.ReadingTime,
		CanonicalURL: h.baseURL + i18n.LocalizePath(locale, "/p/"+p.Slug),
	}
	if err := render.Component(r.Context(), w, http.StatusOK, webtempl.PublicPage(view)); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

func (h *PagePublicHandler) breadcrumbs(ctx context.Context, p pages.Page, locale i18n.Locale) []webtempl.PageBreadcrumb {
	ancestors, err := h.svc.Ancestors(ctx, p)
	if err != nil {
		return nil
	}
	crumbs := make([]webtempl.PageBreadcrumb, 0, len(ancestors))
	for _, a := range ancestors {
		// Only published ancestors are linkable on the public site.
		if !a.Published() {
			continue
		}
		crumbs = append(crumbs, webtempl.PageBreadcrumb{Title: a.Title, URL: i18n.LocalizePath(locale, "/p/"+a.Slug)})
	}
	return crumbs
}
