package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/microcosm-cc/bluemonday"

	"github.com/huseyn0w/cmstack-go/internal/content/search"
	"github.com/huseyn0w/cmstack-go/internal/platform/render"
	webtempl "github.com/huseyn0w/cmstack-go/web/templ"
)

// searchPageSize is the public search results page size.
const searchPageSize = 10

// SearchService is the subset of *search.Service the public handler calls.
type SearchService interface {
	Search(ctx context.Context, query string, page, perPage int) (search.Result, error)
}

// SearchPublicHandler is the thin HTTP boundary for public site search. GET-only
// (idempotent, shareable) — no CSRF, no auth.
type SearchPublicHandler struct {
	svc      SearchService
	siteName string
	site     SiteConfig
}

// WithSite attaches the resolved site-identity + SEO config (M8). Returns the
// receiver.
func (h *SearchPublicHandler) WithSite(s SiteConfig) *SearchPublicHandler {
	h.site = s
	return h
}

// NewSearchPublicHandler constructs the public search handler.
func NewSearchPublicHandler(svc SearchService, siteName string) *SearchPublicHandler {
	if siteName == "" {
		siteName = "CMStack"
	}
	return &SearchPublicHandler{svc: svc, siteName: siteName}
}

// Search renders the results page for ?q=...&page=N. A blank q renders the
// prompt state (no query run); a non-matching q renders the empty state.
func (h *SearchPublicHandler) Search(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	page := pageParam(r)

	res, err := h.svc.Search(r.Context(), query, page, searchPageSize)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}

	cards := make([]webtempl.SearchResultCard, 0, len(res.Hits))
	for _, hit := range res.Hits {
		cards = append(cards, webtempl.SearchResultCard{
			Eyebrow:     hitEyebrow(hit.Type),
			Title:       hit.Title,
			URL:         hit.URL,
			SnippetHTML: sanitizeSnippet(hit.Snippet),
			Date:        searchHitDate(hit),
		})
	}

	view := webtempl.SearchView{
		SiteName:  h.siteName,
		HomeURL:   "/",
		ActionURL: "/search",
		Query:     res.Query,
		Submitted: res.Query != "",
		Fallback:  res.Fallback,
		Cards:     cards,
		Total:     res.Total,
		Pager:     pager(page, searchPageSize, res.Total, "/search", searchQuery(res.Query)),
	}
	// Search result pages are noindex: query pages should not be indexed.
	view.SEO = h.site.BuildSEO(r, SEOInput{
		Title:         "Search",
		Description:   "Search " + h.siteName,
		CanonicalPath: "/search",
		NoIndex:       true,
		OGType:        "website",
	})
	if err := render.Component(r.Context(), w, http.StatusOK, webtempl.PublicSearch(view)); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// hitEyebrow is the human label for a hit type.
func hitEyebrow(t search.HitType) string {
	switch t {
	case search.HitPost:
		return "Post"
	case search.HitPage:
		return "Page"
	case search.HitService:
		return "Service"
	default:
		return "Result"
	}
}

// searchHitDate formats a hit's published date (empty when unpublished/absent).
func searchHitDate(hit search.Hit) string {
	if hit.PublishedAt == nil {
		return ""
	}
	return hit.PublishedAt.Format("January 2, 2006")
}

// searchQuery preserves the active ?q across pagination links.
func searchQuery(q string) string {
	if q == "" {
		return ""
	}
	return url.Values{"q": {q}}.Encode()
}

// snippetOnce builds the mark-only snippet policy exactly once. ts_headline does
// NOT HTML-escape the source text, so a snippet drawn from rich-text body could
// carry arbitrary tags; this policy strips everything EXCEPT <mark> (the
// highlight ts_headline injects), then escapes the rest — safe to render raw.
var (
	snippetOnce sync.Once
	snippetPol  *bluemonday.Policy
)

func snippetPolicy() *bluemonday.Policy {
	snippetOnce.Do(func() {
		p := bluemonday.NewPolicy()
		p.AllowElements("mark")
		snippetPol = p
	})
	return snippetPol
}

// sanitizeSnippet strips all HTML except <mark> from a search snippet.
func sanitizeSnippet(s string) string {
	if s == "" {
		return ""
	}
	return snippetPolicy().Sanitize(s)
}
