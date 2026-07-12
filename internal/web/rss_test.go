package web

import (
	"context"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/huseyn0w/agentic-cms-go/internal/content/posts"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/config"
)

// fakeFeedLister is a network-free FeedPostLister returning a fixed set of posts
// and recording the category slug it was last called with.
type fakeFeedLister struct {
	posts      []posts.Post
	err        error
	gotCatSlug string
	gotTagSlug string
	calls      int
}

func (f *fakeFeedLister) PublicListFiltered(_ context.Context, categorySlug, tagSlug string, _, _ int) ([]posts.Post, int, error) {
	f.calls++
	f.gotCatSlug = categorySlug
	f.gotTagSlug = tagSlug
	if f.err != nil {
		return nil, 0, f.err
	}
	return f.posts, len(f.posts), nil
}

// fakeNamer records slug->name lookups for the per-category channel title.
type fakeNamer struct {
	name string
	ok   bool
	got  string
}

func (n *fakeNamer) NameForSlug(_ context.Context, slug string) (string, bool) {
	n.got = slug
	return n.name, n.ok
}

// channelLink captures a channel-level <link>: the plain RSS <link> carries only
// chardata, the <atom:link> carries href/rel attributes. Both share the local
// name "link", so we collect them together and disambiguate on Href.
type channelLink struct {
	Value string `xml:",chardata"`
	Href  string `xml:"href,attr"`
	Rel   string `xml:"rel,attr"`
}

// plainLink returns the value of the plain RSS <link> (no Href attribute).
func plainLink(links []channelLink) string {
	for _, l := range links {
		if l.Href == "" {
			return l.Value
		}
	}
	return ""
}

// atomSelf returns the <atom:link> (the one carrying an Href attribute).
func atomSelf(links []channelLink) channelLink {
	for _, l := range links {
		if l.Href != "" {
			return l
		}
	}
	return channelLink{}
}

// rssParsed is a minimal RSS 2.0 model for unmarshaling the rendered feed back
// (round-trip assertions instead of raw string matching).
type rssParsed struct {
	XMLName xml.Name `xml:"rss"`
	Version string   `xml:"version,attr"`
	Channel struct {
		Title         string        `xml:"title"`
		Links         []channelLink `xml:"link"`
		Description   string        `xml:"description"`
		Language      string        `xml:"language"`
		LastBuildDate string        `xml:"lastBuildDate"`
		Items         []struct {
			Title       string `xml:"title"`
			Link        string `xml:"link"`
			Description string `xml:"description"`
			PubDate     string `xml:"pubDate"`
			GUID        struct {
				Value       string `xml:",chardata"`
				IsPermaLink string `xml:"isPermaLink,attr"`
			} `xml:"guid"`
		} `xml:"item"`
	} `xml:"channel"`
}

func testFeedSite() SiteConfig {
	return NewSiteConfig(config.Config{
		BaseURL:         "https://site.test/",
		SiteName:        "My Blog",
		SiteDescription: "News & updates",
	})
}

func mustTime(t *testing.T, s string) *time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse time %q: %v", s, err)
	}
	return &v
}

func TestFeed_SiteWide(t *testing.T) {
	published := mustTime(t, "2026-01-02T15:04:05Z")
	older := mustTime(t, "2026-01-01T09:00:00Z")
	lister := &fakeFeedLister{posts: []posts.Post{
		{Title: "First", Slug: "first", Excerpt: "Hello & welcome <b>bold</b>", PublishedAt: published},
		{Title: "Second", Slug: "second", Excerpt: "More news", PublishedAt: older},
	}}
	h := NewFeedHandler(testFeedSite(), lister, nil)

	req := httptest.NewRequest(http.MethodGet, "/rss.xml", nil)
	rec := httptest.NewRecorder()
	h.Feed(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/rss+xml; charset=utf-8" {
		t.Fatalf("content-type = %q", ct)
	}
	if lister.gotCatSlug != "" {
		t.Fatalf("site-wide feed must pass empty category slug, got %q", lister.gotCatSlug)
	}

	var got rssParsed
	if err := xml.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("feed is not valid XML: %v\n%s", err, rec.Body.String())
	}
	if got.Version != "2.0" {
		t.Fatalf("rss version = %q", got.Version)
	}
	if got.Channel.Title != "My Blog" {
		t.Errorf("channel title = %q", got.Channel.Title)
	}
	if link := plainLink(got.Channel.Links); link != "https://site.test" {
		t.Errorf("channel link = %q", link)
	}
	if got.Channel.Description != "News & updates" {
		t.Errorf("channel description = %q", got.Channel.Description)
	}
	if self := atomSelf(got.Channel.Links); self.Href != "https://site.test/rss.xml" || self.Rel != "self" {
		t.Errorf("atom self link = %+v", self)
	}
	// lastBuildDate derives from newest post (deterministic, not time.Now).
	if want := published.Format(time.RFC1123Z); got.Channel.LastBuildDate != want {
		t.Errorf("lastBuildDate = %q, want %q", got.Channel.LastBuildDate, want)
	}
	if len(got.Channel.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(got.Channel.Items))
	}
	it := got.Channel.Items[0]
	if it.Link != "https://site.test/blog/first" {
		t.Errorf("item link = %q", it.Link)
	}
	if it.GUID.Value != "https://site.test/blog/first" || it.GUID.IsPermaLink != "true" {
		t.Errorf("item guid = %+v", it.GUID)
	}
	if it.PubDate != published.Format(time.RFC1123Z) {
		t.Errorf("item pubDate = %q", it.PubDate)
	}
	// Escaping round-trips: the parser gives back the original special chars.
	if it.Description != "Hello & welcome <b>bold</b>" {
		t.Errorf("description did not round-trip safely: %q", it.Description)
	}
}

func TestFeed_CategoryForwardsSlugAndName(t *testing.T) {
	published := mustTime(t, "2026-01-02T15:04:05Z")
	lister := &fakeFeedLister{posts: []posts.Post{
		{Title: "Go rocks", Slug: "go-rocks", Excerpt: "yes", PublishedAt: published},
	}}
	namer := &fakeNamer{name: "Golang", ok: true}
	h := NewFeedHandler(testFeedSite(), lister, namer)

	req := httptest.NewRequest(http.MethodGet, "/categories/go/rss.xml", nil)
	req = withChiParam(req, "slug", "go")
	rec := httptest.NewRecorder()
	h.CategoryFeed(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if lister.gotCatSlug != "go" {
		t.Fatalf("category slug not forwarded, got %q", lister.gotCatSlug)
	}
	if namer.got != "go" {
		t.Fatalf("namer got %q", namer.got)
	}

	var got rssParsed
	if err := xml.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid XML: %v", err)
	}
	if got.Channel.Title != "My Blog — Golang" {
		t.Errorf("category channel title = %q", got.Channel.Title)
	}
	if self := atomSelf(got.Channel.Links); self.Href != "https://site.test/categories/go/rss.xml" {
		t.Errorf("category atom self = %q", self.Href)
	}
}

func TestFeed_CategoryFallsBackToSlug(t *testing.T) {
	lister := &fakeFeedLister{}
	h := NewFeedHandler(testFeedSite(), lister, nil)

	req := httptest.NewRequest(http.MethodGet, "/categories/go/rss.xml", nil)
	req = withChiParam(req, "slug", "go")
	rec := httptest.NewRecorder()
	h.CategoryFeed(rec, req)

	var got rssParsed
	if err := xml.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid XML: %v", err)
	}
	if got.Channel.Title != "My Blog — go" {
		t.Errorf("fallback title = %q", got.Channel.Title)
	}
}

func TestFeed_SkipsNilPublishedAt(t *testing.T) {
	published := mustTime(t, "2026-01-02T15:04:05Z")
	lister := &fakeFeedLister{posts: []posts.Post{
		{Title: "Has date", Slug: "has-date", PublishedAt: published},
		{Title: "No date", Slug: "no-date", PublishedAt: nil},
	}}
	h := NewFeedHandler(testFeedSite(), lister, nil)

	req := httptest.NewRequest(http.MethodGet, "/rss.xml", nil)
	rec := httptest.NewRecorder()
	h.Feed(rec, req)

	var got rssParsed
	if err := xml.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid XML: %v", err)
	}
	if len(got.Channel.Items) != 1 {
		t.Fatalf("items = %d, want 1 (nil PublishedAt skipped)", len(got.Channel.Items))
	}
}

func TestFeed_EmptyResultIsValidRSS(t *testing.T) {
	lister := &fakeFeedLister{posts: nil}
	h := NewFeedHandler(testFeedSite(), lister, nil)

	req := httptest.NewRequest(http.MethodGet, "/rss.xml", nil)
	rec := httptest.NewRecorder()
	h.Feed(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got rssParsed
	if err := xml.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("empty feed must be valid XML: %v", err)
	}
	if len(got.Channel.Items) != 0 {
		t.Fatalf("items = %d, want 0", len(got.Channel.Items))
	}
	if got.Channel.LastBuildDate != "" {
		t.Errorf("empty feed should omit lastBuildDate, got %q", got.Channel.LastBuildDate)
	}
}

func TestFeed_ListerErrorIs500(t *testing.T) {
	lister := &fakeFeedLister{err: context.DeadlineExceeded}
	h := NewFeedHandler(testFeedSite(), lister, nil)

	req := httptest.NewRequest(http.MethodGet, "/rss.xml", nil)
	rec := httptest.NewRecorder()
	h.Feed(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
