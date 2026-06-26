// Package security provides HTTP security middleware: response hardening
// headers and CSRF protection.
package security

import (
	"log/slog"
	"net/http"

	"github.com/justinas/nosurf"
)

// DefaultCSP is the baseline Content-Security-Policy shipped by Headers. It
// assumes htmx and Alpine are self-hosted (script-src 'self').
//
// 'unsafe-eval' is required because the vendored Alpine v3 build evaluates
// x-data / x-on expressions via the Function constructor. The alternative is
// Alpine's dedicated CSP build (which forbids eval); until that is vendored we
// allow 'unsafe-eval'.
// TODO(M1): vendor Alpine's CSP build and drop 'unsafe-eval' from script-src.
//
// style-src allows 'unsafe-inline' for Tailwind/inline styles; img-src allows
// data: URIs; everything else is locked to 'self' with framing and plugins
// denied.
const DefaultCSP = "default-src 'self'; " +
	"script-src 'self' 'unsafe-eval'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; " +
	"font-src 'self'; " +
	"connect-src 'self'; " +
	"object-src 'none'; " +
	"base-uri 'self'; " +
	"frame-ancestors 'none'"

// Headers returns middleware that sets baseline security response headers,
// including a Content-Security-Policy. Pass csp to override DefaultCSP; an empty
// string falls back to DefaultCSP so callers get a working default.
//
// HSTS is only emitted in production, where TLS is terminated upstream; sending
// it in development would poison local http://localhost browsing.
func Headers(production bool, csp ...string) func(http.Handler) http.Handler {
	policy := DefaultCSP
	if len(csp) > 0 && csp[0] != "" {
		policy = csp[0]
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			h.Set("X-XSS-Protection", "0")
			h.Set("Content-Security-Policy", policy)
			if production {
				h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CSRF returns nossurf-based CSRF protection middleware. nosurf is chosen over
// gorilla/csrf because gorilla/csrf is archived/unmaintained, while nosurf is
// actively maintained, dependency-free, and cookie-based (no session coupling).
func CSRF(production bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		h := nosurf.New(next)
		h.SetBaseCookie(http.Cookie{
			Path:     "/",
			HttpOnly: true,
			Secure:   production,
			SameSite: http.SameSiteLaxMode,
		})
		// Log CSRF rejections instead of failing silently, then emit nosurf's
		// default 400 response.
		h.SetFailureHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			slog.Warn("csrf rejection",
				"method", r.Method,
				"path", r.URL.Path,
				"remote_addr", r.RemoteAddr,
				"reason", nosurf.Reason(r),
			)
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		}))
		return h
	}
}

// Token returns the CSRF token for the current request, for embedding in forms.
func Token(r *http.Request) string { return nosurf.Token(r) }
