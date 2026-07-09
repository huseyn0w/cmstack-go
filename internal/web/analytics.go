package web

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"

	webtempl "github.com/huseyn0w/cmstack-go/web/templ"
)

// Analytics settings keys (M15-1). The container ids are settings-backed
// (admin-editable in a later slice) and default to empty = analytics disabled.
const (
	// keyAnalyticsGA4ID holds the Google Analytics 4 (gtag.js) measurement/tag id.
	keyAnalyticsGA4ID = "analytics_ga4_id"
	// keyAnalyticsGTMID holds the Google Tag Manager container id.
	keyAnalyticsGTMID = "analytics_gtm_id"
)

// ga4IDPattern validates a GA4 / gtag.js id. gtag.js accepts several id prefixes
// (G- measurement, GT- tag, AW- Ads, DC- Floodlight); the body is a strict
// upper-alnum/underscore/hyphen allow-list, so a validated id is inert inside
// both an HTML attribute and a JS string literal (no quotes/brackets/spaces).
var ga4IDPattern = regexp.MustCompile(`^(G|GT|AW|DC)-[A-Z0-9_-]{4,20}$`)

// gtmIDPattern validates a Google Tag Manager container id (GTM- + upper-alnum).
var gtmIDPattern = regexp.MustCompile(`^GTM-[A-Z0-9]{4,10}$`)

// AnalyticsSettings is the narrow settings dependency the analytics middleware
// needs: read a raw value by key. *settings.Service satisfies it. Declaring it
// here keeps web decoupled from the settings package and trivially fakeable in
// tests.
type AnalyticsSettings interface {
	// Get returns the value stored under key. The boolean is false when the key
	// is unset; a non-nil error signals a store failure.
	Get(ctx context.Context, key string) (string, bool, error)
}

// validateGA4ID returns the id unchanged when it matches ga4IDPattern, else "".
// Invalid/empty ids are dropped (treated as disabled) so nothing unsafe is ever
// emitted into the page.
func validateGA4ID(id string) string {
	if ga4IDPattern.MatchString(id) {
		return id
	}
	return ""
}

// validateGTMID returns the id unchanged when it matches gtmIDPattern, else "".
func validateGTMID(id string) string {
	if gtmIDPattern.MatchString(id) {
		return id
	}
	return ""
}

// AnalyticsMiddleware builds the public analytics middleware (M15-1). It reads
// the GA4 + GTM ids from the settings store, VALIDATES each against its strict
// pattern (invalid/empty ids dropped = disabled), and stores the resulting
// validated snippets in the request context for the layout to emit. It runs on
// the PUBLIC route group only; admin routes never run it, so the analytics
// context is absent there and nothing is emitted (public-only isolation). A
// settings read error degrades to disabled (logged at warn) rather than failing
// the request.
func AnalyticsMiddleware(svc AnalyticsSettings) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			snips := webtempl.AnalyticsSnippets{
				GA4ID: validateGA4ID(readSetting(r.Context(), svc, keyAnalyticsGA4ID)),
				GTMID: validateGTMID(readSetting(r.Context(), svc, keyAnalyticsGTMID)),
			}
			ctx := withAnalytics(r.Context(), snips)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// readSetting returns the raw value for key, or "" on an unset key or store
// error. A store error is logged at warn and treated as disabled so a transient
// settings failure never breaks a public page render.
func readSetting(ctx context.Context, svc AnalyticsSettings, key string) string {
	v, ok, err := svc.Get(ctx, key)
	if err != nil {
		slog.WarnContext(ctx, "analytics: settings read failed; disabling", "key", key, "err", err)
		return ""
	}
	if !ok {
		return ""
	}
	return v
}

// analyticsViewSource adapts the web package's analytics context accessor to the
// templ package's analyticsSource interface, so the public layout can read the
// validated snippets without importing web (which would cycle). It mirrors
// themeViewSource. Snippets returns the zero value when the middleware did not
// run (admin routes), so those pages emit no analytics.
type analyticsViewSource struct{}

// Snippets returns the validated analytics snippets stored by the middleware, or
// the zero value (disabled) when it did not run for the request.
func (analyticsViewSource) Snippets(ctx context.Context) webtempl.AnalyticsSnippets {
	return analyticsFromContext(ctx)
}
