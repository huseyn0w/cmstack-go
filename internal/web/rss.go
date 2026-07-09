package web

import (
	"context"
	"encoding/xml"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/huseyn0w/cmstack-go/internal/content/categories"
	"github.com/huseyn0w/cmstack-go/internal/content/posts"
	"github.com/huseyn0w/cmstack-go/internal/platform/i18n"
)

// feedItemLimit is the default number of most-recent published posts a feed
// carries when NewFeedHandler is not given an explicit limit.
const feedItemLimit = 20

// FeedPostLister enumerates published posts for the RSS feeds, optionally
// narrowed to a category slug. *posts.Service satisfies it via
// PublicListFiltered (drafts/trashed excluded).
type FeedPostLister interface {
	PublicListFiltered(ctx context.Context, categorySlug, tagSlug string, limit, offset int) ([]posts.Post, int, error)
}

// feedCategoryNamer resolves a category slug to its display name for the
// per-category channel title. It is optional (nil-safe): when absent or when a
// slug is unknown the handler falls back to the slug itself.
type feedCategoryNamer interface {
	NameForSlug(ctx context.Context, slug string) (string, bool)
}

// categoryPublicBySlug is the subset of *categories.Service the feed namer
// adapter needs: a public by-slug lookup returning the category (for its Name).
type categoryPublicBySlug interface {
	PublicBySlug(ctx context.Context, slug string) (categories.Category, error)
}

// CategoryFeedNamer adapts a category by-slug lookup to the feedCategoryNamer
// contract used by the per-category RSS feed for its channel title.
type CategoryFeedNamer struct{ svc categoryPublicBySlug }

// NewCategoryFeedNamer wraps svc (e.g. *categories.Service) as a
// feedCategoryNamer. Returns nil when svc is nil so wiring stays conditional.
func NewCategoryFeedNamer(svc categoryPublicBySlug) *CategoryFeedNamer {
	if svc == nil {
		return nil
	}
	return &CategoryFeedNamer{svc: svc}
}

// NameForSlug resolves slug to its category display name. The boolean is false
// when the slug is unknown (or the lookup errors), so callers fall back to the
// slug.
func (a *CategoryFeedNamer) NameForSlug(ctx context.Context, slug string) (string, bool) {
	c, err := a.svc.PublicBySlug(ctx, slug)
	if err != nil || strings.TrimSpace(c.Name) == "" {
		return "", false
	}
	return c.Name, true
}

// FeedHandler serves RSS 2.0 feeds of PUBLISHED posts: the site-wide /rss.xml
// and the per-category /categories/{slug}/rss.xml. It is locale-agnostic
// (registered on the root router, unprefixed) and depends only on the narrow
// FeedPostLister, an optional category namer, and the resolved SiteConfig.
type FeedHandler struct {
	site  SiteConfig
	posts FeedPostLister
	cats  feedCategoryNamer
	limit int
}

// NewFeedHandler constructs the feed handler with the item limit defaulting to
// feedItemLimit. cats may be nil (the per-category channel title then falls back
// to the slug).
func NewFeedHandler(site SiteConfig, posts FeedPostLister, cats feedCategoryNamer) *FeedHandler {
	return &FeedHandler{site: site, posts: posts, cats: cats, limit: feedItemLimit}
}

// --- RSS 2.0 XML model -------------------------------------------------------

// rssDoc is the root <rss version="2.0"> element with the atom namespace.
type rssDoc struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	Atom    string     `xml:"xmlns:atom,attr"`
	Channel rssChannel `xml:"channel"`
}

// rssChannel is the feed's <channel>: site identity plus an atom:link rel="self".
type rssChannel struct {
	Title         string    `xml:"title"`
	Link          string    `xml:"link"`
	Description   string    `xml:"description"`
	Language      string    `xml:"language,omitempty"`
	LastBuildDate string    `xml:"lastBuildDate,omitempty"`
	AtomLink      atomLink  `xml:"atom:link"`
	Items         []rssItem `xml:"item"`
}

// atomLink is the <atom:link rel="self"> pointing at the feed's own URL.
type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
	Type string `xml:"type,attr"`
}

// rssItem is one <item>: a published post rendered as a feed entry.
type rssItem struct {
	Title       string  `xml:"title"`
	Link        string  `xml:"link"`
	GUID        rssGUID `xml:"guid"`
	PubDate     string  `xml:"pubDate"`
	Description string  `xml:"description"`
}

// rssGUID is the item <guid isPermaLink="true">, identical to the item link.
type rssGUID struct {
	Value       string `xml:",chardata"`
	IsPermaLink string `xml:"isPermaLink,attr"`
}

// Feed serves GET /rss.xml: the site-wide feed of the latest published posts.
func (h *FeedHandler) Feed(w http.ResponseWriter, r *http.Request) {
	doc, ok := h.render(r.Context(), "")
	h.write(w, doc, ok)
}

// CategoryFeed serves GET /categories/{slug}/rss.xml: the latest published posts
// in the category named by the chi URL param "slug".
func (h *FeedHandler) CategoryFeed(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	doc, ok := h.render(r.Context(), slug)
	h.write(w, doc, ok)
}

// write emits the rendered document with the RSS content type, or a 500 when
// rendering failed (ok == false).
func (h *FeedHandler) write(w http.ResponseWriter, doc []byte, ok bool) {
	if !ok {
		http.Error(w, "feed error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(doc)
}

// render builds the complete RSS document (XML header + marshaled rss) for the
// feed narrowed to categorySlug (empty = site-wide). The boolean is false on a
// lister or marshal error. Dates derive from post data only — no time.Now — so
// the output is deterministic.
func (h *FeedHandler) render(ctx context.Context, categorySlug string) ([]byte, bool) {
	rows, _, err := h.posts.PublicListFiltered(ctx, categorySlug, "", h.limit, 0)
	if err != nil {
		return nil, false
	}

	base := strings.TrimSuffix(h.site.BaseURL, "/")

	items := make([]rssItem, 0, len(rows))
	var newest time.Time
	for _, p := range rows {
		if p.PublishedAt == nil {
			continue // defensive: a feed entry needs a pubDate
		}
		link := base + i18n.LocalizePath(i18n.Default(), "/blog/"+p.Slug)
		if p.PublishedAt.After(newest) {
			newest = *p.PublishedAt
		}
		items = append(items, rssItem{
			Title:       p.Title,
			Link:        link,
			GUID:        rssGUID{Value: link, IsPermaLink: "true"},
			PubDate:     p.PublishedAt.Format(time.RFC1123Z),
			Description: p.Excerpt,
		})
	}

	title := h.site.resolveSiteName(ctx)
	selfPath := "/rss.xml"
	if categorySlug != "" {
		title = title + " — " + h.categoryTitle(ctx, categorySlug)
		selfPath = "/categories/" + categorySlug + "/rss.xml"
	}

	lastBuild := ""
	if !newest.IsZero() {
		lastBuild = newest.Format(time.RFC1123Z)
	}

	doc := rssDoc{
		Version: "2.0",
		Atom:    "http://www.w3.org/2005/Atom",
		Channel: rssChannel{
			Title:         title,
			Link:          base,
			Description:   h.site.resolveSiteDescription(ctx),
			Language:      i18n.Default().String(),
			LastBuildDate: lastBuild,
			AtomLink: atomLink{
				Href: base + selfPath,
				Rel:  "self",
				Type: "application/rss+xml",
			},
			Items: items,
		},
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

// categoryTitle resolves the display name for the per-category channel title,
// falling back to the slug when no namer is wired or the slug is unknown.
func (h *FeedHandler) categoryTitle(ctx context.Context, slug string) string {
	if h.cats != nil {
		if name, ok := h.cats.NameForSlug(ctx, slug); ok {
			return name
		}
	}
	return slug
}
