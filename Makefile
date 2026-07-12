# agentic-cms-go — local development helpers.
#
#   make dev         fully dockerized dev: Postgres + Redis + migrate/seed +
#                    server + worker, all in Docker with hot-reload on file save
#   make dev-logs    follow the dockerized server + worker logs
#   make dev-down    stop the dockerized dev stack (keeps DB + Go caches)
#   make dev-clean   stop the dev stack and drop its volumes (wipes DB + caches)
#
#   make dev-native  legacy flow: only Postgres in Docker, server runs via `go run`
#                    (needs `make tools` + local templ/tailwind first)
#
# Run `make` (or `make help`) to list every target.

SHELL := /bin/bash
# Only used by the native (dev-native/run/…) targets. Guarded so the fully
# dockerized `make dev` works on a machine with no local Go toolchain.
GOPATH_BIN := $(shell command -v go >/dev/null 2>&1 && echo "$$(go env GOPATH)/bin")
export PATH := $(PATH):$(GOPATH_BIN)

TAILWIND := ./bin/tailwindcss
PG_CONTAINER := agentic-cms-go-db

# Native HTTP port for `go run ./cmd/server` (matches HTTP_ADDR :8090 in .env.example).
# `make kill` frees it from a server orphaned by a previous Ctrl-C (Postgres runs in
# Docker, so 5434 is not touched here).
DEV_PORTS := 8090

# The Go config reads process env and does NOT auto-load .env, so the DB-facing
# targets (run/worker/seed/migrate-*) source it here. Local dev only.
LOAD_ENV := set -a; [ -f .env ] && source .env; set +a

.DEFAULT_GOAL := help
DEV_COMPOSE := docker compose -f docker-compose.dev.yml

.PHONY: help dev dev-native dev-logs dev-down dev-clean up down reset logs seed migrate test kill clean env db-up db-down tools generate templ sqlc tailwind build run worker migrate-up migrate-down cover lint vet fmt ci docker-build docker-up docker-down docker-logs

help: ## List the common targets
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
	  | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

dev: env ## Fully dockerized dev: Postgres + Redis + migrate/seed + server + worker, with hot-reload
	@echo "starting dockerized dev stack — server: http://localhost:8090"
	$(DEV_COMPOSE) up --build

dev-native: kill env db-up migrate-up seed ## Legacy: only Postgres in Docker, server runs natively via `go run`
	@echo "server: http://localhost:8090"
	@$(LOAD_ENV); go run ./cmd/server

dev-logs: ## Follow the dockerized dev server + worker logs
	$(DEV_COMPOSE) logs -f server worker

dev-down: ## Stop the dockerized dev stack (keeps DB + Go caches)
	$(DEV_COMPOSE) down

dev-clean: ## Stop the dockerized dev stack and drop its volumes (wipes DB + caches)
	$(DEV_COMPOSE) down -v

up: db-up ## Start this stack's dockerized dependency (Postgres); run the app with `make dev`/`make run`

down: ## Stop the local Postgres container (keeps data)
	-docker stop $(PG_CONTAINER)

reset: ## Wipe the DB (remove the Postgres container) and re-bootstrap from scratch
	-docker rm -f $(PG_CONTAINER)
	$(MAKE) dev

logs: ## Follow the local Postgres container logs
	docker logs -f $(PG_CONTAINER)

migrate: migrate-up ## Apply DB migrations (alias for migrate-up)

clean: ## Stop and remove the local Postgres container (destroys data)
	-docker rm -f $(PG_CONTAINER)

kill: ## Free the native HTTP port from any stale server process
	@pids=$$(lsof -ti:$(DEV_PORTS) -sTCP:LISTEN 2>/dev/null); \
	  if [ -n "$$pids" ]; then \
	    echo "freeing stale server on $(DEV_PORTS): $$pids"; \
	    kill $$pids 2>/dev/null || true; sleep 1; \
	    pids=$$(lsof -ti:$(DEV_PORTS) -sTCP:LISTEN 2>/dev/null); \
	    [ -n "$$pids" ] && kill -9 $$pids 2>/dev/null || true; \
	  fi

env: ## Create .env from .env.example if it does not exist yet
	@[ -f .env ] || { cp .env.example .env; echo "created .env from .env.example"; }

db-up: ## Start a local Postgres for this stack on :5434 (matches .env.example)
	@if [ -z "$$(docker ps -q -f name=^$(PG_CONTAINER)$$)" ]; then \
	  if [ -n "$$(docker ps -aq -f name=^$(PG_CONTAINER)$$)" ]; then docker start $(PG_CONTAINER) >/dev/null; \
	  else docker run -d --name $(PG_CONTAINER) \
	    -e POSTGRES_USER=postgres -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=agentic-cms \
	    -p 5434:5432 postgres:16-alpine >/dev/null; fi; \
	fi
	@echo "waiting for postgres…"; until docker exec $(PG_CONTAINER) pg_isready -U postgres >/dev/null 2>&1; do sleep 1; done; echo "postgres ready on :5434"

db-down: ## Stop and remove the local Postgres container
	-docker rm -f $(PG_CONTAINER)

## Install pinned dev tools into GOPATH/bin.
tools:
	go install github.com/a-h/templ/cmd/templ@latest
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
	go install github.com/pressly/goose/v3/cmd/goose@latest
	go install mvdan.cc/gofumpt@latest
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest

## Generate templ components and sqlc code.
generate: templ sqlc

templ:
	templ generate

sqlc:
	cd db && sqlc generate

## Build the Tailwind stylesheet.
tailwind:
	$(TAILWIND) -i web/tailwind.css -o web/static/app.css --minify

build: ## Compile everything
	go build ./...

run: ## Run the HTTP server (loads .env)
	@$(LOAD_ENV); go run ./cmd/server

worker: ## Run the background worker (loads .env)
	@$(LOAD_ENV); go run ./cmd/worker

seed: ## Idempotently seed roles/permissions/admin + demo content en/de/ru (loads .env)
	@$(LOAD_ENV); go run ./cmd/seed
	@$(LOAD_ENV); go run ./cmd/seedcontent

migrate-up: ## Apply DB migrations (loads .env)
	@$(LOAD_ENV); go run ./cmd/migrate up

migrate-down: ## Roll back the last migration (loads .env)
	@$(LOAD_ENV); go run ./cmd/migrate down

test: ## Run the test suite
	go test ./...

cover:
	go test -p 2 -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

lint:
	golangci-lint run

vet:
	go vet ./...

fmt: ## Apply formatting via golangci-lint's pinned gofumpt (single source of truth)
	golangci-lint fmt

ci: generate ## Run the full CI pipeline locally (vet, lint, format, build, test+coverage)
	go vet ./...
	golangci-lint run
	golangci-lint fmt --diff
	go build ./...
	go test -p 2 -timeout 20m -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

docker-build: ## Build the production container image (agentic-cms-go:latest)
	docker build -t agentic-cms-go:latest .

docker-up: ## Start the full prod stack (needs .env.prod — cp from .env.prod.example)
	docker compose --env-file .env.prod up -d --build

docker-down: ## Stop the prod stack (add ARGS=-v to also drop volumes)
	docker compose --env-file .env.prod down $(ARGS)

docker-logs: ## Tail the server + worker logs
	docker compose --env-file .env.prod logs -f server worker
