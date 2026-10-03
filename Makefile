SHELL := /bin/bash
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
TEST_DB_CONTAINER := go-event-api-test-db
TEST_DB_PORT ?= 5441
TEST_DATABASE_URL ?= postgres://app:app@localhost:$(TEST_DB_PORT)/go_event_api_test?sslmode=disable

.PHONY: help run dev build test test-integration test-db-up test-db-down lint fmt swag migrate-up migrate-down migrate-status docker-build docker-up docker-down clean

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

run: ## Run the API with go run (reads .env)
	go run ./cmd/api

dev: ## Run with live reload (requires air)
	air

build: ## Build both binaries into bin/
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/api ./cmd/api
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/migrate ./cmd/migrate

test: ## Run unit tests with the race detector
	go test -race -count=1 ./...

test-db-up: ## Start a throwaway Postgres for integration tests
	@docker rm -f $(TEST_DB_CONTAINER) >/dev/null 2>&1 || true
	docker run -d --name $(TEST_DB_CONTAINER) -e POSTGRES_USER=app -e POSTGRES_PASSWORD=app -e POSTGRES_DB=go_event_api_test -p $(TEST_DB_PORT):5432 postgres:16-alpine >/dev/null
	@until docker exec $(TEST_DB_CONTAINER) pg_isready -U app -d go_event_api_test >/dev/null 2>&1; do sleep 1; done

test-db-down: ## Stop the throwaway Postgres
	@docker rm -f $(TEST_DB_CONTAINER) >/dev/null 2>&1 || true

test-integration: ## Run store tests against a real Postgres (uses docker)
	@$(MAKE) --no-print-directory test-db-up
	TEST_DATABASE_URL=$(TEST_DATABASE_URL) go test -race -count=1 -tags integration ./internal/database/... ; status=$$?; $(MAKE) --no-print-directory test-db-down; exit $$status

lint: ## gofmt check, go vet and staticcheck
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed on:"; gofmt -l .; exit 1)
	go vet ./...
	staticcheck ./...

fmt: ## Format all Go files
	gofmt -w .

swag: ## Regenerate the OpenAPI spec into docs/
	swag init -g cmd/api/main.go --parseDependency --parseInternal -o docs -q

migrate-up: ## Apply migrations (DATABASE_URL from .env)
	go run ./cmd/migrate up

migrate-down: ## Roll back all migrations
	go run ./cmd/migrate down

migrate-status: ## Print the schema version
	go run ./cmd/migrate status

docker-build: ## Build the production image
	docker build --build-arg VERSION=$(VERSION) -t go-event-api:$(VERSION) .

docker-up: ## Run API + Postgres with docker compose
	docker compose up --build

docker-down: ## Stop docker compose and keep the data volume
	docker compose down

clean: ## Remove build output
	rm -rf bin tmp coverage.out
