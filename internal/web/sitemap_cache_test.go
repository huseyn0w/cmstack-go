package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
	"github.com/huseyn0w/cmstack-go/internal/platform/cache"
)

// countingSitemapEnum counts SitemapItems invocations so the test can prove the
// sitemap render was served from cache (no re-enumeration).
type countingSitemapEnum struct {
	calls int
	items []kernel.SitemapItem
}

func (c *countingSitemapEnum) SitemapItems(context.Context) ([]kernel.SitemapItem, error) {
	c.calls++
	return c.items, nil
}

func TestSitemap_CachesRenderedDocument(t *testing.T) {
	posts := &countingSitemapEnum{items: []kernel.SitemapItem{{Slug: "hello", Title: "Hello"}}}
	c := cache.NewMemory()
	h := NewCrawlerHandler(defaultCrawlerSite(), posts, nil, nil, nil, nil).WithCache(c)

	rec1 := httptest.NewRecorder()
	h.Sitemap(rec1, httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil))
	if rec1.Code != http.StatusOK {
		t.Fatalf("first sitemap status = %d", rec1.Code)
	}
	if posts.calls != 1 {
		t.Fatalf("enumerator calls after first request = %d, want 1", posts.calls)
	}
	first := rec1.Body.String()

	rec2 := httptest.NewRecorder()
	h.Sitemap(rec2, httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil))
	if posts.calls != 1 {
		t.Fatalf("enumerator calls after cached request = %d, want 1 (served from cache)", posts.calls)
	}
	if rec2.Body.String() != first {
		t.Fatalf("cached sitemap body differs from first render")
	}
	if ct := rec2.Header().Get("Content-Type"); ct != "application/xml; charset=utf-8" {
		t.Fatalf("cached content-type = %q", ct)
	}
}

func TestSitemap_InvalidationReRenders(t *testing.T) {
	posts := &countingSitemapEnum{items: []kernel.SitemapItem{{Slug: "hello", Title: "Hello"}}}
	c := cache.NewMemory()
	h := NewCrawlerHandler(defaultCrawlerSite(), posts, nil, nil, nil, nil).WithCache(c)

	h.Sitemap(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil))
	h.Sitemap(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil))
	if posts.calls != 1 {
		t.Fatalf("enumerator calls before invalidation = %d, want 1", posts.calls)
	}

	// The invalidator clears the "sitemap:" prefix on publish.
	if err := c.DeleteByPrefix(context.Background(), "sitemap:"); err != nil {
		t.Fatalf("DeleteByPrefix: %v", err)
	}

	h.Sitemap(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil))
	if posts.calls != 2 {
		t.Fatalf("enumerator calls after invalidation = %d, want 2 (must re-render)", posts.calls)
	}
}

func TestSitemap_NilCacheRendersEveryRequest(t *testing.T) {
	posts := &countingSitemapEnum{items: []kernel.SitemapItem{{Slug: "hello", Title: "Hello"}}}
	h := NewCrawlerHandler(defaultCrawlerSite(), posts, nil, nil, nil, nil) // no cache

	h.Sitemap(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil))
	h.Sitemap(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil))
	if posts.calls != 2 {
		t.Fatalf("enumerator calls = %d, want 2 (nil cache renders every request)", posts.calls)
	}
}
