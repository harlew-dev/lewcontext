SHELL := /bin/bash
.DEFAULT_GOAL := help
# Read local development configuration without placing credentials in tracked files.
ifneq (,$(wildcard .env))
include .env
export
endif
.PHONY: help install migrate migrate-status migrate-rollback migration seed api web build test test-integration test-e2e check
help:
	@echo 'Targets: install migrate migrate-status migrate-rollback migration seed api web build test test-integration test-e2e check'
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
migration:
	@test -n "$(name)" || (echo 'Usage: make migration name=add_tasks'; exit 1)
	cd apps/api && go run ./cmd/migrate create $(name)
# make seed [only=demo|volume|users] [count=n] [projects=n] [email=address]
seed:
	cd apps/api && go run ./cmd/seed $(if $(only),-only '$(only)') $(if $(count),-count '$(count)') $(if $(projects),-projects '$(projects)') $(if $(email),-email '$(email)')
api:
	cd apps/api && go run ./cmd/server
web:
	cd apps/web && npm run dev
build:
	mkdir -p bin
	cd apps/api && go build -trimpath -o ../../bin/api ./cmd/server
	cd apps/web && npm run build
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
