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

	// Admin shell wiring (M1-ext). Authz filters the sidebar per user; Roles
	// resolves the role-label badge. Both optional (admin falls back to a stub).
	Authz PermissionChecker
	Roles RoleResolver

	// OAuth (social login, M1-ext). OAuth is the thin handler; it is mounted only
	// when non-nil (providers configured).
	OAuth *accounts.OAuthHandler

	// Profiles (M1-ext). Account is the self-service /account editor (auth-gated);
	// Author is the public /authors/{id} page; Uploads serves stored avatars.
	// All optional so reduced-Deps tests keep working.
	Account *AccountHandler
	Author  *AuthorHandler
	Uploads http.Handler // mounted at UploadsPrefix when non-nil

	// Posts (M2a). PostAdmin is the gated admin posts area; PostPublic is the
	// public /blog. Authors resolves author display names for both. All optional.
	PostAdminSvc  PostAdminService
	PostPublicSvc PostPublicService
	Authors       AuthorNamer
	SiteName      string

	// Pages (M2b). PageAdmin is the gated admin pages area; PagePublic renders a
	// page at /p/{slug}. All optional.
	PageAdminSvc  PageAdminService
	PagePublicSvc PagePublicService

	// Services (M2b). ServiceAdmin is the gated admin services area; ServicePublic
	// is the public /services index + detail. All optional.
	ServiceAdminSvc  ServiceAdminService
	ServicePublicSvc ServicePublicService

	// Taxonomies (M3). Category/Tag admin areas (gated by the `category`/`tag`
	// subjects) and the public archives. The post handlers gain taxonomy
	// selectors/pills when the read services below are wired. All optional.
	CategoryAdminSvc  CategoryAdminService
	CategoryReadSvc   CategoryReader        // post editor tree + per-post reads
	CategoryPublicSvc CategoryPublicService // public archive
	CategoryPostSvc   PostTaxonomyReader    // public detail pills
	TagAdminSvc       TagAdminService
	TagReadSvc        TagReader // post editor flat list + per-post reads
	TagPublicSvc      TagPublicService
	TagPostSvc        PostTagReader // public detail pills
	PostHydrateSvc    PostHydrator  // archive id->post hydration

	// Media (M4). MediaAdminSvc is the gated admin media library; the editor
	// picker reuses the same service via the post/page/service editors. Optional.
	MediaAdminSvc MediaAdminService

	// UploadsPrefix is the URL prefix the uploads handler is mounted at (e.g.
	// "/uploads"); defaults to "/uploads".
	UploadsPrefix string
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

	// User-uploaded files (avatars now). Served outside the CSRF/session group
	// like static assets; the storage handler sets X-Content-Type-Options:
	// nosniff and a locked-down CSP so a stored blob can never execute.
	if d.Uploads != nil {
		prefix := d.UploadsPrefix
		if prefix == "" {
			prefix = "/uploads"
		}
		r.Handle(prefix+"/*", d.Uploads)
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

		// Public author profile page (no auth) — anyone may view it.
		if d.Author != nil {
			gr.Get("/authors/{id}", d.Author.Show)
		}

		// Public blog (no auth for read). Liking requires an authenticated user.
		if d.PostPublicSvc != nil {
			pub := NewPostPublicHandler(d.PostPublicSvc, d.Authors, d.SiteName, d.Config.BaseURL, d.CSRFFunc)
			if d.CategoryPostSvc != nil || d.TagPostSvc != nil {
				pub.WithTaxonomy(d.CategoryPostSvc, d.TagPostSvc)
			}
			gr.Get("/blog", pub.Index)
			gr.Get("/blog/{slug}", pub.Show)
			if d.AuthMW != nil {
				gr.With(d.AuthMW.RequireAuth).Post("/blog/{slug}/like", pub.Like)
				gr.With(d.AuthMW.RequireAuth).Post("/blog/{slug}/unlike", pub.Unlike)
			}
		}

		// Public taxonomy archives (no auth): /categories/{slug} + /tags/{slug}.
		if d.PostHydrateSvc != nil && (d.CategoryPublicSvc != nil || d.TagPublicSvc != nil) {
			tax := NewTaxonomyPublicHandler(d.CategoryPublicSvc, d.TagPublicSvc, d.PostHydrateSvc, d.Authors, d.SiteName)
			if d.CategoryPublicSvc != nil {
				gr.Get("/categories/{slug}", tax.ShowCategory)
			}
			if d.TagPublicSvc != nil {
				gr.Get("/tags/{slug}", tax.ShowTag)
			}
		}

		// Public pages (no auth). A published page renders at /p/{slug}; the
		// hierarchy drives the breadcrumb trail.
		if d.PagePublicSvc != nil {
			pp := NewPagePublicHandler(d.PagePublicSvc, d.SiteName, d.Config.BaseURL)
			gr.Get("/p/{slug}", pp.Show)
		}

		// Public services (no auth): /services index + /services/{slug} detail.
		if d.ServicePublicSvc != nil {
			sp := NewServicePublicHandler(d.ServicePublicSvc, d.SiteName, d.Config.BaseURL)
			gr.Get("/services", sp.Index)
			gr.Get("/services/{slug}", sp.Show)
		}

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

	// Social login (OAuth). Mounted only when providers are configured. State /
	// CSRF is handled by goth; these routes carry the application session so the
	// callback can establish login via the same path as password auth.
	if d.OAuth != nil {
		gr.Get("/auth/{provider}", d.OAuth.Begin)
		gr.Get("/auth/{provider}/callback", d.OAuth.Callback)
	}

	mountAdmin(gr, d)
}

// mountAdmin mounts the authenticated admin area under RequireAuth. The shell
// itself performs per-item permission gating (hidden, not disabled); resource
// routes arrive in later milestones. When the shell deps are not wired it falls
// back to the M1-core stub so reduced-Deps tests keep working.
func mountAdmin(gr chi.Router, d Deps) {
	if d.Authz == nil || d.Roles == nil {
		// Fallback stub (used by reduced-Deps router tests).
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
		return
	}

	shell := adminShellDeps{
		authz:   d.Authz,
		roles:   d.Roles,
		csrf:    d.CSRFFunc,
		siteURL: d.Config.BaseURL,
	}
	gr.With(d.AuthMW.RequireAuth).Get("/admin", shell.dashboard)

	mountPostsAdmin(gr, d, shell)
	mountPagesAdmin(gr, d, shell)
	mountServicesAdmin(gr, d, shell)
	mountCategoriesAdmin(gr, d, shell)
	mountTagsAdmin(gr, d, shell)
	mountMediaAdmin(gr, d, shell)
	mountAccount(gr, d)
}

// mountMediaAdmin wires the gated admin media library (M4). Read routes require
// read:media; upload requires create:media; metadata update requires
// update:media; delete + bulk-delete require delete:media. Media has no
// per-author ownership in the canon — the coarse grant is the gate.
func mountMediaAdmin(gr chi.Router, d Deps, shell adminShellDeps) {
	if d.MediaAdminSvc == nil || d.Authz == nil {
		return
	}
	h := NewMediaAdminHandler(d.MediaAdminSvc, shell, d.CSRFFunc)

	gr.Route("/admin/media", func(mr chi.Router) {
		mr.Use(d.AuthMW.RequireAuth)

		mr.With(d.AuthMW.RequirePermission(accounts.ActionRead, accounts.SubjectMedia)).Group(func(rr chi.Router) {
			rr.Get("/", h.List)
			rr.Get("/picker", h.Picker)
			rr.Get("/{id}/detail", h.Detail)
		})

		mr.With(d.AuthMW.RequirePermission(accounts.ActionCreate, accounts.SubjectMedia)).
			Post("/", h.Upload)

		mr.With(d.AuthMW.RequirePermission(accounts.ActionUpdate, accounts.SubjectMedia)).
			Post("/{id}", h.UpdateMetadata)

		mr.With(d.AuthMW.RequirePermission(accounts.ActionDelete, accounts.SubjectMedia)).Group(func(dr chi.Router) {
			dr.Post("/{id}/delete", h.Delete)
			dr.Post("/bulk", h.Bulk)
		})
	})
}

// mountCategoriesAdmin wires the gated admin categories area. Categories have NO
// per-author ownership: each route requires the matching (action, category)
// grant; the bulk delete uses a coarse delete gate with a per-id re-check.
func mountCategoriesAdmin(gr chi.Router, d Deps, shell adminShellDeps) {
	if d.CategoryAdminSvc == nil || d.Authz == nil {
		return
	}
	h := NewCategoryAdminHandler(d.CategoryAdminSvc, shell, d.CSRFFunc)

	gr.Route("/admin/categories", func(cr chi.Router) {
		cr.Use(d.AuthMW.RequireAuth)

		cr.With(d.AuthMW.RequirePermission(accounts.ActionRead, accounts.SubjectCategory)).Group(func(rr chi.Router) {
			rr.Get("/", h.List)
			rr.Get("/new", h.New)
			rr.Get("/{id}/edit", h.Edit)
		})

		cr.With(d.AuthMW.RequirePermission(accounts.ActionCreate, accounts.SubjectCategory)).
			Post("/", h.Create)

		cr.With(d.AuthMW.RequirePermission(accounts.ActionUpdate, accounts.SubjectCategory)).
			Post("/{id}", h.Update)

		cr.With(d.AuthMW.RequirePermission(accounts.ActionDelete, accounts.SubjectCategory)).Group(func(dr chi.Router) {
			dr.Post("/{id}/delete", h.Delete)
			dr.Post("/bulk", h.Bulk)
		})
	})
}

// mountTagsAdmin wires the gated admin tags area (flat; delete-only bulk).
func mountTagsAdmin(gr chi.Router, d Deps, shell adminShellDeps) {
	if d.TagAdminSvc == nil || d.Authz == nil {
		return
	}
	h := NewTagAdminHandler(d.TagAdminSvc, shell, d.CSRFFunc)

	gr.Route("/admin/tags", func(tr chi.Router) {
		tr.Use(d.AuthMW.RequireAuth)

		tr.With(d.AuthMW.RequirePermission(accounts.ActionRead, accounts.SubjectTag)).Group(func(rr chi.Router) {
			rr.Get("/", h.List)
			rr.Get("/new", h.New)
			rr.Get("/{id}/edit", h.Edit)
		})

		tr.With(d.AuthMW.RequirePermission(accounts.ActionCreate, accounts.SubjectTag)).
			Post("/", h.Create)

		tr.With(d.AuthMW.RequirePermission(accounts.ActionUpdate, accounts.SubjectTag)).
			Post("/{id}", h.Update)

		tr.With(d.AuthMW.RequirePermission(accounts.ActionDelete, accounts.SubjectTag)).Group(func(dr chi.Router) {
			dr.Post("/{id}/delete", h.Delete)
			dr.Post("/bulk", h.Bulk)
		})
	})
}

// mountAccount wires the self-service /account area behind RequireAuth. The
// avatar upload and password change endpoints are additionally rate-limited
// (they are expensive / security-sensitive) with a dedicated per-IP limiter.
func mountAccount(gr chi.Router, d Deps) {
	if d.Account == nil {
		return
	}
	// 1 token/3s, burst 3 (~20/min) for the heavy/sensitive account POSTs.
	limiter := ratelimit.New(1.0/3.0, 3)

	gr.Group(func(g chi.Router) {
		g.Use(d.AuthMW.RequireAuth)
		g.Get("/account", d.Account.Show)
		g.Post("/account", d.Account.SubmitProfile)

		g.Group(func(rl chi.Router) {
			rl.Use(limiter.Middleware)
			rl.Post("/account/avatar", d.Account.SubmitAvatar)
			rl.Post("/account/password", d.Account.SubmitPassword)
		})
	})
}

// mountPostsAdmin wires the gated admin posts area. Read routes require
// read:post; mutating routes require the matching action; per-post OWNERSHIP is
// enforced inside the service (an Author may only act on their own posts).
func mountPostsAdmin(gr chi.Router, d Deps, shell adminShellDeps) {
	if d.PostAdminSvc == nil || d.Authz == nil {
		return
	}
	h := NewPostAdminHandler(d.PostAdminSvc, shell, d.Authors, d.CSRFFunc)
	if d.CategoryReadSvc != nil || d.TagReadSvc != nil {
		h.WithTaxonomy(d.CategoryReadSvc, d.TagReadSvc)
	}

	gr.Route("/admin/posts", func(pr chi.Router) {
		pr.Use(d.AuthMW.RequireAuth)

		// Read.
		pr.With(d.AuthMW.RequirePermission(accounts.ActionRead, accounts.SubjectPost)).Group(func(rr chi.Router) {
			rr.Get("/", h.List)
			rr.Get("/trash", h.Trashed)
			rr.Get("/new", h.New)
			rr.Get("/{id}/edit", h.Edit)
			rr.Get("/{id}/revisions", h.Revisions)
		})

		// Create.
		pr.With(d.AuthMW.RequirePermission(accounts.ActionCreate, accounts.SubjectPost)).
			Post("/", h.Create)

		// Update / publish / restore-revision (service narrows to ownership).
		pr.With(d.AuthMW.RequirePermission(accounts.ActionUpdate, accounts.SubjectPost)).Group(func(ur chi.Router) {
			ur.Post("/{id}", h.Update)
			ur.Post("/{id}/revisions/{rev}/restore", h.RestoreRevision)
			ur.Post("/trash/{id}/restore", h.RestoreTrashed)
		})

		// Trash / permanent delete.
		pr.With(d.AuthMW.RequirePermission(accounts.ActionDelete, accounts.SubjectPost)).Group(func(dr chi.Router) {
			dr.Post("/{id}/trash", h.Trash)
			dr.Post("/trash/{id}/delete", h.PermanentDelete)
		})

		// Bulk list actions (M2c). The route gate is a coarse pre-filter
		// (ActionUpdate); the per-id action permission AND per-post ownership are
		// re-checked inside the service via the reused single-item op, so an
		// Author's bulk only touches their own posts and an unauthorized id is
		// skipped, not failed.
		pr.With(d.AuthMW.RequirePermission(accounts.ActionUpdate, accounts.SubjectPost)).
			Post("/bulk", h.Bulk)
	})
}

// mountPagesAdmin wires the gated admin pages area. Pages have NO per-author
// ownership in the canon: each route requires the matching (action, page) grant
// and the service does no further owner narrowing.
func mountPagesAdmin(gr chi.Router, d Deps, shell adminShellDeps) {
	if d.PageAdminSvc == nil || d.Authz == nil {
		return
	}
	h := NewPageAdminHandler(d.PageAdminSvc, shell, d.Authors, d.CSRFFunc)

	gr.Route("/admin/pages", func(pr chi.Router) {
		pr.Use(d.AuthMW.RequireAuth)

		pr.With(d.AuthMW.RequirePermission(accounts.ActionRead, accounts.SubjectPage)).Group(func(rr chi.Router) {
			rr.Get("/", h.List)
			rr.Get("/trash", h.Trashed)
			rr.Get("/new", h.New)
			rr.Get("/{id}/edit", h.Edit)
			rr.Get("/{id}/revisions", h.Revisions)
		})

		pr.With(d.AuthMW.RequirePermission(accounts.ActionCreate, accounts.SubjectPage)).
			Post("/", h.Create)

		pr.With(d.AuthMW.RequirePermission(accounts.ActionUpdate, accounts.SubjectPage)).Group(func(ur chi.Router) {
			ur.Post("/{id}", h.Update)
			ur.Post("/{id}/revisions/{rev}/restore", h.RestoreRevision)
			ur.Post("/trash/{id}/restore", h.RestoreTrashed)
		})

		pr.With(d.AuthMW.RequirePermission(accounts.ActionDelete, accounts.SubjectPage)).Group(func(dr chi.Router) {
			dr.Post("/{id}/trash", h.Trash)
			dr.Post("/trash/{id}/delete", h.PermanentDelete)
		})

		// Bulk list actions (M2c). Coarse ActionUpdate gate; per-id action
		// permission re-checked inside the service.
		pr.With(d.AuthMW.RequirePermission(accounts.ActionUpdate, accounts.SubjectPage)).
			Post("/bulk", h.Bulk)
	})
}

// mountServicesAdmin wires the gated admin services area. Services have NO
// per-author ownership: each route requires the matching (action, service) grant.
func mountServicesAdmin(gr chi.Router, d Deps, shell adminShellDeps) {
	if d.ServiceAdminSvc == nil || d.Authz == nil {
		return
	}
	h := NewServiceAdminHandler(d.ServiceAdminSvc, shell, d.Authors, d.CSRFFunc)

	gr.Route("/admin/services", func(pr chi.Router) {
		pr.Use(d.AuthMW.RequireAuth)

		pr.With(d.AuthMW.RequirePermission(accounts.ActionRead, accounts.SubjectService)).Group(func(rr chi.Router) {
			rr.Get("/", h.List)
			rr.Get("/trash", h.Trashed)
			rr.Get("/new", h.New)
			rr.Get("/{id}/edit", h.Edit)
			rr.Get("/{id}/revisions", h.Revisions)
		})

		pr.With(d.AuthMW.RequirePermission(accounts.ActionCreate, accounts.SubjectService)).
			Post("/", h.Create)

		pr.With(d.AuthMW.RequirePermission(accounts.ActionUpdate, accounts.SubjectService)).Group(func(ur chi.Router) {
			ur.Post("/{id}", h.Update)
			ur.Post("/{id}/revisions/{rev}/restore", h.RestoreRevision)
			ur.Post("/trash/{id}/restore", h.RestoreTrashed)
		})

		pr.With(d.AuthMW.RequirePermission(accounts.ActionDelete, accounts.SubjectService)).Group(func(dr chi.Router) {
			dr.Post("/{id}/trash", h.Trash)
			dr.Post("/trash/{id}/delete", h.PermanentDelete)
		})

		// Bulk list actions (M2c). Coarse ActionUpdate gate; per-id action
		// permission re-checked inside the service.
		pr.With(d.AuthMW.RequirePermission(accounts.ActionUpdate, accounts.SubjectService)).
			Post("/bulk", h.Bulk)
	})
}

// StaticDirDefault returns the conventional location of the static assets
// directory, relative to the process working directory.
func StaticDirDefault() string {
	return "web/static"
}
