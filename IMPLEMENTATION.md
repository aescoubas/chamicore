# IMPLEMENTATION.md - Phased Implementation Plan

This document is the **source of truth** for what to build, in what order, and what
"done" means for each task. AI agents read this alongside `AGENTS.md` (conventions)
and `templates/service/` (code patterns) before implementing any task.

---

## How to Use This Document

1. **Find the next uncompleted task** — tasks are listed in dependency order within
   each phase. A task is either `[ ]` (pending), `[~]` (in progress), or `[x]` (complete).
2. **Check prerequisites** — each task lists which tasks it depends on. Do not start
   a task until all prerequisites are marked `[x]`.
3. **Read the acceptance criteria** — each task has a "Done when" section that defines
   exactly what "complete" means. Every criterion must be satisfied.
4. **Follow the conventions** — see `AGENTS.md` for patterns, `templates/service/` for
   reference code, and the relevant ADR(s) for design rationale.
5. **Mark progress** — when you start a task, change `[ ]` to `[~]`. When all acceptance
   criteria are met, change `[~]` to `[x]` and commit.

### Task ID Format

`P<phase>.<task>` — e.g., `P0.1` is Phase 0, Task 1.

### Task Structure

```
### P<N>.<M>: <Title> [status]

**Depends on:** P<N>.<M>, ...
**Repo:** chamicore-<name>
**Files:**
- path/to/file.go (description)

**Description:**
What to build and why.

**Done when:**
- [ ] Criterion 1
- [ ] Criterion 2
```

---

## Errata and Supplementary Conventions

These supplement `AGENTS.md` and resolve ambiguities discovered during planning.

### Canonical chamicore-lib Import Paths

The shared library uses **top-level packages** (no `pkg/` prefix). This is the
canonical layout that all services must import:

```go
import (
    "git.cscs.ch/openchami/chamicore-lib/auth"            // JWT middleware, JWKS, dev mode, scopes
    "git.cscs.ch/openchami/chamicore-lib/httputil"         // Envelope types, RFC 9457 helpers, JSON response, validation
    "git.cscs.ch/openchami/chamicore-lib/httputil/client"  // Base HTTP client with retries, error parsing
    "git.cscs.ch/openchami/chamicore-lib/dbutil"           // PostgreSQL pool setup, migration runner
    "git.cscs.ch/openchami/chamicore-lib/identity"         // Component ID validation, type/state/role enums
    "git.cscs.ch/openchami/chamicore-lib/otel"             // OTel SDK init, HTTP metrics/tracing middleware
    "git.cscs.ch/openchami/chamicore-lib/testutil"         // Testcontainers helpers, HTTP test utilities
)
```

The templates in `templates/service/` currently use `chamicore-lib/pkg/http` and
`chamicore-lib/pkg/middleware`. **Task P0.9 updates the templates** to use the
canonical paths listed above. All service implementations must use the canonical
paths, not the legacy template paths.

### Mock Strategy

Use **hand-written struct mocks** with function fields. No code generation tools
(`mockgen`, `mockery`). This keeps tests readable and avoids tool dependencies.

```go
// internal/store/mock_test.go
type mockStore struct {
    PingFn            func(ctx context.Context) error
    ListComponentsFn  func(ctx context.Context, opts ListOptions) ([]model.Component, int, error)
    GetComponentFn    func(ctx context.Context, id string) (model.Component, error)
    CreateComponentFn func(ctx context.Context, m model.Component) (model.Component, error)
    UpdateComponentFn func(ctx context.Context, m model.Component) (model.Component, error)
    DeleteComponentFn func(ctx context.Context, id string) error
}

func (m *mockStore) Ping(ctx context.Context) error { return m.PingFn(ctx) }
// ... one-liner delegation for each method
```

For handler tests, construct a `mockStore` with only the functions needed for that
test case. Unused function fields left nil will panic, which is the desired behavior
(it means the test hit an unexpected code path).

### Request Body Limits

All handlers must wrap `r.Body` with `http.MaxBytesReader` before decoding:

```go
r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB default
```

Bulk import endpoints may use a larger limit (e.g., 10 MB) documented per-endpoint.
The `httputil` package provides a middleware for this.

### Transaction Pattern

Use `sql.Tx` for any store method that writes to multiple tables in one operation.
Begin the transaction in the store method, not the handler:

```go
func (s *PostgresStore) CreateComponentWithInterfaces(ctx context.Context, ...) error {
    tx, err := s.db.BeginTx(ctx, nil)
    if err != nil { return fmt.Errorf("begin tx: %w", err) }
    defer tx.Rollback()
    // ... multiple inserts ...
    return tx.Commit()
}
```

### Filtering Convention

Query parameters use field names directly: `?type=Node&state=Ready`.
Comma-separated values for OR within a field: `?type=Node,Switch`.
Sorting: `?sort=created_at` (ascending) or `?sort=-created_at` (descending).
Phase 1 services implement equality filters only; range filters can be added later.

### ID Generation

| Resource | ID Strategy |
|----------|------------|
| Components (SMD) | Client-provided (validated against regex). If omitted, server generates `<type>-<8 hex chars>`. |
| Boot params (BSS) | Server-generated UUID v4. |
| Cloud-Init payloads | Server-generated UUID v4. |
| Scan jobs (Discovery) | Server-generated UUID v4. |
| Discovery targets | Server-generated UUID v4. |
| Service accounts (Auth) | Server-generated UUID v4. |
| Auth policies | Casbin-managed (composite key). |
| Device credentials | Server-generated UUID v4. |

Use `crypto/rand`-based UUID generation, not `math/rand`.

### Git Workflow

- Branch naming: `<type>/<short-description>` (e.g., `feat/smd-component-crud`, `fix/bss-sync-etag`)
- Commit messages: imperative mood, max 72 chars first line. Reference task ID.
  Example: `P2.1: implement component CRUD handlers and store`
- One commit per completed task (squash if needed before merge).
- MR into `main` for each completed task or logical group of tasks.

### PATCH Semantics

Chamicore uses **JSON Merge Patch** semantics (RFC 7396): pointer fields in the
request struct, `nil` means "don't change", non-nil means "set to this value"
(including zero values). This is already the pattern in the templates.

---

## Service Placeholder Values

When instantiating `templates/service/` for a new service, use these values in the
`sed` replacement script:

### chamicore-smd

| Placeholder | Value |
|---|---|
| `__SERVICE__` | `smd` |
| `__SERVICE_UPPER__` | `SMD` |
| `__SERVICE_FULL__` | `chamicore-smd` |
| `__PORT__` | `27779` |
| `__API_PREFIX__` | `/hsm/v2` |
| `__API_VERSION__` | `hsm/v2` |
| `__SCHEMA__` | `smd` |
| `__RESOURCE__` | `Component` |
| `__RESOURCE_LOWER__` | `component` |
| `__RESOURCE_PLURAL__` | `components` |
| `__RESOURCE_TABLE__` | `components` |

### chamicore-bss

| Placeholder | Value |
|---|---|
| `__SERVICE__` | `bss` |
| `__SERVICE_UPPER__` | `BSS` |
| `__SERVICE_FULL__` | `chamicore-bss` |
| `__PORT__` | `27778` |
| `__API_PREFIX__` | `/boot/v1` |
| `__API_VERSION__` | `boot/v1` |
| `__SCHEMA__` | `bss` |
| `__RESOURCE__` | `BootParam` |
| `__RESOURCE_LOWER__` | `bootparam` |
| `__RESOURCE_PLURAL__` | `bootparams` |
| `__RESOURCE_TABLE__` | `boot_params` |

### chamicore-cloud-init

| Placeholder | Value |
|---|---|
| `__SERVICE__` | `cloudinit` |
| `__SERVICE_UPPER__` | `CLOUDINIT` |
| `__SERVICE_FULL__` | `chamicore-cloud-init` |
| `__PORT__` | `27777` |
| `__API_PREFIX__` | `/cloud-init` |
| `__API_VERSION__` | `cloud-init/v1` |
| `__SCHEMA__` | `cloudinit` |
| `__RESOURCE__` | `Payload` |
| `__RESOURCE_LOWER__` | `payload` |
| `__RESOURCE_PLURAL__` | `payloads` |
| `__RESOURCE_TABLE__` | `payloads` |

### chamicore-auth

| Placeholder | Value |
|---|---|
| `__SERVICE__` | `auth` |
| `__SERVICE_UPPER__` | `AUTH` |
| `__SERVICE_FULL__` | `chamicore-auth` |
| `__PORT__` | `3333` |
| `__API_PREFIX__` | `/auth/v1` |
| `__API_VERSION__` | `auth/v1` |
| `__SCHEMA__` | `auth` |
| `__RESOURCE__` | `ServiceAccount` |
| `__RESOURCE_LOWER__` | `serviceaccount` |
| `__RESOURCE_PLURAL__` | `service-accounts` |
| `__RESOURCE_TABLE__` | `service_accounts` |

Note: chamicore-auth has multiple resource types (service accounts, policies, roles,
credentials, tokens). The template provides the starting point for the primary resource;
additional resource handlers are added manually following the same patterns.

### chamicore-discovery

| Placeholder | Value |
|---|---|
| `__SERVICE__` | `discovery` |
| `__SERVICE_UPPER__` | `DISCOVERY` |
| `__SERVICE_FULL__` | `chamicore-discovery` |
| `__PORT__` | `27776` |
| `__API_PREFIX__` | `/discovery/v1` |
| `__API_VERSION__` | `discovery/v1` |
| `__SCHEMA__` | `discovery` |
| `__RESOURCE__` | `Target` |
| `__RESOURCE_LOWER__` | `target` |
| `__RESOURCE_PLURAL__` | `targets` |
| `__RESOURCE_TABLE__` | `targets` |

---

## Phase 0: Foundation (chamicore-lib)

Everything depends on the shared library. Build it first with comprehensive tests.
chamicore-lib is a pure Go library with no `main` package — it produces no binary.

### P0.1: httputil — envelope types and response helpers [x]

**Depends on:** none
**Repo:** chamicore-lib

**Files:**
- `httputil/envelope.go` — `Resource[T]`, `ResourceList[T]`, `ResourceMetadata`, `ListMetadata`
- `httputil/problem.go` — `ProblemDetail`, `ValidationProblem`, `RespondProblem()`, `RespondValidationProblem()`
- `httputil/response.go` — `RespondJSON()`, `RespondNoContent()`
- `httputil/envelope_test.go` — envelope serialization/deserialization tests
- `httputil/problem_test.go` — problem detail tests
- `httputil/response_test.go` — response helper tests
- `go.mod` — `module git.cscs.ch/openchami/chamicore-lib`

**Description:**
Implement the resource envelope types and HTTP response helpers used by every service.
The envelope pattern is defined in AGENTS.md (Resource Envelope Pattern section) and
ADR-007. Response helpers write JSON with correct Content-Type headers. Problem detail
helpers produce RFC 9457-compliant error responses.

`RespondJSON(w, status, v)` marshals `v` to JSON and writes it with the given status.
`RespondProblem(w, status, title, detail, instance)` writes a ProblemDetail.
`RespondValidationProblem(w, status, title, detail, instance, problems)` writes a
ProblemDetail with field-level validation errors.

**Done when:**
- [ ] `go.mod` exists with module path `git.cscs.ch/openchami/chamicore-lib`
- [ ] `Resource[T]` and `ResourceList[T]` generic types serialize correctly to JSON
- [ ] `RespondJSON` writes `Content-Type: application/json` and correct status code
- [ ] `RespondProblem` produces RFC 9457-compliant JSON including `type`, `title`, `status`, `detail`, `instance`
- [ ] `RespondValidationProblem` includes `errors` array with `field` and `message`
- [ ] All exported functions have doc comments
- [ ] 100% test coverage
- [ ] `golangci-lint run` passes

### P0.2: httputil — middleware stack [x]

**Depends on:** P0.1
**Repo:** chamicore-lib

**Files:**
- `httputil/middleware.go` — `RequestID`, `RequestLogger`, `SecureHeaders`, `ContentType`, `APIVersion`, `CacheControl`, `ETag`, `Recoverer`, `BodyLimit`
- `httputil/middleware_test.go` — tests for each middleware
- `httputil/swagger.go` — `SwaggerHandler()` serving embedded OpenAPI spec + Swagger UI
- `httputil/swagger_test.go`

**Description:**
Implement the 12-layer middleware stack defined in AGENTS.md. Each middleware is a
`func(http.Handler) http.Handler` compatible with go-chi's `r.Use()`.

| Middleware | Behavior |
|-----------|----------|
| `RequestID` | Generate or propagate `X-Request-ID` header |
| `RequestLogger` | Log request method, path, status, duration via zerolog |
| `Recoverer` | Catch panics, log stack trace, return 500 ProblemDetail |
| `SecureHeaders` | Set `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY` |
| `ContentType` | Require `Content-Type: application/json` on POST/PUT/PATCH; set on responses |
| `APIVersion` | Add `API-Version` response header (value passed as parameter) |
| `CacheControl` | GET: `Cache-Control: no-cache` (or configurable TTL); mutations: `no-store` |
| `ETag` | Compute weak ETag from response body hash; process `If-None-Match` (304) |
| `BodyLimit` | Wrap `r.Body` with `http.MaxBytesReader`; configurable max size |
| `SwaggerHandler` | Serve embedded Swagger UI and raw OpenAPI YAML |

Note: `OTelMetrics` and `OTelTracing` middleware are in the `otel` package (P0.7).
`JWTMiddleware` and `RequireScope` are in the `auth` package (P0.4).

**Done when:**
- [ ] Each middleware is a standalone function testable in isolation
- [ ] `RequestID` generates UUID if no `X-Request-ID` header present; propagates if present
- [ ] `Recoverer` returns RFC 9457 ProblemDetail on panic, not a bare 500
- [ ] `ContentType` returns `415 Unsupported Media Type` for wrong Content-Type on mutations
- [ ] `ETag` returns `304 Not Modified` when `If-None-Match` matches response ETag
- [ ] `BodyLimit` returns `413 Request Entity Too Large` when body exceeds limit
- [ ] `SwaggerHandler` serves HTML at `/api/docs` and YAML at `/api/openapi.yaml`
- [ ] 100% test coverage
- [ ] `golangci-lint run` passes

### P0.3: httputil/client — base HTTP client [x]

**Depends on:** P0.1
**Repo:** chamicore-lib

**Files:**
- `httputil/client/client.go` — `Client`, `ClientConfig`, `Get`, `Post`, `Put`, `Patch`, `Delete`, `PutWithHeaders`
- `httputil/client/client_test.go`

**Description:**
Base HTTP client used by all service `pkg/client/` SDKs. Features:
- Bearer token injection (static or via refresh callback)
- `X-Request-ID` propagation from context
- Automatic retries on 429, 502, 503, 504 with exponential backoff + jitter
- Non-2xx responses parsed into `*APIError` (wrapping `ProblemDetail`)
- Configurable timeout, max retries, retry wait bounds
- All methods accept `context.Context`

**Done when:**
- [ ] `Client.Get(ctx, path, &result)` unmarshals JSON response into result
- [ ] `Client.Post(ctx, path, body, &result)` sends JSON body, unmarshals response
- [ ] `PutWithHeaders` sends additional headers (used for `If-Match` on PUT)
- [ ] Bearer token added to every request when configured
- [ ] Retries on 429/502/503/504 with exponential backoff (verified via httptest)
- [ ] Non-2xx responses return `*APIError` with parsed ProblemDetail fields
- [ ] `X-Request-ID` propagated from context to outgoing request headers
- [ ] Context cancellation stops retries
- [ ] 100% test coverage

### P0.4: auth — JWT middleware and JWKS [x]

**Depends on:** P0.1
**Repo:** chamicore-lib

**Files:**
- `auth/middleware.go` — `JWTMiddleware(jwksURL)`, `RequireScope(scope)`
- `auth/claims.go` — `Claims` struct, `ClaimsFromContext(ctx)`
- `auth/jwks.go` — JWKS fetcher with auto-refresh
- `auth/devmode.go` — dev mode bypass with synthetic admin claims
- `auth/middleware_test.go`
- `auth/claims_test.go`
- `auth/jwks_test.go`
- `auth/devmode_test.go`

**Description:**
JWT validation middleware for go-chi. Uses `lestrrat-go/jwx/v2` for token parsing
and JWKS fetching. Extracts claims into context for downstream handlers.

`JWTMiddleware(jwksURL)` validates the `Authorization: Bearer <token>` header,
verifies the signature against the JWKS, checks expiration, and injects `Claims`
into context.

`RequireScope(scope)` checks that the claims in context contain the required scope.
Returns `403 Forbidden` (ProblemDetail) if the scope is missing.

Dev mode: when `devMode=true` is passed to `JWTMiddleware`, it skips validation and
injects synthetic admin claims with all scopes. Logs a prominent warning.

**Done when:**
- [ ] `JWTMiddleware` returns `401 Unauthorized` for missing/malformed/expired tokens
- [ ] `JWTMiddleware` validates signature against JWKS endpoint
- [ ] JWKS auto-refreshes on a configurable interval (default: 5 minutes)
- [ ] `ClaimsFromContext(ctx)` returns extracted claims (sub, scope, roles, sa, etc.)
- [ ] `RequireScope` returns `403 Forbidden` ProblemDetail when scope is missing
- [ ] Dev mode injects synthetic admin claims and logs warning
- [ ] All error responses are RFC 9457 ProblemDetail format
- [ ] 100% test coverage (use httptest with pre-generated test JWTs)

### P0.5: dbutil — PostgreSQL connection and migrations [x]

**Depends on:** none
**Repo:** chamicore-lib

**Files:**
- `dbutil/postgres.go` — `Connect(cfg)`, `PoolConfig` struct
- `dbutil/migrate.go` — `RunMigrations(db, migrationsPath)`
- `dbutil/postgres_test.go`
- `dbutil/migrate_test.go`

**Description:**
Database connection helpers. `Connect(cfg)` opens a `*sql.DB` with configurable pool
settings (max open, max idle, lifetime, idle time) from a `PoolConfig`. Sets
`search_path` via the DSN or an initial `SET search_path TO <schema>` statement.

`RunMigrations(db, path)` wraps `golang-migrate` to apply pending migrations and log
the result. Returns the current version and whether the schema is dirty.

**Done when:**
- [ ] `Connect` returns a configured `*sql.DB` with pool settings from `PoolConfig`
- [ ] Connection pool parameters match ADR-014 budgets (configurable per-service)
- [ ] `RunMigrations` applies pending `.up.sql` files and reports version
- [ ] `RunMigrations` returns a clear error if migrations are dirty
- [ ] Integration test (build tag `integration`) uses testcontainers to verify against real PostgreSQL
- [ ] 100% test coverage

### P0.6: identity — component ID validation and enums [x]

**Depends on:** none
**Repo:** chamicore-lib

**Files:**
- `identity/id.go` — `ValidateID(id)`, `GenerateID(componentType)`, ID regex
- `identity/types.go` — component type, state, role, net-type enums
- `identity/id_test.go`
- `identity/types_test.go`

**Description:**
Component identity utilities defined in ADR-010.

ID regex: `^[a-zA-Z0-9][a-zA-Z0-9._-]{0,253}[a-zA-Z0-9]$`
Minimum length: 2. Maximum length: 255.

`GenerateID(componentType)` produces `<type-lower>-<8 hex chars>` using `crypto/rand`.

Enum types use `string` underlying type with exported constants:
- ComponentType: `Cabinet`, `BMC`, `Node`, `Processor`, `Memory`, `Accelerator`, `NIC`, `Drive`, `PDU`, `Switch`
- ComponentState: `Empty`, `Populated`, `Off`, `On`, `Standby`, `Halt`, `Ready`
- ComponentRole: `Compute`, `Service`, `Management`, `Application`, `Storage`, `System`

Each enum type has a `Valid() bool` method and a `ParseX(s string) (X, error)` function.

**Done when:**
- [ ] `ValidateID` accepts valid IDs including xname strings
- [ ] `ValidateID` rejects empty, too-long, whitespace, and special-char IDs
- [ ] `GenerateID("Node")` produces `node-<8hex>` format
- [ ] All enum types have `Valid()` method and `Parse` function
- [ ] Unknown enum values are rejected by `Parse`
- [ ] Enum string representations match the ADR-010 values exactly
- [ ] 100% test coverage

### P0.7: otel — OpenTelemetry instrumentation [x]

**Depends on:** P0.1
**Repo:** chamicore-lib

**Files:**
- `otel/init.go` — `Init(ctx, cfg)` returns shutdown function
- `otel/config.go` — `Config` struct
- `otel/http.go` — `HTTPTracing()`, `HTTPMetrics(serviceName)` middleware
- `otel/db.go` — `InstrumentDB(db, schemaName)` wraps `*sql.DB` with span creation
- `otel/prometheus.go` — `PrometheusHandler()` returns `promhttp.Handler`
- `otel/init_test.go`
- `otel/http_test.go`

**Description:**
OTel SDK bootstrap and HTTP/DB instrumentation as defined in AGENTS.md (Observability
section) and ADR-009. `Init()` sets up trace and metric providers, configures OTLP
exporters, and returns a shutdown function. `HTTPTracing()` creates spans for incoming
requests. `HTTPMetrics()` records request count, duration, and size histograms.

**Done when:**
- [ ] `Init` configures trace provider with OTLP gRPC exporter when traces enabled
- [ ] `Init` configures meter provider with Prometheus exporter when metrics enabled
- [ ] `HTTPTracing` creates a span per request with method, route, status attributes
- [ ] `HTTPMetrics` records `http_server_request_duration_seconds` histogram
- [ ] `PrometheusHandler` serves `/metrics` endpoint in Prometheus format
- [ ] `InstrumentDB` adds spans for database queries with `db.operation` attribute
- [ ] All features are no-ops when disabled (no panics, no overhead)
- [ ] 100% test coverage (use noop exporters for unit tests)

### P0.8: testutil — test helpers [x]

**Depends on:** P0.5
**Repo:** chamicore-lib

**Files:**
- `testutil/postgres.go` — `NewTestPostgres(t, migrationsPath)` returns `*sql.DB` + cleanup
- `testutil/http.go` — `NewTestServer(t, handler)`, `AssertProblemDetail(t, resp, status)`
- `testutil/postgres_test.go`
- `testutil/http_test.go`

**Description:**
Test utilities shared across all services.

`NewTestPostgres(t, migrationsPath)` uses `testcontainers-go` to spin up a PostgreSQL
container, applies migrations, and returns a connected `*sql.DB`. Registers a `t.Cleanup`
to terminate the container.

`AssertProblemDetail(t, resp, expectedStatus)` reads the response body, decodes it as
a `ProblemDetail`, and asserts the status code matches.

**Done when:**
- [ ] `NewTestPostgres` starts a real PostgreSQL container and applies migrations
- [ ] Returned `*sql.DB` is usable for queries immediately
- [ ] Container is cleaned up automatically via `t.Cleanup`
- [ ] `AssertProblemDetail` correctly decodes and asserts RFC 9457 responses
- [ ] Integration test verifies the full lifecycle (container start, migrate, query, cleanup)
- [ ] 100% test coverage

### P0.9: update templates to use canonical import paths [x]

**Depends on:** P0.1, P0.2, P0.3, P0.4
**Repo:** chamicore (monorepo, `templates/` directory)

**Files:**
- `templates/service/cmd/service/main.go`
- `templates/service/internal/server/server.go`
- `templates/service/internal/server/handlers.go`
- `templates/service/pkg/client/client.go`

**Description:**
Update template imports from `chamicore-lib/pkg/http` and `chamicore-lib/pkg/middleware`
to the canonical paths: `chamicore-lib/httputil`, `chamicore-lib/auth`, `chamicore-lib/otel`.
Also add the test template files described below.

**Additional template files to create:**
- `templates/service/internal/server/handlers_test.go` — handler test template with mock store
- `templates/service/internal/store/mock_test.go` — mock store implementation template
- `templates/service/internal/store/postgres_test.go` — integration test template with testcontainers

**Done when:**
- [ ] All template files use canonical `chamicore-lib/` import paths (no `pkg/http`, `pkg/middleware`)
- [ ] Test template for handlers demonstrates mock store usage and table-driven tests
- [ ] Test template for postgres demonstrates testcontainers + `//go:build integration`
- [ ] Mock store template follows the hand-written function-field pattern
- [ ] `grep -r 'chamicore-lib/pkg/' templates/` returns zero results
- [ ] Templates still compile conceptually (no broken import references)

---

## Phase 1: Auth Service (chamicore-auth)

All other services depend on chamicore-auth for JWKS and JWT validation.
Reference: [ADR-011](ARCHITECTURE/ADR-011-consolidated-auth-service.md).

### P1.1: auth service scaffold and JWKS endpoint [x]

**Depends on:** P0.1, P0.2, P0.4, P0.5, P0.7, P0.8
**Repo:** chamicore-auth

**Files:**
- Full service scaffold from templates (placeholder replacement with auth values)
- `internal/authn/jwks.go` — JWKS key generation, storage, rotation
- `internal/authn/token.go` — JWT creation (sign with RS256)
- `migrations/postgres/000001_init.up.sql` — `auth` schema, `service_accounts`, `revoked_tokens` tables
- `migrations/postgres/000001_init.down.sql`
- `api/openapi.yaml` — JWKS + token endpoints

**Description:**
Bootstrap the auth service using the template scaffold. Implement the minimum viable
auth surface: JWKS endpoint and token issuance.

Key endpoints for this task:
- `GET /.well-known/jwks.json` — serves the public keys (no auth)
- `GET /.well-known/openid-configuration` — OIDC discovery metadata (no auth)
- `GET /health`, `GET /readiness`, `GET /version`, `GET /metrics`

Generate an RSA key pair on first startup (store in DB or file). Serve the public key
as JWKS. This enables other services to validate tokens immediately.

**Done when:**
- [ ] Service compiles and starts with `go run cmd/chamicore-auth/main.go`
- [ ] `GET /.well-known/jwks.json` returns valid JWKS with at least one RSA public key
- [ ] `GET /.well-known/openid-configuration` returns issuer, jwks_uri, supported algorithms
- [ ] Health, readiness, version, metrics endpoints work
- [ ] Migrations create `auth` schema with `service_accounts` and `revoked_tokens` tables
- [ ] OpenAPI spec covers all implemented endpoints
- [ ] Swagger UI accessible at `/api/docs`
- [ ] 100% test coverage (unit + integration with testcontainers)
- [ ] `golangci-lint run` passes

### P1.2: token exchange and service accounts [x]

**Depends on:** P1.1
**Repo:** chamicore-auth

**Files:**
- `internal/authn/exchange.go` — OIDC token exchange logic
- `internal/authn/service_account.go` — service account CRUD
- `internal/server/handlers_token.go` — token endpoint handlers
- `internal/server/handlers_service_account.go` — service account CRUD handlers
- Tests for all new files

**Description:**
Implement the token exchange flow and service account management.

Token exchange (`POST /auth/v1/token`):
- Accept an external IdP token (JWT) in the request body
- Validate it against the IdP's JWKS (configurable IdP list)
- Issue a short-lived Chamicore JWT with mapped claims (sub, scope, roles, name, email)
- For service accounts: accept `client_id` + `client_secret` grant

Service accounts (`/auth/v1/service-accounts`):
- Full CRUD with the standard envelope pattern
- Secret is hashed (bcrypt) before storage; never returned in responses
- `scopes` field defines the maximum scopes the service account can request

Dev mode: when enabled, `POST /auth/v1/token` issues admin tokens without validation.

**Done when:**
- [ ] `POST /auth/v1/token` with valid external JWT returns a Chamicore JWT
- [ ] Chamicore JWT contains correct claims: `iss`, `sub`, `aud`, `exp`, `iat`, `jti`, `scope`, `roles`
- [ ] `POST /auth/v1/token` with `client_id`/`client_secret` issues service account token with `sa: true`
- [ ] Service account CRUD works (create, list, get, delete)
- [ ] Service account secrets are bcrypt-hashed in DB, never returned in API responses
- [ ] Invalid/expired external tokens return `401 Unauthorized`
- [ ] Dev mode issues admin tokens without external IdP validation
- [ ] Token lifetime is configurable via environment variable
- [ ] 100% test coverage

### P1.3: token revocation [x]

**Depends on:** P1.2
**Repo:** chamicore-auth

**Files:**
- `internal/authn/revocation.go` — revocation logic
- `internal/server/handlers_revocation.go` — revocation endpoints
- Tests for all new files

**Description:**
Implement token revocation for security incident response.

Endpoints:
- `POST /auth/v1/token/revoke` — revoke a token by JTI (admin only)
- `GET /auth/v1/revocations?active=true` — list active revocations (service accounts use this to refresh their cache; supports `If-None-Match` for ETag-based polling)

Revoked tokens are stored with their `expires_at` timestamp. Expired revocations are
automatically cleaned up (background goroutine or on-query filter).

**Done when:**
- [ ] `POST /auth/v1/token/revoke` with valid JTI marks token as revoked
- [ ] `GET /auth/v1/revocations?active=true` returns only non-expired revocations
- [ ] Revocations endpoint supports `If-None-Match` / ETag for efficient polling
- [ ] Expired revocations are excluded from responses
- [ ] 100% test coverage

### P1.4: Casbin policy engine [x]

**Depends on:** P1.2
**Repo:** chamicore-auth

**Files:**
- `internal/authz/casbin.go` — Casbin enforcer setup with PostgreSQL adapter
- `internal/authz/policy.go` — policy management logic
- `internal/server/handlers_policy.go` — policy CRUD endpoints
- `internal/server/handlers_role.go` — role membership endpoints
- Tests for all new files

**Description:**
Integrate Casbin for RBAC/ABAC policy management. Policies determine which scopes
are embedded in tokens at issuance time.

Endpoints:
- `GET /auth/v1/policies` — list policies
- `POST /auth/v1/policies` — create/update policy
- `DELETE /auth/v1/policies/{id}` — delete policy
- `GET /auth/v1/roles` — list roles and memberships
- `POST /auth/v1/roles/{role}/members` — add member to role
- `DELETE /auth/v1/roles/{role}/members/{sub}` — remove member

Use `casbin/gorm-adapter/v3` for PostgreSQL storage of policies.

**Done when:**
- [ ] Casbin enforcer initializes with PostgreSQL adapter
- [ ] Policy CRUD endpoints work with standard envelope responses
- [ ] Role membership management works (add, remove, list members)
- [ ] Token exchange (P1.2) uses Casbin to determine scopes for the issued token
- [ ] Default policies created on first startup (admin role with full access)
- [ ] 100% test coverage

### P1.5: device credential store [x]

**Depends on:** P1.1
**Repo:** chamicore-auth

**Files:**
- `internal/store/credentials.go` — credential CRUD in store
- `internal/server/handlers_credential.go` — credential endpoints
- `migrations/postgres/000002_device_credentials.up.sql`
- `migrations/postgres/000002_device_credentials.down.sql`
- Tests for all new files

**Description:**
Device credential management for use by chamicore-discovery.
See ADR-011 (device credentials section) and ADR-013.

Endpoints:
- `POST /auth/v1/credentials` — store a credential set
- `GET /auth/v1/credentials` — list credentials (secrets redacted)
- `GET /auth/v1/credentials/{id}` — retrieve credential (secrets included, requires `read:credentials` scope)
- `PUT /auth/v1/credentials/{id}` — update credential
- `DELETE /auth/v1/credentials/{id}` — delete credential

Credentials include: `name`, `type` (e.g., `redfish`, `ipmi`), `username`, `secret`
(encrypted at rest), `tags` (JSONB for flexible metadata like "site", "rack").

**Done when:**
- [ ] Full CRUD for device credentials with envelope responses
- [ ] Secrets are encrypted at rest in the database (AES-GCM with key from env var)
- [ ] `GET /auth/v1/credentials` (list) redacts secrets in responses
- [ ] `GET /auth/v1/credentials/{id}` returns secrets only with `read:credentials` scope
- [ ] Tags stored as JSONB, queryable via `?tag=site:east`
- [ ] 100% test coverage

### P1.6: auth client SDK [x]

**Depends on:** P1.1, P1.2, P0.3
**Repo:** chamicore-auth

**Files:**
- `pkg/client/client.go` — typed HTTP client for auth API
- `pkg/types/types.go` — public request/response types
- `pkg/client/client_test.go`

**Description:**
Typed Go client SDK for chamicore-auth. Used by other services for token management,
by the CLI for login flows, and by integration tests. Built on the
`chamicore-lib/httputil/client` base.

Methods: `ExchangeToken`, `RefreshToken`, `RevokeToken`, `GetJWKS`,
`ListServiceAccounts`, `CreateServiceAccount`, `DeleteServiceAccount`,
`ListCredentials`, `GetCredential`, `CreateCredential`, `UpdateCredential`,
`DeleteCredential`, `ListPolicies`, `CreatePolicy`, `DeletePolicy`,
`ListRoles`, `AddRoleMember`, `RemoveRoleMember`, `ListRevocations`.

**Done when:**
- [ ] All auth API endpoints have corresponding typed client methods
- [ ] Client handles token refresh automatically when configured
- [ ] `APIError` is returned for non-2xx responses with parsed ProblemDetail
- [ ] 100% test coverage (using httptest server mocking the auth API)

---

## Phase 2: Core Inventory (chamicore-smd)

SMD is the central inventory service. BSS, Cloud-Init, Discovery, and Kea-Sync
all depend on it. Reference: [ADR-010](ARCHITECTURE/ADR-010-component-identifiers.md).

### P2.1: SMD scaffold and component CRUD [ ]

**Depends on:** P0.1 through P0.8, P1.1 (JWKS endpoint must exist for JWT validation)
**Repo:** chamicore-smd

**Files:**
- Full service scaffold from templates (placeholder replacement with SMD values)
- `internal/model/component.go` — Component domain model
- `internal/server/handlers_component.go` — Component CRUD handlers
- `internal/store/store.go` — Store interface with component methods
- `internal/store/postgres_component.go` — PostgreSQL component implementation
- `pkg/types/component.go` — public Component types
- `migrations/postgres/000001_init.up.sql` — `smd` schema, `components` table
- `api/openapi.yaml` — Component endpoints

**Description:**
Bootstrap SMD with the Component resource as defined in ADR-010. The `components`
table schema:

```sql
CREATE SCHEMA IF NOT EXISTS smd;
SET search_path TO smd;

CREATE TABLE components (
    id          VARCHAR(255) PRIMARY KEY,
    type        VARCHAR(63)  NOT NULL,
    state       VARCHAR(32)  NOT NULL DEFAULT 'Empty',
    role        VARCHAR(32)  NOT NULL DEFAULT '',
    nid         BIGINT,
    parent_id   VARCHAR(255) REFERENCES components(id),
    rack        TEXT,
    unit        INT,
    slot        INT          DEFAULT 0,
    sub_slot    INT          DEFAULT 0,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);
```

Component IDs validated by `identity.ValidateID()`. Component types, states, and roles
validated by the `identity` package enums. Client-provided ID is used if present;
otherwise server generates via `identity.GenerateID(type)`.

Filter query parameters: `?type=Node`, `?state=Ready`, `?role=Compute`, `?rack=R12`,
`?parent_id=bmc-xyz`, `?nid=1001`.

**Done when:**
- [ ] Full CRUD for components (POST 201 + Location, GET 200 + ETag, PUT + If-Match, PATCH, DELETE 204)
- [ ] Component IDs validated against ADR-010 regex
- [ ] Type, state, role validated against `identity` enums; unknown values rejected (422)
- [ ] Server generates ID when not provided by client
- [ ] List supports filtering by type, state, role, rack, parent_id, nid
- [ ] List supports pagination (limit, offset) with correct total count
- [ ] `parent_id` enforces referential integrity via FK
- [ ] All responses use resource envelope pattern
- [ ] ETags on GET single; If-Match required on PUT (412 on mismatch; 428 if missing)
- [ ] Integration tests with testcontainers verify all DB operations
- [ ] OpenAPI spec matches implementation exactly
- [ ] 100% test coverage
- [ ] `golangci-lint run` passes

### P2.2: network interfaces [ ]

**Depends on:** P2.1
**Repo:** chamicore-smd

**Files:**
- `internal/model/interface.go` — EthernetInterface model
- `internal/server/handlers_interface.go` — interface CRUD handlers (nested under components)
- `internal/store/postgres_interface.go` — PostgreSQL interface implementation
- `pkg/types/interface.go` — public interface types
- `migrations/postgres/000002_interfaces.up.sql` — `ethernet_interfaces` table
- `migrations/postgres/000002_interfaces.down.sql`
- Tests for all new files

**Description:**
Ethernet interfaces are associated with components. They store MAC addresses and
IP addresses used by BSS (boot script lookup by MAC) and Kea-Sync (DHCP reservations).

```sql
CREATE TABLE smd.ethernet_interfaces (
    id            VARCHAR(255) PRIMARY KEY DEFAULT gen_random_uuid()::text,
    component_id  VARCHAR(255) NOT NULL REFERENCES components(id) ON DELETE CASCADE,
    mac_addr      TEXT         NOT NULL,
    ip_addrs      JSONB        NOT NULL DEFAULT '[]',
    description   TEXT         NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    UNIQUE(mac_addr)
);
CREATE INDEX idx_eth_component ON smd.ethernet_interfaces (component_id);
CREATE INDEX idx_eth_mac ON smd.ethernet_interfaces (mac_addr);
```

Endpoints nested under components:
- `GET /hsm/v2/Inventory/EthernetInterfaces` — list all interfaces (filterable by component_id, mac)
- `GET /hsm/v2/Inventory/EthernetInterfaces/{id}` — get by interface ID
- `POST /hsm/v2/Inventory/EthernetInterfaces` — create (component_id required)
- `PATCH /hsm/v2/Inventory/EthernetInterfaces/{id}` — update
- `DELETE /hsm/v2/Inventory/EthernetInterfaces/{id}` — delete

MAC uniqueness is enforced at the DB level. Duplicate MAC returns `409 Conflict`.

**Done when:**
- [ ] Full CRUD for ethernet interfaces
- [ ] MAC address uniqueness enforced; duplicate returns 409
- [ ] Cascade delete when parent component is deleted
- [ ] Filterable by `component_id` and `mac_addr`
- [ ] Creating a component with embedded interfaces works (transaction)
- [ ] 100% test coverage

### P2.3: groups and partitions [ ]

**Depends on:** P2.1
**Repo:** chamicore-smd

**Files:**
- `internal/model/group.go` — Group, Partition models
- `internal/server/handlers_group.go` — group/partition handlers
- `internal/store/postgres_group.go`
- `pkg/types/group.go`
- `migrations/postgres/000003_groups.up.sql`
- `migrations/postgres/000003_groups.down.sql`
- Tests

**Description:**
Groups are named sets of component IDs. Partitions are exclusive groups (a component
can belong to at most one partition). Used for policy targeting and batch operations.

```sql
CREATE TABLE smd.groups (
    name         VARCHAR(255) PRIMARY KEY,
    description  TEXT         NOT NULL DEFAULT '',
    tags         JSONB        NOT NULL DEFAULT '{}',
    exclusive    BOOLEAN      NOT NULL DEFAULT false,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE TABLE smd.group_members (
    group_name    VARCHAR(255) NOT NULL REFERENCES groups(name) ON DELETE CASCADE,
    component_id  VARCHAR(255) NOT NULL REFERENCES components(id) ON DELETE CASCADE,
    PRIMARY KEY (group_name, component_id)
);
```

Endpoints:
- `GET/POST /hsm/v2/groups` — list, create groups
- `GET/PUT/DELETE /hsm/v2/groups/{name}` — single group operations
- `POST /hsm/v2/groups/{name}/members` — add members
- `DELETE /hsm/v2/groups/{name}/members/{component_id}` — remove member
- Same pattern for `/hsm/v2/partitions` (exclusive=true enforced)

**Done when:**
- [ ] Group CRUD with member management works
- [ ] Partition exclusivity enforced (adding a component to a second partition returns 409)
- [ ] Deleting a component cascades to remove it from all groups
- [ ] 100% test coverage

### P2.4: sync endpoints for downstream services [ ]

**Depends on:** P2.1, P2.2
**Repo:** chamicore-smd

**Files:**
- `internal/server/handlers_sync.go` — sync-optimized list endpoint
- Tests

**Description:**
BSS and Cloud-Init need to poll SMD for component data. Implement an efficient
endpoint optimized for sync consumers as defined in ADR-014.

`GET /hsm/v2/State/Components` already supports filtering and pagination.
For sync, add support for:
- `fields` query parameter to request only specific fields: `?fields=id,mac,role,nid,state`
  (reduces response size for sync consumers)
- `If-None-Match` with ETag on the full list endpoint (returns `304` if nothing changed)
- The list ETag is computed from the most recent `updated_at` across all matching components

This is not a new endpoint — it enhances the existing list endpoint.

**Done when:**
- [ ] `?fields=id,mac,role` returns only requested fields (sparse fieldset)
- [ ] List endpoint supports `If-None-Match` header; returns `304` when data hasn't changed
- [ ] List ETag is based on `MAX(updated_at)` of the result set
- [ ] Sync consumers (BSS, Cloud-Init) can efficiently poll without transferring unchanged data
- [ ] 100% test coverage

### P2.5: SMD client SDK [ ]

**Depends on:** P2.1, P2.2, P2.3, P0.3
**Repo:** chamicore-smd

**Files:**
- `pkg/client/client.go` — typed HTTP client
- `pkg/types/types.go` — consolidated public types
- `pkg/client/client_test.go`

**Description:**
Typed Go client for SMD. Used by BSS (sync), Cloud-Init (sync), Discovery (register
components), Kea-Sync (list interfaces), CLI, and UI.

Methods: `ListComponents(opts)`, `GetComponent(id)`, `CreateComponent(req)`,
`UpdateComponent(id, etag, req)`, `PatchComponent(id, req)`, `DeleteComponent(id)`,
`ListEthernetInterfaces(opts)`, `GetEthernetInterface(id)`,
`CreateEthernetInterface(req)`, `DeleteEthernetInterface(id)`,
`ListGroups(opts)`, `GetGroup(name)`, `CreateGroup(req)`, etc.

The `ListComponents` method supports the `IfNoneMatch` option for sync consumers
(returns `nil, nil` on 304 to signal "no changes").

**Done when:**
- [ ] All SMD endpoints have corresponding typed client methods
- [ ] `ListComponents` with `IfNoneMatch` correctly handles 304 responses
- [ ] `APIError` parsing works for all error status codes
- [ ] 100% test coverage

---

## Phase 3: Boot Path (BSS, Cloud-Init, Kea-Sync)

Critical boot-path services. These are self-sufficient during boot storms per ADR-014.

### P3.1: BSS scaffold and boot parameter CRUD [ ]

**Depends on:** P0.1 through P0.8, P1.1
**Repo:** chamicore-bss

**Files:**
- Full service scaffold (BSS placeholder values)
- `internal/model/bootparam.go` — BootParam model
- `internal/server/handlers_bootparam.go` — CRUD handlers
- `internal/store/postgres_bootparam.go`
- `pkg/types/bootparam.go`
- `migrations/postgres/000001_init.up.sql` — `bss` schema, `boot_params` table (per ADR-014)
- `api/openapi.yaml`

**Description:**
Bootstrap BSS. The `boot_params` table stores per-component and per-role boot
parameters with denormalized MAC addresses (per ADR-014):

```sql
CREATE SCHEMA IF NOT EXISTS bss;
SET search_path TO bss;

CREATE TABLE boot_params (
    id            VARCHAR(255) PRIMARY KEY DEFAULT gen_random_uuid()::text,
    component_id  VARCHAR(255) NOT NULL,
    mac           TEXT,
    role          VARCHAR(32)  NOT NULL DEFAULT '',
    kernel_uri    TEXT         NOT NULL,
    initrd_uri    TEXT         NOT NULL,
    cmdline       TEXT         NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_boot_params_mac ON bss.boot_params (mac) WHERE mac IS NOT NULL;
CREATE INDEX idx_boot_params_component ON bss.boot_params (component_id);
CREATE INDEX idx_boot_params_role ON bss.boot_params (role);
```

CRUD endpoints at `/boot/v1/bootparams` (authenticated).

**Done when:**
- [ ] Full CRUD for boot parameters with envelope responses
- [ ] MAC uniqueness enforced at DB level
- [ ] Filter by `component_id`, `mac`, `role`
- [ ] 100% test coverage (unit + integration)

### P3.2: iPXE boot script endpoint [ ]

**Depends on:** P3.1
**Repo:** chamicore-bss

**Files:**
- `internal/server/handlers_bootscript.go` — boot script handler
- `internal/bootscript/render.go` — iPXE script template rendering
- Tests

**Description:**
The boot script endpoint is the hot path during boot storms. It is **unauthenticated**
(iPXE clients don't carry JWTs). Per ADR-014, it serves entirely from the local
`bss.boot_params` table with no cross-service calls.

`GET /boot/v1/bootscript?mac=<mac>` resolution:
1. Query `bss.boot_params WHERE mac = $1`
2. If not found, extract role from the component's denormalized role field and query
   `bss.boot_params WHERE role = $1 AND mac IS NULL` (role-based default)
3. If still not found, return `404`
4. Render iPXE script from template with kernel_uri, initrd_uri, cmdline

Response: `Content-Type: text/plain` (iPXE script), not JSON.

```
#!ipxe
kernel <kernel_uri> <cmdline>
initrd <initrd_uri>
boot
```

This endpoint must be outside the JWT middleware group.

**Done when:**
- [ ] `GET /boot/v1/bootscript?mac=aa:bb:cc:dd:ee:ff` returns iPXE script (text/plain)
- [ ] MAC-based lookup works
- [ ] Role-based fallback works when no MAC-specific params exist
- [ ] 404 returned when no matching boot params found
- [ ] Endpoint is **unauthenticated** (no JWT middleware)
- [ ] Response is valid iPXE script syntax
- [ ] Performance: <1ms for a single DB query (verified in integration tests)
- [ ] 100% test coverage

### P3.3: BSS sync loop [ ]

**Depends on:** P3.1, P2.5 (SMD client SDK)
**Repo:** chamicore-bss

**Files:**
- `internal/sync/syncer.go` — background sync loop
- `internal/sync/syncer_test.go`
- `internal/server/handlers_sync.go` — sync trigger and status endpoints

**Description:**
Background goroutine that polls SMD for component data and reconciles the local
`bss.boot_params` table. Per ADR-014, this is the Phase 1 change propagation mechanism.

The sync loop:
1. Calls `smdClient.ListComponents(ctx, opts)` with `IfNoneMatch: lastETag`
2. On 304: no changes, sleep until next interval
3. On 200: diff against local data, upsert new/changed MACs, delete removed components
4. Update sync status (timestamp, ETag, counts, errors)

Endpoints:
- `POST /boot/v1/sync` — trigger immediate sync (admin, `write:sync` scope)
- `GET /boot/v1/sync/status` — last sync time, ETag, counts (`read:sync` scope)

Sync must complete successfully once before `/readiness` returns 200.

Configuration:
- `CHAMICORE_BSS_SYNC_INTERVAL=30s`
- `CHAMICORE_BSS_SYNC_ON_STARTUP=true`
- `CHAMICORE_BSS_SMD_URL=http://localhost:27779`

**Done when:**
- [ ] Background sync loop polls SMD on configurable interval
- [ ] ETag-based conditional requests avoid unnecessary data transfer
- [ ] Reconciliation correctly handles adds, updates, and deletes
- [ ] `/readiness` returns 503 until first successful sync
- [ ] `POST /boot/v1/sync` triggers immediate sync
- [ ] `GET /boot/v1/sync/status` reports last sync time and counts
- [ ] Sync errors are logged but don't crash the service
- [ ] 100% test coverage (mock SMD client, verify reconciliation logic)

### P3.4: Cloud-Init scaffold and payload CRUD [ ]

**Depends on:** P0.1 through P0.8, P1.1
**Repo:** chamicore-cloud-init

**Files:**
- Full service scaffold (cloud-init placeholder values)
- `internal/model/payload.go`
- `internal/server/handlers_payload.go`
- `internal/store/postgres_payload.go`
- `pkg/types/payload.go`
- `migrations/postgres/000001_init.up.sql` — `cloudinit` schema, `payloads` table (per ADR-014)
- `api/openapi.yaml`

**Description:**
Bootstrap the Cloud-Init service. The `payloads` table stores per-component cloud-init
data:

```sql
CREATE SCHEMA IF NOT EXISTS cloudinit;
SET search_path TO cloudinit;

CREATE TABLE payloads (
    id            VARCHAR(255) PRIMARY KEY DEFAULT gen_random_uuid()::text,
    component_id  VARCHAR(255) NOT NULL UNIQUE,
    role          VARCHAR(32)  NOT NULL DEFAULT '',
    user_data     TEXT         NOT NULL DEFAULT '',
    meta_data     JSONB        NOT NULL DEFAULT '{}',
    vendor_data   TEXT         NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_payloads_component ON cloudinit.payloads (component_id);
CREATE INDEX idx_payloads_role ON cloudinit.payloads (role);
```

CRUD at `/cloud-init/payloads` (authenticated). Also: unauthenticated serving endpoints
for booting nodes.

Serving endpoints (unauthenticated, outside JWT middleware):
- `GET /cloud-init/{component_id}/user-data` — returns raw user-data (text/plain or text/yaml)
- `GET /cloud-init/{component_id}/meta-data` — returns meta-data (application/json)
- `GET /cloud-init/{component_id}/vendor-data` — returns raw vendor-data

**Done when:**
- [ ] Full CRUD for payloads with envelope responses (authenticated)
- [ ] Unauthenticated serving endpoints return raw cloud-init data
- [ ] `component_id` uniqueness enforced
- [ ] 100% test coverage

### P3.5: Cloud-Init sync loop [ ]

**Depends on:** P3.4, P2.5 (SMD client SDK)
**Repo:** chamicore-cloud-init

**Files:**
- `internal/sync/syncer.go`
- `internal/sync/syncer_test.go`
- `internal/server/handlers_sync.go`

**Description:**
Same pattern as BSS sync (P3.3). Polls SMD for component metadata and reconciles
the local `cloudinit.payloads` table. Syncs component_id, role, and any metadata
needed for template rendering.

**Done when:**
- [ ] Background sync loop polls SMD on configurable interval
- [ ] ETag-based conditional requests
- [ ] Reconciliation handles adds, updates, deletes
- [ ] `/readiness` returns 503 until first successful sync
- [ ] Sync trigger and status endpoints work
- [ ] 100% test coverage

### P3.6: Kea-Sync service [ ]

**Depends on:** P2.5 (SMD client SDK)
**Repo:** chamicore-kea-sync

**Files:**
- `cmd/chamicore-kea-sync/main.go`
- `internal/config/config.go`
- `internal/sync/syncer.go` — main sync loop
- `internal/kea/client.go` — Kea control agent HTTP client
- `internal/kea/reservation.go` — DHCP reservation model
- Tests

**Description:**
Kea-Sync is a headless daemon (no HTTP API) that watches SMD for components with
MAC addresses and pushes DHCP reservations to the Kea DHCP server via its control
agent REST API.

Sync loop:
1. Poll `smdClient.ListEthernetInterfaces(ctx, opts)` with ETag
2. Diff against current Kea reservations
3. Push new/updated reservations via Kea control agent `reservation-add` command
4. Remove stale reservations via `reservation-del`

The Kea control agent API uses a JSON-RPC-like format:
```json
{"command": "reservation-add", "service": ["dhcp4"], "arguments": {...}}
```

Configuration:
- `CHAMICORE_KEASYNC_SMD_URL=http://localhost:27779`
- `CHAMICORE_KEASYNC_KEA_URL=http://localhost:8000`
- `CHAMICORE_KEASYNC_SYNC_INTERVAL=30s`

**Done when:**
- [ ] Service starts, polls SMD for ethernet interfaces
- [ ] Pushes DHCP reservations to Kea control agent
- [ ] Handles add, update, delete of reservations
- [ ] ETag-based polling avoids unnecessary work
- [ ] Graceful shutdown completes in-flight sync
- [ ] Kea control agent errors are logged, don't crash the service
- [ ] 100% test coverage (mock Kea API via httptest)

### P3.7: BSS and Cloud-Init client SDKs [ ]

**Depends on:** P3.1, P3.4, P0.3
**Repos:** chamicore-bss, chamicore-cloud-init

**Files:**
- `chamicore-bss/pkg/client/client.go` + tests
- `chamicore-bss/pkg/types/types.go`
- `chamicore-cloud-init/pkg/client/client.go` + tests
- `chamicore-cloud-init/pkg/types/types.go`

**Description:**
Typed Go clients for BSS and Cloud-Init. Used by the CLI and integration tests.

**Done when:**
- [ ] All CRUD endpoints have typed client methods for both services
- [ ] Client handles ETag / conditional requests
- [ ] 100% test coverage for both clients

---

## Phase 4: Discovery and CLI

### P4.1: Discovery service scaffold and target CRUD [ ]

**Depends on:** P0.1 through P0.8, P1.1
**Repo:** chamicore-discovery

**Files:**
- Full service scaffold (discovery placeholder values)
- `internal/model/target.go`, `internal/model/scan.go`
- `internal/server/handlers_target.go`
- `internal/store/postgres_target.go`
- `pkg/types/types.go`
- `migrations/postgres/000001_init.up.sql` — `discovery` schema, `targets` + `scan_jobs` tables
- `api/openapi.yaml`

**Description:**
Bootstrap the discovery service with target management. Targets define what to scan
(addresses, driver, credentials, schedule). See ADR-013.

**Done when:**
- [ ] Full CRUD for discovery targets with envelope responses
- [ ] `scan_jobs` table created (used by P4.2)
- [ ] 100% test coverage

### P4.2: driver interface and Redfish driver [ ]

**Depends on:** P4.1, P1.5 (device credentials), P2.5 (SMD client SDK)
**Repo:** chamicore-discovery

**Files:**
- `internal/driver/driver.go` — `Driver` interface
- `internal/driver/redfish/redfish.go` — Redfish driver implementation
- `internal/driver/manual/manual.go` — manual/API driver
- `internal/driver/csv/csv.go` — CSV/JSON import driver
- `internal/scanner/scanner.go` — scan orchestrator (concurrent scans, result collection)
- `internal/server/handlers_scan.go` — scan CRUD endpoints
- Tests for all

**Description:**
Implement the pluggable driver architecture and the Redfish driver as the primary
discovery mechanism. See ADR-013 for the driver interface.

Scan endpoints:
- `POST /discovery/v1/scans` — start a scan
- `GET /discovery/v1/scans` — list scan jobs
- `GET /discovery/v1/scans/{id}` — get scan status/results
- `DELETE /discovery/v1/scans/{id}` — cancel running scan
- `POST /discovery/v1/targets/{id}/scan` — scan a specific target
- `GET /discovery/v1/drivers` — list available drivers

The scanner orchestrates concurrent discovery, fetches credentials from
chamicore-auth, and registers discovered components in SMD.

**Done when:**
- [ ] `Driver` interface defined with `Name()`, `Discover()`, `Probe()`
- [ ] Redfish driver discovers BMCs and extracts component information
- [ ] CSV driver imports components from CSV/JSON files
- [ ] Manual driver accepts direct component definitions
- [ ] Scanner runs concurrent scans with configurable concurrency limit
- [ ] Discovered components registered in SMD via the SMD client SDK
- [ ] Credentials fetched from chamicore-auth via the auth client SDK
- [ ] Scan jobs tracked in DB with state transitions (pending -> running -> completed/failed)
- [ ] All scan endpoints work
- [ ] 100% test coverage (mock Redfish BMC responses, mock SMD/auth clients)

### P4.3: discovery CLI mode [ ]

**Depends on:** P4.2
**Repo:** chamicore-discovery

**Files:**
- `internal/cli/root.go` — Cobra root command with `serve` vs CLI subcommands
- `internal/cli/serve.go` — starts the HTTP server (equivalent to current main)
- `internal/cli/scan.go` — standalone scan command
- `internal/cli/probe.go` — standalone probe command
- `internal/cli/import.go` — CSV/JSON import command
- `internal/cli/drivers.go` — list available drivers
- `internal/cli/targets.go` — target management (service client mode)
- `internal/cli/scans.go` — scan management (service client mode)
- `internal/cli/output.go` — table/JSON/YAML output formatting
- `cmd/chamicore-discovery/main.go` — updated to use Cobra root
- Tests

**Description:**
Implement the dual-mode binary. See ADR-013 (Dual-Mode Operation section).

Standalone mode (no running service needed):
```bash
chamicore-discovery scan 10.0.0.1 --driver redfish --username admin --password <s>
chamicore-discovery probe 10.0.0.1 --driver redfish
chamicore-discovery import nodes.csv --format csv --dry-run
chamicore-discovery drivers
```

Service client mode (talks to running discovery service):
```bash
chamicore-discovery targets list
chamicore-discovery scans status <id>
```

Server mode:
```bash
chamicore-discovery serve
```

**Done when:**
- [ ] `chamicore-discovery serve` starts the HTTP server
- [ ] `chamicore-discovery scan` performs standalone discovery without a running service
- [ ] `chamicore-discovery probe` queries a single BMC
- [ ] `chamicore-discovery import` reads CSV/JSON and registers in SMD
- [ ] `chamicore-discovery drivers` lists available drivers
- [ ] `chamicore-discovery targets list` talks to running service
- [ ] All commands support `--output table|json|yaml` and `--dry-run` where appropriate
- [ ] 100% test coverage

### P4.4: CLI scaffold and auth commands [ ]

**Depends on:** P1.6 (auth client SDK), P0.3
**Repo:** chamicore-cli

**Files:**
- `cmd/chamicore/main.go` — Cobra root command
- `internal/config/config.go` — config file loading (`~/.chamicore/config.yaml`)
- `internal/output/output.go` — table/JSON/YAML output formatting
- `internal/auth/login.go` — `chamicore auth login` (OIDC browser flow)
- `internal/auth/token.go` — `chamicore auth token` (show current token)
- `internal/auth/whoami.go` — `chamicore auth whoami` (show token claims)
- Tests

**Description:**
Bootstrap the CLI with configuration management and auth commands.

Config file (`~/.chamicore/config.yaml`):
```yaml
endpoint: https://chamicore.example.com
auth:
  token: <cached JWT>
  refresh_token: <cached refresh token>
output: table
```

Environment variable overrides: `CHAMICORE_ENDPOINT`, `CHAMICORE_TOKEN`.

**Done when:**
- [ ] `chamicore --help` shows available subcommands
- [ ] Config loaded from `~/.chamicore/config.yaml` with env var overrides
- [ ] `chamicore auth login` initiates OIDC flow and caches token
- [ ] `chamicore auth token` displays the current cached token
- [ ] `chamicore auth whoami` decodes and displays token claims
- [ ] `--output table|json|yaml` flag works globally
- [ ] 100% test coverage

### P4.5: CLI per-service subcommands [ ]

**Depends on:** P4.4, P2.5, P3.7, P1.6
**Repo:** chamicore-cli

**Files:**
- `internal/smd/components.go` — `chamicore smd components list|get|create|update|delete`
- `internal/smd/interfaces.go` — `chamicore smd interfaces list|get|create|delete`
- `internal/smd/groups.go` — `chamicore smd groups list|get|create|delete`
- `internal/bss/bootparams.go` — `chamicore bss bootparams list|get|create|update|delete`
- `internal/cloudinit/payloads.go` — `chamicore cloud-init payloads list|get|create|update|delete`
- `internal/auth/policies.go` — `chamicore auth policy list|create|delete`
- `internal/auth/roles.go` — `chamicore auth role list|add-member|remove-member`
- `internal/auth/serviceaccounts.go` — `chamicore auth service-account list|create|delete`
- `internal/auth/credentials.go` — `chamicore auth credential list|get|create|update|delete`
- `internal/discovery/targets.go` — `chamicore discovery target list|create|update|delete`
- `internal/discovery/scans.go` — `chamicore discovery scan list|status|trigger|cancel`
- Tests for each subcommand

**Description:**
Implement per-service subcommands using the typed client SDKs from each service's
`pkg/client/` package. Each subcommand group mirrors the service's API.

**Done when:**
- [ ] All service CRUD operations accessible via CLI
- [ ] Output formatting works (table, JSON, YAML) for all commands
- [ ] Error messages include service name and HTTP status
- [ ] `--help` on every subcommand shows usage and available flags
- [ ] 100% test coverage

### P4.6: CLI composite workflows [ ]

**Depends on:** P4.5
**Repo:** chamicore-cli

**Files:**
- `internal/composite/provision.go` — `chamicore node provision`
- `internal/composite/decommission.go` — `chamicore node decommission`
- Tests

**Description:**
Composite commands that orchestrate multiple services in sequence.

`chamicore node provision --id node-abc --kernel <uri> --initrd <uri> --user-data <file>`:
1. Create/update component in SMD (if not exists)
2. Set boot params in BSS
3. Configure cloud-init payload
4. Report summary

`chamicore node decommission --id node-abc`:
1. Delete cloud-init payload
2. Delete boot params
3. Set component state to "Empty" in SMD
4. Report summary

**Done when:**
- [ ] `chamicore node provision` orchestrates SMD + BSS + Cloud-Init
- [ ] `chamicore node decommission` reverses the provisioning
- [ ] Partial failures report which steps succeeded and which failed
- [ ] `--dry-run` shows what would be done without making changes
- [ ] 100% test coverage

---

## Phase 5: UI and Deployment

### P5.1: Docker Compose dev stack [ ]

**Depends on:** P1.1 (auth exists), P2.1 (SMD exists)
**Repo:** chamicore-deploy

**Files:**
- `docker-compose.yml` — full dev stack
- `docker-compose.override.yml` — dev-mode defaults (DEV_MODE=true, ports exposed)
- `.env.example` — example environment file
- `Makefile` — `compose-up`, `compose-down`, `compose-logs`
- `grafana/dashboards/` — pre-built dashboards (optional, can defer)
- `prometheus/prometheus.yml` — scrape configs for all services

**Description:**
Docker Compose stack for local development. Includes PostgreSQL, all Chamicore services,
NATS (for future use), Prometheus, Grafana, and Jaeger/Tempo.

All services run in dev mode (`DEV_MODE=true`) with all ports exposed to localhost.
PostgreSQL auto-creates the `chamicore` database with per-service schemas via
init scripts.

**Done when:**
- [ ] `docker compose up` starts PostgreSQL + all available Chamicore services
- [ ] Services connect to shared PostgreSQL, each with its own schema
- [ ] Dev mode enabled by default (no auth required for development)
- [ ] Prometheus scrapes all `/metrics` endpoints
- [ ] `make compose-up` and `make compose-down` work from the monorepo root
- [ ] `.env.example` documents all configurable variables

### P5.2: Helm charts [ ]

**Depends on:** P5.1
**Repo:** chamicore-deploy

**Files:**
- `charts/chamicore/Chart.yaml`
- `charts/chamicore/values.yaml`
- `charts/chamicore/templates/` — per-service deployments, services, configmaps
- `charts/chamicore/templates/tests/` — Helm test pods

**Description:**
Helm chart for production Kubernetes deployment. Single umbrella chart with
per-service sub-charts or templates. See ADR-008.

**Done when:**
- [ ] `helm install chamicore ./charts/chamicore` deploys the full stack
- [ ] Per-service resource limits, replica counts, and probes configured
- [ ] Values file documents all overridable settings
- [ ] `helm test chamicore` runs smoke tests
- [ ] Ingress templates support configurable host/TLS

### P5.3: Web UI backend (Go BFF) [ ]

**Depends on:** P1.6, P2.5, P3.7
**Repo:** chamicore-ui

**Files:**
- `cmd/chamicore-ui/main.go`
- `internal/server/server.go` — chi router for BFF API
- `internal/server/handlers_dashboard.go` — aggregated dashboard data
- `internal/server/handlers_inventory.go` — component browser proxying
- `internal/session/session.go` — cookie-based session management
- `internal/proxy/proxy.go` — service proxy/aggregation layer
- `internal/config/config.go`

**Description:**
Go backend that serves the Vue.js SPA and provides a BFF API. Handles OIDC login
with chamicore-auth, maintains sessions, and proxies/aggregates API calls to
microservices on behalf of the frontend.

**Done when:**
- [ ] Serves embedded Vue.js SPA on `/`
- [ ] BFF API proxies requests to SMD, BSS, Cloud-Init, Auth
- [ ] Session-based auth with secure HTTP-only cookies
- [ ] OIDC login flow via chamicore-auth
- [ ] 100% test coverage on Go backend

### P5.4: Web UI frontend (Vue.js) [ ]

**Depends on:** P5.3
**Repo:** chamicore-ui

**Files:**
- `frontend/src/` — Vue.js application
- `frontend/package.json`, `frontend/vite.config.ts`, `frontend/tsconfig.json`

**Description:**
Vue.js 3 SPA with TypeScript. Key views: Dashboard, Inventory, Boot Configuration,
Cloud-Init, Discovery, Users & Roles. Communicates only with the BFF backend.

**Done when:**
- [ ] Dashboard view shows system overview (component counts, recent events)
- [ ] Inventory view lists and filters components
- [ ] Boot config view manages BSS boot parameters
- [ ] Cloud-Init view manages payloads
- [ ] User/role management view for Casbin policies
- [ ] All views work end-to-end through the BFF backend
- [ ] TypeScript strict mode, ESLint, Prettier passing
- [ ] Vitest unit tests for stores and key components

---

## Phase 6: Cross-Cutting Quality

### P6.1: system integration tests [ ]

**Depends on:** P2.1, P3.1, P3.4, P1.2, P5.1
**Repo:** chamicore (monorepo `tests/` directory)

**Files:**
- `tests/go.mod`
- `tests/system/boot_path_test.go` — register node in SMD -> set boot params -> verify boot script
- `tests/system/auth_flow_test.go` — authenticate -> use token -> verify rejection with bad token
- `tests/system/cloud_init_test.go` — register node -> configure payload -> verify served data
- `tests/system/discovery_test.go` — mock BMC -> discover -> verify SMD registration

**Description:**
Cross-service integration tests using Docker Compose. Written in Go using service
`pkg/client/` packages. Build tag: `//go:build system`.

**Done when:**
- [ ] Boot path test works end-to-end (SMD -> BSS -> boot script)
- [ ] Auth flow test verifies token issuance, usage, and rejection
- [ ] Cloud-init test verifies payload serving
- [ ] All tests use typed client SDKs
- [ ] Tests run against Docker Compose stack

### P6.2: smoke tests [ ]

**Depends on:** P5.1
**Repo:** chamicore (monorepo `tests/smoke/` directory)

**Files:**
- `tests/smoke/health_test.go` — verify all services are reachable
- `tests/smoke/crud_test.go` — one happy-path operation per service

**Description:**
Quick health verification tests. Build tag: `//go:build smoke`. Must complete in <30s.

**Done when:**
- [ ] Each service health endpoint returns 200
- [ ] One create + read operation per service succeeds
- [ ] Total runtime < 30 seconds

### P6.3: load tests [ ]

**Depends on:** P6.2
**Repo:** chamicore (monorepo `tests/load/` directory)

**Files:**
- `tests/load/boot_storm.js` — k6 boot storm script
- `tests/load/cloud_init_storm.js` — k6 cloud-init storm script
- `tests/load/inventory_scale.js` — k6 inventory at scale
- `tests/load/baselines.json` — performance baseline thresholds

**Description:**
k6 load test scripts as defined in AGENTS.md (Load and Performance Tests section)
and ADR-012. Boot storm simulation at 10,000+ VUs.

**Done when:**
- [ ] Boot storm test: 10,000 concurrent boot script requests at p99 < 100ms
- [ ] Cloud-init storm test: 10,000 concurrent payload fetches at p99 < 100ms
- [ ] Inventory scale test: 50,000 components, single GET p99 < 10ms
- [ ] `baselines.json` contains initial performance thresholds
- [ ] `make test-load` runs the full suite
- [ ] `make test-load-quick` runs an abbreviated version (1,000 VUs, 2 min)

### P6.4: Makefile update [ ]

**Depends on:** P4.1
**Repo:** chamicore (monorepo)

**Files:**
- `Makefile`

**Description:**
Add `chamicore-discovery` to the `SERVICES` variable in the top-level Makefile.

**Done when:**
- [ ] `SERVICES` includes `chamicore-discovery`
- [ ] `make build`, `make test`, `make lint` include discovery

---

## Phase 7: Event-Driven Architecture (NATS JetStream)

> **This phase implements [ADR-015](ARCHITECTURE/ADR-015-event-driven-architecture.md).**
> It is intentionally after all synchronous services are complete and tested. Phase 7
> adds NATS JetStream as the event backbone, replacing polling-based sync loops with
> event-driven change propagation. Phase 0-6 services work without events; Phase 7
> adds events as a performance and decoupling enhancement.

### P7.1: events/ core packages in chamicore-lib [ ]

**Depends on:** P0.1
**Repo:** chamicore-lib

**Files:**
- `events/event.go` — Event envelope struct (CloudEvents-compatible: `id`, `source`, `type`, `subject`, `time`, `data`)
- `events/publisher.go` — `Publisher` interface: `Publish(ctx, event) error`
- `events/subscriber.go` — `Subscriber` interface: `Subscribe(ctx, subject, handler) error`, `Close() error`
- `events/subjects.go` — Subject hierarchy helpers (e.g., `chamicore.smd.components.created`)
- Tests for all files

**Description:**
Define the core event abstractions that services use to publish and subscribe to events.
The interfaces are transport-agnostic; NATS JetStream is one implementation. A no-op
publisher is provided for services that don't need events (or for testing).

Event envelope follows CloudEvents 1.0 structure:
```go
type Event struct {
    ID              string          `json:"id"`               // UUID v4
    Source          string          `json:"source"`           // e.g., "chamicore-smd"
    Type            string          `json:"type"`             // e.g., "chamicore.smd.components.created"
    Subject         string          `json:"subject"`          // e.g., "node-a1b2c3"
    Time            time.Time       `json:"time"`
    DataContentType string          `json:"datacontenttype"`  // "application/json"
    Data            json.RawMessage `json:"data"`             // payload
}
```

Subject hierarchy convention:
```
chamicore.<service>.<resource>.<action>
chamicore.smd.components.created
chamicore.smd.components.updated
chamicore.smd.components.deleted
chamicore.bss.bootparams.updated
chamicore.auth.tokens.revoked
```

**Done when:**
- [ ] `Event` struct with CloudEvents-compatible fields
- [ ] `Publisher` interface with `Publish(ctx, Event) error`
- [ ] `Subscriber` interface with `Subscribe(ctx, subject, func(Event) error) error` and `Close() error`
- [ ] `NoopPublisher` for testing and services that don't publish
- [ ] `SubjectFor(service, resource, action) string` helper
- [ ] 100% test coverage
- [ ] `golangci-lint run` passes

### P7.2: NATS JetStream publisher/subscriber [ ]

**Depends on:** P7.1
**Repo:** chamicore-lib

**Files:**
- `events/nats/publisher.go` — JetStream publisher (publishes events to NATS streams)
- `events/nats/subscriber.go` — JetStream subscriber (durable push/pull consumers)
- `events/nats/stream.go` — Stream and consumer creation helpers (idempotent)
- `events/nats/config.go` — NATS connection config (URL, credentials, stream names)
- Integration tests (with `//go:build integration` tag, using testcontainers for NATS)

**Description:**
Implement the `Publisher` and `Subscriber` interfaces using NATS JetStream.

Stream configuration:
- One stream per service: `CHAMICORE_SMD`, `CHAMICORE_BSS`, `CHAMICORE_AUTH`, etc.
- Subjects: `chamicore.<service>.>` (wildcard captures all service events)
- Retention: `WorkQueue` for consumer-driven processing, `Limits` for event log
- Storage: `File` (persistent)
- Max age: configurable (default: 7 days)
- Replicas: 1 for dev, 3 for production

Consumer configuration:
- Durable consumers with explicit ack
- Ack wait: 30 seconds (configurable)
- Max deliver: 5 (dead-letter after exhausting retries)

**Done when:**
- [ ] `nats.NewPublisher(cfg)` returns a `Publisher` backed by JetStream
- [ ] `nats.NewSubscriber(cfg)` returns a `Subscriber` backed by JetStream durable consumers
- [ ] `nats.EnsureStream(conn, streamCfg)` creates/updates streams idempotently
- [ ] Automatic reconnection with backoff on NATS disconnect
- [ ] Integration tests pass with NATS testcontainer
- [ ] 100% test coverage on non-integration code
- [ ] `golangci-lint run` passes

### P7.3: transactional outbox pattern [ ]

**Depends on:** P7.2, P0.5
**Repo:** chamicore-lib

**Files:**
- `events/outbox/writer.go` — Writes events to an `outbox` table within a DB transaction
- `events/outbox/relay.go` — Relay daemon: reads unsent events from outbox, publishes to NATS, marks sent
- `events/outbox/migration.go` — Helper to generate outbox table migration SQL for any schema
- Integration tests (testcontainers for both PostgreSQL and NATS)

**Description:**
Implement the transactional outbox pattern so services can atomically write domain
changes and publish events in the same database transaction. This prevents the
"dual write" problem where a DB write succeeds but the event publish fails (or vice versa).

Outbox table (per-service schema):
```sql
CREATE TABLE outbox (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type  TEXT NOT NULL,
    subject     TEXT NOT NULL,
    data        JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    sent_at     TIMESTAMPTZ
);

CREATE INDEX idx_outbox_unsent ON outbox (created_at) WHERE sent_at IS NULL;
```

The relay daemon:
1. Polls for unsent events (`WHERE sent_at IS NULL ORDER BY created_at LIMIT 100`)
2. Publishes each event to NATS via the `Publisher` interface
3. Marks events as sent (`UPDATE outbox SET sent_at = NOW() WHERE id = $1`)
4. Optionally uses PostgreSQL `LISTEN/NOTIFY` for low-latency notification instead of polling

**Done when:**
- [ ] `outbox.Write(tx, event)` inserts an event into the outbox table within an existing transaction
- [ ] `outbox.NewRelay(db, publisher, cfg)` starts a relay that publishes unsent events
- [ ] Relay processes events in order, with configurable batch size and poll interval
- [ ] Relay handles publisher failures gracefully (retry with backoff, no data loss)
- [ ] `outbox.MigrationSQL(schema)` generates the outbox DDL for any schema name
- [ ] Integration tests verify the full flow: write → relay → NATS → subscriber receives
- [ ] 100% test coverage on non-integration code
- [ ] `golangci-lint run` passes

### P7.4: add event publishing to SMD [ ]

**Depends on:** P7.3, P2.1
**Repo:** chamicore-smd

**Files:**
- `migrations/postgres/000002_add_outbox.up.sql`
- `migrations/postgres/000002_add_outbox.down.sql`
- Updated store methods to write outbox events within transactions
- Updated `cmd/smd/main.go` to start outbox relay

**Description:**
Add event publishing to SMD's component CRUD operations. When a component is created,
updated, or deleted, an event is written to the outbox table in the same transaction
as the domain change. The outbox relay publishes these events to NATS.

Events published:
- `chamicore.smd.components.created` — after successful component creation
- `chamicore.smd.components.updated` — after successful component update
- `chamicore.smd.components.deleted` — after successful component deletion

**Done when:**
- [ ] Outbox migration applied to `smd` schema
- [ ] `CreateComponent`, `UpdateComponent`, `DeleteComponent` write outbox events in the same transaction
- [ ] Outbox relay starts alongside the HTTP server in `main.go`
- [ ] Events are published to NATS `CHAMICORE_SMD` stream
- [ ] Existing HTTP sync endpoints (ETags) continue to work unchanged
- [ ] Integration tests verify events are published on component changes
- [ ] 100% test coverage maintained
- [ ] `golangci-lint run` passes

### P7.5: event-driven sync for BSS and Cloud-Init [ ]

**Depends on:** P7.4, P3.3, P3.5
**Repo:** chamicore-bss, chamicore-cloud-init

**Files (per service):**
- `internal/sync/events.go` — NATS subscriber that listens for SMD component events
- Updated `internal/sync/sync.go` — hybrid sync: events for real-time, polling as fallback
- Updated `cmd/<service>/main.go` — start event subscriber alongside polling loop

**Description:**
Replace the polling-only sync loops in BSS and Cloud-Init with a hybrid approach:
events from NATS for real-time updates, with polling as a fallback safety net.

When a `chamicore.smd.components.created` or `chamicore.smd.components.updated` event
arrives, the service immediately syncs the affected component instead of waiting for
the next poll cycle. The polling loop continues to run at a reduced frequency (e.g.,
every 5 minutes instead of every 30 seconds) as a consistency backstop.

**Done when:**
- [ ] BSS subscribes to `chamicore.smd.components.>` events
- [ ] Cloud-Init subscribes to `chamicore.smd.components.>` events
- [ ] Event-triggered sync updates the local cache within seconds of an SMD change
- [ ] Polling loop continues as fallback at reduced frequency
- [ ] Services start and work correctly even if NATS is unavailable (graceful degradation)
- [ ] Integration tests verify event-driven sync latency is < 5 seconds
- [ ] 100% test coverage maintained
- [ ] `golangci-lint run` passes

### P7.6: NATS in deployment stack [ ]

**Depends on:** P7.2, P5.1
**Repo:** chamicore-deploy

**Files:**
- Updated `docker-compose.yml` — add NATS server container
- Updated Helm values — add NATS chart dependency
- `CHAMICORE_NATS_URL` environment variable added to all service configs

**Description:**
Add NATS JetStream to the deployment stack (both Docker Compose for development and
Helm for production). All services receive the NATS URL via environment variable but
operate correctly without it (events are optional; sync falls back to polling).

**Done when:**
- [ ] `docker-compose.yml` includes NATS JetStream server
- [ ] All services configured with `CHAMICORE_NATS_URL` environment variable
- [ ] Helm values include NATS chart as optional dependency
- [ ] `make compose-up` starts NATS alongside other services
- [ ] Services start without NATS and fall back to polling-only sync
- [ ] `golangci-lint run` passes (if applicable)

---

## Progress Tracking

| Phase | Tasks | Complete | Status |
|-------|-------|----------|--------|
| Phase 0: Foundation | P0.1 — P0.9 | 9/9 | Complete |
| Phase 1: Auth | P1.1 — P1.6 | 6/6 | Complete |
| Phase 2: SMD | P2.1 — P2.5 | 0/5 | Not started |
| Phase 3: Boot Path | P3.1 — P3.7 | 0/7 | Not started |
| Phase 4: Discovery + CLI | P4.1 — P4.6 | 0/6 | Not started |
| Phase 5: UI + Deploy | P5.1 — P5.4 | 0/4 | Not started |
| Phase 6: Quality | P6.1 — P6.4 | 0/4 | Not started |
| Phase 7: Events (NATS) | P7.1 — P7.6 | 0/6 | Not started |
| **Total** | | **15/47** | |
