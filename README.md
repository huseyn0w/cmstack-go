# CMStack-Go

Analog of WordPress written in a Go stack — lighter, simpler, faster, and more secure. Server-rendered.

The Go member of the **cmstack** CMS family: the same product as
[`cmstack-django`](../cmstack-django), [`cmstack-laravel`](../cmstack-laravel), and
[`cmstack-ts`](../cmstack-ts), built clean from day one in idiomatic Go.

It implements the shared canon at the repo root: the [Feature Matrix](../FEATURE_MATRIX.md)
(what every stack must do) and the [Design System](../DESIGN_SYSTEM.md) (one visually identical,
quiet-luxury UI).

> **Status:** under active construction. Foundation + Auth + Content (Posts/Pages/Services) are
> implemented and tested; remaining modules are tracked in [Roadmap & parity](#roadmap--parity)
> and [`BUILD_PLAN.md`](BUILD_PLAN.md).

## Stack

| Concern | Choice |
|---|---|
| Language / HTTP | Go 1.26, [`net/http`](https://pkg.go.dev/net/http) + [chi v5](https://github.com/go-chi/chi) router |
| Templating | [templ](https://github.com/a-h/templ) (compile-time typed components) + [Tailwind CSS](https://tailwindcss.com) standalone CLI (no Node) |
| Interactivity | [htmx](https://htmx.org) v2 + [Alpine.js](https://alpinejs.dev) v3 — server-rendered islands, near-zero JS |
| Database | PostgreSQL via [pgx v5](https://github.com/jackc/pgx); type-safe queries with [sqlc](https://sqlc.dev); migrations with [goose](https://github.com/pressly/goose) |
| Auth | session-based ([scs](https://github.com/alexedwards/scs)), argon2id hashing, social login via [goth](https://github.com/markbates/goth) (Google + GitHub) |
| Authorization | hand-rolled DB-backed `(action, subject)` RBAC (CASL-equivalent) |
| Events | in-process typed bus + **transactional outbox** (sync in-tx listeners / async via a worker relay) |
| Validation | [go-playground/validator](https://github.com/go-playground/validator) |
| Sanitization | [bluemonday](https://github.com/microcosm-cc/bluemonday) (write-time) |
| Media | magic-byte MIME validation ([mimetype](https://github.com/gabriel-vasile/mimetype)), pluggable `Storage` (local → S3) |
| Testing | `go test` + [testify](https://github.com/stretchr/testify), [testcontainers-go](https://golang.testcontainers.org) (real Postgres), Playwright (E2E) |
| Quality | [golangci-lint](https://golangci-lint.run) v2 + gofumpt |

Full rationale and the per-concern mapping from the mature stacks lives in [`BUILD_PLAN.md`](BUILD_PLAN.md) §2.

## Requirements

- Go 1.26+
- PostgreSQL 14+ (extensions `citext`, `pgcrypto` — enabled by migrations)
- Docker (for integration tests via testcontainers; optional Redis for caching, later milestones)
- Dev tools (installed by `make tools`): `templ`, `sqlc`, `goose`, `golangci-lint`, `gofumpt`, and the Tailwind standalone binary (`./bin/tailwindcss`)

## Quick start

```bash
make tools                  # install templ, sqlc, goose, golangci-lint, gofumpt, tailwind
cp .env.example .env         # set DATABASE_URL, SESSION_KEY, ADMIN_EMAIL/PASSWORD, etc.
make generate                # templ generate + sqlc generate
make tailwind                # build web/static/app.css
make migrate-up              # apply DB migrations
go run ./cmd/seed            # seed roles, permissions, default administrator
make run                     # start the server (http://localhost:8090)
go run ./cmd/worker          # (separate process) outbox relay + scheduled publishing
```

## Architecture

Strict, one-directional layering — enforced by adversarial review at every milestone:

```
HTTP handler  (thin: decode → validate DTO → call service → render/encode; ZERO logic/data access)
   → service   (ALL business logic; data only via repository interfaces; side effects only via events)
      → repository  (the only layer touching sqlc/pgx)
         → database (PostgreSQL)
service → event bus → listeners        (side effects: email, cache invalidation, search reindex, audit)
```

- **Handlers never contain business logic or data access.** They are the HTTP boundary, nothing more.
- **Services never touch the DB directly** (only via repositories) and **never fire side effects inline** —
  they emit a domain event; listeners handle the effect.
- **Events are classified**: *synchronous* listeners run inside the same DB transaction (atomic, roll back
  on error); *asynchronous* listeners are delivered via a **transactional outbox** (`db.RunInTx` writes the
  event row in-tx; the `cmd/worker` relay drains it after commit, with per-row error isolation + dead-letter).
- **Dependencies are wired explicitly** (constructor injection in `cmd/server`) — no globals, no DI framework.

Patterns earn their place (Repository, Service, Observer/event-bus, Transactional outbox, Middleware,
Strategy/Adapter for storage & OAuth). Rejected over-engineering (Casbin, CQRS, generic base-repositories,
DI containers) is documented in [`BUILD_PLAN.md`](BUILD_PLAN.md) §3.

## Project layout

```
cmd/
  server/        # main entrypoint: config, pgx pool, wiring, graceful shutdown
  worker/        # outbox relay + scheduled-publishing scan
  migrate/ seed/ # goose runner; idempotent roles/permissions/admin seed
internal/
  platform/      # cross-cutting infra: config, db (pgx + RunInTx), events (bus + outbox),
                 #   render (templ), session, security (argon2id, CSRF, headers, CSP),
                 #   storage, mailer, oauth, ratelimit, validate, logging
  accounts/      # users, roles, permissions, authorizer, auth, profiles, oauth
  content/
    kernel/      # shared: status, slug, sanitizer, reading-time, revisions
    posts/  pages/  services/   # bounded content types (handler→service→repo each)
  web/           # router assembly, middleware, admin + public HTTP handlers
db/
  migrations/    # goose .sql migrations (checked in)
  queries/       # sqlc .sql query files
web/
  templ/         # .templ component sources (layout, admin shell, public, components)
  static/        # compiled Tailwind CSS, vendored htmx/alpine/editor, self-hosted fonts
e2e/             # Playwright browser tests
```

## Features

Implemented so far (✅) — tracked against the canonical [Feature Matrix](../FEATURE_MATRIX.md):

- ✅ **Auth & accounts**: email/username + password (argon2id), signup + email verification, password
  reset/change, social login (Google + GitHub), sessions with fixation + post-credential-change
  invalidation, per-IP rate limiting, anti-enumeration.
- ✅ **Roles & permissions**: Administrator / Editor / Author / Member, granular `(action, subject)`
  permissions enforced server-side, permission-gated admin shell (sidebar reflects permissions).
- ✅ **Profiles**: self-service `/account` (bio, website, socials, avatar upload), public author page
  (`/authors/{id}`) with `ProfilePage`/`Person` JSON-LD, no email leak.
- ✅ **Content**: Posts (revisions + restore, draft/publish with preserved `publishedAt`, scheduled
  publishing, trash/restore, likes, reading time, rich-text + write-time sanitization), Pages
  (hierarchy, template selector, revisions, trash), Services (GEO type with ordered FAQ).
- ✅ **Admin lists**: tables with status tabs, pagination, and bulk actions (select-all + bulk
  trash/restore/publish, per-id permission/ownership re-checked).
- ✅ **Platform**: design-system tokens (light/dark), self-hosted variable fonts, security headers + CSP,
  CSRF, health/readiness endpoints, transactional outbox.

See [Roadmap & parity](#roadmap--parity) for what's next.

## Commands

Common `make` targets (see the [`Makefile`](Makefile) for the full list):

| Target | Action |
|---|---|
| `make tools` | install all dev tools |
| `make generate` | `templ generate` + `sqlc generate` |
| `make tailwind` | build `web/static/app.css` |
| `make build` / `make run` | build / run the server |
| `make test` / `make cover` | run tests / with coverage |
| `make lint` / `make vet` | golangci-lint / go vet |
| `make migrate-up` / `make migrate-down` | apply / roll back migrations |

## Testing

```bash
go test ./...          # full suite (integration tests use testcontainers — Docker required)
go test -short ./...   # skip integration tests
make cover             # coverage report
```

Every architectural layer is tested: domain, repositories (real Postgres), services, handlers,
middleware, validators, event listeners, and templ components. Integration tests spin up Postgres via
testcontainers; E2E browser flows (Playwright) cover the canonical critical paths. **No "passing" claim
is made without showing the real command output.**

## Continuous integration

[`.github/workflows/ci.yml`](.github/workflows/ci.yml) runs on every push/PR:
`make generate` → `go vet` → `golangci-lint` → `go test ./...` → `go build ./...`.

## Roadmap & parity

Done: **M0** foundation · **M1** auth/authz/profiles/admin-shell · **M2** content (posts/pages/services
+ bulk actions). In progress / upcoming (see [`BUILD_PLAN.md`](BUILD_PLAN.md) §4 for the full task board):

- **M3** Taxonomies (categories tree, tags, filtered listings)
- **M4** Media library (thumbnails, editor picker, S3 driver)
- **M5** Comments (threaded, moderation, notifications)
- **M6** Search (Postgres FTS) · **M7** i18n (per-locale content) · **M8** SEO + GEO (meta, JSON-LD,
  sitemap, robots, llms.txt) · **M9** Themes · **M10** Plugins · **M11** Menus · **M12** Contact ·
  **M13** Caching (Redis) · **M14** Email · **M15** Analytics + Settings · **M16** RSS · **M17** REST
  API + MCP server · **M18** security sweep · **M19** UI/Lighthouse pass · **M20** coverage + E2E + CI.

## License

See [LICENSE](LICENSE).
