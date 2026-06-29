# CMStack-Go — HANDOFF

Living continuation doc. Refreshed at every milestone. Pairs with `BUILD_PLAN.md` (full plan/stack
mapping/event classification) and the read-only canon `../FEATURE_MATRIX.md` + `../DESIGN_SYSTEM.md`.

**Last updated:** M3 done, M4 (media) in progress. Remote `main` current through M3 (pushed). Module `github.com/huseyn0w/cmstack-go`. (Local branch name varies — push with `git push origin HEAD:main`, fast-forward.)

## Toolchain / how to run
- Go 1.26.4 (`/opt/homebrew/bin/go`). Always `export PATH="$PATH:$(go env GOPATH)/bin"` so templ/sqlc/goose/golangci-lint resolve.
- Tools: templ 0.3.1020, sqlc 1.31.1, goose 3.27.1, golangci-lint 2.12.2, gofumpt 0.10.0, tailwind standalone 4.3.1 (`./bin/tailwindcss`).
- Docker running → testcontainers integration tests run on full `go test ./...` (use `-short` to skip).
- Postgres needs `citext` + `pgcrypto` (migrations enable them).
- Commands: `make generate` (templ+sqlc), `make tailwind`, `make build`, `make test`, `make lint`, `make migrate-up`. Seed: `go run ./cmd/seed`. Server: `go run ./cmd/server`. Worker (outbox relay): `go run ./cmd/worker`.
- Verify gate before any "done": `go build ./... && go vet ./... && golangci-lint run && go test ./...` — all must be green; show output.

## Architecture (hold this line — adversarial review enforces)
`handler (thin: decode→validate→service→render, ZERO logic/data-access) → service (all logic; data only via repo interfaces; side effects only via events) → repository (only sqlc/pgx user) → db`.
Events: `db.RunInTx(ctx,pool,fn)` is the blessed tx seam; `events.Bus.Publish(ctx,tx,evt)`; **sync** listeners run in-tx (atomic, roll back on error), **async** go to the `outbox` table in-tx → `cmd/worker` relay drains post-commit (per-row error isolation + dead-letter after 5 attempts). Explicit constructor DI in `cmd/server/main.go` via `web.Deps`; no globals.
Authz: hand-rolled `accounts.Authorizer.Can(ctx,userID,action,subject)` (Casbin rejected). Permissions `(action,subject)`, `manage`=any action, `all`=any subject; loaded from `role_permissions`, cached with 60s safety TTL.

## DONE
- **M0** foundation: chi router, slog, caarlos0/env config, pgxpool+`RunInTx`, sqlc(pgx/v5)+goose, event bus+transactional outbox+relay, scs sessions, nosurf CSRF, security headers + baseline CSP (prod-gated HSTS), go-playground/validator, templ base `layout.templ` with full DESIGN_SYSTEM tokens (light/dark) + self-hosted woff2 (Newsreader/Inter/Geist Mono), htmx2+alpine3, `/health` + `/health/ready`, Makefile, `.golangci.yml` (0 issues), CI workflow, testcontainers harness.
- **M1** auth (hardened, security-reviewed): users/roles/permissions/role_permissions, email-verification + password-reset tokens (sha256, single-use **atomic consume**, expiring), `password_changed_at` epoch (session invalidation on reset/change), oauth_accounts; idempotent seed (4 roles Administrator/Editor/Author/Member + full permission matrix + default admin from env). argon2id hasher. AuthService: register/login/logout/change-pw/forgot/reset/verify-email with **anti-enumeration** (real dummy-hash + generic errors), signup + email-verify toggles via `SettingsProvider` (config-backed; admin UI=M15). Social login via **goth** (Google+GitHub, enabled only with env keys; `internal/platform/oauth`, routes `/auth/{provider}` + callback). **Admin shell** `web/templ/admin.templ` (AdminBase/sidebar 260px/topbar 56px per DESIGN_SYSTEM §5, permission-gated nav HIDDEN not disabled, dark/light toggle). Profiles + `/account` (name/bio/website/socials + avatar upload + change-password). Public author `/authors/{id}` (no email leak) + ProfilePage/Person JSON-LD (escaped). `internal/platform/storage` (LocalStorage + magic-byte avatar validation, SVG reject, /uploads nosniff+traversal guard). Per-IP rate limiting on auth POSTs. Tests every layer incl testcontainers concurrency.

## Migrations on disk
`00001_init` (outbox, schema_meta) · `00002_auth` · `00003_auth_hardening` · `00004_oauth` · `00005_posts` (posts, post_likes, revisions) · `00006_pages_services` (pages, services, service_faqs) · `00007_taxonomies` (categories, tags, post_categories, post_tags). M4 adds `00008_*` (media).

## DONE since M1
- **M2** content: kernel (`internal/content/kernel`: status DRAFT/PUBLISHED, slug+dedupe, bluemonday sanitizer pinned, reading-time, generic `revisions` snapshot+restore). **Posts** (`internal/content/posts`): full CRUD, publish-once/preserve, revisions+restore UI, trash/restore/permanent-delete, scheduled publish (worker periodic `PublishDue`), likes, reading time, self-hosted rich-text editor island (Alpine+contenteditable, no Node/CDN) + write-time sanitize, **ownership enforced in service** (Author=own only). **Pages** (hierarchy+cycle-prevention, template selector allow-list, revisions, trash). **Services** (GEO type: summary/price/area_served + ordered `service_faqs`, JSON-LD seam→M8). **Bulk actions** (`internal/web/bulk_admin.go` + `web/templ/bulk_admin.templ`): select-all + bulk trash/restore/publish across posts/pages/services, per-id perm/ownership re-checked. Public: `/blog` index + `/blog/{slug}` article, BlogPosting JSON-LD seam. Adversarial-reviewed (clean; minor fixes applied). Events: `content.revision.created` sync, `content.published` async.
- **M3** taxonomies: **categories** (`internal/content/categories`: self-ref tree, cycle prevention, slug dedupe, BuildTree) + **tags** (flat); `post_categories`/`post_tags` M2M assigned **in the post write tx**; admin tree+parent-picker, editors, bulk delete; public `/categories/{slug}` + `/tags/{slug}` archives, `?category=&?tag=` filters on `/blog`, taxonomy pills on post detail, **related-posts** block. Subjects `category`+`tag` added to authorizer + seed.

## PENDING (ordered — resume here)
- **M4 Media** (IN PROGRESS) — extend `internal/platform/storage` (Strategy: local default + S3 via aws-sdk-go-v2); media domain (upload + magic-byte validation, SVG/polyglot reject, ext-from-MIME, decompression-bomb guard, thumbnails+dims), library grid admin UI, **fill the post-editor media-insert seam** (`web/templ/posts_editor.templ` `// TODO(M4)`), per-asset metadata, bulk delete. Migration 00008.
- **M5** Comments · **M6** Search (PG FTS over posts/pages/services) · **M7** i18n (per-locale content translation — biggest gap; category/tag/content `*_translations` tables; `// TODO(M7)` seams already in taxonomy + content models) · **M8** SEO+GEO (fill BlogPosting/Service/FAQPage JSON-LD seams, per-content meta/canonical/noindex fields, sitemap, robots, llms.txt) · **M9** Themes · **M10** Plugins · **M11** Menus · **M12** Contact · **M13** Caching(Redis; in-proc ratelimit→Redis) · **M14** Email(SMTP; LogMailer now; comment notif) · **M15** Analytics+Settings UI (SettingsProvider config-backed now) · **M16** RSS · **M17** REST API + MCP · **M18** security sweep · **M19** UI/Lighthouse · **M20** coverage+E2E+CI. (Full detail in BUILD_PLAN §4.)

## Carry-over notes / seams for later milestones
- `RequirePermission` middleware exists+tested but only `/admin` is gated so far — **M2 must gate every admin content route + action**, and add **ownership scoping** so Author `update:post` = own posts only (seed grant is coarse; enforce in post service).
- Author public page `Posts` is an empty `TODO(M2)` seam — fill with author's published posts in M2.
- Mailer is dev `LogMailer` (logs verify/reset links); real SMTP = M14. SettingsProvider is config-backed; admin Settings UI = M15. Rate limiter is in-proc; Redis option = M13.
- Relay dispatch path is real (worker drains outbox → listeners). Email listener registered.

## Open questions
- None blocking. Stack choices settled (see BUILD_PLAN §2). Proceeding autonomously per user directive; ask only on irreversible/product decisions the canon doesn't answer.

## Continuation prompt (paste into a fresh window)
> Resume building CMStack-Go (`/Users/huseyn0w/Desktop/SWE/cmstack/cmstack-go`). Read `HANDOFF.md` + `BUILD_PLAN.md` + canon `../FEATURE_MATRIX.md` + `../DESIGN_SYSTEM.md`. You are the ORCHESTRATOR: delegate implementation to subagents (TDD, adversarial verification with 2–3 skeptics per module), keep your context lean, protect the strict layering (thin handlers → services → repos; side effects via events/outbox; sync vs async classification). Go at `/opt/homebrew/bin/go`, `export PATH="$PATH:$(go env GOPATH)/bin"`, Docker running. Verify every "done" with real `go build/vet/test` + `golangci-lint` output — no claims without evidence. Commit per milestone (no co-author trailer). **Respond in Russian; code/docs in English.** Resume from the first PENDING milestone (M2 Content core). Work autonomously; only ask on genuinely critical/irreversible decisions.
