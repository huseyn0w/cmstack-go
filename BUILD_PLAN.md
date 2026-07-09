# cmstack-go (CMStack-Go) — Build Plan

**Product:** CMStack-Go — the Go member of the cmstack CMS family. Same product as `cmstack-django` /
`cmstack-laravel` / `cmstack-ts`, built clean from day one in idiomatic Go, server-rendered.
**Module path:** `github.com/huseyn0w/cmstack-go`
**Canon (read-only, at repo root `../`):** `../FEATURE_MATRIX.md` (Target = what to build, Canonical =
which project to mirror) · `../DESIGN_SYSTEM.md` (single UI source of truth).

This file is the orchestrator's control surface. It holds the architecture decision, the validated
stack mapping, the domain-event classification, the milestone task board, and the per-layer test
status. It is refreshed as subagents report and feeds `HANDOFF.md`.

---

## 1. Architecture decision (clean from day one)

**Strict layering — non-negotiable:**

```
HTTP handler (thin: decode → validate DTO → call service → encode/render)
  → service (ALL business logic; orchestration; emits domain events)
    → repository (interface; ALL data access; the only layer touching sqlc/SQL)
      → db (Postgres via pgx)
service → event bus → observers/listeners (side effects: email, cache invalidation, search reindex, audit)
```

- **Handlers contain ZERO business logic and ZERO data access.** Only the HTTP boundary. A handler doing
  anything else is a defect and is rejected in adversarial review.
- **Services never touch the DB directly** — only through repository interfaces. Services never fire side
  effects inline — they emit a domain event; listeners handle the effect.
- **Dependency wiring is explicit** (constructor injection, assembled in `cmd/server`). No global state,
  no service locator, no DI-magic framework.
- **Event classification is mandatory** (see §5): synchronous in-transaction listeners for atomic effects;
  asynchronous queued listeners (river) for fire-and-forget effects, delivered via a transactional outbox.

### Project layout (idiomatic Go)

```
cmstack-go/
  cmd/
    server/        # main entrypoint: config load, pgx pool, wiring, http.Server, graceful shutdown
    migrate/       # goose migration runner (or invoked via Makefile)
    worker/        # river worker process (async listeners, scheduled jobs)
  internal/
    platform/      # cross-cutting infra: config, db (pgx pool), httpx, render (templ), session,
                   #   logging, validation, events (bus + outbox), cache, mailer, storage
    <domain>/      # one package per bounded context, each with: model.go, repository.go (iface),
                   #   <name>_repo_pg.go (sqlc-backed impl), service.go, handler.go, events.go,
                   #   listeners.go, dto.go, *_test.go
    web/           # http router assembly, middleware, templ components, static assets pipeline
    seoutil/ etc.  # shared helpers used across domains (kept small, justified)
  db/
    migrations/    # goose .sql migrations (checked in)
    queries/       # sqlc .sql query files
    sqlc.yaml
  web/
    templ/         # .templ component sources (layout, components, public, admin)
    static/        # compiled css (tailwind), htmx, alpine, fonts (self-hosted woff2)
    tailwind.css   # tailwind input
  e2e/             # playwright-go browser tests
  Makefile         # build, run, test, lint, generate (sqlc + templ), tailwind, migrate
  .env.example
```

**Bounded contexts (`internal/<domain>/`):** `accounts` (auth, users, roles, permissions, profiles),
`content` (posts, pages, services, revisions, scheduling, trash), `taxonomy` (categories, tags),
`media`, `comments`, `search`, `seo` (SEO + GEO + llms + sitemap/robots/feeds), `i18n`,
`themes`, `plugins`, `menus`, `contact`, `settings`, `apiweb` (public REST + admin write API),
`mcp` (AI management server), `health`.

---

## 2. Stack Mapping (validated against 2026 maintenance status)

| Concern | Mature stacks use | Chosen Go library | Why | Maturity note |
|---|---|---|---|---|
| HTTP router | Django urls / Laravel router / Nest+Next | **go-chi/chi v5** | `net/http`-native, middleware composition fits layered design, no framework lock-in | Active, ~22k★, tracks latest Go |
| DB query layer | Django ORM / Eloquent+repos / Prisma | **sqlc** (+ pgx/v5 driver) | Type-safe Go from hand-written SQL; compile-time safety; clean repo layer | v1.31 (Apr 2026), active |
| Migrations | framework migrations | **pressly/goose** | Minimal SQL migrations, pairs with sqlc on Postgres | Active since 2012 |
| PG driver | psycopg / PDO / Prisma | **jackc/pgx v5** | Community default, sqlc target, native PG types | Active; lib/pq is EOL |
| Templating | Django tmpl / Blade / RSC | **a-h/templ** + Tailwind standalone CLI | Compile-time type-safe components, no Node toolchain | ~10k★, active (pre-2.0 — risk noted) |
| Interactivity | Alpine+Trix / Alpine+jQuery / React | **htmx v2 + Alpine.js v3** | Server-rendered islands, near-zero JS (design budget) | Both active |
| Rich-text editor | Trix / TinyMCE / Tiptap | **Tiptap (JS island)** | Canonical = ts/Tiptap; embed as Alpine island, output sanitized server-side | Tiptap active |
| Sessions + hashing | allauth / session / Auth.js+argon2 | **alexedwards/scs v2** (PG store) + **argon2id** (`x/crypto`) | OWASP server-side sessions; argon2id is canonical hash | scs v2.9, x/crypto current |
| OAuth social | allauth / Socialite / Auth.js | **markbates/goth** | Drop-in Google+GitHub, wraps x/oauth2 | goth v1.82, active |
| Authorization | Groups+perms / JSON caps / CASL | **Hand-rolled DB-backed (action, subject) RBAC** (Casbin rejected) | Exactly mirrors canonical ts CASL `(action, subject)`; flat roles → ~30 lines + DB `role_permissions` as single source of truth + in-proc cache; Casbin's policy-CSV/adapter machinery is overkill here | n/a (stdlib + cache) |
| Jobs/queue/scheduler | cron cmd / sync queue / none | **riverqueue/river** (Postgres) | Transactional enqueue (in-tx, no dual-write) + periodic jobs (scheduled publishing) | v1 stable (Jun 2026) |
| Validation | forms/serializers / FormRequest / Zod | **go-playground/validator v10** | Declarative struct-tag validation at DTO boundary | v10.30, active |
| Domain events | signals / events+observers / hook bus | **Hand-rolled typed bus + transactional outbox** | ~50 lines idiomatic Go; sync in-tx + async via outbox→river | n/a |
| Full-text search | PG FTS+fallback / raw / tsvector | **Postgres tsvector + GIN**, ILIKE fallback | Built-in, no infra; canonical = django FTS-with-fallback | Core PG feature |
| Caching | LocMem / model-cache / revalidate | **go-redis v9 + ristretto v2** | L2 Redis (multi-worker correctness) + L1 in-process | go-redis v9.19, ristretto v2 |
| Image processing | Pillow / intervention / sharp | **kovidgoyal/imaging** fork (or bimg/libvips) | Thumbnails + dims + decompression-bomb guard | original imaging unmaintained — use fork |
| MIME sniffing | (varied) / magic / magic-byte | **gabriel-vasile/mimetype** | Magic-byte detection; derive ext from validated MIME (anti-polyglot) | v1.4, active |
| S3 storage | django-storages / disks / StorageDriver | **aws-sdk-go-v2** behind `Storage` iface | Driver abstraction, local default, S3 drop-in | Official, weekly releases |
| HTML sanitization | nh3 / mews-purifier / sanitize-html | **microcosm-cc/bluemonday** (pinned) | De-facto allowlist sanitizer, write-time | maintenance-mode — pin version |
| Email | Django mail / Mailable / (gap) | **wneessen/go-mail** | Modern MIME/TLS/auth | v0.6, active |
| RSS/Atom | (net-new all) | **gorilla/feeds** | Unified Atom/RSS/JSON | maintained (post-2023 revival) |
| Sitemap/robots | framework sitemaps | **snabb/sitemap** + hand-rolled robots | Sitemapindex/50k/escaping is fiddly; robots trivial | stable |
| Config/env | split settings / config / env parse | **caarlos0/env** | Env→typed struct, 12-factor | active |
| Testing | pytest / Pest+Dusk / Vitest+Playwright | **testing + testify**, table-driven; **testcontainers-go** (integration DB); **playwright-go** (E2E) | Canonical = ts Playwright; auto-wait suits htmx swaps | testify ~26k★; pw-go active |
| Lint/format | ruff+black+mypy / Pint+Larastan / Biome+tsc | **golangci-lint v2 + gofumpt** | Standard combo; v2 bundles formatter | v2.12 (May 2026) |
| reCAPTCHA | django-recaptcha / service / optional | hand-rolled v3 verify (no-op without keys) | Trivial HTTP verify; canonical = graceful no-op | n/a |
| MCP server | (planned) / laravel-mcp 28 / mcp 48 | **modelcontextprotocol/go-sdk** + OAuth 2.1 | Port ts 48-tool surface; OAuth floor (laravel model) | official Go SDK |

### Three highest-risk choices (lead sanity-check — proceeding on best judgment per autonomy directive)
1. **Router: chi v5** (not std ServeMux nor gin/echo). chi gives middleware ergonomics for a layered app
   without framework lock-in. Reversible — handlers are `http.HandlerFunc`-shaped either way.
2. **DB: sqlc + pgx** (not GORM). Compile-time-safe SQL, clean repository layer; dynamic queries
   (search/filter facets) hand-written via pgx. Costs a codegen step; aligns with strict layering.
3. **UI: templ + Tailwind standalone** (not html/template, no Node). Type-safe components, zero JS
   toolchain. templ is pre-2.0 (the one "new" bet) — mitigated by pinning and codegen being checked in.

---

## 3. Patterns adopted (each earns its place) & rejected

**Adopted:** Repository (data access behind interface, swappable, testable) · Service (business logic
home, keeps handlers thin) · Observer/event bus (decouples side effects; sync vs async classification) ·
Transactional outbox (atomic async event delivery, no dual-write) · Middleware (auth, session, CSRF,
rate-limit, locale, tenancy-free) · Strategy/Adapter (Storage local↔S3, OAuth providers via goth) ·
Factory (test fixtures/builders).

**Rejected (noted to avoid overkill):** Casbin (hand-rolled DB-backed (action,subject) RBAC is exactly CASL-equivalent for flat roles, avoids policy-CSV/adapter machinery, keeps `role_permissions` as the single source of truth) · CQRS / event-sourcing (no need; outbox covers async) ·
Hexagonal ports-everywhere ceremony (repository interface is enough) · Generic "BaseRepository"
inheritance (Go favors small focused interfaces) · DI container framework (explicit wiring is clearer) ·
GraphQL/tRPC (REST is canon) · external search engine (PG FTS is canon).

---

## 4. Milestones (ordered) — task board

Status: ⬜ queued · 🟡 in-progress · ✅ done · 🔴 blocked. Model: who implements (lead = orchestrator
plans/integrates/reviews; Opus-sub = critical impl; Sonnet-sub = low-risk impl; Haiku-sub = mechanical).
Each feature names the **reference** project to mirror (from FEATURE_MATRIX Canonical column).

| # | Milestone | Key features (ref project) | Model | Status | Result |
|---|---|---|---|---|---|
| M0 | **Foundation/scaffolding** | go.mod, layout, chi server, config (env), pgx pool, sqlc+goose, templ+Tailwind pipeline, base layout + design tokens (DESIGN_SYSTEM), htmx/Alpine, self-hosted fonts, session+CSRF middleware, error/render helpers, event bus + outbox skeleton, river worker skeleton, `/health` + `/health/ready` (ts), test harness (testify+testcontainers), Makefile, golangci-lint, CI skeleton | lead+Opus-sub | ✅ | Skeleton + adversarial hardening: build/vet/lint(0)/test all green; **RunInTx + bus/outbox wired, testcontainers tx-integration proves sync-rollback & same-tx outbox**; fonts self-hosted (Newsreader/Inter/Geist Mono woff2); CSP added; tokens match DESIGN_SYSTEM §2. Deferred→M1: home handler, proxy real-IP, Alpine CSP build, outbox async dispatch. Tools: go1.26.4, templ0.3.1020, sqlc1.31.1, goose3.27.1, golangci-lint2.12.2, tailwind4.3.1 |
| M1 | **Accounts / Auth / Authz** | User/Role/Permission DB (ts CASL→hand-rolled), argon2id login, signup+default role (django), email verification (django), password reset (django/laravel), social login Google+GitHub via goth (laravel breadth/ts wiring), rate-limited auth (ts/django), profiles+avatar+self-edit (laravel), public author page+ProfilePage JSON-LD (ts), password change (laravel), permission-gated admin shell (ts) | Opus-sub | ✅ | core+hardening+ext done & committed (fc93af9/7dfe804/5c8933c); 2 HIGH+3 MED auth security fixes (enumeration oracle, token race, session-invalidation, authz TTL, relay isolation); social login + admin shell + storage(min) all landed; tests every layer incl testcontainers; **carry→M2: gate content routes + Author ownership scoping; fill author Posts seam** |
| M2 | **Content core** | Post (ts) + Page hierarchical+template (django) + Service GEO type (django); draft/published enum + publishedAt preserved (ts); revisions snapshot (ts) + **restore UI** (net-new); soft-delete/trash/restore (ts); scheduled publishing river-periodic (net-new); reading time (laravel); related posts (laravel); rich-text Tiptap island + bluemonday sanitize on save (ts); bulk admin list actions (laravel) | Opus-sub | ⬜ | — |
| M3 | **Taxonomies** | Categories self-ref tree + M2M + per-locale (ts), category tree admin UI (django/ts), Tags M2M + archive (ts), taxonomy-filtered listings public+admin (ts) | Sonnet-sub | ⬜ | — |
| M4 | **Media** | Upload + magic-byte MIME validation, SVG reject, ext from MIME (ts); media library grid + metadata edit (ts); thumbnails + dims + bomb guard (django); picker in editor (laravel); Storage driver local→S3 (ts); per-asset metadata (ts) | Opus-sub | ⬜ | — |
| M5 | **Comments** | Threaded tree + status enum incl TRASH (ts); guest+auth (ts); moderation queue + status tabs + pending badge (ts); strip author email in public payload (ts); reCAPTCHA v3 optional (django/laravel); per-IP rate limit (ts); author self-edit window (laravel); comment-notification email (net-new) | Opus-sub | ⬜ | — |
| M6 | **Search** | PG tsvector FTS + ILIKE fallback (django); scope posts+pages+services (laravel); locale-scoped (django); public paginated results + empty state (ts) | Sonnet-sub | ⬜ | — |
| M7 | **i18n** | Per-locale content translation tables (django parler→PG); locale routing as-needed (ts); UI strings en/de/ru (ts); language switcher (ts); dashboard per-locale tab strip on forms (django); fallback to default (django) | Opus-sub | ✅ | M7a routing+catalogs+switcher; M7b overlay translation for posts/pages/services/**categories/tags** (migrations 00011–00014), editor `?language=xx` tab strip, base-fallback reads. Build/vet/lint/test green. |
| M8 | **SEO + GEO** | Per-content meta title/desc locale-aware + fallback (django); canonical override (django/laravel); noindex per-item + global (django); OG+Twitter + image fallback (laravel); JSON-LD full set + `<`/`>`/`&` escaped (laravel); sitemap.xml + hreflang alternates, drafts excluded (django); dynamic robots + AI-crawler toggle (django); hreflang head + x-default (django/ts); verification tags (django/laravel); SEO settings dashboard (laravel); GEO business identity + geoStatement (laravel); services→Service/ItemList schema (django); FAQ→FAQPage (django); llms.txt + llms-full.txt (django) | Opus-sub | ✅ | 5 slices: (1) meta data layer migr 00015 + overlay; (2) SEO head (canonical/robots/OG/Twitter/hreflang+x-default/verification) + env site-identity; (3) JSON-LD WebSite/Organization/Breadcrumb/Article/Service/FAQPage/ItemList (1 escaper); (4) /sitemap.xml (+hreflang)/robots.txt(AI-toggle)/llms.txt+full; (5) editor SEO panel (locale-aware). SEO settings **dashboard** UI deferred to M15 (fields are env-backed now). build/vet/lint/test green. |
| M9 | **Themes** | Runtime-swappable themes + fallback (ts); catalogue+metadata (ts); admin switcher (ts/django); `.theme-<id>` scoped CSS vars per DESIGN_SYSTEM (ts) | Sonnet-sub | ✅ | M9-1: DB settings store (migr 00016 `site_settings` k/v + `internal/settings`, cached) + `internal/theme` registry (default/sepia/noir, Resolve fallback) + `.theme-<id>` token overrides in tokens.css + public ThemeResolver middleware (context-based, admin-isolated). M9-2: `/admin/appearance` switcher (live palette preview, activate, read/update:theme gated). build/vet/lint/test green. |
| M10 | **Plugins** | Hook registry actions+filters+render-region (django); discovery + runtime enable/disable (django); plugin admin UI (django); sample reading-time plugin (django/ts); render-region template hooks (django) | Opus-sub | ✅ | M10-1: `internal/plugin` Manager (actions/filters/render-regions, per-callback panic recovery, registration-order dispatch); enable/disable via M9 settings store (`plugin:<id>`); sample reading-time `post_content` filter; layout `head`/`body_end` regions + post filter wired. M10-2: `/admin/plugins` manager (toggle, read/update:plugin gated). build/vet/lint/test green. |
| M11 | **Menus** | Admin menu builder, drag-sortable + keyboard-accessible, items→posts/pages/categories/custom, per-locale (laravel); public menu render in header/footer (laravel) | Sonnet-sub | ✅ | M11-1: `internal/content/menus` (migr 00017 menus/menu_items/menu_item_translations; service CRUD+reorder+ResolveForLocation w/ locale overlay + URL localization). M11-2: `/admin/menus` builder (item picker post/page/category/custom, keyboard up/down reorder, inline en/de/ru labels). M11-3: public header/footer render via registered menu source. Reorder is keyboard up/down (drag deferred). build/vet/lint/test green. |
| M12 | **Forms / Contact** | Contact form → admin email, reCAPTCHA, settings-driven recipient (laravel) | Sonnet-sub | ✅ | `internal/contact` — public /contact form (reCAPTCHA v3 + rate-limit + validate) → async `contact.submitted` outbox event → NotifyListener emails recipient (settings `contact_recipient` → cfg.ContactRecipient → AdminEmail). No migration (outbox = durability); worker registers the listener. Custom form builder out of scope (v1). build/vet/lint/test green. |
| M13 | **Caching & Performance** | Settings + hot-read cache, invalidate on write (laravel); page/fragment cache, invalidate on publish (laravel); Redis backend (laravel) | Opus-sub | ✅ | M13-1: `internal/platform/cache` (Cache iface; Memory/Redis(go-redis, SCAN)/Noop; CACHE_DRIVER). M13-2: anonymous page-response cache middleware (bypass session-cookie/HX/non-200/query; key locale+theme+path), cached sitemap + menu resolve; sync `content.published` listener clears page:/sitemap:, menu writes clear menu:. Unpublish/silent-edit bounded by TTL (noted). build/vet/lint/test green. |
| M14 | **Email / Notifications** | Transactional email backend wired for auth+contact (django/laravel); comment-notification email (net-new) | Sonnet-sub | ✅ | SMTP backend (go-mail v0.8.0) behind existing mailer ifaces; `mailer.New` factory (log\|smtp\|noop) with log-fallback on init error; multipart text+html, HTML-escaped user data, Reply-To on contact; injectable `sender` seam (network-free tests); `buildMailer` wired in server+worker. Config MAIL_DRIVER/SMTP_HOST/PORT/USERNAME/PASSWORD/MAIL_FROM/MAIL_FROM_NAME/SMTP_TLS. build/vet/lint/test green. |
| M15 | **Analytics & Settings** | GA4+GTM from settings, public-pages-only (django/laravel); general site settings (laravel); env secrets + `.env.example` (django) | Sonnet-sub | ⬜ | — |
| M16 | **RSS / Feeds** | `/rss.xml` + per-category feeds, published posts (net-new) | Sonnet-sub | ⬜ | — |
| M17 | **Public REST API + MCP** | Public read API + gated write API, validated (ts); MCP server OAuth 2.1 (laravel) porting ts 48-tool surface 1:1; health endpoints (ts) | Opus-sub | ⬜ | — |
| M18 | **Security hardening (cross-cut)** | write-time sanitize, JSON-LD escape, SVG/polyglot reject, path-traversal guards, CSRF+secure cookies+HSTS+nosniff+frame-deny (django), reCAPTCHA graceful | Opus-sub | ⬜ | — |
| M19 | **UI completeness pass** | Every public + admin surface to DESIGN_SYSTEM; Lighthouse ≥95 mobile (perf/SEO/a11y/best-practices); WCAG 2.1 AA; CWV budget; reduced-motion | lead+Opus-sub | ⬜ | — |
| M20 | **Tests / Quality / CI** | unit+integration ≥80% services/repos, 100% critical paths; E2E browser canonical flows (auth, content create→publish, media upload, search, dashboard) public+admin; coverage report; CI lint→typecheck(vet)→test→build→e2e | lead+Opus-sub | ⬜ | — |

Cross-cutting concerns (security, settings, caching, a11y, SEO escaping) are woven into each milestone,
not bolted on at the end; M18–M20 are the final verification sweeps.

**Per milestone:** TDD (tests first against canonical behavior) → implement handler→service→repository →
emit events/listeners → adversarial verification (2–3 Opus skeptics: parity-with-canon / correctness /
security / perf-concurrency) → check off here → refresh HANDOFF.md.

---

## 5. Domain event classification (sync in-tx vs async queued)

Synchronous = must run in the SAME DB transaction as the write (atomic, rolls back on failure),
implemented as in-transaction listeners. Asynchronous = fire-and-forget via river (transactional
outbox: event row written in-tx, relay publishes after commit). To be finalized per milestone; initial:

| Event | Trigger | Sync (in-tx) | Async (river) |
|---|---|---|---|
| `content.revision.created` | post/page update | ✅ snapshot must be atomic with update | |
| `content.published` | publish | ✅ status/publishedAt write | ⏩ cache invalidation, search reindex, sitemap touch |
| `content.scheduled.due` | river periodic | | ⏩ auto-publish job |
| `comment.created` | guest/auth submit | ✅ persist + pending status | ⏩ notification email to author/moderators |
| `comment.moderated` | approve/spam/trash | ✅ status write | ⏩ cache invalidation |
| `media.uploaded` | upload | ✅ persist row + dims | ⏩ thumbnail generation |
| `account.registered` | signup | ✅ user + default role | ⏩ verification email |
| `account.password_reset_requested` | reset | ✅ token persist | ⏩ reset email |
| `contact.submitted` | contact form | | ⏩ admin email |
| `search.reindex` | content write | | ⏩ tsvector refresh (or GENERATED column = sync, no event) |
| `cache.invalidate` | any public-visible write | | ⏩ purge keys / revalidate fragments |

Atomic effects (counts, revisions, status) are sync in-tx listeners — never detached goroutines/queued.

---

## 6. Per-layer test status (no layer at zero)

Targets: ≥80% on services/repositories, 100% critical paths (auth, content CRUD, publishing, media,
search). Templates checked by render tests + E2E; factories exercised by tests using them.

| Layer | Approach | Status |
|---|---|---|
| domain models/structs | table-driven unit | ⬜ |
| repositories | testcontainers-go (real Postgres) | ⬜ |
| services | unit w/ repo+bus mocks/fakes | ⬜ |
| handlers | httptest + service fakes | ⬜ |
| middleware | httptest (auth, csrf, rate-limit, locale) | ⬜ |
| validators (DTO) | table-driven unit | ⬜ |
| event listeners (observers) | unit (sync) + worker integration (async) | ⬜ |
| templ components | render-to-string assertions | ⬜ |
| factories/fixtures | exercised by consuming tests | ⬜ |
| background workers (river) | integration | ⬜ |
| CLI commands (cmd/*) | smoke/integration | ⬜ |
| E2E browser | playwright-go, data-testid selectors | ⬜ |

---

## 7. Toolchain prerequisites (environment)

Go was **not installed** on this machine — installing Go (Homebrew) as the enabling step. Also needed
(install via `go install` / standalone): `templ`, `sqlc`, `goose`, `golangci-lint`, `gofumpt`, river CLI,
Tailwind standalone binary, Playwright browsers. Postgres + Redis for integration (testcontainers/docker).
Recorded in Makefile `make tools`. **No build/test claims without showing real command output.**

---

## 8. Open questions / decisions log
- Module path `github.com/huseyn0w/cmstack-go` (from git remote). ✔
- Product display name: **CMStack-Go** (per README). ✔
- Locales: en (default), de, ru — matches ts/django canon. ✔
- Proceeding on stack choices per autonomy directive; will surface only genuinely irreversible/product
  decisions the canon doesn't answer.
