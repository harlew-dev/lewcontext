SHELL := /bin/bash
.DEFAULT_GOAL := help
# Read local development configuration without placing credentials in tracked files.
ifneq (,$(wildcard .env))
include .env
export
endif
.PHONY: help install migrate migrate-status migrate-rollback migrate-fresh migration seed prune dev api web web-build build serve test test-integration test-e2e check
help:
	@echo 'Targets: install migrate migrate-status migrate-rollback migrate-fresh migration seed prune dev api web web-build build serve test test-integration test-e2e check'
install:
	cd apps/api && go mod download
	cd apps/web && npm ci
migrate:
	cd apps/api && go run ./cmd/migrate
migrate-status:
	cd apps/api && go run ./cmd/migrate status
# Previews the last migration group; CONFIRM=1 runs its destructive down SQL.
migrate-rollback:
	cd apps/api && go run ./cmd/migrate rollback $(if $(CONFIRM),-confirm)
# Development only, while migrations are squashed: lists the tables it would drop;
# CONFIRM=1 drops every table and migrates from scratch. Refused when APP_ENV=production.
migrate-fresh:
	cd apps/api && go run ./cmd/migrate fresh $(if $(CONFIRM),-confirm)
migration:
	@test -n "$(name)" || (echo 'Usage: make migration name=add_tasks'; exit 1)
	cd apps/api && go run ./cmd/migrate create $(name)
# make seed [only=demo|volume|users] [count=n] [projects=n] [email=address]
seed:
	cd apps/api && go run ./cmd/seed $(if $(only),-only '$(only)') $(if $(count),-count '$(count)') $(if $(projects),-projects '$(projects)') $(if $(email),-email '$(email)')
# make prune [target=events [older-than=90d]] [CONFIRM=1]: deletes old rows from log tables.
# Previews unless CONFIRM=1 and a target; without a target previews every one at its default age.
prune:
	cd apps/api && go run ./cmd/prune $(if $(older-than),-older-than '$(older-than)') $(if $(CONFIRM),-confirm) $(target)
# Development in one terminal: rebuilds the embedded web app, then runs the API and Vite together (scripts/dev.sh).
dev:
	./scripts/dev.sh
# Or each in its own terminal: the API on API_ADDR, and Vite on :3000 proxying /api to it.
api:
	cd apps/api && go run ./cmd/server
web:
	cd apps/web && npm run dev
# Builds the web app and copies it where the Go binary embeds it (internal/web/dist).
web-build:
	./scripts/build-web.sh
# One binary serving the API and the web app.
build: web-build
	mkdir -p bin
	cd apps/api && go build -trimpath -o ../../bin/api ./cmd/server
# Production-like: the built binary serves both on :3000 (APP_ORIGIN). Stop make api and make web first.
serve: build
	API_ADDR=127.0.0.1:3000 ./bin/api
test:
	cd apps/api && go test -race ./...
	cd apps/web && npm test
test-integration:
	@test -n "$$TEST_DATABASE_URL" || (echo 'Set TEST_DATABASE_URL to a dedicated test database URL'; exit 1)
	cd apps/api && go test -race -count=1 ./...
test-e2e:
	cd apps/web && npm run test:e2e
check:
	@test -z "$$(gofmt -l apps/api)" || (echo 'Run gofmt -w apps/api'; exit 1)
	cd apps/api && go vet ./...
	cd apps/web && npm run typecheck && npm run lint && npm run format:check
