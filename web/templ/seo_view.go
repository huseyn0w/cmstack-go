package templ

// MetaTag is a generic name/content <meta> pair (used for verification tags).
type MetaTag struct {
	Name    string
	Content string
}

// HreflangLink is one <link rel="alternate" hreflang> entry: the hreflang value
// ("x-default" for the default locale) and an ABSOLUTE href.
type HreflangLink struct {
	Hreflang string
	Href     string
}

// SEOView is the resolved per-page document-head view-model. It is built by the
// web package's SEO builder and carried (optionally) on LayoutData so the base
// layout can emit canonical/robots/Open Graph/Twitter/hreflang/verification tags.
// The type lives in the templ package (like LayoutData) so templates can
// reference it without importing web (which would cycle).
type SEOView struct {
	// DocTitle is the full <title> text.
	DocTitle string
	// Description is the meta description (and OG/Twitter description source).
	Description string
	// Canonical is the absolute canonical URL.
	Canonical string
	// Robots is the robots directive ("index, follow" | "noindex, follow").
	Robots string
	// OGType is the Open Graph type ("website" | "article").
	OGType string
	// OGTitle is the Open Graph title.
	OGTitle string
	// OGDescription is the Open Graph description.
	OGDescription string
	// OGImage is the absolute Open Graph image URL; may be empty.
	OGImage string
	// OGURL is the Open Graph URL (equals Canonical).
	OGURL string
	// OGSiteName is the Open Graph site name.
	OGSiteName string
	// TwitterCard is the Twitter card type ("summary_large_image" | "summary").
	TwitterCard string
	// TwitterSite is the site's Twitter/X handle (e.g. "@agentic-cms"); may be empty.
	TwitterSite string
	// Alternates lists the per-locale hreflang links (absolute hrefs, incl.
	// x-default).
	Alternates []HreflangLink
	// Verifications lists search-engine verification meta tags.
	Verifications []MetaTag
}
