SHELL := /bin/bash
.DEFAULT_GOAL := help
# Read local development configuration without placing credentials in tracked files.
ifneq (,$(wildcard .env))
include .env
export
endif
# With MAIL_DRIVER=log, email goes to one mailbox file whichever directory a
# target runs from. Normally it goes to Mailpit (make db): http://localhost:8025.
MAIL_FILE ?= $(CURDIR)/tmp/mail.log
export MAIL_FILE
.PHONY: help install app-key migrate migrate-status migrate-rollback migrate-fresh migration seed prune admin dev api web web-build build serve serve-test test test-e2e check db docker-dev docker-app docker-up docker-migrate docker-seed docker-down
help:
	@echo 'Targets: install app-key migrate migrate-status migrate-rollback migrate-fresh migration seed prune admin dev api web web-build build serve serve-test test test-e2e check db docker-dev docker-app docker-up docker-migrate docker-seed docker-down'
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
# make app-key: writes a new APP_KEY into .env if it has none (never replaces one:
# that locks out every account with two-factor authentication). Run before the first make dev.
app-key:
	cd apps/api && go run ./cmd/admin app-key generate -env ../../.env
# make admin cmd=grant|revoke|reset-mfa|unblock email=you@example.com [CONFIRM=1], make admin cmd=list|blocks,
# make admin cmd="route-key generate": the operator's commands (cmd/admin). Changes preview unless CONFIRM=1.
admin:
	@test -n "$(cmd)" || (cd apps/api && go run ./cmd/admin; exit 2)
	cd apps/api && go run ./cmd/admin $(cmd) $(if $(email),-email '$(email)') $(if $(CONFIRM),-confirm)
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
# Docker (compose.yaml): PostgreSQL 17 with pgvector, and the app built from the Dockerfile.
# The database and Mailpit (the local inbox, http://localhost:8025), for make dev and the tests
# on this machine. Also creates the Go tests' database (lewcontext_test, .env.testing) if it's
# missing; e2e runs create their own.
db:
	docker compose up -d --wait postgres mailpit
	@docker compose exec -T postgres psql -U lewcontext -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname = 'lewcontext_test'" | grep -q 1 || docker compose exec -T postgres createdb -U lewcontext lewcontext_test
# The whole app in Docker on :3000 in one command (stop make dev / make serve first): starts the
# database, builds the image, migrates and seeds, then starts the app once the schema is ready.
docker-dev: db
	docker compose build app
	docker compose run --rm app migrate
	docker compose run --rm app seed
	docker compose up -d --wait app
# After changing the code (UI or API): rebuild the image and restart only the app container.
# No migrate or seed, and the database keeps running. The web build is embedded in the Go
# binary, so a UI change still rebuilds the image; Docker's cache skips unchanged steps.
docker-app:
	docker compose up -d --build --no-deps --wait app
# Or step by step: start everything, then migrate and seed.
docker-up:
	docker compose up -d --build --wait
docker-migrate:
	docker compose run --rm app migrate
docker-seed:
	docker compose run --rm app seed
# Stops the containers; the database volume (and its data) is kept.
docker-down:
	docker compose down
# Tests run with .env.testing and nothing from .env (scripts/test-env.sh).
# Go (unit and PostgreSQL integration) and Vitest. Needs the database: make db.
test:
	./scripts/test-env.sh sh -c 'cd apps/api && go test -race ./... && cd ../web && npm test'
# The e2e server on E2E_PORT (3100) against a freshly rebuilt lewcontext_test_<port>.
# Playwright starts it; run it yourself only to check something by hand.
serve-test: build
	E2E_PORT='$(E2E_PORT)' ./scripts/test-env.sh ./scripts/serve-test.sh
# make test-e2e [E2E_PORT=3101|auto] [E2E_REUSE=1]: checks the database, reserves the port
# (another run's port stops it at once, suggesting a free one), then runs Playwright, which
# builds and starts make serve-test. E2E_REUSE=1 tests a server you already started instead.
test-e2e:
	E2E_PORT='$(E2E_PORT)' E2E_REUSE='$(E2E_REUSE)' ./scripts/test-env.sh ./scripts/e2e-preflight.sh sh -c 'cd apps/web && npm run test:e2e'
check:
	@test -z "$$(gofmt -l apps/api)" || (echo 'Run gofmt -w apps/api'; exit 1)
	cd apps/api && go vet ./...
	cd apps/web && npm run typecheck && npm run lint && npm run format:check
