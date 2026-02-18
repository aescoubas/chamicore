# ADR-007: API Design Conventions

## Status

Accepted

## Date

2025-02-18

## Context

With multiple services exposing REST APIs, we need consistent conventions for URL structure,
HTTP methods, request/response formats, error handling, versioning, content negotiation,
validation, and operational endpoints. We also need type-safe clients for inter-service
communication and CLI usage, a standard middleware stack, and interactive API documentation.

Inconsistency across services creates confusion for API consumers and makes the CLI harder
to implement. Upstream OpenCHAMI generally follows RESTful conventions but has inconsistencies
across services. We want to codify a clear, comprehensive standard.

## Decision

### URL Structure

- APIs are versioned in the URL path: `/<prefix>/v<N>/`.
- Resource names are plural where appropriate.
- Hierarchical resources use nested paths: `/hsm/v2/State/Components/{id}/NICs`.
- Service-specific prefixes:

| Service | Prefix |
|---------|--------|
| SMD | `/hsm/v2/` |
| BSS | `/boot/v1/` |
| Cloud-Init | `/cloud-init/` |
| Auth | `/auth/v1/` |

### HTTP Methods and Status Codes

| Method | Usage | Success | Not Found | Validation | Conflict |
|--------|-------|---------|-----------|------------|----------|
| GET (one) | Retrieve resource | `200` | `404` | `400` | - |
| GET (list) | Retrieve collection | `200` | `200` (empty) | `400` | - |
| POST | Create resource | `201` + Location | - | `400`/`422` | `409` |
| PUT | Full replace | `200` | `404` | `400`/`422` | - |
| PATCH | Partial update | `200` | `404` | `400`/`422` | - |
| DELETE | Remove resource | `204` | `404` | - | - |

### Content Negotiation

- Default and primary content type: `application/json`.
- Request bodies must set `Content-Type: application/json`; reject with `415` otherwise.
- Handlers inspect `Accept` header; respond with `406 Not Acceptable` for unsupported types.
- All responses include a `Content-Type` header.

### Request/Response Validation

All incoming requests are validated before reaching business logic:

1. **Structural** (400): JSON well-formedness, required fields, correct types, no unknown fields.
2. **Semantic** (422): Business rules (valid component ID format, known enum values, value ranges).

Validation errors return field-level detail:

```json
{
  "type": "about:blank",
  "title": "Validation Error",
  "status": 422,
  "detail": "Request body contains invalid fields",
  "instance": "/hsm/v2/State/Components",
  "errors": [
    {"field": "ID", "message": "invalid component ID format"},
    {"field": "Role", "message": "unknown role 'SuperCompute'"}
  ]
}
```

### Error Format (RFC 9457)

All error responses use [Problem Details](https://www.rfc-editor.org/rfc/rfc9457):

```json
{
  "type": "about:blank",
  "title": "Not Found",
  "status": 404,
  "detail": "Component node-a1b2c3 not found in inventory",
  "instance": "/hsm/v2/State/Components/node-a1b2c3"
}
```

### Resource Envelope Pattern

All responses use a Kubernetes-inspired envelope for consistent structure:

```json
{
  "kind": "Component",
  "apiVersion": "hsm/v2",
  "metadata": { "id": "node-a1b2c3", "etag": "a1b2c3d4", "createdAt": "...", "updatedAt": "..." },
  "spec": { "type": "Node", "state": "Ready", "role": "Compute", "nid": 1001 }
}
```

List responses:

```json
{
  "kind": "ComponentList",
  "apiVersion": "hsm/v2",
  "metadata": { "total": 1500, "limit": 100, "offset": 0 },
  "items": [...]
}
```

Envelope fields: `kind` (resource type), `apiVersion` (matching URL prefix),
`metadata` (id, etag, timestamps, pagination), `spec` (resource payload), `items` (list only).
Implemented as generic Go types in `chamicore-lib/httputil`.

### Conditional Requests (ETags)

Services support ETags for cache validation and optimistic concurrency control:

- **Cache validation**: Server returns `ETag` header on GET. Client sends
  `If-None-Match` on subsequent GETs; server returns `304 Not Modified` if unchanged.
- **Optimistic concurrency**: Client sends `If-Match` on PUT/PATCH. Server returns
  `412 Precondition Failed` if the resource was modified since the client's last read,
  preventing lost updates from concurrent edits.
- ETags are derived from `updatedAt` or a content hash, carried in `metadata.etag`.
- ETag middleware in `chamicore-lib/httputil` handles conditional header processing.
- Required on single-resource GETs and PUT/PATCH. Optional on list endpoints.

### OpenAPI and Swagger UI

- Every service has `api/openapi.yaml` as the source of truth for its API.
- Each service serves interactive documentation:
  - `GET /api/openapi.yaml` - Raw spec.
  - `GET /api/docs` - Swagger UI (embedded static assets, no CDN).
- Specs include schemas, examples, and error responses.
- CI validates that the spec matches actual handler behavior.

### Type-Safe HTTP Clients

Each service provides a typed Go client in `pkg/client/`:

- Methods accept and return typed structs (not raw JSON).
- Automatic retries on `429`, `502`, `503`, `504` with exponential backoff and jitter.
- Non-2xx responses parsed into structured `*APIError` matching RFC 9457.
- Context propagation for cancellation and deadlines.
- Request ID propagation (`X-Request-ID`) for distributed tracing.
- Bearer token injection with optional refresh callback.
- Base client in `chamicore-lib/httputil/client/`.

### Middleware Stack

Standard ordered middleware via `chamicore-lib`:

1. **HTTPTracing** - OTel distributed tracing spans.
2. **HTTPMetrics** - OTel request metrics (duration, count, size).
3. **RequestID** - Generate/propagate `X-Request-ID`.
4. **RequestLogger** - Zerolog structured request logging (includes trace ID).
5. **Recoverer** - Panic recovery -> `500`.
6. **SecureHeaders** - `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`.
7. **ContentType** - Enforce `application/json` content negotiation.
8. **APIVersion** - Set `API-Version` response header.
9. **CacheControl** - `Cache-Control` headers. GET: configurable TTL. Mutations: `no-store`.
10. **ETag** - Process `If-None-Match` / `If-Match` for conditional requests.
11. **JWTMiddleware** - Validate Bearer JWT via JWKS, inject claims.
12. **RequireScope** - Per-route scope enforcement from JWT claims.

### Pagination

Offset-based via query parameters (`?limit=100&offset=0`).
Metadata carried in the resource envelope `metadata` field.
Default limit: 100. Maximum: 10000.

### Filtering

- Query parameters: `?type=Node&state=Ready`.
- Multiple values: `?type=Node&type=NodeBMC`.
- Simple equality filters in v1.

### Health and Operational Endpoints

Every service exposes these outside the versioned prefix, without authentication:

| Endpoint | Purpose |
|----------|---------|
| `GET /health` | Liveness probe (`200` always) |
| `GET /readiness` | Readiness probe (`200` or `503`) |
| `GET /version` | Build version, commit, build time |
| `GET /api/docs` | Swagger UI |
| `GET /api/openapi.yaml` | OpenAPI 3.0 spec |

## Consequences

### Positive

- Consistent API experience across all services with structured envelope pattern.
- RFC 9457 errors with field-level validation detail improve developer experience.
- Swagger UI enables interactive API exploration without external tools.
- Type-safe clients with retries make inter-service communication robust.
- Standard middleware stack ensures consistent security, logging, caching, and observability.
- Content negotiation and validation catch errors early with clear messages.
- ETags prevent lost updates from concurrent edits and reduce bandwidth on unchanged resources.
- Simple per-service path versioning avoids API gateway complexity.
- Clear conventions simplify CLI implementation (chamicore-cli).

### Negative

- Envelope pattern adds overhead compared to flat JSON responses.
  - Accepted: Consistency and predictability outweigh the small size increase.
- Strict validation may reject requests that upstream OpenCHAMI would accept.
  - Accepted: Strictness prevents bugs; document deviations for migration.
- Swagger UI adds an embedded dependency to each service binary.
  - Mitigated: Embedded at build time, no runtime CDN dependency.

### Neutral

- OpenAPI specs require maintenance alongside handler code.
- Middleware ordering is critical and must be documented clearly.
- Client retry policy may need tuning per deployment environment.
- CloudEvents / event-driven patterns are explicitly deferred; can be layered on later
  if scale or decoupling requirements justify the added infrastructure.
- Hub/spoke API gateway is unnecessary at current scale but can be added without
  changing service code (services already set `API-Version` headers).
