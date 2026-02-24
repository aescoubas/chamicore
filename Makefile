# Chamicore - Top-level Makefile
# Cross-service task runner for the submodule-based monorepo.

SERVICES := chamicore-smd chamicore-bss chamicore-cloud-init chamicore-kea-sync chamicore-discovery chamicore-power chamicore-auth chamicore-ui chamicore-cli
SHARED   := chamicore-lib chamicore-deploy
DEPLOY   := chamicore-deploy
K6       := k6
K6_RUNNER ?= ./scripts/run-k6.sh
K6_DOCKER_IMAGE ?= grafana/k6:0.49.0
K6_PROMETHEUS_RW_SERVER_URL ?= http://127.0.0.1:9090/api/v1/write
LOAD_QUICK_VUS ?= 20
LOAD_QUICK_DURATION ?= 20s
LOAD_QUICK_BOOT_P99_TARGET_MS ?= 250
LOAD_QUICK_BOOT_ERROR_RATE_MAX ?= 0.01
LOAD_QUICK_CLOUDINIT_P99_TARGET_MS ?= 250
LOAD_QUICK_CLOUDINIT_ERROR_RATE_MAX ?= 0.01
LOAD_QUICK_INVENTORY_P99_TARGET_MS ?= 300
LOAD_QUICK_INVENTORY_ERROR_RATE_MAX ?= 0.01
COVER_MIN ?= 100.0
GOLANGCI_LINT_VERSION ?= v1.64.8
GOLANGCI_LINT_CMD ?= go run github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
QUALITY_THRESHOLDS_FILE ?= quality/thresholds.txt
QUALITY_THRESHOLDS_ABS := $(abspath $(QUALITY_THRESHOLDS_FILE))
QUALITY_MUTATION_SCORES ?= quality/mutation-scores.txt
QUALITY_REQUIRE_MUTATION ?= 0
QUALITY_REPORT_DIR ?= quality/reports

SERVICE_DIRS := $(addprefix services/,$(SERVICES))
SHARED_DIRS  := $(addprefix shared/,$(SHARED))
ALL_DIRS     := $(SERVICE_DIRS) $(SHARED_DIRS)

.PHONY: help init update build test test-shuffle test-cover test-integration test-system test-smoke test-load test-load-quick test-all lint quality-ratchet quality-coverage quality-mutation quality-gate quality-db release-gate docker-build compose-up compose-down compose-vm-up compose-vm-down clean

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
		if [ -f "$$dir/go.mod" ]; then \
			echo "==> Building $$dir"; \
			cd $$dir && go build ./... && cd - > /dev/null || exit 1; \
		fi; \
	done

## --- Test ---

test: ## Run tests for all services and shared libraries
	@for dir in $(ALL_DIRS); do \
		if [ -f "$$dir/go.mod" ]; then \
			echo "==> Testing $$dir"; \
			cd $$dir && go test -race -count=1 ./... && \
			cd - > /dev/null || exit 1; \
		fi; \
	done

test-shuffle: ## Run tests with package shuffle enabled
	@for dir in $(ALL_DIRS); do \
		if [ -f "$$dir/go.mod" ]; then \
			echo "==> Shuffled tests for $$dir"; \
			cd $$dir && go test -shuffle=on -count=1 ./... && \
			cd - > /dev/null || exit 1; \
		fi; \
	done

test-cover: ## Run unit tests with coverage report (100% enforced)
	@for dir in $(ALL_DIRS); do \
		if [ -f "$$dir/go.mod" ]; then \
			echo "==> Coverage for $$dir"; \
			cd $$dir && go test -coverprofile=coverage.out -race ./... && \
			go tool cover -func=coverage.out && \
			total=$$(go tool cover -func=coverage.out | awk '/^total:/ {gsub("%","",$$3); print $$3}') && \
			module_min="$(COVER_MIN)" && \
			if [ -f "$(QUALITY_THRESHOLDS_ABS)" ]; then \
				configured_min=$$(awk -v scope="$$dir" '$$1 == "coverage" && $$2 == scope {print $$3; exit}' "$(QUALITY_THRESHOLDS_ABS)"); \
				if [ -n "$$configured_min" ]; then module_min="$$configured_min"; fi; \
			fi && \
			awk "BEGIN {exit !($$total + 0 >= $$module_min + 0)}" || { \
				echo "Coverage threshold not met for $$dir: $$total% < $$module_min%"; \
				exit 1; \
			}; \
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
	K6_PROMETHEUS_RW_SERVER_URL=$(K6_PROMETHEUS_RW_SERVER_URL) K6_DOCKER_IMAGE=$(K6_DOCKER_IMAGE) $(K6_RUNNER) run --out experimental-prometheus-rw tests/load/boot_storm.js
	K6_PROMETHEUS_RW_SERVER_URL=$(K6_PROMETHEUS_RW_SERVER_URL) K6_DOCKER_IMAGE=$(K6_DOCKER_IMAGE) $(K6_RUNNER) run --out experimental-prometheus-rw tests/load/cloud_init_storm.js
	K6_PROMETHEUS_RW_SERVER_URL=$(K6_PROMETHEUS_RW_SERVER_URL) K6_DOCKER_IMAGE=$(K6_DOCKER_IMAGE) $(K6_RUNNER) run --out experimental-prometheus-rw tests/load/inventory_scale.js

test-load-quick: test-smoke ## Run abbreviated load test (default: 20 VUs, 20s)
	@echo "==> Running quick load suite ($(LOAD_QUICK_VUS) VUs, $(LOAD_QUICK_DURATION) per scenario)"
	K6_PROMETHEUS_RW_SERVER_URL=$(K6_PROMETHEUS_RW_SERVER_URL) K6_DOCKER_IMAGE=$(K6_DOCKER_IMAGE) $(K6_RUNNER) run --out experimental-prometheus-rw -e QUICK=true -e QUICK_VUS=$(LOAD_QUICK_VUS) -e QUICK_DURATION=$(LOAD_QUICK_DURATION) -e BOOT_P99_TARGET_MS=$(LOAD_QUICK_BOOT_P99_TARGET_MS) -e BOOT_ERROR_RATE_MAX=$(LOAD_QUICK_BOOT_ERROR_RATE_MAX) tests/load/boot_storm.js
	K6_PROMETHEUS_RW_SERVER_URL=$(K6_PROMETHEUS_RW_SERVER_URL) K6_DOCKER_IMAGE=$(K6_DOCKER_IMAGE) $(K6_RUNNER) run --out experimental-prometheus-rw -e QUICK=true -e QUICK_VUS=$(LOAD_QUICK_VUS) -e QUICK_DURATION=$(LOAD_QUICK_DURATION) -e CLOUDINIT_P99_TARGET_MS=$(LOAD_QUICK_CLOUDINIT_P99_TARGET_MS) -e CLOUDINIT_ERROR_RATE_MAX=$(LOAD_QUICK_CLOUDINIT_ERROR_RATE_MAX) tests/load/cloud_init_storm.js
	K6_PROMETHEUS_RW_SERVER_URL=$(K6_PROMETHEUS_RW_SERVER_URL) K6_DOCKER_IMAGE=$(K6_DOCKER_IMAGE) $(K6_RUNNER) run --out experimental-prometheus-rw -e QUICK=true -e QUICK_VUS=$(LOAD_QUICK_VUS) -e QUICK_DURATION=$(LOAD_QUICK_DURATION) -e INVENTORY_P99_TARGET_MS=$(LOAD_QUICK_INVENTORY_P99_TARGET_MS) -e INVENTORY_ERROR_RATE_MAX=$(LOAD_QUICK_INVENTORY_ERROR_RATE_MAX) tests/load/inventory_scale.js

test-all: test-cover test-integration test-system test-smoke ## Run all test levels (unit + integration + system + smoke)

## --- Lint ---

lint: ## Lint all services and shared libraries
	@for dir in $(ALL_DIRS); do \
		if [ -f "$$dir/go.mod" ]; then \
			echo "==> Linting $$dir"; \
			cd $$dir && $(GOLANGCI_LINT_CMD) run ./... && \
			cd - > /dev/null || exit 1; \
		fi; \
	done

quality-ratchet: ## Ensure quality thresholds only ratchet upward
	./scripts/quality/check-threshold-ratchet.sh $(QUALITY_THRESHOLDS_FILE)

quality-coverage: ## Enforce per-module coverage thresholds from quality config
	./scripts/quality/check-coverage-thresholds.sh $(QUALITY_THRESHOLDS_FILE)

quality-mutation: ## Enforce mutation thresholds from quality config
	./scripts/quality/check-mutation-thresholds.sh $(QUALITY_THRESHOLDS_FILE) $(QUALITY_MUTATION_SCORES) $(QUALITY_REQUIRE_MUTATION)

quality-gate: ## Run local quality gate (ratchet, lint, race, shuffle, coverage, integration)
	@$(MAKE) quality-ratchet
	@$(MAKE) lint
	@$(MAKE) test
	@$(MAKE) test-shuffle
	@$(MAKE) test-cover
	@$(MAKE) quality-coverage
	@$(MAKE) test-integration

quality-db: ## Validate migration up/down/up, schema expectations, and query plans
	./scripts/quality-db.sh

release-gate: ## Run release gate and emit signed report (set RELEASE_TAG=vX.Y.Z to tag)
	QUALITY_THRESHOLDS_FILE=$(QUALITY_THRESHOLDS_FILE) \
	QUALITY_MUTATION_SCORES=$(QUALITY_MUTATION_SCORES) \
	QUALITY_REQUIRE_MUTATION=$(QUALITY_REQUIRE_MUTATION) \
	QUALITY_REPORT_DIR=$(QUALITY_REPORT_DIR) \
	RELEASE_TAG=$(RELEASE_TAG) \
	./scripts/release-gate.sh

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

compose-vm-up: ## Start development environment and boot a libvirt VM
	$(MAKE) -C shared/$(DEPLOY) compose-libvirt-up

compose-vm-down: ## Stop libvirt VM and development environment
	$(MAKE) -C shared/$(DEPLOY) compose-libvirt-down

## --- Clean ---

clean: ## Clean build artifacts for all services
	@for dir in $(ALL_DIRS); do \
		if [ -f "$$dir/go.mod" ]; then \
			echo "==> Cleaning $$dir"; \
			cd $$dir && go clean ./... || true; \
		fi; \
	done
