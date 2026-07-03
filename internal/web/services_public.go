package web

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/huseyn0w/cmstack-go/internal/content/services"
	"github.com/huseyn0w/cmstack-go/internal/platform/i18n"
	"github.com/huseyn0w/cmstack-go/internal/platform/render"
	webtempl "github.com/huseyn0w/cmstack-go/web/templ"
)

// servicePublicPageSize is the public services index page size.
const servicePublicPageSize = 12

// ServicePublicService is the subset of *services.Manager the public handler
// calls.
type ServicePublicService interface {
	PublicBySlug(ctx context.Context, slug string) (services.Service, error)
	// PublicBySlugLocale overlays the active-locale translation with base (en)
	// fallback (M7b-2); the default locale resolves to the base row.
	PublicBySlugLocale(ctx context.Context, slug string, locale i18n.Locale) (services.Service, error)
	PublicList(ctx context.Context, limit, offset int) ([]services.Service, int, error)
}

// ServicePublicHandler is the thin HTTP boundary for the public services area.
type ServicePublicHandler struct {
	svc      ServicePublicService
	siteName string
	baseURL  string
	site     SiteConfig
}

// WithSite attaches the resolved site-identity + SEO config (M8). Returns the
// receiver.
func (h *ServicePublicHandler) WithSite(s SiteConfig) *ServicePublicHandler {
	h.site = s
	return h
}

// NewServicePublicHandler constructs the public services handler.
func NewServicePublicHandler(svc ServicePublicService, siteName, baseURL string) *ServicePublicHandler {
	if siteName == "" {
		siteName = "CMStack"
	}
	return &ServicePublicHandler{svc: svc, siteName: siteName, baseURL: baseURL}
}

// Index renders the paginated published-services grid.
func (h *ServicePublicHandler) Index(w http.ResponseWriter, r *http.Request) {
	page := pageParam(r)
	items, total, err := h.svc.PublicList(r.Context(), servicePublicPageSize, (page-1)*servicePublicPageSize)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	cards := make([]webtempl.PublicServiceCard, 0, len(items))
	for _, s := range items {
		cards = append(cards, webtempl.PublicServiceCard{
			Title:   s.Title,
			URL:     "/services/" + s.Slug,
			Summary: s.Summary,
			Price:   s.Price,
		})
	}
	locale := LocaleFromContext(r.Context())
	view := webtempl.PublicServiceIndexView{
		SiteName: h.siteName,
		HomeURL:  "/",
		Cards:    cards,
		Pager:    pager(page, servicePublicPageSize, total, "/services", ""),
	}
	view.SEO = h.site.BuildSEO(r, SEOInput{
		Title:         "Services",
		Description:   "Services offered by " + h.siteName,
		CanonicalPath: i18n.LocalizePath(locale, "/services"),
		OGType:        "website",
	})
	if err := render.Component(r.Context(), w, http.StatusOK, webtempl.PublicServiceIndex(view)); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// Show renders a single published service by slug.
func (h *ServicePublicHandler) Show(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	locale := LocaleFromContext(r.Context())
	svc, err := h.svc.PublicBySlugLocale(r.Context(), slug, locale)
	if errors.Is(err, services.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}

	faqs := make([]webtempl.PublicServiceFAQ, 0, len(svc.FAQs))
	for _, f := range svc.FAQs {
		faqs = append(faqs, webtempl.PublicServiceFAQ{Question: f.Question, AnswerHTML: f.Answer})
	}
	publishedAt := svc.UpdatedAt
	if svc.PublishedAt != nil {
		publishedAt = *svc.PublishedAt
	}
	canonical := svc.CanonicalURL
	if canonical == "" {
		canonical = h.baseURL + i18n.LocalizePath(locale, "/services/"+svc.Slug)
	}
	metaTitle := svc.MetaTitle
	if metaTitle == "" {
		metaTitle = svc.Title
	}
	metaDesc := svc.MetaDescription
	if metaDesc == "" {
		metaDesc = svc.Summary
	}
	view := webtempl.PublicServiceView{
		SiteName:     h.siteName,
		HomeURL:      i18n.LocalizePath(locale, "/"),
		Title:        svc.Title,
		Slug:         svc.Slug,
		Summary:      svc.Summary,
		BodyHTML:     svc.Body,
		Price:        svc.Price,
		AreaServed:   svc.AreaServed,
		FAQs:         faqs,
		PublishedAt:  publishedAt,
		ReadingTime:  svc.ReadingTime,
		CanonicalURL: canonical,
	}
	view.SEO = h.site.BuildSEO(r, SEOInput{
		Title:        metaTitle,
		Description:  metaDesc,
		CanonicalURL: canonical,
		NoIndex:      svc.NoIndex,
		OGType:       "website",
	})
	// TODO(M8): emit Service + FAQPage JSON-LD from svc.JSONLD(view.CanonicalURL).
	if err := render.Component(r.Context(), w, http.StatusOK, webtempl.PublicServiceDetail(view)); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}
