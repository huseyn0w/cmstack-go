SHELL := /bin/bash
GOPATH_BIN := $(shell go env GOPATH)/bin
export PATH := $(PATH):$(GOPATH_BIN)

TAILWIND := ./bin/tailwindcss

.PHONY: tools generate templ sqlc tailwind build run worker seed migrate-up migrate-down test cover lint vet fmt

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

build:
	go build ./...

run:
	go run ./cmd/server

worker:
	go run ./cmd/worker

## Idempotently seed roles/permissions/admin (also runs at server startup).
seed:
	go run ./cmd/seed

migrate-up:
	go run ./cmd/migrate up

migrate-down:
	go run ./cmd/migrate down

test:
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
