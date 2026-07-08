package web

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/huseyn0w/cmstack-go/internal/platform/i18n"
)

// AdminLocaleHandler sets the operator-chosen admin UI language via a cookie and
// redirects back to the page the switch was triggered from. It is the runtime
// counterpart to the public URL-prefix switcher: admin surfaces are unprefixed,
// so their language lives in the `admin_locale` cookie (read by the locale
// middleware for admin paths). It performs no rendering.
type AdminLocaleHandler struct {
	// secure marks the cookie Secure in production (HTTPS-only).
	secure bool
}

// NewAdminLocaleHandler constructs the handler. secure should be true in
// production so the cookie is only sent over HTTPS.
func NewAdminLocaleHandler(secure bool) *AdminLocaleHandler {
	return &AdminLocaleHandler{secure: secure}
}

// Set handles GET /admin/locale/{code}. It validates {code} against the
// supported locales, persists it in the admin_locale cookie, and redirects back
// to a safe local `next` path (defaulting to /admin). An unsupported code is a
// no-op on the cookie but still redirects, so the switcher can never 500.
func (h *AdminLocaleHandler) Set(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	if loc, ok := i18n.Parse(code); ok {
		http.SetCookie(w, &http.Cookie{
			Name:     adminLocaleCookie,
			Value:    loc.String(),
			Path:     "/",
			MaxAge:   31536000, // 1 year
			HttpOnly: true,
			Secure:   h.secure,
			SameSite: http.SameSiteLaxMode,
		})
	}
	http.Redirect(w, r, safeNext(r.URL.Query().Get("next")), http.StatusSeeOther)
}

// safeNext sanitizes the post-switch redirect target: only a same-origin,
// rooted path is allowed (open-redirect guard). Anything else (absolute URL,
// protocol-relative "//host", empty) falls back to /admin.
func safeNext(next string) string {
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		return "/admin"
	}
	return next
}
