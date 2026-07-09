package web

import (
	"context"
	"encoding/xml"
	"net/http"
	"strings"
	"time"

	"github.com/huseyn0w/cmstack-go/internal/content/categories"
	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
	"github.com/huseyn0w/cmstack-go/internal/content/tags"
	"github.com/huseyn0w/cmstack-go/internal/platform/cache"
	"github.com/huseyn0w/cmstack-go/internal/platform/i18n"
)

// sitemapCacheKey is the single cache key under which the fully rendered
// sitemap.xml document (including the XML header) is stored. It shares the
// "sitemap:" prefix the invalidator clears on content publish.
const sitemapCacheKey = "sitemap:xml"

// sitemapCacheTTL bounds sitemap staleness as a backstop; publish-time
// invalidation is the primary freshness mechanism.
const sitemapCacheTTL = time.Hour

// SitemapEnumerator is the narrow read contract the crawler routes (sitemap.xml,
// llms.txt, llms-full.txt) depend on per content type. *posts.Service,
// *pages.Service and *services.Manager satisfy it via their SitemapItems method.
// It returns only lightweight rows (no bodies).
type SitemapEnumerator interface {
	SitemapItems(ctx context.Context) ([]kernel.SitemapItem, error)
}

// TaxonomyEnumerator is the narrow read contract for enumerating the taxonomy
// archives (categories/tags) in the crawler routes. It maps onto the existing
// AllFlat methods via thin adapters in the router wiring.
type TaxonomyEnumerator interface {
	SitemapTaxonomy(ctx context.Context) ([]kernel.SitemapItem, error)
}

// categoryFlatLister is the subset of *categories.Service the sitemap taxonomy
// adapter needs: a flat listing of every category.
type categoryFlatLister interface {
	AllFlat(ctx context.Context) ([]categories.Category, error)
}

// tagFlatLister is the subset of *tags.Service the sitemap taxonomy adapter
// needs: a flat listing of every tag.
type tagFlatLister interface {
	AllFlat(ctx context.Context) ([]tags.Tag, error)
}

// CategorySitemapAdapter adapts a category AllFlat listing to the taxonomy
// enumerator contract used by the crawler routes.
type CategorySitemapAdapter struct{ svc categoryFlatLister }

// NewCategorySitemapAdapter wraps svc (e.g. *categories.Service) as a
// TaxonomyEnumerator. Returns nil when svc is nil so wiring stays conditional.
func NewCategorySitemapAdapter(svc categoryFlatLister) *CategorySitemapAdapter {
	if svc == nil {
		return nil
	}
	return &CategorySitemapAdapter{svc: svc}
}

// SitemapTaxonomy enumerates every category as a lightweight SitemapItem.
func (a *CategorySitemapAdapter) SitemapTaxonomy(ctx context.Context) ([]kernel.SitemapItem, error) {
	rows, err := a.svc.AllFlat(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]kernel.SitemapItem, 0, len(rows))
	for _, c := range rows {
		out = append(out, kernel.SitemapItem{Slug: c.Slug, Title: c.Name, UpdatedAt: c.UpdatedAt})
	}
	return out, nil
}

// TagSitemapAdapter adapts a tag AllFlat listing to the taxonomy enumerator
// contract used by the crawler routes.
type TagSitemapAdapter struct{ svc tagFlatLister }

// NewTagSitemapAdapter wraps svc (e.g. *tags.Service) as a TaxonomyEnumerator.
// Returns nil when svc is nil so wiring stays conditional.
func NewTagSitemapAdapter(svc tagFlatLister) *TagSitemapAdapter {
	if svc == nil {
		return nil
	}
	return &TagSitemapAdapter{svc: svc}
}

// SitemapTaxonomy enumerates every tag as a lightweight SitemapItem.
func (a *TagSitemapAdapter) SitemapTaxonomy(ctx context.Context) ([]kernel.SitemapItem, error) {
	rows, err := a.svc.AllFlat(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]kernel.SitemapItem, 0, len(rows))
	for _, t := range rows {
		out = append(out, kernel.SitemapItem{Slug: t.Slug, Title: t.Name, UpdatedAt: t.UpdatedAt})
	}
	return out, nil
}

// CrawlerHandler serves the domain-root crawler files: /sitemap.xml,
// /robots.txt, /llms.txt, /llms-full.txt. It is locale-agnostic (registered on
// the root router, unprefixed) and depends only on the narrow enumerators plus
// the resolved SiteConfig.
type CrawlerHandler struct {
	site       SiteConfig
	posts      SitemapEnumerator
	pages      SitemapEnumerator
	services   SitemapEnumerator
	categories TaxonomyEnumerator
	tags       TaxonomyEnumerator
	// cache, when non-nil, memoizes the rendered sitemap.xml under
	// sitemapCacheKey. It is invalidated on content publish (the "sitemap:"
	// prefix). A nil cache preserves the original render-every-request behavior.
	cache cache.Cache
}

// WithCache attaches an object cache used to memoize the rendered sitemap.xml.
// A nil cache leaves caching off (current behavior). It returns the handler for
// chaining and is safe to call during wiring only (not concurrently).
func (h *CrawlerHandler) WithCache(c cache.Cache) *CrawlerHandler {
	h.cache = c
	return h
}

// NewCrawlerHandler constructs the crawler handler. Any enumerator may be nil
// (reduced-Deps wiring); nil enumerators simply contribute no URLs.
func NewCrawlerHandler(site SiteConfig, posts, pages, svcs SitemapEnumerator, cats, tags TaxonomyEnumerator) *CrawlerHandler {
	return &CrawlerHandler{
		site:       site,
		posts:      posts,
		pages:      pages,
		services:   svcs,
		categories: cats,
		tags:       tags,
	}
}

// --- XML sitemap model ------------------------------------------------------

// urlset is the root <urlset> element with the sitemap + xhtml namespaces.
type urlset struct {
	XMLName xml.Name  `xml:"urlset"`
	XMLNS   string    `xml:"xmlns,attr"`
	Xhtml   string    `xml:"xmlns:xhtml,attr"`
	URLs    []siteURL `xml:"url"`
}

// siteURL is one <url> entry with its per-locale <xhtml:link> alternates.
type siteURL struct {
	Loc        string      `xml:"loc"`
	LastMod    string      `xml:"lastmod,omitempty"`
	Alternates []xhtmlLink `xml:"xhtml:link"`
}

// xhtmlLink is a per-locale <xhtml:link rel="alternate" hreflang=..> element.
type xhtmlLink struct {
	Rel      string `xml:"rel,attr"`
	Hreflang string `xml:"hreflang,attr"`
	Href     string `xml:"href,attr"`
}

// w3cDate formats a lastmod in W3C date form (YYYY-MM-DD). A zero time yields "".
func w3cDate(item kernel.SitemapItem) string {
	if item.UpdatedAt.IsZero() {
		return ""
	}
	return item.UpdatedAt.UTC().Format("2006-01-02")
}

// absoluteFor builds the absolute URL for rootedPath in loc (BaseURL trimmed +
// LocalizePath). The default locale is unprefixed.
func (h *CrawlerHandler) absoluteFor(loc i18n.Locale, rootedPath string) string {
	base := strings.TrimSuffix(h.site.BaseURL, "/")
	return base + i18n.LocalizePath(loc, rootedPath)
}

// alternatesFor mirrors i18n.Alternates semantics: one <xhtml:link> per
// supported locale plus an x-default entry pointing at the default-locale URL.
func (h *CrawlerHandler) alternatesFor(rootedPath string) []xhtmlLink {
	locs := i18n.All()
	out := make([]xhtmlLink, 0, len(locs))
	for _, loc := range locs {
		hreflang := loc.String()
		if loc.IsDefault() {
			hreflang = "x-default"
		}
		out = append(out, xhtmlLink{
			Rel:      "alternate",
			Hreflang: hreflang,
			Href:     h.absoluteFor(loc, rootedPath),
		})
	}
	return out
}

// urlFor builds a full <url> entry for rootedPath: the default-locale absolute
// loc, the lastmod, and the per-locale hreflang alternates.
func (h *CrawlerHandler) urlFor(rootedPath, lastmod string) siteURL {
	return siteURL{
		Loc:        h.absoluteFor(i18n.Default(), rootedPath),
		LastMod:    lastmod,
		Alternates: h.alternatesFor(rootedPath),
	}
}

// Sitemap serves GET /sitemap.xml: a valid urlset with per-URL hreflang
// alternates for the home, /blog, /services, every published post/service/page,
// and every category/tag archive. GlobalNoindex does NOT suppress the sitemap
// (staging robots handles blocking).
//
// Deferral (v1): per-item noindex is NOT special-cased — drafts are already
// excluded by the PUBLISHED filter; individually noindexed-but-published items
// remain listed.
func (h *CrawlerHandler) Sitemap(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Cache hit: replay the stored document (XML header included) verbatim.
	if h.cache != nil {
		if raw, ok, err := h.cache.Get(ctx, sitemapCacheKey); err == nil && ok {
			w.Header().Set("Content-Type", "application/xml; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(raw)
			return
		}
	}

	doc, ok := h.render(ctx)
	if !ok {
		http.Error(w, "sitemap error", http.StatusInternalServerError)
		return
	}

	if h.cache != nil {
		_ = h.cache.Set(ctx, sitemapCacheKey, doc, sitemapCacheTTL)
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(doc)
}

// render builds the complete sitemap document (XML header + marshaled urlset) as
// a single byte slice. The boolean is false on an enumeration or marshal error.
func (h *CrawlerHandler) render(ctx context.Context) ([]byte, bool) {
	urls := make([]siteURL, 0, 16)

	// Static hubs.
	urls = append(urls, h.urlFor("/", ""))
	if h.posts != nil {
		urls = append(urls, h.urlFor("/blog", ""))
	}
	if h.services != nil {
		urls = append(urls, h.urlFor("/services", ""))
	}

	appendItems := func(en SitemapEnumerator, prefix string) bool {
		if en == nil {
			return true
		}
		items, err := en.SitemapItems(ctx)
		if err != nil {
			return false
		}
		for _, it := range items {
			urls = append(urls, h.urlFor(prefix+it.Slug, w3cDate(it)))
		}
		return true
	}
	appendTaxonomy := func(en TaxonomyEnumerator, prefix string) bool {
		if en == nil {
			return true
		}
		items, err := en.SitemapTaxonomy(ctx)
		if err != nil {
			return false
		}
		for _, it := range items {
			urls = append(urls, h.urlFor(prefix+it.Slug, w3cDate(it)))
		}
		return true
	}

	enumerated := appendItems(h.posts, "/blog/") &&
		appendItems(h.services, "/services/") &&
		appendItems(h.pages, "/p/") &&
		appendTaxonomy(h.categories, "/categories/") &&
		appendTaxonomy(h.tags, "/tags/")
	if !enumerated {
		return nil, false
	}

	doc := urlset{
		XMLNS: "http://www.sitemaps.org/schemas/sitemap/0.9",
		Xhtml: "http://www.w3.org/1999/xhtml",
		URLs:  urls,
	}
	out, err := xml.Marshal(doc)
	if err != nil {
		return nil, false
	}

	buf := make([]byte, 0, len(xml.Header)+len(out))
	buf = append(buf, xml.Header...)
	buf = append(buf, out...)
	return buf, true
}
