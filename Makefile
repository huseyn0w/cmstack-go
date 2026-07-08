# cmstack-go — local development helpers.
#
#   make dev     one command: .env + local Postgres + migrate + seed, then run
#   make up      start the dockerized dependency (Postgres); the app runs natively
#   make down    stop the Postgres container (keeps data)
#
# NOTE: unlike the other stacks this one has no docker-compose — only Postgres runs
# in Docker; the server/worker run natively via `go run`. The up/down/logs/clean
# targets therefore operate on the single Postgres container.
#
# Run `make` (or `make help`) to list every target.

SHELL := /bin/bash
GOPATH_BIN := $(shell go env GOPATH)/bin
export PATH := $(PATH):$(GOPATH_BIN)

TAILWIND := ./bin/tailwindcss
PG_CONTAINER := cmstack-go-db

# Native HTTP port for `go run ./cmd/server` (matches HTTP_ADDR :8090 in .env.example).
# `make kill` frees it from a server orphaned by a previous Ctrl-C (Postgres runs in
# Docker, so 5434 is not touched here).
DEV_PORTS := 8090

# The Go config reads process env and does NOT auto-load .env, so the DB-facing
# targets (run/worker/seed/migrate-*) source it here. Local dev only.
LOAD_ENV := set -a; [ -f .env ] && source .env; set +a

.DEFAULT_GOAL := help
.PHONY: help dev up down reset logs seed migrate test kill clean env db-up db-down tools generate templ sqlc tailwind build run worker migrate-up migrate-down cover lint vet fmt

help: ## List the common targets
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
	  | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

dev: kill env db-up migrate-up seed ## One command: .env + local Postgres + migrate + seed, then run
	@echo "server: http://localhost:8090"
	@$(LOAD_ENV); go run ./cmd/server

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
	    -e POSTGRES_USER=postgres -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=cmstack \
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
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

lint:
	golangci-lint run

vet:
	go vet ./...

fmt:
	gofumpt -w .
