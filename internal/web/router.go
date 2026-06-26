// Package web assembles the HTTP router and middleware chain. Wiring is
// explicit: dependencies are passed in via Deps; there is no global state.
package web

import (
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/huseyn0w/cmstack-go/internal/health"
	"github.com/huseyn0w/cmstack-go/internal/platform/config"
	"github.com/huseyn0w/cmstack-go/internal/platform/events"
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

		// TODO(M1): replace this inline closure with a real home handler in
		// its own package once the content domain exists.
		gr.Get("/", func(w http.ResponseWriter, req *http.Request) {
			if err := render.Component(req.Context(), w, http.StatusOK, webtempl.Home()); err != nil {
				http.Error(w, "render error", http.StatusInternalServerError)
			}
		})
	})

	return r
}

// StaticDirDefault returns the conventional location of the static assets
// directory, relative to the process working directory.
func StaticDirDefault() string {
	return "web/static"
}
