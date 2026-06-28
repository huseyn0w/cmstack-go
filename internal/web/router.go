// Package web assembles the HTTP router and middleware chain. Wiring is
// explicit: dependencies are passed in via Deps; there is no global state.
package web

import (
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/health"
	"github.com/huseyn0w/cmstack-go/internal/platform/config"
	"github.com/huseyn0w/cmstack-go/internal/platform/events"
	"github.com/huseyn0w/cmstack-go/internal/platform/ratelimit"
	"github.com/huseyn0w/cmstack-go/internal/platform/render"
	"github.com/huseyn0w/cmstack-go/internal/platform/security"
	"github.com/huseyn0w/cmstack-go/internal/platform/session"
	webtempl "github.com/huseyn0w/cmstack-go/web/templ"
)

// Deps are the explicit dependencies the router needs. Services are injected so
// the router owns no construction logic.
type Deps struct {
	Config config.Config
	Health *health.Handler
	// Bus is the domain event bus. It is injected explicitly even though no
	// domain listeners exist yet in M0, so handlers added in later milestones
	// publish through the same wired instance.
	Bus           *events.Bus
	Session       *scs.SessionManager
	StaticDir     string // filesystem path to web/static (served at /static)
	LoggerHandler func(http.Handler) http.Handler

	// Auth wiring (M1). All optional so M0-style tests that only exercise
	// health/home keep working with a zero Deps.
	Auth     *accounts.Handler // thin auth HTTP boundary
	AuthMW   *AuthMiddleware   // session/auth/permission middleware
	CSRFFunc func(*http.Request) string
}

// Router builds the chi router with the full middleware chain and mounts all
// routes. The order matters: requestID and real-IP first so downstream logging
// and recovery see them; security headers early; CSRF and session around the
// dynamic routes.
func Router(d Deps) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	// NOTE: chi's middleware.RealIP is intentionally NOT used. It trusts
	// client-supplied X-Forwarded-For / X-Real-IP headers unconditionally,
	// which is spoofable (GHSA-3fxj-6jh8-hvhx). Real client IP resolution will
	// be handled explicitly behind the known reverse proxy when one is in front
	// of the app; until then r.RemoteAddr is the honest source and is logged.
	if d.LoggerHandler != nil {
		r.Use(d.LoggerHandler)
	} else {
		r.Use(middleware.Logger)
	}
	r.Use(middleware.Recoverer)
	r.Use(security.Headers(d.Config.IsProduction()))

	// Static assets — no CSRF/session needed.
	if d.StaticDir != "" {
		fs := http.StripPrefix("/static/", http.FileServer(http.Dir(d.StaticDir)))
		r.Handle("/static/*", fs)
	}

	// Health endpoints — no session/CSRF (probed by orchestrators).
	if d.Health != nil {
		d.Health.Routes(r)
	}

	// Application routes carry session + CSRF.
	r.Group(func(gr chi.Router) {
		if d.Session != nil {
			gr.Use(session.Middleware(d.Session))
		}
		gr.Use(security.CSRF(d.Config.IsProduction()))
		// CurrentUser loads the authenticated user into context for downstream
		// auth/permission middleware and handlers.
		if d.AuthMW != nil {
			gr.Use(d.AuthMW.CurrentUser)
		}

		// TODO(M1-ext): replace this inline closure with a real home handler in
		// its own package once the content domain exists.
		gr.Get("/", func(w http.ResponseWriter, req *http.Request) {
			if err := render.Component(req.Context(), w, http.StatusOK, webtempl.Home()); err != nil {
				http.Error(w, "render error", http.StatusInternalServerError)
			}
		})

		mountAuthRoutes(gr, d)
	})

	return r
}

// mountAuthRoutes wires the authentication pages, their guest/auth gates, and
// per-IP rate limiting on the sensitive POST endpoints. It is a no-op when auth
// is not wired (M0-style tests).
func mountAuthRoutes(gr chi.Router, d Deps) {
	if d.Auth == nil || d.AuthMW == nil {
		return
	}

	// Per-IP token-bucket limiter for credential endpoints: 5 burst, refill
	// 1 per 6s (≈10/min). In-proc now; Redis-backed option noted for M13.
	limiter := ratelimit.New(1.0/6.0, 5)

	// Guest-only auth pages (login, signup, forgot, reset). Authenticated users
	// are redirected to /admin by RequireGuest.
	gr.Group(func(g chi.Router) {
		g.Use(d.AuthMW.RequireGuest)

		g.Get("/login", d.Auth.ShowLogin)
		g.Get("/signup", d.Auth.ShowSignup)
		g.Get("/forgot-password", d.Auth.ShowForgotPassword)
		g.Get("/reset-password", d.Auth.ShowResetPassword)

		// Rate-limited POSTs.
		g.Group(func(rl chi.Router) {
			rl.Use(limiter.Middleware)
			rl.Post("/login", d.Auth.SubmitLogin)
			rl.Post("/signup", d.Auth.SubmitSignup)
			rl.Post("/forgot-password", d.Auth.SubmitForgotPassword)
			rl.Post("/reset-password", d.Auth.SubmitResetPassword)
		})
	})

	// Email verification is reachable whether or not signed in.
	gr.Get("/verify-email", d.Auth.VerifyEmail)

	// Logout requires an active session.
	gr.With(d.AuthMW.RequireAuth).Post("/logout", d.Auth.SubmitLogout)

	// Authenticated admin stub (full shell = M1-ext).
	gr.With(d.AuthMW.RequireAuth).Get("/admin", func(w http.ResponseWriter, req *http.Request) {
		u, _ := UserFromContext(req.Context())
		name := u.Name
		if name == "" {
			name = u.Email
		}
		csrf := ""
		if d.CSRFFunc != nil {
			csrf = d.CSRFFunc(req)
		}
		if err := render.Component(req.Context(), w, http.StatusOK, webtempl.AdminStub(name, csrf)); err != nil {
			http.Error(w, "render error", http.StatusInternalServerError)
		}
	})
}

// StaticDirDefault returns the conventional location of the static assets
// directory, relative to the process working directory.
func StaticDirDefault() string {
	return "web/static"
}
