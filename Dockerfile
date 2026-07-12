# syntax=docker/dockerfile:1

# ── Stage 1: build ────────────────────────────────────────────────────────────
# Compiles the static Go binaries (server, worker, migrate) and the minified
# Tailwind stylesheet. The generated templ *_templ.go files are committed, so no
# templ step is needed here; only app.css must be built (it is gitignored).
FROM golang:1.26-bookworm AS build

# TARGETARCH is provided by BuildKit (amd64 / arm64). Map it to the Tailwind
# standalone release asset naming (x64 / arm64).
ARG TARGETARCH
ARG TAILWIND_VERSION=v4.3.1

WORKDIR /src

# Cache module downloads across builds.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Full source (needed for `go build` AND for Tailwind to scan the .templ files
# referenced by web/tailwind.css @source globs).
COPY . .

# Fetch the Tailwind standalone CLI and build the minified stylesheet. The
# @source globs in web/tailwind.css are relative to web/, so build from repo root.
RUN set -eux; \
    case "${TARGETARCH}" in \
      amd64) tw_arch=x64 ;; \
      arm64) tw_arch=arm64 ;; \
      *) echo "unsupported TARGETARCH: ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    curl -fsSL -o /usr/local/bin/tailwindcss \
      "https://github.com/tailwindlabs/tailwindcss/releases/download/${TAILWIND_VERSION}/tailwindcss-linux-${tw_arch}"; \
    chmod +x /usr/local/bin/tailwindcss; \
    tailwindcss -i web/tailwind.css -o web/static/app.css --minify

# Build the binaries. CGO is off (all deps are pure Go) so the result is a fully
# static binary that runs on distroless/static. timetzdata embeds the zoneinfo DB
# so time formatting never needs a system tzdata package.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    set -eux; \
    CGO_ENABLED=0 GOFLAGS=-trimpath \
      go build -tags timetzdata -ldflags "-s -w" -o /out/server  ./cmd/server; \
    CGO_ENABLED=0 GOFLAGS=-trimpath \
      go build -tags timetzdata -ldflags "-s -w" -o /out/worker  ./cmd/worker; \
    CGO_ENABLED=0 GOFLAGS=-trimpath \
      go build -tags timetzdata -ldflags "-s -w" -o /out/migrate ./cmd/migrate; \
    mkdir -p /out/uploads

# ── Stage 2: runtime ──────────────────────────────────────────────────────────
# distroless/static: no shell, no package manager, includes CA certificates.
# Runs as the unprivileged "nonroot" user (uid 65532).
FROM gcr.io/distroless/static-debian12:nonroot AS runtime

WORKDIR /app

# Binaries.
COPY --from=build /out/server  /app/server
COPY --from=build /out/worker  /app/worker
COPY --from=build /out/migrate /app/migrate

# Runtime file dependencies read from disk relative to the working directory:
#   - web/static  → served at /static (includes fonts + the built app.css)
#   - db/migrations → applied by the migrate binary (goose)
COPY --from=build /src/web/static     /app/web/static
COPY --from=build /src/db/migrations  /app/db/migrations

# Local blob storage target (UPLOAD_DIR default ./uploads). Created owned by
# nonroot so a fresh named volume mounted here inherits writable ownership.
COPY --from=build --chown=65532:65532 /out/uploads /app/uploads
USER 65532:65532

EXPOSE 8090

# Default to the web server. CMD (not ENTRYPOINT) so compose `command:` can fully
# swap the binary for the worker/migrate services.
CMD ["/app/server"]
