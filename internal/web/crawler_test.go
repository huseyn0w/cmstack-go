package web

import (
	"context"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
)

// --- fakes -------------------------------------------------------------------

type fakeSitemapEnum struct {
	items []kernel.SitemapItem
	err   error
}

func (f fakeSitemapEnum) SitemapItems(context.Context) ([]kernel.SitemapItem, error) {
	return f.items, f.err
}

type fakeTaxonomyEnum struct {
	items []kernel.SitemapItem
	err   error
}

func (f fakeTaxonomyEnum) SitemapTaxonomy(context.Context) ([]kernel.SitemapItem, error) {
	return f.items, f.err
}

func testCrawler(site SiteConfig) *CrawlerHandler {
	posts := fakeSitemapEnum{items: []kernel.SitemapItem{
		{Slug: "hello", Title: "Hello World", Description: "A first post", UpdatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)},
	}}
	pages := fakeSitemapEnum{items: []kernel.SitemapItem{{Slug: "about", Title: "About"}}}
	svcs := fakeSitemapEnum{items: []kernel.SitemapItem{{Slug: "consulting", Title: "Consulting", Description: "We help"}}}
	cats := fakeTaxonomyEnum{items: []kernel.SitemapItem{{Slug: "news", Title: "News"}}}
	tags := fakeTaxonomyEnum{items: []kernel.SitemapItem{{Slug: "go", Title: "Go"}}}
	return NewCrawlerHandler(site, posts, pages, svcs, cats, tags)
}

func defaultCrawlerSite() SiteConfig {
	return SiteConfig{
		BaseURL:         "https://example.test",
		SiteName:        "Example",
		SiteDescription: "A quiet-luxury CMS.",
		AllowAICrawlers: true,
	}
}

// --- sitemap -----------------------------------------------------------------

func TestSitemap_WellFormedWithAlternates(t *testing.T) {
	h := testCrawler(defaultCrawlerSite())
	req := httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil)
	rec := httptest.NewRecorder()
	h.Sitemap(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("sitemap status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/xml") {
		t.Fatalf("content-type = %q, want application/xml", ct)
	}
	body := rec.Body.String()

	// Well-formed: parse into a mirror struct.
	var parsed struct {
		XMLName xml.Name `xml:"urlset"`
		URLs    []struct {
			Loc        string `xml:"loc"`
			Alternates []struct {
				Hreflang string `xml:"hreflang,attr"`
				Href     string `xml:"href,attr"`
			} `xml:"link"`
		} `xml:"url"`
	}
	if err := xml.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("sitemap is not well-formed XML: %v", err)
	}
	if len(parsed.URLs) == 0 {
		t.Fatal("sitemap has no <url> entries")
	}

	// Home, blog hub, and the seeded post + taxonomy URLs are present (absolute).
	for _, want := range []string{
		"https://example.test/",
		"https://example.test/blog",
		"https://example.test/blog/hello",
		"https://example.test/p/about",
		"https://example.test/services/consulting",
		"https://example.test/categories/news",
		"https://example.test/tags/go",
	} {
		if !strings.Contains(body, "<loc>"+want+"</loc>") {
			t.Errorf("sitemap missing <loc>%s</loc>", want)
		}
	}
	// hreflang alternates including x-default and a prefixed locale.
	if !strings.Contains(body, `hreflang="x-default"`) {
		t.Error("sitemap missing x-default alternate")
	}
	if !strings.Contains(body, `hreflang="de"`) || !strings.Contains(body, "https://example.test/de/blog/hello") {
		t.Error("sitemap missing localized (de) alternate for a post")
	}
	// lastmod from UpdatedAt.
	if !strings.Contains(body, "<lastmod>2026-06-01</lastmod>") {
		t.Error("sitemap missing lastmod for the seeded post")
	}
}

func TestSitemap_EnumeratorErrorIs500(t *testing.T) {
	site := defaultCrawlerSite()
	h := NewCrawlerHandler(site, fakeSitemapEnum{err: context.DeadlineExceeded}, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil)
	rec := httptest.NewRecorder()
	h.Sitemap(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("enumerator error = %d, want 500", rec.Code)
	}
}

// --- robots ------------------------------------------------------------------

func TestRobots_DefaultAllowsAIAndAdvertisesSitemap(t *testing.T) {
	h := testCrawler(defaultCrawlerSite())
	req := httptest.NewRequest(http.MethodGet, "/robots.txt", nil)
	rec := httptest.NewRecorder()
	h.Robots(rec, req)

	body := rec.Body.String()
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("content-type = %q, want text/plain", ct)
	}
	if !strings.Contains(body, "Sitemap: https://example.test/sitemap.xml") {
		t.Error("robots missing Sitemap directive")
	}
	if !strings.Contains(body, "Disallow: /admin/") {
		t.Error("robots should disallow /admin/")
	}
	if strings.Contains(body, "GPTBot") {
		t.Error("robots should NOT block GPTBot when AllowAICrawlers is true")
	}
}

func TestRobots_BlocksAICrawlersWhenDisabled(t *testing.T) {
	site := defaultCrawlerSite()
	site.AllowAICrawlers = false
	h := testCrawler(site)
	req := httptest.NewRequest(http.MethodGet, "/robots.txt", nil)
	rec := httptest.NewRecorder()
	h.Robots(rec, req)

	body := rec.Body.String()
	for _, ua := range []string{"GPTBot", "ClaudeBot", "CCBot", "Google-Extended"} {
		if !strings.Contains(body, "User-agent: "+ua) {
			t.Errorf("robots should block %s when AllowAICrawlers is false", ua)
		}
	}
}

func TestRobots_GlobalNoindexBlocksEverything(t *testing.T) {
	site := defaultCrawlerSite()
	site.GlobalNoindex = true
	h := testCrawler(site)
	req := httptest.NewRequest(http.MethodGet, "/robots.txt", nil)
	rec := httptest.NewRecorder()
	h.Robots(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "User-agent: *\nDisallow: /\n") {
		t.Errorf("global noindex robots should disallow everything, got:\n%s", body)
	}
}

// --- llms --------------------------------------------------------------------

func TestLLMs_OverviewAndFull(t *testing.T) {
	h := testCrawler(defaultCrawlerSite())

	// /llms.txt — concise overview, links but no descriptions.
	req := httptest.NewRequest(http.MethodGet, "/llms.txt", nil)
	rec := httptest.NewRecorder()
	h.LLMs(rec, req)
	body := rec.Body.String()
	if !strings.HasPrefix(body, "# Example\n") {
		t.Errorf("llms.txt should start with the site H1, got:\n%s", body)
	}
	if !strings.Contains(body, "> A quiet-luxury CMS.") {
		t.Error("llms.txt missing site description blockquote")
	}
	if !strings.Contains(body, "- [Hello World](https://example.test/blog/hello)") {
		t.Error("llms.txt missing the seeded post link")
	}
	if strings.Contains(body, "A first post") {
		t.Error("llms.txt (concise) should NOT include item descriptions")
	}

	// /llms-full.txt — same structure plus per-item descriptions.
	reqF := httptest.NewRequest(http.MethodGet, "/llms-full.txt", nil)
	recF := httptest.NewRecorder()
	h.LLMsFull(recF, reqF)
	full := recF.Body.String()
	if !strings.Contains(full, "- [Hello World](https://example.test/blog/hello): A first post") {
		t.Errorf("llms-full.txt should include the item description, got:\n%s", full)
	}
}
