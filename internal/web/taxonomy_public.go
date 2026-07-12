package web

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/huseyn0w/agentic-cms-go/internal/content/categories"
	"github.com/huseyn0w/agentic-cms-go/internal/content/posts"
	"github.com/huseyn0w/agentic-cms-go/internal/content/tags"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/render"
	webtempl "github.com/huseyn0w/agentic-cms-go/web/templ"
)

// CategoryPublicService is the subset of *categories.Service the archive needs.
type CategoryPublicService interface {
	PublicBySlug(ctx context.Context, slug string) (categories.Category, error)
	PublishedPostIDs(ctx context.Context, categoryID uuid.UUID, limit, offset int) ([]uuid.UUID, int, error)
}

// TagPublicService is the subset of *tags.Service the archive needs.
type TagPublicService interface {
	PublicBySlug(ctx context.Context, slug string) (tags.Tag, error)
	PublishedPostIDs(ctx context.Context, tagID uuid.UUID, limit, offset int) ([]uuid.UUID, int, error)
}

// PostHydrator hydrates ordered, published post ids into posts (kept narrow so
// the archive depends only on what it uses). *posts.Service satisfies it.
type PostHydrator interface {
	PublishedByIDs(ctx context.Context, ids []uuid.UUID) ([]posts.Post, error)
}

// TaxonomyPublicHandler renders the public category + tag archives: a filtered,
// paginated list of published posts under a term, with breadcrumbs + empty
// state. It is read-only (no auth).
type TaxonomyPublicHandler struct {
	categories CategoryPublicService
	tags       TagPublicService
	posts      PostHydrator
	authors    AuthorNamer
	siteName   string
	site       SiteConfig
}

// WithSite attaches the resolved site-identity + SEO config (M8). Returns the
// receiver.
func (h *TaxonomyPublicHandler) WithSite(s SiteConfig) *TaxonomyPublicHandler {
	h.site = s
	return h
}

// NewTaxonomyPublicHandler constructs the public taxonomy archive handler.
func NewTaxonomyPublicHandler(cats CategoryPublicService, tagSvc TagPublicService, postSvc PostHydrator, authors AuthorNamer, siteName string) *TaxonomyPublicHandler {
	if siteName == "" {
		siteName = "Agentic CMS"
	}
	return &TaxonomyPublicHandler{categories: cats, tags: tagSvc, posts: postSvc, authors: authors, siteName: siteName}
}

// ShowCategory renders /categories/{slug}.
func (h *TaxonomyPublicHandler) ShowCategory(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	cat, err := h.categories.PublicBySlug(r.Context(), slug)
	if errors.Is(err, categories.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	page := pageParam(r)
	ids, total, err := h.categories.PublishedPostIDs(r.Context(), cat.ID, publicPageSize, (page-1)*publicPageSize)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	cards := h.cards(r.Context(), ids)
	view := webtempl.TaxonomyArchiveView{
		SiteName:    h.siteName,
		HomeURL:     "/",
		Kind:        "Category",
		Name:        cat.Name,
		Description: cat.Description,
		Cards:       cards,
		Pager:       pager(page, publicPageSize, total, "/categories/"+slug, ""),
	}
	view.SEO = h.site.BuildSEO(r, SEOInput{
		Title:         cat.Name,
		Description:   cat.Description,
		CanonicalPath: "/categories/" + slug,
		OGType:        "website",
	})
	archiveURL := h.site.absolute("/categories/" + slug)
	crumbs := []webtempl.Breadcrumb{
		{Name: h.siteName, URL: h.site.absolute("/")},
		{Name: "Categories", URL: h.site.absolute("/categories")},
		{Name: cat.Name, URL: archiveURL},
	}
	view.JSONLD = compact(
		webtempl.BreadcrumbListJSONLD(crumbs),
		webtempl.ItemListJSONLD(archiveURL, h.postCardItems(cards)),
	)
	h.render(w, r, webtempl.TaxonomyArchive(view))
}

// ShowTag renders /tags/{slug}.
func (h *TaxonomyPublicHandler) ShowTag(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	tag, err := h.tags.PublicBySlug(r.Context(), slug)
	if errors.Is(err, tags.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	page := pageParam(r)
	ids, total, err := h.tags.PublishedPostIDs(r.Context(), tag.ID, publicPageSize, (page-1)*publicPageSize)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	cards := h.cards(r.Context(), ids)
	view := webtempl.TaxonomyArchiveView{
		SiteName: h.siteName,
		HomeURL:  "/",
		Kind:     "Tag",
		Name:     tag.Name,
		Cards:    cards,
		Pager:    pager(page, publicPageSize, total, "/tags/"+slug, ""),
	}
	view.SEO = h.site.BuildSEO(r, SEOInput{
		Title:         tag.Name,
		Description:   "Posts tagged " + tag.Name,
		CanonicalPath: "/tags/" + slug,
		OGType:        "website",
	})
	archiveURL := h.site.absolute("/tags/" + slug)
	crumbs := []webtempl.Breadcrumb{
		{Name: h.siteName, URL: h.site.absolute("/")},
		{Name: "Tags", URL: h.site.absolute("/tags")},
		{Name: tag.Name, URL: archiveURL},
	}
	view.JSONLD = compact(
		webtempl.BreadcrumbListJSONLD(crumbs),
		webtempl.ItemListJSONLD(archiveURL, h.postCardItems(cards)),
	)
	h.render(w, r, webtempl.TaxonomyArchive(view))
}

// cards hydrates ordered post ids into public post cards.
func (h *TaxonomyPublicHandler) cards(ctx context.Context, ids []uuid.UUID) []webtempl.PublicPostCard {
	if len(ids) == 0 {
		return nil
	}
	items, err := h.posts.PublishedByIDs(ctx, ids)
	if err != nil {
		return nil
	}
	cards := make([]webtempl.PublicPostCard, 0, len(items))
	for _, p := range items {
		cards = append(cards, webtempl.PublicPostCard{
			Title:       p.Title,
			URL:         "/blog/" + p.Slug,
			Excerpt:     p.Excerpt,
			AuthorName:  h.authorName(ctx, p.AuthorID),
			Date:        publishedDate(p),
			ReadingTime: p.ReadingTime,
		})
	}
	return cards
}

func (h *TaxonomyPublicHandler) authorName(ctx context.Context, id uuid.UUID) string {
	if h.authors == nil {
		return "Author"
	}
	u, err := h.authors.GetByID(ctx, id)
	if err != nil {
		return "Author"
	}
	if u.Name != "" {
		return u.Name
	}
	if u.Username != "" {
		return u.Username
	}
	return "Author"
}

func (h *TaxonomyPublicHandler) render(w http.ResponseWriter, r *http.Request, c webtempl.Component) {
	if err := render.Component(r.Context(), w, http.StatusOK, c); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}
