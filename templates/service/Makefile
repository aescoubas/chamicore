# TEMPLATE: Standard Makefile for __SERVICE_FULL__
# Copy this file and replace all __PLACEHOLDER__ markers with your service values.

# ---------------------------------------------------------------------------
# Variables
# ---------------------------------------------------------------------------
SERVICE       := __SERVICE__
SERVICE_FULL  := __SERVICE_FULL__
MODULE        := git.cscs.ch/openchami/$(SERVICE_FULL)

VERSION       ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT        ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
BUILD_DATE    ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS       := -s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.buildDate=$(BUILD_DATE)

GO            := go
GOTEST        := $(GO) test
GOBUILD       := $(GO) build
BINARY        := bin/$(SERVICE)

DOCKER_IMAGE  := openchami/$(SERVICE_FULL)
DOCKER_TAG    ?= $(VERSION)

LINT          := golangci-lint
SWAGGER       := swag

DB_DSN        ?= postgres://chamicore:chamicore@localhost:5432/$(SERVICE)?sslmode=disable

# ---------------------------------------------------------------------------
# Default
# ---------------------------------------------------------------------------
.PHONY: all
all: lint test build

# ---------------------------------------------------------------------------
# Build
# ---------------------------------------------------------------------------
.PHONY: build
build: ## Build the service binary
	$(GOBUILD) -ldflags '$(LDFLAGS)' -o $(BINARY) ./cmd/service

# ---------------------------------------------------------------------------
# Test
# ---------------------------------------------------------------------------
.PHONY: test
test: ## Run unit tests
	$(GOTEST) -race -count=1 ./...

.PHONY: test-cover
test-cover: ## Run unit tests with coverage report
	$(GOTEST) -race -count=1 -coverprofile=coverage.out -covermode=atomic ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

.PHONY: test-integration
test-integration: ## Run integration tests (requires running database)
	$(GOTEST) -race -count=1 -tags=integration -run Integration ./... \
		-db-dsn="$(DB_DSN)"

# ---------------------------------------------------------------------------
# Lint
# ---------------------------------------------------------------------------
.PHONY: lint
lint: ## Run golangci-lint
	$(LINT) run ./...

# ---------------------------------------------------------------------------
# Docker
# ---------------------------------------------------------------------------
.PHONY: docker-build
docker-build: ## Build Docker image
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-t $(DOCKER_IMAGE):$(DOCKER_TAG) \
		-t $(DOCKER_IMAGE):latest \
		.

# ---------------------------------------------------------------------------
# OpenAPI / Swagger
# ---------------------------------------------------------------------------
.PHONY: swagger
swagger: ## Generate Swagger documentation from annotations
	$(SWAGGER) init -g cmd/service/main.go -o api --parseDependency

# ---------------------------------------------------------------------------
# Database migrations
# ---------------------------------------------------------------------------
.PHONY: migrate-up
migrate-up: ## Apply all pending migrations
	migrate -database "$(DB_DSN)" -path migrations/postgres up

.PHONY: migrate-down
migrate-down: ## Roll back the last migration
	migrate -database "$(DB_DSN)" -path migrations/postgres down 1

.PHONY: migrate-create
migrate-create: ## Create a new migration (usage: make migrate-create NAME=add_foo)
	migrate create -ext sql -dir migrations/postgres -seq $(NAME)

# ---------------------------------------------------------------------------
# Clean
# ---------------------------------------------------------------------------
.PHONY: clean
clean: ## Remove build artifacts
	rm -rf bin/ coverage.out coverage.html

# ---------------------------------------------------------------------------
# Help
# ---------------------------------------------------------------------------
.PHONY: help
help: ## Show this help message
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'
