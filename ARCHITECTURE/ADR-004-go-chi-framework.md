# ADR-004: Go + go-chi Framework

## Status

Accepted

## Date

2025-02-18

## Context

We need to choose a programming language, HTTP framework, and core library set for
all Chamicore services. Upstream OpenCHAMI is written in Go and uses a mix of frameworks
and libraries across services (go-chi, gorilla, custom routers).

Key requirements:
- Consistent stack across all services.
- Lightweight and idiomatic Go.
- Good middleware ecosystem.
- Compatible with stdlib `net/http` patterns.
- Active maintenance and community.

## Decision

All services will be written in **Go 1.24+** using the following core libraries:

| Purpose | Library | Rationale |
|---------|---------|-----------|
| HTTP framework | `go-chi/chi/v5` | Lightweight, stdlib-compatible, excellent middleware. Used by upstream SMD. |
| Logging | `rs/zerolog` | Fast structured logging. Zero-allocation. |
| JWT | `lestrrat-go/jwx/v2` | Full OIDC/JWKS support, well-maintained. |
| PostgreSQL driver | `lib/pq` | Standard, stable PostgreSQL driver for Go. |
| SQL query builder | `Masterminds/squirrel` | Composable query building. Used by upstream SMD. |
| DB migrations | `golang-migrate/migrate/v4` | Standard migration tooling with PostgreSQL support. |
| CLI framework | `spf13/cobra` | Industry standard for Go CLIs. |
| Test assertions | `stretchr/testify` | Widely used assertions and mocking. |
| Test containers | `testcontainers/testcontainers-go` | Real PostgreSQL in tests. |

### Libraries Explicitly Excluded

| Library | Replacement | Reason |
|---------|-------------|--------|
| `gorilla/handlers` | go-chi middleware | Gorilla is archived; chi has equivalent middleware. |
| `sirupsen/logrus` | `rs/zerolog` | Zerolog is faster and lower allocation. |
| `Cray-HPE/hms-*` | `chamicore-lib` | Legacy libraries with tight coupling. |
| `hashicorp/vault` | Env vars + k8s secrets | Simpler secret management for our use case. |

## Consequences

### Positive

- go-chi is stdlib-compatible: handlers are standard `http.HandlerFunc`.
- Consistent library choices prevent bikeshedding and reduce cognitive load.
- All libraries are actively maintained with strong communities.
- go-chi's middleware pattern (composable, ordered) is clean and testable.
- Zerolog's structured logging integrates well with log aggregation systems.

### Negative

- Teams familiar with other frameworks (Gin, Echo, Fiber) need to learn go-chi patterns.
  - Mitigated: go-chi closely follows stdlib patterns, minimal learning curve.
- `lib/pq` is in maintenance mode; `pgx` is the more modern alternative.
  - Accepted: `lib/pq` is stable and sufficient. Can migrate to `pgx` later if needed.

### Neutral

- Library choices should be revisited periodically as the Go ecosystem evolves.
- New libraries can be added when justified, following the ADR process.
