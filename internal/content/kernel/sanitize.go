package kernel

import (
	"regexp"
	"sync"

	"github.com/microcosm-cc/bluemonday"
)

// imgDimensionPattern restricts img width/height to plain integers (pixels), so
// the attribute can carry layout dimensions (preventing CLS) without opening an
// injection vector.
var imgDimensionPattern = regexp.MustCompile(`^[0-9]{1,5}$`)

// imgLoadingPattern restricts img loading to the two valid keywords.
var imgLoadingPattern = regexp.MustCompile(`^(lazy|eager)$`)

// sanitizerOnce builds the rich-text policy exactly once. The policy is
// immutable and safe for concurrent use, so a single shared instance is reused
// for every sanitize call.
var (
	sanitizerOnce sync.Once
	richTextPol   *bluemonday.Policy
)

// richTextPolicy returns the shared bluemonday policy for editorial rich text.
// The allowlist is deliberately narrow: only the tags the self-hosted editor
// emits are permitted; scripts, svg, event-handler attributes, and dangerous
// URL schemes (javascript:, data:) are stripped. img is restricted to http(s)
// so a data:image/svg+xml payload cannot smuggle markup. Links are forced to
// rel="nofollow noopener noreferrer".
func richTextPolicy() *bluemonday.Policy {
	sanitizerOnce.Do(func() {
		p := bluemonday.NewPolicy()

		// Block-level + inline editorial tags.
		p.AllowElements(
			"p", "br", "hr",
			"h2", "h3", "h4",
			"ul", "ol", "li",
			"blockquote",
			"pre", "code",
			"strong", "em", "u", "s",
			"mark", "sup", "sub",
			"img",
		)

		// Links: href limited to http/https/mailto; force safe rel; allow target.
		p.AllowAttrs("href").OnElements("a")
		p.AllowAttrs("title").OnElements("a")
		p.AllowStandardURLs()                        // strips javascript:/vbscript: etc.
		p.AllowURLSchemes("http", "https", "mailto") // explicit allowlist
		p.RequireNoFollowOnLinks(true)
		p.AddTargetBlankToFullyQualifiedLinks(true)
		p.RequireNoReferrerOnLinks(true)

		// Images: src (http/https only via the URL scheme rule above) + alt, plus
		// intrinsic dimensions and native lazy-loading so content images avoid
		// layout shift and defer offscreen loads. width/height are integer-only and
		// loading is keyword-only, so none can carry an injection payload.
		p.AllowAttrs("src", "alt").OnElements("img")
		p.AllowAttrs("alt").OnElements("img")
		p.AllowAttrs("width", "height").Matching(imgDimensionPattern).OnElements("img")
		p.AllowAttrs("loading").Matching(imgLoadingPattern).OnElements("img")

		// Code blocks may carry a language-* class for syntax highlighting.
		p.AllowAttrs("class").Matching(bluemonday.SpaceSeparatedTokens).OnElements("code", "pre")

		richTextPol = p
	})
	return richTextPol
}

// SanitizeRichText strips every disallowed tag/attribute/scheme from html,
// returning markup that is safe to render verbatim. It is applied server-side on
// EVERY content save — the rendered output trusts this guarantee.
func SanitizeRichText(html string) string {
	return richTextPolicy().Sanitize(html)
}

// plainTextOnce builds the strip-everything policy once.
var (
	plainTextOnce sync.Once
	plainTextPol  *bluemonday.Policy
)

// SanitizePlainText removes ALL markup, returning text-only content. It is used
// for plain fields (e.g. a service summary) that are rendered as text — any tags
// in the input are stripped defensively so a value rendered verbatim elsewhere
// can never carry markup.
func SanitizePlainText(s string) string {
	plainTextOnce.Do(func() {
		plainTextPol = bluemonday.StrictPolicy()
	})
	return plainTextPol.Sanitize(s)
}
