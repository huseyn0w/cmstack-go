package web

import (
	"net/http"
	"strings"

	"github.com/huseyn0w/cmstack-go/internal/platform/config"
	webtempl "github.com/huseyn0w/cmstack-go/web/templ"
)

// SiteConfig is the site-identity + SEO configuration threaded to the public
// handlers. It is populated once from config in main.go and carried on web.Deps.
type SiteConfig struct {
	BaseURL         string
	SiteName        string
	SiteDescription string
	DefaultOGImage  string
	TwitterHandle   string
	GlobalNoindex   bool
	// AllowAICrawlers governs whether robots.txt permits the well-known AI
	// crawlers (M8). When false, robots.txt emits an explicit Disallow: / block
	// per AI user-agent; when true (default) they are not blocked.
	AllowAICrawlers bool
	// Verifications are the search-engine verification meta tags emitted in the
	// head (Google/Bing/Yandex/Pinterest, whichever are configured).
	Verifications []webtempl.MetaTag
	// Org is the site publisher's business identity, used to emit the
	// Organization JSON-LD (home page) and as the `publisher` node in
	// BlogPosting. Rooted logo paths are absolutized against BaseURL.
	Org webtempl.OrgIdentity
}

// NewSiteConfig builds the SiteConfig from the loaded app config, assembling the
// verification meta tags from whichever tokens are set.
func NewSiteConfig(cfg config.Config) SiteConfig {
	var verifications []webtempl.MetaTag
	add := func(name, content string) {
		if content != "" {
			verifications = append(verifications, webtempl.MetaTag{Name: name, Content: content})
		}
	}
	add("google-site-verification", cfg.GoogleSiteVerification)
	add("msvalidate.01", cfg.BingSiteVerification)
	add("yandex-verification", cfg.YandexVerification)
	add("p:domain_verify", cfg.PinterestVerification)

	s := SiteConfig{
		BaseURL:         cfg.BaseURL,
		SiteName:        cfg.SiteName,
		SiteDescription: cfg.SiteDescription,
		DefaultOGImage:  cfg.DefaultOGImage,
		TwitterHandle:   cfg.TwitterHandle,
		GlobalNoindex:   cfg.GlobalNoindex,
		AllowAICrawlers: cfg.AllowAICrawlers,
		Verifications:   verifications,
	}

	orgName := cfg.OrgName
	if orgName == "" {
		orgName = cfg.SiteName
	}
	s.Org = webtempl.OrgIdentity{
		Name:         orgName,
		LegalName:    cfg.OrgLegalName,
		LogoURL:      s.absolutizeIfRooted(cfg.OrgLogo),
		Email:        cfg.OrgEmail,
		Phone:        cfg.OrgPhone,
		Street:       cfg.OrgStreet,
		Locality:     cfg.OrgLocality,
		Region:       cfg.OrgRegion,
		PostalCode:   cfg.OrgPostalCode,
		Country:      cfg.OrgCountry,
		URL:          strings.TrimSuffix(cfg.BaseURL, "/"),
		SameAs:       cfg.SameAs,
		GeoStatement: cfg.GeoStatement,
	}
	return s
}

// OrganizationJSONLD returns the site publisher's Organization JSON-LD (empty
// when no org name is configured — which cannot happen since Name falls back to
// SiteName). Exposed so the home handler can emit it.
func (s SiteConfig) OrganizationJSONLD() string {
	if s.Org.Name == "" {
		return ""
	}
	return webtempl.OrganizationJSONLD(s.Org)
}

// WebSiteJSONLD returns the WebSite JSON-LD for the home page, wiring the
// Sitelinks SearchAction to the site's /search endpoint.
func (s SiteConfig) WebSiteJSONLD() string {
	home := strings.TrimSuffix(s.BaseURL, "/")
	name := s.SiteName
	if name == "" {
		name = s.Org.Name
	}
	return webtempl.WebSiteJSONLD(name, home, home+"/search?q={search_term_string}")
}

// homeJSONLD returns the site-level JSON-LD blocks emitted on the home page:
// Organization (publisher) + WebSite (with SearchAction).
func (s SiteConfig) homeJSONLD() []string {
	return []string{s.OrganizationJSONLD(), s.WebSiteJSONLD()}
}

// SEOInput is the per-page seed the caller passes to BuildSEO. Title/Description
// are the already-resolved page values (meta||fallback); CanonicalURL wins over
// CanonicalPath when non-empty; NoIndex forces a page-level noindex; OGType
// selects the Open Graph type ("website" default, "article" for posts).
type SEOInput struct {
	Title         string
	Description   string
	CanonicalPath string
	CanonicalURL  string
	NoIndex       bool
	OGType        string
}

// BuildSEO resolves a per-page SEOView from the site config + the request +
// per-page input: it computes the document title, description (with the site
// fallback), the absolute canonical URL, the robots directive, the Open Graph +
// Twitter Card blocks, the per-locale hreflang alternates (absolute, incl.
// x-default), and the verification tags.
func (s SiteConfig) BuildSEO(r *http.Request, in SEOInput) *webtempl.SEOView {
	title := in.Title
	docTitle := s.SiteName
	if title != "" && title != s.SiteName {
		docTitle = title + " · " + s.SiteName
	}

	description := in.Description
	if description == "" {
		description = s.SiteDescription
	}

	canonical := in.CanonicalURL
	if canonical == "" {
		canonical = s.absolute(in.CanonicalPath)
	} else {
		canonical = s.absolutizeIfRooted(canonical)
	}

	robots := "index, follow"
	if in.NoIndex || s.GlobalNoindex {
		robots = "noindex, follow"
	}

	ogType := in.OGType
	if ogType == "" {
		ogType = "website"
	}
	ogTitle := title
	if ogTitle == "" {
		ogTitle = s.SiteName
	}
	ogImage := s.absolutizeIfRooted(s.DefaultOGImage)

	twitterCard := "summary"
	if ogImage != "" {
		twitterCard = "summary_large_image"
	}

	return &webtempl.SEOView{
		DocTitle:      docTitle,
		Description:   description,
		Canonical:     canonical,
		Robots:        robots,
		OGType:        ogType,
		OGTitle:       ogTitle,
		OGDescription: description,
		OGImage:       ogImage,
		OGURL:         canonical,
		OGSiteName:    s.SiteName,
		TwitterCard:   twitterCard,
		TwitterSite:   s.TwitterHandle,
		Alternates:    s.alternates(r),
		Verifications: s.Verifications,
	}
}

// alternates maps the request's per-locale alternate PATHS onto absolute
// hreflang links (incl. the x-default entry).
func (s SiteConfig) alternates(r *http.Request) []webtempl.HreflangLink {
	alts := AlternatesFromContext(r.Context())
	out := make([]webtempl.HreflangLink, 0, len(alts))
	for _, a := range alts {
		out = append(out, webtempl.HreflangLink{
			Hreflang: a.Hreflang,
			Href:     s.absolute(a.URL),
		})
	}
	return out
}

// absolute joins a rooted path onto BaseURL, trimming exactly one trailing slash
// from BaseURL so the result has no double slash. An empty path yields BaseURL.
func (s SiteConfig) absolute(path string) string {
	base := strings.TrimSuffix(s.BaseURL, "/")
	if path == "" {
		return base
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

// absolutizeIfRooted returns v unchanged when it is already absolute (has a
// scheme) or empty; a rooted path is resolved against BaseURL.
func (s SiteConfig) absolutizeIfRooted(v string) string {
	if v == "" {
		return ""
	}
	if strings.Contains(v, "://") {
		return v
	}
	return s.absolute(v)
}
