# ADR-006: Authentication (OIDC/JWT)

## Status

Superseded by [ADR-011](ADR-011-consolidated-auth-service.md)

## Date

2025-02-18

## Context

Chamicore services need authentication and authorization for API access. The system must
support both human users (via web UI or CLI) and service-to-service communication.

Upstream OpenCHAMI uses OPAAL for OIDC-based authentication with JWT tokens. The
JWKS (JSON Web Key Set) endpoint allows services to independently verify tokens
without calling back to the auth service on every request.

## Decision

We will implement OIDC/JWT-based authentication via the **chamicore-opaal** service:

### Authentication Flow

```
1. Client authenticates with OPAAL (via OIDC provider or direct login)
2. OPAAL issues a signed JWT
3. Client includes JWT as Bearer token in API requests
4. Service middleware validates JWT using OPAAL's JWKS endpoint
5. Claims are extracted and made available to handlers via context
```

### JWKS Endpoint

- OPAAL serves `/.well-known/jwks.json` with its public keys.
- Services fetch JWKS at startup and cache it with periodic refresh.
- JWT validation uses `lestrrat-go/jwx/v2` for JWKS fetching and token verification.

### JWT Claims

Standard claims plus custom fields:

```json
{
  "iss": "chamicore-opaal",
  "sub": "user-id-or-service-account",
  "aud": ["chamicore"],
  "exp": 1700000000,
  "iat": 1699990000,
  "scope": "read:components write:components admin"
}
```

### Middleware Architecture

The JWT validation middleware lives in `chamicore-lib/auth/`:

```go
// In service setup:
r := chi.NewRouter()
r.Use(auth.JWTMiddleware(auth.Config{
    JWKSURL:    cfg.JWKSURL,
    Issuer:     "chamicore-opaal",
    Audience:   "chamicore",
}))
```

### Development Mode

When `CHAMICORE_<SERVICE>_DEV_MODE=true`:
- JWT validation is bypassed entirely.
- A synthetic claims object with full admin scope is injected into context.
- A prominent warning is logged on startup:
  ```
  WARNING: Dev mode enabled - authentication is disabled. DO NOT use in production.
  ```

## Consequences

### Positive

- Stateless token validation: services don't need to call OPAAL on every request.
- Standard OIDC flow allows integration with external identity providers.
- JWKS rotation is handled transparently via periodic refresh.
- Dev mode enables rapid local development without running OPAAL.
- Consistent auth middleware across all services via chamicore-lib.

### Negative

- JWT tokens cannot be revoked before expiration (standard JWT limitation).
  - Mitigated: Use short-lived tokens with refresh flow.
- JWKS endpoint must be available when services start (or use cached keys).
  - Mitigated: Services retry JWKS fetch with backoff; dev mode as fallback.
- Dev mode is a security risk if accidentally enabled in production.
  - Mitigated: Prominent logging, health endpoint includes dev mode status.

### Neutral

- Token format and claims may need to evolve as authorization requirements mature.
- Role-based access control (RBAC) can be layered on top of JWT claims later.
