# Chamicore - Top-level Makefile
# Cross-service task runner for the submodule-based monorepo.

SERVICES := chamicore-smd chamicore-bss chamicore-cloud-init chamicore-kea-sync chamicore-discovery chamicore-auth chamicore-ui chamicore-cli
SHARED   := chamicore-lib chamicore-deploy
DEPLOY   := chamicore-deploy
K6       := k6
K6_PROMETHEUS_RW_SERVER_URL ?= http://127.0.0.1:9090/api/v1/write

SERVICE_DIRS := $(addprefix services/,$(SERVICES))
SHARED_DIRS  := $(addprefix shared/,$(SHARED))
ALL_DIRS     := $(SERVICE_DIRS) $(SHARED_DIRS)

.PHONY: help init update build test test-cover test-integration test-system test-smoke test-load test-load-quick test-all lint docker-build compose-up compose-down clean

help: ## Show this help message
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

## --- Submodule Management ---

init: ## Initialize and clone all submodules
	git submodule update --init --recursive

update: ## Update all submodules to latest remote commits
	git submodule update --remote --merge

## --- Build ---

build: ## Build all services
	@for dir in $(SERVICE_DIRS); do \
		if [ -f "$$dir/Makefile" ]; then \
			echo "==> Building $$dir"; \
			$(MAKE) -C $$dir build || exit 1; \
		fi; \
	done

## --- Test ---

test: ## Run tests for all services and shared libraries
	@for dir in $(ALL_DIRS); do \
		if [ -f "$$dir/Makefile" ]; then \
			echo "==> Testing $$dir"; \
			$(MAKE) -C $$dir test || exit 1; \
		fi; \
	done

test-cover: ## Run unit tests with coverage report (100% enforced)
	@for dir in $(ALL_DIRS); do \
		if [ -f "$$dir/go.mod" ]; then \
			echo "==> Coverage for $$dir"; \
			cd $$dir && go test -coverprofile=coverage.out -race ./... && \
			go tool cover -func=coverage.out && \
			cd - > /dev/null || exit 1; \
		fi; \
	done

test-integration: ## Run per-service integration tests (requires Docker for testcontainers)
	@for dir in $(ALL_DIRS); do \
		if [ -f "$$dir/go.mod" ]; then \
			echo "==> Integration tests for $$dir"; \
			cd $$dir && go test -tags integration -race -count=1 ./... && \
			cd - > /dev/null || exit 1; \
		fi; \
	done

test-system: compose-up ## Run cross-service system integration tests (starts full stack)
	@echo "==> Running system integration tests"
	cd tests && go test -tags system -race -count=1 -timeout 5m ./...

test-smoke: compose-up ## Run smoke tests against live stack (quick health check)
	@echo "==> Running smoke tests"
	cd tests && go test -tags smoke -race -count=1 -timeout 30s ./smoke/...

test-load: test-smoke ## Run full load/performance tests (requires k6)
	@echo "==> Running full load suite (boot, cloud-init, inventory)"
	@command -v $(K6) >/dev/null 2>&1 || { echo "k6 not found in PATH"; exit 1; }
	K6_PROMETHEUS_RW_SERVER_URL=$(K6_PROMETHEUS_RW_SERVER_URL) $(K6) run --out experimental-prometheus-rw tests/load/boot_storm.js
	K6_PROMETHEUS_RW_SERVER_URL=$(K6_PROMETHEUS_RW_SERVER_URL) $(K6) run --out experimental-prometheus-rw tests/load/cloud_init_storm.js
	K6_PROMETHEUS_RW_SERVER_URL=$(K6_PROMETHEUS_RW_SERVER_URL) $(K6) run --out experimental-prometheus-rw tests/load/inventory_scale.js

test-load-quick: test-smoke ## Run abbreviated load test (1,000 VUs, 2 min)
	@echo "==> Running quick load suite (1,000 VUs, 2 min per scenario)"
	@command -v $(K6) >/dev/null 2>&1 || { echo "k6 not found in PATH"; exit 1; }
	K6_PROMETHEUS_RW_SERVER_URL=$(K6_PROMETHEUS_RW_SERVER_URL) $(K6) run --out experimental-prometheus-rw -e QUICK=true tests/load/boot_storm.js
	K6_PROMETHEUS_RW_SERVER_URL=$(K6_PROMETHEUS_RW_SERVER_URL) $(K6) run --out experimental-prometheus-rw -e QUICK=true tests/load/cloud_init_storm.js
	K6_PROMETHEUS_RW_SERVER_URL=$(K6_PROMETHEUS_RW_SERVER_URL) $(K6) run --out experimental-prometheus-rw -e QUICK=true tests/load/inventory_scale.js

test-all: test-cover test-integration test-system test-smoke ## Run all test levels (unit + integration + system + smoke)

## --- Lint ---

lint: ## Lint all services and shared libraries
	@for dir in $(ALL_DIRS); do \
		if [ -f "$$dir/Makefile" ]; then \
			echo "==> Linting $$dir"; \
			$(MAKE) -C $$dir lint || exit 1; \
		fi; \
	done

## --- Docker ---

docker-build: ## Build Docker images for all services
	@for dir in $(SERVICE_DIRS); do \
		if [ -f "$$dir/Dockerfile" ]; then \
			echo "==> Building Docker image for $$dir"; \
			$(MAKE) -C $$dir docker-build || exit 1; \
		fi; \
	done

## --- Docker Compose (Development) ---

compose-up: ## Start development environment with Docker Compose
	$(MAKE) -C shared/$(DEPLOY) compose-up

compose-down: ## Stop development environment
	$(MAKE) -C shared/$(DEPLOY) compose-down

## --- Clean ---

clean: ## Clean build artifacts for all services
	@for dir in $(ALL_DIRS); do \
		if [ -f "$$dir/Makefile" ]; then \
			echo "==> Cleaning $$dir"; \
			$(MAKE) -C $$dir clean || true; \
		fi; \
	done
