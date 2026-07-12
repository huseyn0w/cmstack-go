// Package web assembles the HTTP router and middleware chain. Wiring is
// explicit: dependencies are passed in via Deps; there is no global state.
package web

import (
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/huseyn0w/agentic-cms-go/internal/accounts"
	"github.com/huseyn0w/agentic-cms-go/internal/health"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/cache"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/config"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/events"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/ratelimit"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/render"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/security"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/session"
	"github.com/huseyn0w/agentic-cms-go/internal/plugin"
	webtempl "github.com/huseyn0w/agentic-cms-go/web/templ"
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

	// Account tokens (M17-4). APITokenSvc backs the self-service
	// /account/tokens list/create/revoke area over the SAME apitoken.Service
	// instance used by the REST bearer-auth middleware. Optional; nil leaves
	// the area unmounted.
	APITokenSvc APITokenService

	// Posts (M2a). PostAdmin is the gated admin posts area; PostPublic is the
	// public /blog. Authors resolves author display names for both. All optional.
	PostAdminSvc  PostAdminService
	PostPublicSvc PostPublicService
	Authors       AuthorNamer
	SiteName      string

	// Site is the resolved site-identity + SEO config (M8). Threaded to every
	// public handler that emits a document head. Optional so reduced-Deps tests
	// keep working (an empty SiteConfig yields a minimal, still-valid head).
	Site SiteConfig

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

	// Comments (M5). CommentPublicSvc backs the public /blog/{slug}/comments
	// thread + submit; CommentAdminSvc backs the gated /admin/comments moderation
	// area. CommentPostTitler resolves target-post titles for the moderation rows
	// (optional). RecaptchaSiteKey is exposed to the public form's v3 token hook.
	// All optional so reduced-Deps tests keep working.
	CommentPublicSvc  CommentsPublicService
	CommentAdminSvc   CommentsAdminService
	CommentPostTitler CommentPostTitler

	// Dashboard stat readers (optional). Populated from the concrete content
	// services; each feeds one /admin stat card. A nil reader renders that card as
	// a dash, so reduced-Deps tests and partial wiring degrade gracefully.
	DashboardPostCounter    PublishedCounter
	DashboardPageCounter    PublishedCounter
	DashboardCommentCounter PendingCommentCounter

	// Contact (M12). ContactSvc backs the public reCAPTCHA-protected /contact form
	// (GET renders it, POST submits it). The form emails a settings-driven
	// recipient via the async contact.submitted outbox event. Public, no auth;
	// the POST is rate-limited. Optional so reduced-Deps tests keep working — the
	// area is mounted only when the service is wired.
	ContactSvc ContactPublicService

	// Search (M6). SearchSvc backs the public /search results page (FTS with an
	// ILIKE fallback across published posts/pages/services). Public, GET-only,
	// no auth. Optional so reduced-Deps tests keep working.
	SearchSvc SearchService

	// Crawler routes (M8). These enumerate published content for the domain-root
	// crawler files (/sitemap.xml, /robots.txt, /llms.txt, /llms-full.txt), which
	// are served UNPREFIXED + locale-agnostic on the root router. Each is
	// optional; a nil enumerator simply contributes no URLs. Categories/Tags are
	// enumerated via the taxonomy adapters (AllFlat -> SitemapItem).
	SitemapPostSvc     SitemapEnumerator
	SitemapPageSvc     SitemapEnumerator
	SitemapServiceSvc  SitemapEnumerator
	SitemapCategorySvc TaxonomyEnumerator
	SitemapTagSvc      TaxonomyEnumerator

	// RSS feeds (M16). FeedPostSvc enumerates published posts (optionally narrowed
	// to a category slug) for the domain-root feeds (/rss.xml,
	// /categories/{slug}/rss.xml), served UNPREFIXED + locale-agnostic on the root
	// router like the sitemap. Optional: a nil FeedPostSvc simply leaves the feed
	// routes unregistered. FeedCategoryNamer optionally resolves a category slug to
	// its display name for the per-category channel title; nil falls back to the
	// slug.
	FeedPostSvc       FeedPostLister
	FeedCategoryNamer feedCategoryNamer

	// UploadsPrefix is the URL prefix the uploads handler is mounted at (e.g.
	// "/uploads"); defaults to "/uploads".
	UploadsPrefix string

	// Locale is the i18n resolver (M7a). When wired, its middleware runs at the
	// head of the PUBLIC route group: it resolves the active locale from the URL
	// prefix ("as-needed": en unprefixed, /de + /ru prefixed), strips the prefix
	// so downstream routes match unchanged, and threads the locale + translator
	// to templ. Admin routes are intentionally NOT localized (they stay en).
	// Optional so reduced-Deps tests keep working (they then render as en).
	Locale *LocaleResolver

	// PageCache is the anonymous public page-response cache (M13-2). When wired,
	// its middleware runs OUTERMOST on the public content group (but AFTER the
	// locale + theme middleware, so the cache key sees the resolved locale/theme):
	// a hit short-circuits before any rendering. It only ever caches complete,
	// anonymous 200 text/html responses and bypasses any request that carries the
	// session cookie, is an htmx partial, or has a query string. Optional; a nil
	// PageCache disables caching (reduced-Deps tests keep working).
	PageCache *PageCache

	// Cache is the shared object cache (M13-2). It is threaded into the crawler
	// handler so the rendered sitemap.xml is memoized (and invalidated on publish
	// via the "sitemap:" prefix). Optional; a nil cache leaves the sitemap
	// rendered per request (current behavior). The menu cache is wired directly
	// into the menu service in cmd/server, not via Deps.
	Cache cache.Cache

	// Theme is the public-theme resolver (M9-1). When wired, its middleware runs
	// on the PUBLIC route group only: it reads the active theme id from the
	// settings store, validates it against the in-code registry, and stores the
	// resolved id in the request context so the layout re-scopes the color tokens
	// via a `.theme-<id>` <html> class. Admin routes never run it, so they fall
	// back to the base palette (theme isolation). Optional so reduced-Deps tests
	// keep working (they then render on the default theme).
	Theme *ThemeResolver

	// Menus (M11-2). MenuAdminSvc backs the gated /admin/menus builder; the three
	// content listers populate the item picker + resolve internal slugs to URLs.
	// The post/page/category read services are reused via narrow interfaces.
	// Optional; the area is mounted only when the menu service + authz are wired.
	MenuAdminSvc      MenuAdminService
	MenuPostListerSvc menuPostLister
	MenuPageListerSvc menuPageLister
	MenuCatListerSvc  menuCategoryLister
	// MenuPublicSvc backs the public header/footer menu rendering (M11-3): the
	// layout resolves the menu assigned to each location for the active locale.
	// Optional; nil leaves the header/footer without managed menus.
	MenuPublicSvc MenuPublicService

	// AppearanceSvc backs the gated /admin/appearance theme switcher (M9-2): it
	// reads and persists the active theme id. *settings.Service satisfies it.
	// Optional; when nil the appearance area is not mounted.
	AppearanceSvc AppearanceSettings

	// UserAdminSvc backs the gated /admin/users list + per-user name/role edit
	// form. *accounts.UserAdminService satisfies it (reuse the same instance
	// wired for the REST API). Optional; when nil the area is not mounted.
	UserAdminSvc UsersAdminService

	// SettingsReader is the live admin-editable site/SEO override reader (M15-2).
	// When non-nil, SiteConfig consults it at render time so the two admin
	// dashboards' overrides take effect on public pages immediately (override ||
	// config). *settings.Service satisfies it (reuse the same instance as
	// AppearanceSvc/AnalyticsSvc). Optional; a nil reader leaves the config-only
	// path byte-identical (no overlay).
	SettingsReader SiteOverrideReader

	// AnalyticsSvc backs the public GA4 + GTM snippet injection (M15-1): it reads
	// the (settings-backed, admin-editable in a later slice) analytics container
	// ids. *settings.Service satisfies it. When non-nil, AnalyticsMiddleware is
	// applied on the PUBLIC route group only, so the validated ids are injected on
	// public pages while admin routes emit nothing (public-only isolation).
	// Optional; a nil service leaves analytics disabled.
	AnalyticsSvc AnalyticsSettings

	// Plugins is the in-process plugin manager (M10-1). When non-nil, Router
	// registers the templ render-region source (so the public layout injects
	// enabled plugins' head/body_end fragments) and threads the manager into the
	// public post handler (so the "post_content" filter runs over the rendered
	// body). Optional; a nil manager is a no-op (no regions, no filtering).
	Plugins *plugin.Manager

	// APIMounter mounts the stateless REST API group (M17-1) on the ROOT router,
	// OUTSIDE the session/CSRF group, alongside the health/crawler routes. It is a
	// hook (a closure the caller wires to api.Mount) so the direction of the
	// dependency stays api -> web: internal/api imports internal/web, so
	// internal/web must never import internal/api. Optional; a nil hook leaves the
	// API unmounted (reduced-Deps tests keep working).
	APIMounter func(chi.Router)
}

// Router builds the chi router with the full middleware chain and mounts all
// routes. The order matters: requestID and real-IP first so downstream logging
// and recovery see them; security headers early; CSRF and session around the
// dynamic routes.
func Router(d Deps) http.Handler {
	// Register the templ render-region source when a plugin manager is wired, so
	// the public layout injects enabled plugins' head/body_end fragments. A nil
	// manager leaves the accessor unset (PluginRegion yields nothing).
	if d.Plugins != nil {
		webtempl.SetPluginSource(pluginRegionSource{mgr: d.Plugins})
	}

	// Register the public-menu source when the menu service is wired, so the
	// layout renders the managed header/footer menus for the active locale. A nil
	// service leaves the accessor unset (MenuForLocation yields nothing).
	if d.MenuPublicSvc != nil {
		webtempl.SetMenuSource(menuPublicSource{svc: d.MenuPublicSvc})
	}

	// Wire the live settings overlay onto the site config BEFORE any handler (or
	// the crawler) copies it (M15-2). SiteConfig carries the reader as an
	// interface value, so every downstream .WithSite(d.Site) copy shares it and
	// reflects override writes live. A nil reader is a no-op (config-only path
	// stays byte-identical).
	if d.SettingsReader != nil {
		d.Site = d.Site.WithOverrides(d.SettingsReader)
	}

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

	// Static assets — no CSRF/session needed. A Cache-Control is set so browsers
	// (and any CDN) cache the CSS/JS/fonts instead of re-fetching every load;
	// http.FileServer still emits ETag/Last-Modified, so a changed asset is
	// revalidated (304) within the window rather than served stale forever.
	if d.StaticDir != "" {
		fs := http.StripPrefix("/static/", http.FileServer(http.Dir(d.StaticDir)))
		r.Handle("/static/*", staticCacheControl(fs))
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

	// Crawler-facing files (M8). Registered on the ROOT router (unprefixed and
	// locale-agnostic): /sitemap.xml, /robots.txt, /llms.txt, /llms-full.txt must
	// resolve at the domain root regardless of any locale prefix. They live here
	// alongside /health rather than inside the localized public group so the
	// locale middleware (which wraps the whole router) never rewrites their paths.
	// Emit them only for the unprefixed form; /de/sitemap.xml need not exist.
	crawler := NewCrawlerHandler(
		d.Site,
		d.SitemapPostSvc, d.SitemapPageSvc, d.SitemapServiceSvc,
		d.SitemapCategorySvc, d.SitemapTagSvc,
	)
	if d.Cache != nil {
		crawler.WithCache(d.Cache)
	}
	r.Get("/sitemap.xml", crawler.Sitemap)
	r.Get("/robots.txt", crawler.Robots)
	r.Get("/llms.txt", crawler.LLMs)
	r.Get("/llms-full.txt", crawler.LLMsFull)

	// RSS feeds (M16). Registered on the ROOT router (unprefixed, locale-agnostic)
	// alongside the sitemap. Only wired when the post enumerator is present. The
	// per-category path lives under /categories/{slug}/rss.xml on the root router;
	// it does not collide with the localized public /categories/{slug} archive
	// (a distinct router group — chi resolves them independently).
	if d.FeedPostSvc != nil {
		feed := NewFeedHandler(d.Site, d.FeedPostSvc, d.FeedCategoryNamer)
		r.Get("/rss.xml", feed.Feed)
		r.Get("/categories/{slug}/rss.xml", feed.CategoryFeed)
	}

	// REST API (M17-1). Mounted on the ROOT router — OUTSIDE the session/CSRF
	// group below — because bearer-token auth is stateless and CSRF-exempt. The
	// hook wires api.Mount; the direction stays api -> web (web never imports api).
	if d.APIMounter != nil {
		d.APIMounter(r)
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

		// Public-facing content routes (M9-1): wrapped in a nested group so the
		// theme middleware runs ONLY here. It reads the active theme from settings,
		// validates it against the registry, and stores the resolved id in context
		// so the layout re-scopes the color tokens. Admin/auth routes are mounted
		// on the OUTER group (mountAuthRoutes below), so they never run the theme
		// middleware and fall back to the base palette (theme isolation).
		gr.Group(func(pr chi.Router) {
			if d.Theme != nil {
				pr.Use(d.Theme.Middleware)
			}

			// Anonymous page-response cache (M13-2). Placed AFTER the theme
			// middleware so the cache key sees the resolved locale/theme, and
			// outermost among the content handlers so a hit short-circuits before
			// any render. It self-limits to anonymous, query-less, non-htmx GETs.
			if d.PageCache != nil {
				pr.Use(d.PageCache.Middleware)
			}

			// Public analytics (M15-1). Reads + validates the GA4/GTM ids from
			// settings and stores the validated snippets in context for the layout
			// to emit. Runs on this PUBLIC group only, so admin routes (mounted on
			// the outer group) emit no analytics. It is safe to run after the page
			// cache: the ids are identical for all anonymous users, so caching the
			// analytics-injected HTML is correct.
			if d.AnalyticsSvc != nil {
				pr.Use(AnalyticsMiddleware(d.AnalyticsSvc))
			}

			// TODO(M1-ext): replace this inline closure with a real home handler in
			// its own package once the content domain exists.
			pr.Get("/", func(w http.ResponseWriter, req *http.Request) {
				seo := d.Site.BuildSEO(req, SEOInput{
					Title:         d.Site.SiteName,
					Description:   d.Site.SiteDescription,
					CanonicalPath: "/",
					OGType:        "website",
				})
				if err := render.Component(req.Context(), w, http.StatusOK, webtempl.HomeStructured(seo, d.Site.homeJSONLD(req.Context()))); err != nil {
					http.Error(w, "render error", http.StatusInternalServerError)
				}
			})

			// Public author profile page (no auth) — anyone may view it.
			if d.Author != nil {
				d.Author.WithSite(d.Site)
				pr.Get("/authors/{id}", d.Author.Show)
			}

			// Public blog (no auth for read). Liking requires an authenticated user.
			if d.PostPublicSvc != nil {
				pub := NewPostPublicHandler(d.PostPublicSvc, d.Authors, d.SiteName, d.Config.BaseURL, d.CSRFFunc)
				pub.WithSite(d.Site)
				if d.Plugins != nil {
					pub.WithPlugins(d.Plugins)
				}
				if d.CategoryPostSvc != nil || d.TagPostSvc != nil {
					pub.WithTaxonomy(d.CategoryPostSvc, d.TagPostSvc)
				}
				pr.Get("/blog", pub.Index)
				pr.Get("/blog/{slug}", pub.Show)
				if d.AuthMW != nil {
					pr.With(d.AuthMW.RequireAuth).Post("/blog/{slug}/like", pub.Like)
					pr.With(d.AuthMW.RequireAuth).Post("/blog/{slug}/unlike", pub.Unlike)
				}
			}

			// Public comments (M5). The thread + top-level submit are open to guests
			// (spam-checked + rate-limited inside the service); self-edit/delete are
			// auth-gated (the handler additionally verifies ownership + window).
			if d.CommentPublicSvc != nil {
				ch := NewCommentsPublicHandler(d.CommentPublicSvc, d.CSRFFunc, d.Config.RecaptchaSiteKey)
				pr.Get("/blog/{slug}/comments", ch.Thread)
				pr.Post("/blog/{slug}/comments", ch.Submit)
				if d.AuthMW != nil {
					pr.With(d.AuthMW.RequireAuth).Post("/blog/{slug}/comments/{id}/edit", ch.SelfEdit)
					pr.With(d.AuthMW.RequireAuth).Post("/blog/{slug}/comments/{id}/delete", ch.SelfDelete)
				}
			}

			// Public taxonomy archives (no auth): /categories/{slug} + /tags/{slug}.
			if d.PostHydrateSvc != nil && (d.CategoryPublicSvc != nil || d.TagPublicSvc != nil) {
				tax := NewTaxonomyPublicHandler(d.CategoryPublicSvc, d.TagPublicSvc, d.PostHydrateSvc, d.Authors, d.SiteName)
				tax.WithSite(d.Site)
				if d.CategoryPublicSvc != nil {
					pr.Get("/categories/{slug}", tax.ShowCategory)
				}
				if d.TagPublicSvc != nil {
					pr.Get("/tags/{slug}", tax.ShowTag)
				}
			}

			// Public pages (no auth). A published page renders at /p/{slug}; the
			// hierarchy drives the breadcrumb trail.
			if d.PagePublicSvc != nil {
				pp := NewPagePublicHandler(d.PagePublicSvc, d.SiteName, d.Config.BaseURL)
				pp.WithSite(d.Site)
				pr.Get("/p/{slug}", pp.Show)
			}

			// Public services (no auth): /services index + /services/{slug} detail.
			if d.ServicePublicSvc != nil {
				sp := NewServicePublicHandler(d.ServicePublicSvc, d.SiteName, d.Config.BaseURL)
				sp.WithSite(d.Site)
				pr.Get("/services", sp.Index)
				pr.Get("/services/{slug}", sp.Show)
			}

			// Public contact form (M12, no auth). GET /contact renders the
			// reCAPTCHA-protected form; POST /contact submits it (validated,
			// spam-checked, rate-limited inside the service; the route adds a per-IP
			// limiter for defense-in-depth). The recaptcha site key is reused from
			// config (same v3 hook as comments).
			if d.ContactSvc != nil {
				cc := NewContactPublicHandler(d.ContactSvc, d.SiteName, d.Config.BaseURL, d.CSRFFunc, d.Config.RecaptchaSiteKey)
				cc.WithSite(d.Site)
				pr.Get("/contact", cc.Show)
				// ~8/min per IP (mirrors the comment submit budget).
				contactLimiter := ratelimit.New(8.0/60.0, 8)
				pr.With(contactLimiter.Middleware).Post("/contact", cc.Submit)
			}

			// Public search (M6, no auth). GET /search renders the results page (FTS
			// with an ILIKE fallback) across published posts/pages/services.
			if d.SearchSvc != nil {
				sh := NewSearchPublicHandler(d.SearchSvc, d.SiteName)
				sh.WithSite(d.Site)
				pr.Get("/search", sh.Search)
			}
		})

		mountAuthRoutes(gr, d)
	})

	// i18n locale resolution (M7a) wraps the whole chi router as an OUTER handler,
	// not a chi middleware: chi resolves the route from the path BEFORE its
	// middleware chain runs, so a prefix strip performed inside the chain would be
	// too late (chi would already have 404'd /de/blog). Wrapping outside means the
	// prefix is stripped and the locale/translator are in context before chi ever
	// sees the request. "As-needed": /de + /ru select and strip; everything else
	// (including every unprefixed /admin route) resolves to the default en with the
	// path unchanged, so admin stays en without separate wiring.
	if d.Locale != nil {
		return d.Locale.Middleware(r)
	}
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
		authz:    d.Authz,
		roles:    d.Roles,
		csrf:     d.CSRFFunc,
		siteURL:  d.Config.BaseURL,
		posts:    d.DashboardPostCounter,
		pages:    d.DashboardPageCounter,
		comments: d.DashboardCommentCounter,
	}
	gr.With(d.AuthMW.RequireAuth).Get("/admin", shell.dashboard)

	// Admin UI language switch (cookie-based). Any authenticated operator may set
	// their back-office language; it requires no special permission. The locale
	// middleware reads the resulting `admin_locale` cookie for admin surfaces.
	locale := NewAdminLocaleHandler(d.Config.IsProduction())
	gr.With(d.AuthMW.RequireAuth).Get("/admin/locale/{code}", locale.Set)

	mountPostsAdmin(gr, d, shell)
	mountPagesAdmin(gr, d, shell)
	mountServicesAdmin(gr, d, shell)
	mountCategoriesAdmin(gr, d, shell)
	mountTagsAdmin(gr, d, shell)
	mountMediaAdmin(gr, d, shell)
	mountCommentsAdmin(gr, d, shell)
	mountAppearanceAdmin(gr, d, shell)
	mountSettingsAdmin(gr, d, shell)
	mountUsersAdmin(gr, d, shell)
	mountMenusAdmin(gr, d, shell)
	mountPluginsAdmin(gr, d, shell)
	mountAccount(gr, d, shell)
}

// mountMenusAdmin wires the gated admin menu builder (M11-2). Read routes
// require read:menu; creating a menu requires create:menu; every edit + reorder
// + per-locale label POST requires update:menu; deletes require delete:menu.
// Menus have NO per-author ownership — the coarse grant is the gate. Mounted only
// when the menu service + authz are wired.
func mountMenusAdmin(gr chi.Router, d Deps, shell adminShellDeps) {
	if d.MenuAdminSvc == nil || d.Authz == nil {
		return
	}
	h := NewMenuAdminHandler(d.MenuAdminSvc, d.MenuPostListerSvc, d.MenuPageListerSvc, d.MenuCatListerSvc, shell, d.CSRFFunc)

	gr.Route("/admin/menus", func(mr chi.Router) {
		mr.Use(d.AuthMW.RequireAuth)

		// Read.
		mr.With(d.AuthMW.RequirePermission(accounts.ActionRead, accounts.SubjectMenu)).Group(func(rr chi.Router) {
			rr.Get("/", h.List)
			rr.Get("/{id}", h.Edit)
			rr.Get("/{id}/items/{itemID}/edit", h.EditItem)
		})

		// Create (new menu).
		mr.With(d.AuthMW.RequirePermission(accounts.ActionCreate, accounts.SubjectMenu)).
			Post("/", h.Create)

		// Update: settings, add/edit item, reorder (move).
		mr.With(d.AuthMW.RequirePermission(accounts.ActionUpdate, accounts.SubjectMenu)).Group(func(ur chi.Router) {
			ur.Post("/{id}", h.UpdateSettings)
			ur.Post("/{id}/items", h.AddItem)
			ur.Post("/{id}/items/{itemID}/move", h.MoveItem)
			ur.Post("/{id}/items/{itemID}/edit", h.UpdateItem)
		})

		// Delete: whole menu + single item (item removal is a coarse delete gate).
		mr.With(d.AuthMW.RequirePermission(accounts.ActionDelete, accounts.SubjectMenu)).Group(func(dr chi.Router) {
			dr.Post("/{id}/delete", h.Delete)
			dr.Post("/{id}/items/{itemID}/delete", h.DeleteItemHandler)
		})
	})
}

// mountPluginsAdmin wires the gated admin plugin manager (M10-2). Listing
// plugins requires read:plugin; toggling one requires update:plugin. The
// catalogue is the in-code registry; enabled state persists via the manager.
func mountPluginsAdmin(gr chi.Router, d Deps, shell adminShellDeps) {
	if d.Plugins == nil || d.Authz == nil {
		return
	}
	h := NewPluginAdminHandler(d.Plugins, shell, d.CSRFFunc)

	gr.Route("/admin/plugins", func(pr chi.Router) {
		pr.Use(d.AuthMW.RequireAuth)
		pr.With(d.AuthMW.RequirePermission(accounts.ActionRead, accounts.SubjectPlugin)).
			Get("/", h.Show)
		pr.With(d.AuthMW.RequirePermission(accounts.ActionUpdate, accounts.SubjectPlugin)).
			Post("/toggle", h.Toggle)
	})
}

// mountAppearanceAdmin wires the gated admin appearance (theme switcher) area
// (M9). Listing themes requires read:theme; activating one requires
// update:theme. The theme catalogue is the in-code registry; the active choice
// is persisted via the settings service.
func mountAppearanceAdmin(gr chi.Router, d Deps, shell adminShellDeps) {
	if d.AppearanceSvc == nil || d.Authz == nil {
		return
	}
	h := NewAppearanceHandler(d.AppearanceSvc, shell, d.CSRFFunc)

	gr.Route("/admin/appearance", func(ar chi.Router) {
		ar.Use(d.AuthMW.RequireAuth)
		ar.With(d.AuthMW.RequirePermission(accounts.ActionRead, accounts.SubjectTheme)).
			Get("/", h.Show)
		ar.With(d.AuthMW.RequirePermission(accounts.ActionUpdate, accounts.SubjectTheme)).
			Post("/activate", h.Activate)
	})
}

// mountSettingsAdmin wires the two gated admin settings dashboards (M15-2):
// General (site identity) and SEO & GEO (indexing, verification, analytics,
// Organization). Both read behind read:setting and write behind update:setting.
// The SEO dashboard also edits the M15-1 analytics ids. Mounted only when the
// settings service (SettingsReader) + authz are wired; the writes go through the
// same *settings.Service that backs the live overlay, so a save is immediately
// reflected on public pages.
func mountSettingsAdmin(gr chi.Router, d Deps, shell adminShellDeps) {
	store, ok := d.SettingsReader.(SettingsStore)
	if !ok || d.Authz == nil {
		return
	}

	general := NewSettingsGeneralHandler(store, d.Site, shell, d.CSRFFunc)
	seo := NewSettingsSEOHandler(store, d.Site, shell, d.CSRFFunc)

	gr.Route("/admin/settings/general", func(sr chi.Router) {
		sr.Use(d.AuthMW.RequireAuth)
		sr.With(d.AuthMW.RequirePermission(accounts.ActionRead, accounts.SubjectSetting)).
			Get("/", general.Show)
		sr.With(d.AuthMW.RequirePermission(accounts.ActionUpdate, accounts.SubjectSetting)).
			Post("/", general.Save)
	})

	gr.Route("/admin/settings/seo", func(sr chi.Router) {
		sr.Use(d.AuthMW.RequireAuth)
		sr.With(d.AuthMW.RequirePermission(accounts.ActionRead, accounts.SubjectSetting)).
			Get("/", seo.Show)
		sr.With(d.AuthMW.RequirePermission(accounts.ActionUpdate, accounts.SubjectSetting)).
			Post("/", seo.Save)
	})
}

// mountUsersAdmin wires the gated admin users area: a list of every account
// (with resolved role label) and a per-user name/role edit form. Read routes
// require read:user; the edit POST requires update:user. There is no
// create/delete surface here — accounts are provisioned through signup/invite,
// and the admin surface only edits the two admin-owned fields (name, role);
// role-existence validation and the last-administrator guard live in
// UserAdminService. Mounted only when the user-admin service + authz are wired.
func mountUsersAdmin(gr chi.Router, d Deps, shell adminShellDeps) {
	if d.UserAdminSvc == nil || d.Authz == nil {
		return
	}
	h := NewUsersAdminHandler(d.UserAdminSvc, shell, d.CSRFFunc)

	gr.Route("/admin/users", func(ur chi.Router) {
		ur.Use(d.AuthMW.RequireAuth)
		ur.With(d.AuthMW.RequirePermission(accounts.ActionRead, accounts.SubjectUser)).Group(func(rr chi.Router) {
			rr.Get("/", h.List)
			rr.Get("/{id}/edit", h.Edit)
		})
		ur.With(d.AuthMW.RequirePermission(accounts.ActionUpdate, accounts.SubjectUser)).
			Post("/{id}", h.Update)
	})
}

// mountCommentsAdmin wires the gated admin comment-moderation area (M5). The
// list/badge read routes require read:comment; the moderation status changes +
// bulk require update:comment; permanent delete requires delete:comment.
// Comments have no per-author ownership in moderation — the coarse grant is the
// gate, and the service re-checks each action.
func mountCommentsAdmin(gr chi.Router, d Deps, shell adminShellDeps) {
	if d.CommentAdminSvc == nil || d.Authz == nil {
		return
	}
	h := NewCommentsAdminHandler(d.CommentAdminSvc, shell, d.CommentPostTitler, d.CSRFFunc)

	gr.Route("/admin/comments", func(cr chi.Router) {
		cr.Use(d.AuthMW.RequireAuth)

		cr.With(d.AuthMW.RequirePermission(accounts.ActionRead, accounts.SubjectComment)).
			Get("/", h.List)

		// Status changes + bulk are a coarse update gate; delete is coarse delete.
		cr.With(d.AuthMW.RequirePermission(accounts.ActionUpdate, accounts.SubjectComment)).Group(func(ur chi.Router) {
			ur.Post("/{id}/approve", h.Approve)
			ur.Post("/{id}/spam", h.Spam)
			ur.Post("/{id}/trash", h.Trash)
			ur.Post("/bulk", h.Bulk)
		})

		cr.With(d.AuthMW.RequirePermission(accounts.ActionDelete, accounts.SubjectComment)).
			Post("/{id}/delete", h.Delete)
	})
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
// The /account/tokens area (list/create/revoke, M17-4) shares the SAME
// limiter for its create endpoint and is mounted independently: it requires
// only d.APITokenSvc, so it still mounts if d.Account happens to be nil.
func mountAccount(gr chi.Router, d Deps, shell adminShellDeps) {
	// 1 token/3s, burst 3 (~20/min) for the heavy/sensitive account POSTs.
	limiter := ratelimit.New(1.0/3.0, 3)

	if d.Account != nil {
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

	if d.APITokenSvc != nil {
		h := NewAccountTokensHandler(d.APITokenSvc, shell, d.CSRFFunc)

		gr.Group(func(g chi.Router) {
			g.Use(d.AuthMW.RequireAuth)
			g.Get("/account/tokens", h.List)
			g.Post("/account/tokens/{id}/revoke", h.Revoke)

			g.Group(func(rl chi.Router) {
				rl.Use(limiter.Middleware)
				rl.Post("/account/tokens", h.Create)
			})
		})
	}
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

// staticCacheControl advertises a one-day public cache policy for /static
// assets, which browsers otherwise omit for a bare FileServer (a Lighthouse
// "efficient cache policy" finding). A day (not a year) is used because the
// assets are not content-fingerprinted; the underlying FileServer still emits
// ETag/Last-Modified, so a changed asset is revalidated (304) at the window
// edge instead of served stale indefinitely.
func staticCacheControl(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		next.ServeHTTP(w, r)
	})
}
