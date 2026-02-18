# Chamicore Service Templates

This directory contains the golden reference templates for creating new Chamicore microservices. Every new service implementation should begin by copying these templates and performing the placeholder replacements described below.

## Quick Start

### 1. Copy the template

```bash
cp -r templates/service/ /path/to/chamicore-myservice/
```

### 2. Replace placeholders

Perform a global search-and-replace across all files for these placeholder tokens:

| Placeholder | Description | Example |
|---|---|---|
| `__SERVICE__` | Service name, lowercase | `smd` |
| `__SERVICE_UPPER__` | Service name, UPPERCASE | `SMD` |
| `__SERVICE_FULL__` | Full repository name | `chamicore-smd` |
| `__PORT__` | Default listen port | `27779` |
| `__API_PREFIX__` | API route prefix | `/hsm/v2` |
| `__API_VERSION__` | API version string | `hsm/v2` |
| `__SCHEMA__` | PostgreSQL schema name | `smd` |
| `__RESOURCE__` | Primary resource name (PascalCase) | `Component` |
| `__RESOURCE_LOWER__` | Primary resource name (lowercase) | `component` |
| `__RESOURCE_PLURAL__` | Primary resource name (plural, lowercase) | `components` |
| `__RESOURCE_TABLE__` | Database table name | `components` |

Using sed (run from the service root):

```bash
SERVICE="smd"
SERVICE_UPPER="SMD"
SERVICE_FULL="chamicore-smd"
PORT="27779"
API_PREFIX="/hsm/v2"
API_VERSION="hsm/v2"
SCHEMA="smd"
RESOURCE="Component"
RESOURCE_LOWER="component"
RESOURCE_PLURAL="components"
RESOURCE_TABLE="components"

find . -type f \( -name '*.go' -o -name '*.yaml' -o -name '*.yml' -o -name '*.sql' \
  -o -name 'Makefile' -o -name 'Dockerfile' -o -name '*.md' \) | while read f; do
  sed -i \
    -e "s|__SERVICE_UPPER__|${SERVICE_UPPER}|g" \
    -e "s|__SERVICE_FULL__|${SERVICE_FULL}|g" \
    -e "s|__SERVICE__|${SERVICE}|g" \
    -e "s|__PORT__|${PORT}|g" \
    -e "s|__API_PREFIX__|${API_PREFIX}|g" \
    -e "s|__API_VERSION__|${API_VERSION}|g" \
    -e "s|__SCHEMA__|${SCHEMA}|g" \
    -e "s|__RESOURCE_LOWER__|${RESOURCE_LOWER}|g" \
    -e "s|__RESOURCE_PLURAL__|${RESOURCE_PLURAL}|g" \
    -e "s|__RESOURCE_TABLE__|${RESOURCE_TABLE}|g" \
    -e "s|__RESOURCE__|${RESOURCE}|g" \
    "$f"
done
```

Note: The order of replacements matters -- `__SERVICE_UPPER__` and `__SERVICE_FULL__` must be replaced before `__SERVICE__` to avoid partial matches. Similarly, `__RESOURCE_LOWER__`, `__RESOURCE_PLURAL__`, and `__RESOURCE_TABLE__` must be replaced before `__RESOURCE__`.

### 3. Initialize the Go module

```bash
cd /path/to/chamicore-myservice/
go mod init git.cscs.ch/openchami/chamicore-myservice
go mod tidy
```

### 4. Customize

After placeholder replacement, search for `// TEMPLATE:` comments throughout the codebase. These mark locations where service-specific customization is expected:

```bash
grep -rn "// TEMPLATE:" .
```

Each comment explains what to add, modify, or remove for your specific service.

## Template File Index

| File | Purpose |
|---|---|
| `cmd/service/main.go` | Entry point: config, logging, OTel, DB, migrations, server, graceful shutdown |
| `internal/config/config.go` | Configuration loaded from environment variables with defaults |
| `internal/server/server.go` | Chi router with the standard 12-layer middleware stack |
| `internal/server/handlers.go` | Complete CRUD handler set with validation, ETags, RFC 9457 errors |
| `internal/store/store.go` | Store interface with CRUD methods and sentinel errors |
| `internal/store/postgres.go` | PostgreSQL implementation using squirrel query builder |
| `internal/model/model.go` | Internal domain model types (not exposed to consumers) |
| `pkg/client/client.go` | Typed HTTP client SDK for service-to-service and CLI use |
| `pkg/types/types.go` | Public API types (resource envelope, request/response types) |
| `Makefile` | Build, test, lint, docker, swagger, migration targets |
| `Dockerfile` | Multi-stage build: Go builder into distroless runtime |
| `.gitlab-ci.yml` | GitLab CI pipeline: lint, test, build, docker, release |
| `.golangci.yml` | golangci-lint configuration |
| `.goreleaser.yml` | GoReleaser configuration for binary releases |
| `api/openapi.yaml` | OpenAPI 3.0 spec with envelopes, pagination, ETags, RFC 9457 |
| `migrations/postgres/000001_init.up.sql` | Initial schema creation migration |
| `migrations/postgres/000001_init.down.sql` | Rollback for initial migration |

## Architecture Decisions

These templates encode the following architectural choices:

- **Router**: go-chi/chi/v5 with a strict 12-layer middleware stack.
- **Logging**: rs/zerolog with structured fields and console output in dev mode.
- **Database**: PostgreSQL via lib/pq with Masterminds/squirrel for query building.
- **Migrations**: golang-migrate/migrate/v4 with file-based SQL migrations.
- **Auth**: JWT validation via lestrrat-go/jwx/v2 using a JWKS URL.
- **Observability**: OpenTelemetry for traces and metrics, Prometheus for scraping.
- **Error format**: RFC 9457 Problem Details (application/problem+json).
- **API envelope**: Kubernetes-inspired resource envelope (kind, apiVersion, metadata, spec).
- **Concurrency control**: ETag-based with If-Match/If-None-Match headers.
- **Shared library**: `git.cscs.ch/openchami/chamicore-lib` provides reusable middleware, HTTP helpers, and the base client.
