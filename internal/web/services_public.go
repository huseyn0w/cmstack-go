package web

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/huseyn0w/cmstack-go/internal/content/services"
	"github.com/huseyn0w/cmstack-go/internal/platform/render"
	webtempl "github.com/huseyn0w/cmstack-go/web/templ"
)

// servicePublicPageSize is the public services index page size.
const servicePublicPageSize = 12

// ServicePublicService is the subset of *services.Manager the public handler
// calls.
type ServicePublicService interface {
	PublicBySlug(ctx context.Context, slug string) (services.Service, error)
	PublicList(ctx context.Context, limit, offset int) ([]services.Service, int, error)
}

// ServicePublicHandler is the thin HTTP boundary for the public services area.
type ServicePublicHandler struct {
	svc      ServicePublicService
	siteName string
	baseURL  string
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
	view := webtempl.PublicServiceIndexView{
		SiteName: h.siteName,
		HomeURL:  "/",
		Cards:    cards,
		Pager:    pager(page, servicePublicPageSize, total, "/services", ""),
	}
	if err := render.Component(r.Context(), w, http.StatusOK, webtempl.PublicServiceIndex(view)); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// Show renders a single published service by slug.
func (h *ServicePublicHandler) Show(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	svc, err := h.svc.PublicBySlug(r.Context(), slug)
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
	view := webtempl.PublicServiceView{
		SiteName:     h.siteName,
		HomeURL:      "/",
		Title:        svc.Title,
		Slug:         svc.Slug,
		Summary:      svc.Summary,
		BodyHTML:     svc.Body,
		Price:        svc.Price,
		AreaServed:   svc.AreaServed,
		FAQs:         faqs,
		PublishedAt:  publishedAt,
		ReadingTime:  svc.ReadingTime,
		CanonicalURL: h.baseURL + "/services/" + svc.Slug,
	}
	// TODO(M8): emit Service + FAQPage JSON-LD from svc.JSONLD(view.CanonicalURL).
	if err := render.Component(r.Context(), w, http.StatusOK, webtempl.PublicServiceDetail(view)); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}
