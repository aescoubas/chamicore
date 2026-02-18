# ADR-011: Consolidated Authentication and Authorization Service

## Status

Accepted (supersedes [ADR-006](ADR-006-authentication-oidc-jwt.md))

## Date

2025-02-18

## Context

Upstream OpenCHAMI uses three separate components for authentication and authorization:

1. **Ory Hydra** - A full-featured, third-party OIDC provider (~80MB binary). Runs login
   flows and issues identity tokens. Heavy for deployments that already have an IdP.
2. **OPAAL** - A thin Go shim that bridges external IdPs to OpenCHAMI. Proxies tokens from
   Hydra to clients. Minimal logic; essentially a pass-through.
3. **Tokensmith** (emerging) - A token exchange and authorization engine. Takes broad
   identity tokens, exchanges them for constrained service-specific tokens. Uses Casbin
   for RBAC/ABAC policy enforcement.

This three-service stack has several problems:

- **Operational complexity**: Three services to deploy, configure, monitor, and debug for
  what is fundamentally one concern (auth).
- **Ory Hydra is overkill**: Most HPC sites already run an Identity Provider (Keycloak,
  Azure AD, Okta, GitHub). Running a second OIDC provider adds redundancy and confusion.
- **OPAAL is nearly empty**: It adds minimal value as a standalone service. Its logic is
  a few hundred lines of token proxying.
- **Split AuthN/AuthZ across services**: The tight coupling between "who are you?" and
  "what can you do?" means these services must always be deployed together and configured
  in lockstep.

Chamicore's goals of operational simplicity and reduced service count make consolidation
the clear choice.

## Decision

Replace OPAAL, Ory Hydra, and Tokensmith with a single **`chamicore-auth`** service that
handles the complete authentication and authorization lifecycle.

### Architecture

```
External IdP (Keycloak, Okta, Azure AD, GitHub, LDAP...)
       │
       │  OIDC / OAuth2
       ▼
┌─────────────────────────────────────┐
│           chamicore-auth            │
│                                     │
│  ┌───────────────┐ ┌─────────────┐ │
│  │ Authentication │ │Authorization│ │
│  │               │ │             │ │
│  │ OIDC          │ │ Casbin      │ │
│  │ Federation    │ │ RBAC/ABAC   │ │
│  │               │ │             │ │
│  │ Token         │ │ Scope       │ │
│  │ Exchange      │ │ Enforcement │ │
│  │               │ │             │ │
│  │ Service       │ │ Policy      │ │
│  │ Accounts      │ │ Management  │ │
│  └───────────────┘ └─────────────┘ │
│                                     │
│  JWKS /.well-known/jwks.json        │
│  Token Refresh & Revocation         │
│  Audit Logging                      │
└─────────────────────────────────────┘
       │
       │  Short-lived, scoped Chamicore JWTs
       ▼
  SMD, BSS, Cloud-Init, Kea-Sync, CLI
```

### Authentication (AuthN)

**"Who are you?"** - Chamicore-auth does NOT run its own login UI or user database.
It federates with external Identity Providers via standard OIDC.

- **OIDC Federation**: Discovers external IdP configuration via `/.well-known/openid-configuration`.
  Validates incoming identity tokens using the IdP's JWKS.
- **Token Exchange** ([RFC 8693](https://www.rfc-editor.org/rfc/rfc8693)): Accepts a valid
  external identity token and exchanges it for a short-lived, scoped Chamicore JWT.
  The Chamicore token carries only the claims needed for authorization decisions.
- **Service Accounts**: For service-to-service communication and automation. Created via
  API, issued long-lived credentials (rotatable), scoped to specific operations.
- **Multiple IdP Support**: Can federate with multiple IdPs simultaneously (e.g., Keycloak
  for users, GitHub for CI bots).

### Authorization (AuthZ)

**"What are you allowed to do?"** - Enforced at two levels:

1. **At chamicore-auth** (token issuance): Policies determine what scopes/claims are
   embedded in the Chamicore JWT. A user authenticated via the IdP only gets the scopes
   their role permits.

2. **At each service** (token validation): Services use `chamicore-lib/auth` middleware
   to validate the JWT and enforce required scopes per route.

#### Policy Engine: Casbin

[Casbin](https://casbin.org/) provides the policy engine for authorization decisions:

- **RBAC** (Role-Based Access Control): Define roles (`admin`, `operator`, `viewer`) with
  permissions mapped to API operations.
  ```
  p, admin, /hsm/v2/State/Components/*, (GET)|(POST)|(PUT)|(PATCH)|(DELETE)
  p, operator, /hsm/v2/State/Components/*, (GET)|(PATCH)
  p, viewer, /hsm/v2/State/Components/*, GET
  ```

- **ABAC** (Attribute-Based Access Control): Fine-grained rules based on resource
  attributes (e.g., rack, type, role).
  ```
  p, site-admin-east, /hsm/v2/State/Components/*, *, rack == "R01" || rack == "R02"
  ```

- **Policy storage**: Policies stored in PostgreSQL (same database, `auth` schema).
  Hot-reloadable without service restart.

- **Policy API**: CRUD endpoints for managing policies at runtime.
  ```
  GET    /auth/v1/policies
  POST   /auth/v1/policies
  DELETE /auth/v1/policies/{id}
  GET    /auth/v1/roles
  POST   /auth/v1/roles/{role}/members
  ```

### Token Format

Chamicore JWTs issued by chamicore-auth:

```json
{
  "iss": "chamicore-auth",
  "sub": "user-uuid-or-service-account-id",
  "aud": ["chamicore"],
  "exp": 1700001800,
  "iat": 1700000000,
  "jti": "unique-token-id",
  "scope": "read:components write:bootparams",
  "roles": ["operator"],
  "name": "Jane Doe",
  "email": "jane@example.com",
  "sa": false
}
```

| Claim | Description |
|-------|-------------|
| `iss` | Always `chamicore-auth` |
| `sub` | User ID from IdP or service account ID |
| `aud` | `["chamicore"]` |
| `exp` | Short-lived: 15-60 minutes (configurable) |
| `jti` | Unique token identifier (for revocation) |
| `scope` | Space-separated list of granted scopes |
| `roles` | Casbin roles assigned to this subject |
| `sa` | `true` if this is a service account token |

### Endpoints

| Endpoint | Purpose | Auth |
|----------|---------|------|
| `POST /auth/v1/token` | Token exchange (external IdP token -> Chamicore JWT) | External token |
| `POST /auth/v1/token/refresh` | Refresh an expired Chamicore JWT | Refresh token |
| `POST /auth/v1/token/revoke` | Revoke a token by JTI | Admin |
| `GET /.well-known/jwks.json` | JWKS endpoint for token verification | None |
| `GET /.well-known/openid-configuration` | OIDC discovery metadata | None |
| `GET /auth/v1/policies` | List authorization policies | Admin |
| `POST /auth/v1/policies` | Create/update policy | Admin |
| `DELETE /auth/v1/policies/{id}` | Delete policy | Admin |
| `GET /auth/v1/roles` | List roles and memberships | Admin |
| `POST /auth/v1/roles/{role}/members` | Add member to role | Admin |
| `DELETE /auth/v1/roles/{role}/members/{sub}` | Remove member from role | Admin |
| `GET /auth/v1/service-accounts` | List service accounts | Admin |
| `POST /auth/v1/service-accounts` | Create service account | Admin |
| `DELETE /auth/v1/service-accounts/{id}` | Delete service account | Admin |
| `POST /auth/v1/credentials` | Store a device credential set (e.g., BMC creds) | Admin |
| `GET /auth/v1/credentials` | List credential sets (metadata only, no secrets) | Admin |
| `GET /auth/v1/credentials/{id}` | Retrieve credential (secrets included) | `read:credentials` scope |
| `PUT /auth/v1/credentials/{id}` | Update credential | Admin |
| `DELETE /auth/v1/credentials/{id}` | Delete credential | Admin |
| `GET /health` | Liveness probe | None |
| `GET /readiness` | Readiness probe | None |
| `GET /version` | Build info | None |
| `GET /metrics` | Prometheus metrics | None |

### Service-to-Service Authentication

Internal services authenticate to each other using service account tokens:

1. Each service is provisioned with a service account at deployment time.
2. Service accounts have tightly scoped permissions (e.g., BSS can only read SMD components).
3. Tokens are issued via `POST /auth/v1/token` with service account credentials.
4. The `chamicore-lib/auth` client handles token refresh automatically.

### Development Mode

When `CHAMICORE_AUTH_DEV_MODE=true`:
- Token validation is bypassed across all services (via `chamicore-lib/auth` middleware).
- A synthetic admin JWT is injected into all requests.
- No external IdP is required.
- A prominent warning is logged: `WARNING: Dev mode enabled - auth disabled. DO NOT use in production.`

### Audit Logging

All authentication and authorization events are logged:

```json
{
  "event": "token_issued",
  "sub": "jane@example.com",
  "roles": ["operator"],
  "scopes": "read:components write:bootparams",
  "idp": "keycloak",
  "ip": "10.0.1.42",
  "timestamp": "2025-03-01T12:00:00Z"
}
```

```json
{
  "event": "authz_denied",
  "sub": "jane@example.com",
  "action": "DELETE",
  "resource": "/hsm/v2/State/Components/node-a1b2c3",
  "reason": "role 'operator' lacks 'delete:components' scope",
  "timestamp": "2025-03-01T12:01:00Z"
}
```

### Database Schema (auth schema)

```sql
CREATE SCHEMA IF NOT EXISTS auth;

-- Service accounts
CREATE TABLE auth.service_accounts (
    id          VARCHAR(255) PRIMARY KEY,
    name        VARCHAR(255) NOT NULL UNIQUE,
    secret_hash TEXT         NOT NULL,
    scopes      TEXT         NOT NULL DEFAULT '',
    enabled     BOOLEAN      NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- Token revocation list (for JTI-based revocation)
CREATE TABLE auth.revoked_tokens (
    jti         VARCHAR(255) PRIMARY KEY,
    expires_at  TIMESTAMPTZ  NOT NULL,
    revoked_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- Device credentials (BMC, IPMI, SNMP, etc.) used by chamicore-discovery
CREATE TABLE auth.device_credentials (
    id          VARCHAR(255) PRIMARY KEY,
    name        VARCHAR(255) NOT NULL UNIQUE,
    type        VARCHAR(63)  NOT NULL DEFAULT 'device',
    username    TEXT         NOT NULL DEFAULT '',
    secret_enc  TEXT         NOT NULL,     -- encrypted at rest
    tags        JSONB        NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- Casbin policies are stored via Casbin's PostgreSQL adapter
```

## Consequences

### Positive

- **One service instead of three**: OPAAL + Hydra + Tokensmith consolidated into
  chamicore-auth. Simpler deployment, monitoring, and debugging.
- **No bundled OIDC provider**: Delegates authentication to the site's existing IdP.
  No need to run Ory Hydra alongside Keycloak/Okta/Azure AD.
- **Token exchange pattern**: External tokens are exchanged for short-lived, minimally
  scoped Chamicore tokens. Services never see raw IdP tokens.
- **Granular authorization**: Casbin enables RBAC and ABAC policies beyond simple
  scope strings. Policies are manageable via API and hot-reloadable.
- **Audit trail**: All auth events are logged with structured data for compliance
  and incident investigation.
- **Service-to-service auth**: First-class support for service accounts with
  auto-refreshing tokens, not an afterthought.
- **Clean separation within one codebase**: AuthN and AuthZ are separate Go packages
  (`internal/authn/`, `internal/authz/`) but deployed as one service.

### Negative

- **Single point of failure**: If chamicore-auth is down, no new tokens can be issued.
  - Mitigated: Existing valid JWTs continue to work (stateless validation via JWKS).
    Services cache JWKS keys. Only new token issuance is affected.
  - Mitigated: Deploy multiple replicas behind a load balancer.
- **More complex than OPAAL alone**: chamicore-auth has more responsibilities.
  - Accepted: The complexity exists regardless; it's better contained in one well-tested
    service than spread across three loosely coordinated ones.
- **Casbin learning curve**: Operators need to understand Casbin policy syntax.
  - Mitigated: Ship sensible default policies. Provide CLI commands for policy management
    (`chamicore auth policy list`, `chamicore auth role add`).

### Neutral

- External IdP setup is a deployment prerequisite (but this is true of any federated auth).
- Token revocation requires checking the revocation list (JTI lookup), adding minimal latency.
- Policy format may evolve as authorization requirements mature.
- The `chamicore-lib/auth` middleware used by other services is unchanged; it still validates
  JWTs via JWKS. The change is transparent to consuming services.
