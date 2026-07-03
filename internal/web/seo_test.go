package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
	"github.com/huseyn0w/cmstack-go/internal/content/posts"
	"github.com/huseyn0w/cmstack-go/internal/health"
	"github.com/huseyn0w/cmstack-go/internal/platform/config"
	"github.com/huseyn0w/cmstack-go/internal/platform/i18n"
	"github.com/huseyn0w/cmstack-go/internal/platform/security"
	"github.com/huseyn0w/cmstack-go/internal/platform/session"
	webtempl "github.com/huseyn0w/cmstack-go/web/templ"
)

// baseSite is a representative SiteConfig for the builder unit tests.
func baseSite() SiteConfig {
	return SiteConfig{
		BaseURL:         "https://site.test/", // trailing slash exercises trimming
		SiteName:        "CMStack",
		SiteDescription: "A server-rendered CMS",
		DefaultOGImage:  "/static/og.png",
		TwitterHandle:   "@cmstack",
		Verifications:   []webtempl.MetaTag{{Name: "google-site-verification", Content: "tok"}},
	}
}

// seoReq is a bare request (no locale middleware); AlternatesFromContext then
// yields the root alternates, which still include the x-default entry.
func seoReq() *http.Request {
	return httptest.NewRequest(http.MethodGet, "/blog/hello", nil)
}

func TestBuildSEO_TitleSuffixing(t *testing.T) {
	s := baseSite()
	got := s.BuildSEO(seoReq(), SEOInput{Title: "Hello World"})
	if got.DocTitle != "Hello World · CMStack" {
		t.Errorf("DocTitle = %q, want %q", got.DocTitle, "Hello World · CMStack")
	}

	// Empty title collapses to just the site name.
	if got := s.BuildSEO(seoReq(), SEOInput{Title: ""}); got.DocTitle != "CMStack" {
		t.Errorf("empty-title DocTitle = %q, want %q", got.DocTitle, "CMStack")
	}
	// Title == SiteName collapses to just the site name (no "CMStack · CMStack").
	if got := s.BuildSEO(seoReq(), SEOInput{Title: "CMStack"}); got.DocTitle != "CMStack" {
		t.Errorf("sitename-title DocTitle = %q, want %q", got.DocTitle, "CMStack")
	}
}

func TestBuildSEO_DescriptionFallsBackToSite(t *testing.T) {
	s := baseSite()
	// Explicit description wins.
	if got := s.BuildSEO(seoReq(), SEOInput{Description: "explicit"}); got.Description != "explicit" {
		t.Errorf("Description = %q, want %q", got.Description, "explicit")
	}
	// Empty description falls back to the site description.
	got := s.BuildSEO(seoReq(), SEOInput{})
	if got.Description != "A server-rendered CMS" {
		t.Errorf("fallback Description = %q, want site description", got.Description)
	}
	if got.OGDescription != "A server-rendered CMS" {
		t.Errorf("OGDescription = %q, want site description", got.OGDescription)
	}
}

func TestBuildSEO_Canonical(t *testing.T) {
	s := baseSite()
	// From a rooted path, absolute-ized against BaseURL (trailing slash trimmed).
	if got := s.BuildSEO(seoReq(), SEOInput{CanonicalPath: "/blog/hello"}); got.Canonical != "https://site.test/blog/hello" {
		t.Errorf("Canonical from path = %q", got.Canonical)
	}
	// Explicit absolute CanonicalURL wins unchanged.
	explicit := "https://other.example/x"
	if got := s.BuildSEO(seoReq(), SEOInput{CanonicalURL: explicit, CanonicalPath: "/ignored"}); got.Canonical != explicit {
		t.Errorf("Canonical explicit = %q, want %q", got.Canonical, explicit)
	}
	// OGURL mirrors Canonical.
	got := s.BuildSEO(seoReq(), SEOInput{CanonicalPath: "/blog/hello"})
	if got.OGURL != got.Canonical {
		t.Errorf("OGURL %q != Canonical %q", got.OGURL, got.Canonical)
	}
}

func TestBuildSEO_Robots(t *testing.T) {
	s := baseSite()
	if got := s.BuildSEO(seoReq(), SEOInput{}); got.Robots != "index, follow" {
		t.Errorf("default Robots = %q", got.Robots)
	}
	if got := s.BuildSEO(seoReq(), SEOInput{NoIndex: true}); got.Robots != "noindex, follow" {
		t.Errorf("noindex Robots = %q", got.Robots)
	}
	// Global noindex forces noindex even when the page allows indexing.
	g := baseSite()
	g.GlobalNoindex = true
	if got := g.BuildSEO(seoReq(), SEOInput{NoIndex: false}); got.Robots != "noindex, follow" {
		t.Errorf("global-noindex Robots = %q", got.Robots)
	}
}

func TestBuildSEO_TwitterCardByImage(t *testing.T) {
	s := baseSite()
	// DefaultOGImage set -> large image card + absolute image URL.
	got := s.BuildSEO(seoReq(), SEOInput{})
	if got.TwitterCard != "summary_large_image" {
		t.Errorf("TwitterCard = %q, want summary_large_image", got.TwitterCard)
	}
	if got.OGImage != "https://site.test/static/og.png" {
		t.Errorf("OGImage = %q, want absolute", got.OGImage)
	}
	if got.TwitterSite != "@cmstack" {
		t.Errorf("TwitterSite = %q", got.TwitterSite)
	}
	// No image -> plain summary card, empty image.
	noImg := baseSite()
	noImg.DefaultOGImage = ""
	got2 := noImg.BuildSEO(seoReq(), SEOInput{})
	if got2.TwitterCard != "summary" || got2.OGImage != "" {
		t.Errorf("no-image card = %q image = %q, want summary / empty", got2.TwitterCard, got2.OGImage)
	}
}

func TestBuildSEO_AlternatesAbsoluteWithXDefault(t *testing.T) {
	s := baseSite()
	got := s.BuildSEO(seoReq(), SEOInput{})
	if len(got.Alternates) == 0 {
		t.Fatal("no alternates emitted")
	}
	sawXDefault := false
	for _, a := range got.Alternates {
		if !strings.HasPrefix(a.Href, "https://site.test") {
			t.Errorf("alternate href not absolute: %q", a.Href)
		}
		if a.Hreflang == "x-default" {
			sawXDefault = true
		}
	}
	if !sawXDefault {
		t.Error("alternates missing x-default entry")
	}
}

func TestBuildSEO_OGTypeAndVerifications(t *testing.T) {
	s := baseSite()
	// OGType defaults to website.
	if got := s.BuildSEO(seoReq(), SEOInput{}); got.OGType != "website" {
		t.Errorf("default OGType = %q", got.OGType)
	}
	if got := s.BuildSEO(seoReq(), SEOInput{OGType: "article"}); got.OGType != "article" {
		t.Errorf("OGType = %q, want article", got.OGType)
	}
	got := s.BuildSEO(seoReq(), SEOInput{})
	if len(got.Verifications) != 1 || got.Verifications[0].Content != "tok" {
		t.Errorf("Verifications = %+v", got.Verifications)
	}
}

// TestNewSiteConfig_VerificationTags asserts the config->SiteConfig mapping emits
// exactly the verification tags whose tokens are set.
func TestNewSiteConfig_VerificationTags(t *testing.T) {
	cfg := config.Config{
		BaseURL:                "https://site.test",
		SiteName:               "CMStack",
		GoogleSiteVerification: "g",
		YandexVerification:     "y",
	}
	sc := NewSiteConfig(cfg)
	if len(sc.Verifications) != 2 {
		t.Fatalf("Verifications len = %d, want 2: %+v", len(sc.Verifications), sc.Verifications)
	}
	names := map[string]string{}
	for _, v := range sc.Verifications {
		names[v.Name] = v.Content
	}
	if names["google-site-verification"] != "g" || names["yandex-verification"] != "y" {
		t.Errorf("unexpected verification tags: %+v", sc.Verifications)
	}
}

// buildSEOEnv mirrors buildPublicEnv but threads a populated SiteConfig so the
// rendered head emits the full SEO block.
func buildSEOEnv(t *testing.T, svc PostPublicService) http.Handler {
	t.Helper()
	sess := session.NewManager(false)
	cat, _ := i18n.LoadCatalog()
	return Router(Deps{
		Config:        config.Config{AppEnv: "test", BaseURL: "https://site.test"},
		Health:        health.NewHandler(health.NewService(nil)),
		Session:       sess,
		AuthMW:        NewAuthMiddleware(sess, fakeUsers{users: map[uuid.UUID]accounts.User{}}, allowAllAuthz{}),
		CSRFFunc:      security.Token,
		PostPublicSvc: svc,
		Authors:       fakeUsers{users: map[uuid.UUID]accounts.User{}},
		SiteName:      "CMStack",
		Site: SiteConfig{
			BaseURL:        "https://site.test",
			SiteName:       "CMStack",
			DefaultOGImage: "/static/og.png",
			TwitterHandle:  "@cmstack",
		},
		Locale: NewLocaleResolver(cat),
	})
}

// TestPublicPost_HeadEmitsSEO asserts the post detail page emits the M8 head:
// canonical link, article og:type, an x-default hreflang link, and a robots meta.
func TestPublicPost_HeadEmitsSEO(t *testing.T) {
	p := posts.Post{
		ID: uuid.New(), Title: "Hello", Slug: "hello", Body: "<p>Body</p>",
		Excerpt: "An excerpt", Status: kernel.StatusPublished, AuthorID: uuid.New(),
		PublishedAt: ptrTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)), ReadingTime: 2,
	}
	r := buildSEOEnv(t, stubPostPublic{bySlug: p})
	req := httptest.NewRequest(http.MethodGet, "/blog/hello", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/blog/hello = %d\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`<link rel="canonical" href="https://site.test/blog/hello"`,
		`property="og:type" content="article"`,
		`hreflang="x-default"`,
		`name="robots" content="index, follow"`,
		`name="twitter:card" content="summary_large_image"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("post head missing %q\n---\n%s", want, body)
		}
	}
}

// TestPublicPost_NoIndexHead asserts a noindex post emits the noindex robots
// directive.
func TestPublicPost_NoIndexHead(t *testing.T) {
	p := posts.Post{
		ID: uuid.New(), Title: "Hidden", Slug: "hidden", Body: "<p>x</p>",
		Status: kernel.StatusPublished, AuthorID: uuid.New(), NoIndex: true,
		PublishedAt: ptrTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
	}
	r := buildSEOEnv(t, stubPostPublic{bySlug: p})
	req := httptest.NewRequest(http.MethodGet, "/blog/hidden", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `name="robots" content="noindex, follow"`) {
		t.Errorf("noindex post did not emit noindex robots:\n%s", rec.Body.String())
	}
}
