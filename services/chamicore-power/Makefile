# Standard Makefile for chamicore-power

SERVICE       := power
SERVICE_FULL  := chamicore-power
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

LINT_VERSION  ?= v1.64.8
LINT          := go run github.com/golangci/golangci-lint/cmd/golangci-lint@$(LINT_VERSION)

DB_DSN        ?= postgres://chamicore:chamicore@localhost:5432/chamicore?sslmode=disable&search_path=power

.PHONY: all
all: lint test build

.PHONY: build
build:
	$(GOBUILD) -ldflags '$(LDFLAGS)' -o $(BINARY) ./cmd/chamicore-power

.PHONY: test
test:
	$(GOTEST) -race -count=1 ./...

.PHONY: test-cover
test-cover:
	$(GOTEST) -race -count=1 -coverprofile=coverage.out -covermode=atomic ./...
	$(GO) tool cover -html=coverage.out -o coverage.html

.PHONY: test-integration
test-integration:
	$(GOTEST) -race -count=1 -tags=integration ./...

.PHONY: lint
lint:
	$(LINT) run ./...

.PHONY: docker-build
docker-build:
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-t $(DOCKER_IMAGE):$(DOCKER_TAG) \
		-t $(DOCKER_IMAGE):latest \
		.

.PHONY: migrate-up
migrate-up:
	migrate -database "$(DB_DSN)" -path migrations/postgres up

.PHONY: migrate-down
migrate-down:
	migrate -database "$(DB_DSN)" -path migrations/postgres down 1

.PHONY: migrate-create
migrate-create:
	migrate create -ext sql -dir migrations/postgres -seq $(NAME)

.PHONY: clean
clean:
	rm -rf bin/ coverage.out coverage.html
