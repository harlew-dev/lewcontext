SHELL := /bin/bash
.DEFAULT_GOAL := help
# Read local development configuration without placing credentials in tracked files.
ifneq (,$(wildcard .env))
include .env
export
endif
.PHONY: help install migrate seed api web build test test-integration test-e2e check
help:
	@echo 'Targets: install migrate seed api web build test test-integration test-e2e check'
install:
	cd apps/api && go mod download
	cd apps/web && npm ci
migrate:
	cd apps/api && go run ./cmd/migrate
seed:
	cd apps/api && go run ./cmd/seed
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
