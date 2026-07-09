package templ

import "context"

// AnalyticsSnippets carries the ALREADY-VALIDATED analytics container ids for a
// request. Each id has been validated by the web layer against a strict
// allow-list charset (GA4: `^(G|GT|AW|DC)-[A-Z0-9_-]{4,20}$`, GTM:
// `^GTM-[A-Z0-9]{4,10}$`) before being placed here, so string-building the
// snippets is safe. An empty field means "omit that snippet" (disabled).
type AnalyticsSnippets struct {
	// GA4ID is the validated gtag.js measurement/tag id (e.g. "G-ABC1234"), or
	// "" when GA4 is disabled.
	GA4ID string
	// GTMID is the validated Google Tag Manager container id (e.g. "GTM-ABCD12"),
	// or "" when GTM is disabled.
	GTMID string
}

// analyticsSource is satisfied by the web package's context accessor. It is an
// interface (rather than a direct import) so the templ package does not import
// the web package, avoiding an import cycle. The web package registers its
// accessor via SetAnalyticsSource at init, mirroring SetThemeSource.
type analyticsSource interface {
	Snippets(ctx context.Context) AnalyticsSnippets
}

// analyticsSrc is the registered accessor; nil until the web package wires it.
var analyticsSrc analyticsSource

// SetAnalyticsSource registers the context accessor used by AnalyticsHead. The
// web package calls this from an init function so the public layout can read the
// per-request validated analytics ids without importing web (which would cycle).
// When no source is registered the accessor stays nil and AnalyticsHead yields
// the zero value (analytics disabled).
func SetAnalyticsSource(s analyticsSource) { analyticsSrc = s }

// AnalyticsHead returns the validated analytics snippets for the request, or the
// zero value (both ids empty = disabled) when no source is registered or the
// analytics middleware did not run. Because admin routes never run the
// middleware, Snippets there returns the zero value and nothing is emitted.
func AnalyticsHead(ctx context.Context) AnalyticsSnippets {
	if analyticsSrc == nil {
		return AnalyticsSnippets{}
	}
	return analyticsSrc.Snippets(ctx)
}

// ga4Head builds the gtag.js head snippet for a validated GA4 id: the async
// loader <script> plus the inline gtag() bootstrap. The id is guaranteed to
// match `^(G|GT|AW|DC)-[A-Z0-9_-]{4,20}$` by the web layer, so it contains only
// characters that are inert inside both an HTML attribute value and a JS string
// literal (no quotes, angle brackets, whitespace, or backslashes). String
// concatenation is therefore injection-safe here. (If the id source ever
// weakens, prefer templ.JSONString for the JS-literal use and templ.EscapeString
// / an attribute-context escape for the URL.)
func ga4Head(id string) string {
	return `<script async src="https://www.googletagmanager.com/gtag/js?id=` + id + `"></script>` +
		`<script>window.dataLayer=window.dataLayer||[];function gtag(){dataLayer.push(arguments);}` +
		`gtag('js',new Date());gtag('config','` + id + `');</script>`
}

// gtmHead builds the Google Tag Manager head <script> snippet for a validated
// GTM container id. The id is guaranteed to match `^GTM-[A-Z0-9]{4,10}$`, an
// even stricter charset than GA4, so string concatenation is injection-safe.
func gtmHead(id string) string {
	return `<script>(function(w,d,s,l,i){w[l]=w[l]||[];w[l].push({'gtm.start':` +
		`new Date().getTime(),event:'gtm.js'});var f=d.getElementsByTagName(s)[0],` +
		`j=d.createElement(s),dl=l!='dataLayer'?'&l='+l:'';j.async=true;` +
		`j.src='https://www.googletagmanager.com/gtm.js?id='+i+dl;` +
		`f.parentNode.insertBefore(j,f);})(window,document,'script','dataLayer','` + id + `');</script>`
}

// gtmBodyStart builds the Google Tag Manager <noscript> fallback iframe that GTM
// requires immediately after <body> opens. The id is validated (see gtmHead), so
// concatenation into the iframe src is injection-safe.
func gtmBodyStart(id string) string {
	return `<noscript><iframe src="https://www.googletagmanager.com/ns.html?id=` + id +
		`" height="0" width="0" style="display:none;visibility:hidden"></iframe></noscript>`
}
