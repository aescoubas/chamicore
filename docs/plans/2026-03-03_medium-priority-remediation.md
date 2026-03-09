# Medium-Priority Remediation Plan

**Date:** 2026-03-03
**Source:** [Codebase Quality Report](../2026-03-03_codebase_report.md) — Medium Priority Recommendations
**Scope:** 3 work streams addressing store interface granularity, HA PostgreSQL strategy, and circuit breakers

---

## Work Stream 4: Split Large Store Interfaces

### Problem

Store interfaces are monolithic. The two largest are chamicore-auth (20 methods) and
chamicore-smd (20 methods). Every handler receives the full interface even though it only
uses a subset. This creates broad coupling surfaces and inflates mock structs in tests.

The codebase report cited 36 methods for chamicore-auth; the actual `Store` interface has
20 methods. The 36 figure likely counted methods across `Store` plus the `authn` package
interfaces (`KeyManager`, `TokenIssuer`, etc.). The core issue stands: a single 20-method
interface is too wide for Go's interface segregation principle.

**Note:** chamicore-power already demonstrates the target pattern — it extracts a separate
`transitionStore` interface alongside the main `Store`, showing the team recognizes the
value of segregation but hasn't applied it consistently.

### Current State

| Service | Interface | Methods | Mock Size (LoC) |
|---------|-----------|---------|-----------------|
| chamicore-auth | `Store` | 20 | ~106 |
| chamicore-smd | `Store` | 20 | ~80 |
| chamicore-bss | `Store` | 8 | ~75 |
| chamicore-cloud-init | `Store` | 7 | ~46 |
| chamicore-discovery | `Store` | 8 | ~62 |
| chamicore-power | `Store` + `transitionStore` | 7 + 4 | Already split |

Only chamicore-auth and chamicore-smd warrant splitting. BSS, Cloud-Init, and Discovery
are small enough that splitting adds overhead without meaningful benefit.

### Design Principles

1. **Split the interface, not the implementation.** `PostgresStore` continues to implement
   all methods on a single struct with a shared `*sql.DB` pool. The interfaces are consumed
   by handlers, not by the store itself.
2. **Domain-aligned grouping.** Each interface corresponds to a resource domain (tokens,
   credentials, components, etc.).
3. **Composite interface preserved.** Keep a `Store` type alias that embeds all sub-interfaces
   for callers that need the full surface (e.g., `main.go` wiring, integration tests).
4. **Mocks shrink proportionally.** A handler test for credential endpoints only needs a
   5-method mock, not a 20-method mock.

### Tasks

#### 4.1 — chamicore-auth: Split Store into domain interfaces

**Proposed interfaces:**

```go
// store.go

// HealthChecker is implemented by stores that support liveness checks.
type HealthChecker interface {
    Ping(ctx context.Context) error
}

// SigningKeyStore manages JWT signing key lifecycle.
type SigningKeyStore interface {
    GetActiveSigningKey(ctx context.Context) (*model.SigningKey, error)
    CreateSigningKey(ctx context.Context, key *model.SigningKey) error
    ListSigningKeys(ctx context.Context) ([]model.SigningKey, error)
}

// RevocationStore manages token revocation records.
type RevocationStore interface {
    CreateRevocation(ctx context.Context, rev *model.Revocation) error
    IsRevoked(ctx context.Context, jti string) (bool, error)
    ListActiveRevocations(ctx context.Context) ([]model.Revocation, error)
    CleanExpiredRevocations(ctx context.Context) (int64, error)
}

// ServiceAccountStore manages service account CRUD.
type ServiceAccountStore interface {
    ListServiceAccounts(ctx context.Context) ([]model.ServiceAccount, error)
    GetServiceAccount(ctx context.Context, id string) (*model.ServiceAccount, error)
    GetServiceAccountByName(ctx context.Context, name string) (*model.ServiceAccount, error)
    CreateServiceAccount(ctx context.Context, sa *model.ServiceAccount) error
    DeleteServiceAccount(ctx context.Context, id string) error
}

// CredentialStore manages device credential CRUD.
type CredentialStore interface {
    ListCredentials(ctx context.Context) ([]model.Credential, error)
    GetCredential(ctx context.Context, id string) (*model.Credential, error)
    CreateCredential(ctx context.Context, cred *model.Credential) error
    UpdateCredential(ctx context.Context, cred *model.Credential) error
    DeleteCredential(ctx context.Context, id string) error
}

// Store is the composite interface for full store access (wiring, integration tests).
type Store interface {
    HealthChecker
    SigningKeyStore
    RevocationStore
    ServiceAccountStore
    CredentialStore
}
```

**Server injection changes:**

```go
// Before:
type Server struct {
    store store.Store
}

// After: handlers receive only the interfaces they need
type Server struct {
    signingKeys  store.SigningKeyStore
    revocations  store.RevocationStore
    serviceAccts store.ServiceAccountStore
    credentials  store.CredentialStore
    health       store.HealthChecker
    // ... other deps unchanged (enforcer, keyManager, etc.)
}
```

**Files touched:**
- `services/chamicore-auth/internal/store/store.go` (split interface definition)
- `services/chamicore-auth/internal/server/server.go` (update Server struct + constructor)
- `services/chamicore-auth/internal/server/handlers_*.go` (update `s.store.` → `s.credentials.`, etc.)
- `services/chamicore-auth/internal/server/mock_test.go` (split into domain-specific mocks)
- `services/chamicore-auth/internal/server/*_test.go` (update mock wiring in tests)

**Migration strategy:**
1. Define sub-interfaces in `store.go`, keep composite `Store` embedding them all
2. Change `PostgresStore` to satisfy each sub-interface (no code changes needed — it already does)
3. Update `Server` struct to accept sub-interfaces
4. Update handler methods to use the narrow field
5. Split mock into per-domain mocks in tests
6. Verify: `go build ./...` and `go test ./...` pass

#### 4.2 — chamicore-smd: Split Store into domain interfaces

**Proposed interfaces:**

```go
// store.go

type HealthChecker interface {
    Ping(ctx context.Context) error
}

// ComponentStore manages component lifecycle and queries.
type ComponentStore interface {
    ListComponents(ctx context.Context, opts ListComponentsOpts) ([]model.Component, int, error)
    GetComponent(ctx context.Context, id string) (*model.Component, error)
    CreateComponent(ctx context.Context, c *model.Component) error
    CreateOrUpdateComponentsBulk(ctx context.Context, components []model.Component) ([]BulkItemResult, error)
    CreateComponentWithInterfaces(ctx context.Context, c *model.Component, ifaces []model.EthernetInterface) error
    UpdateComponent(ctx context.Context, c *model.Component) error
    PatchComponent(ctx context.Context, id string, patch model.ComponentPatch) (*model.Component, error)
    DeleteComponent(ctx context.Context, id string) error
    PurgeComponent(ctx context.Context, id string) error
    RestoreComponent(ctx context.Context, id string) error
    MaxComponentUpdatedAt(ctx context.Context) (time.Time, error)
}

// InterfaceStore manages ethernet interface CRUD.
type InterfaceStore interface {
    ListEthernetInterfaces(ctx context.Context, opts ListInterfacesOpts) ([]model.EthernetInterface, int, error)
    GetEthernetInterface(ctx context.Context, id string) (*model.EthernetInterface, error)
    CreateEthernetInterface(ctx context.Context, iface *model.EthernetInterface) error
    PatchEthernetInterface(ctx context.Context, id string, patch model.InterfacePatch) (*model.EthernetInterface, error)
    DeleteEthernetInterface(ctx context.Context, id string) error
}

// GroupStore manages groups, partitions, and membership.
type GroupStore interface {
    ListGroups(ctx context.Context, opts ListGroupsOpts) ([]model.Group, int, error)
    GetGroup(ctx context.Context, name string) (*model.Group, error)
    CreateGroup(ctx context.Context, g *model.Group) error
    UpdateGroup(ctx context.Context, g *model.Group) error
    DeleteGroup(ctx context.Context, name string) error
    AddGroupMembers(ctx context.Context, name string, members []string) error
    RemoveGroupMember(ctx context.Context, name string, member string) error
}

// Store is the composite for full access.
type Store interface {
    HealthChecker
    ComponentStore
    InterfaceStore
    GroupStore
}
```

**Files touched:**
- `services/chamicore-smd/internal/store/store.go` (split interface definition)
- `services/chamicore-smd/internal/server/server.go` (update Server struct)
- `services/chamicore-smd/internal/server/handlers_component.go` (use `s.components.`)
- `services/chamicore-smd/internal/server/handlers_interface.go` (use `s.interfaces.`)
- `services/chamicore-smd/internal/server/handlers_group.go` (use `s.groups.`)
- `services/chamicore-smd/internal/server/mock_test.go` (split mocks)
- `services/chamicore-smd/internal/server/*_test.go` (update mock wiring)

#### 4.3 — Update AGENTS.md with interface segregation guidance

**What:** Add a section to AGENTS.md documenting the interface segregation pattern so
future services follow it from the start.

**Content:**
- When to split: > 10 methods or > 2 distinct resource domains
- How to split: domain-aligned sub-interfaces + composite `Store` type
- Mock strategy: one mock struct per sub-interface
- Server injection: narrow interface fields, not the composite

**Files touched:**
- `AGENTS.md` (add section under "Coding Conventions")

### Acceptance Criteria

- `PostgresStore` satisfies the composite `Store` interface (compile-time check)
- No handler file imports the composite `Store` — only the sub-interface it needs
- Mock structs in tests have ≤ 8 methods each
- All existing tests pass with no behavioral changes
- `go vet ./...` and `golangci-lint run` pass

---

## Work Stream 5: Document HA PostgreSQL Strategy

### Problem

All services depend on a single PostgreSQL instance. ADR-003 acknowledges this as a risk
and mentions "streaming replication, Patroni" as mitigations, but no ADR documents the
actual HA strategy. The Helm chart deploys PostgreSQL as a single-replica `Deployment`
(not even a `StatefulSet`) with a `ReadWriteOnce` PVC.

**Current deployment reality:**
- Docker Compose: single `postgres:16-alpine` container
- Helm: single-replica Deployment, 20Gi PVC, no replication
- No PgBouncer, no Patroni, no managed PostgreSQL documentation
- No backup strategy documented
- RPO/RTO undefined

### Approach

Write **ADR-018: PostgreSQL High Availability** covering three deployment tiers. This is a
documentation task — no code changes in this work stream, but the Helm chart gets updated
to support the recommended production topology.

### Tasks

#### 5.1 — Write ADR-018: PostgreSQL High Availability

**Structure:**

```
Status: Proposed
Context: Single PostgreSQL instance is a SPOF. Production HPC deployments
         need defined RPO/RTO and automated failover.

Decision: Define three deployment tiers with increasing HA guarantees.

Tiers:
  Tier 0 — Development (current):
    - Single PostgreSQL instance (Docker Compose or single-replica Helm)
    - No replication, no automated failover
    - RPO: last manual backup. RTO: manual restore time.
    - Acceptable for: dev, CI, demos

  Tier 1 — Standard Production:
    - Primary + 1 synchronous standby (streaming replication)
    - Patroni for automated failover + leader election
    - PgBouncer for connection pooling and transparent failover
    - WAL archiving to object storage (S3/MinIO) for PITR
    - RPO: 0 (synchronous replication). RTO: < 30 seconds (Patroni failover).
    - Acceptable for: production clusters with moderate SLAs

  Tier 2 — Managed PostgreSQL:
    - Cloud-provider managed service (AWS RDS, Azure Database, Google Cloud SQL)
    - Multi-AZ deployment with automated failover
    - Automated backups, point-in-time recovery
    - Connection via service DSN; no Patroni/PgBouncer needed
    - RPO/RTO: per provider SLA (typically RPO=0, RTO < 1 minute)
    - Acceptable for: cloud-hosted production

Consequences:
  - Tier 1 adds operational complexity (Patroni, etcd/Consul, PgBouncer)
  - Tier 2 adds cloud vendor dependency
  - All services connect via DSN env var — no code changes needed for any tier
  - Per-service schemas remain unchanged
  - Connection pooling defaults (25 max open, 5 idle) may need tuning for Tier 1/2
```

**Application-level considerations to document:**
- Services use `CHAMICORE_<SERVICE>_DB_DSN` — point this at PgBouncer (Tier 1) or managed
  endpoint (Tier 2) instead of direct PostgreSQL
- `dbutil.Connect()` pool settings may need adjustment: managed PostgreSQL typically has
  lower connection limits, offset by PgBouncer's connection multiplexing
- Boot-path services (BSS, Cloud-Init) tolerate PostgreSQL downtime during operation
  because they serve from local cache; only startup sync requires DB
- Auth service is most sensitive to PostgreSQL availability — JWKS serving and token
  validation depend on signing key retrieval from DB

**Files touched:**
- `ARCHITECTURE/ADR-018-postgresql-high-availability.md` (new)

#### 5.2 — Update Helm chart for Tier 1 support

**What:** Make the Helm chart capable of deploying either Tier 0 (current default) or
Tier 1 (Patroni + PgBouncer) via values overrides.

**Approach — External operator, not built-in:**
Rather than bundling Patroni into the chamicore Helm chart, document and support the use of
existing PostgreSQL operators:
- [CloudNativePG](https://cloudnative-pg.io/) (CNCF Sandbox)
- [Zalando postgres-operator](https://github.com/zalando/postgres-operator)
- [CrunchyData PGO](https://github.com/CrunchyData/postgres-operator)

The chamicore chart adds a toggle:

```yaml
# values.yaml
postgresql:
  # "internal" = chart-managed single instance (Tier 0, default)
  # "external" = user-provided (Tier 1 operator or Tier 2 managed)
  mode: internal

  # Used when mode=external
  external:
    host: ""
    port: 5432
    database: chamicore
    # Secret containing username + password
    existingSecret: ""
    secretUsernameKey: username
    secretPasswordKey: password
    sslMode: require
```

When `mode=external`:
- The chart does not deploy its own PostgreSQL Deployment/PVC
- Service DSN env vars are constructed from the external config
- Init containers run migrations against the external database
- Health checks verify external DB connectivity

**Files touched:**
- `shared/chamicore-deploy/charts/chamicore/values.yaml` (add `postgresql.mode` + external config)
- `shared/chamicore-deploy/charts/chamicore/templates/postgres.yaml` (conditional on `mode=internal`)
- `shared/chamicore-deploy/charts/chamicore/templates/_helpers.tpl` (DSN construction helper)
- `shared/chamicore-deploy/charts/chamicore/templates/configmap.yaml` or service templates
  (use DSN from helper)

#### 5.3 — Document backup and restore procedure

**What:** A runbook in `docs/operations/` covering backup and restore for each tier.

**Content:**
- Tier 0: `pg_dump` / `pg_restore` commands, cron schedule example
- Tier 1: WAL archiving setup, PITR restore procedure, Patroni switchover/failover commands
- Tier 2: Provider-specific backup/restore references
- Schema-aware restore: how to restore a single service's schema without affecting others
- Migration re-run after restore: services auto-migrate on startup, so a restore to an
  older migration state is handled automatically

**Files touched:**
- `docs/operations/postgresql-backup-restore.md` (new)

### Acceptance Criteria

- ADR-018 is reviewed and accepted
- Helm chart deploys successfully with `postgresql.mode=internal` (default, no regression)
- Helm chart deploys successfully with `postgresql.mode=external` pointing at an external
  PostgreSQL (tested with a CloudNativePG cluster or docker-hosted PostgreSQL)
- Backup/restore runbook covers all three tiers with copy-paste-ready commands
- `dbutil.Connect()` pool configuration documented for each tier

---

## Work Stream 6: Add Circuit Breakers

### Problem

The codebase has solid retry logic (exponential backoff with jitter in `httputil/client`,
startup fast-retry in sync loops) but no circuit breakers. When a dependency is down, callers
retry on fixed intervals indefinitely:

| Caller | Dependency | Current Behavior on Failure | Gap |
|--------|-----------|----------------------------|-----|
| BSS sync loop | SMD API | Retry every 5 min forever | No backoff escalation, no circuit open state |
| Cloud-Init sync loop | SMD API | Retry every 5 min forever | Same |
| Kea-Sync sync loop | SMD API + Kea API | Retry every interval forever | Also uses `context.Background()` (can't cancel) |
| Discovery driver | Redfish BMCs | 3 retries per BMC, then fail | Per-target only; no fleet-level breaker |
| Power executor | Redfish BMCs | 3 retries, then fail task | No cache fallback for system path resolution |
| Outbox relay | NATS | Retry every poll interval forever | No circuit breaker |
| HTTP client | Any upstream | 4 attempts with backoff, then fail | Transport errors (conn refused, DNS) not retried |

**Additional gap:** The `httputil/client` does not retry transport-level errors (connection
refused, DNS failure, TLS handshake failure). Only HTTP status codes 429/502/503/504 trigger
retries.

### Approach

Add a lightweight circuit breaker to `chamicore-lib` and integrate it into the two highest-
impact call sites: SMD sync loops and Redfish calls. Also fix the transport-error retry gap
in the base HTTP client.

**Library choice:** Implement a minimal circuit breaker in `chamicore-lib` rather than adding
an external dependency. The pattern is simple (closed → open → half-open) and avoids adding
`sony/gobreaker` or similar to the dependency tree. This aligns with the project's preference
for hand-written, focused implementations.

### Tasks

#### 6.1 — chamicore-lib: Implement circuit breaker package

**What:** New package `chamicore-lib/resilience/` with a state-machine circuit breaker.

**API:**

```go
package resilience

// State represents the circuit breaker state.
type State int

const (
    StateClosed   State = iota // Normal operation — requests pass through
    StateOpen                  // Tripped — requests fail fast
    StateHalfOpen              // Probing — one request allowed to test recovery
)

// Config controls circuit breaker behavior.
type Config struct {
    FailureThreshold int           // Consecutive failures to trip (default: 5)
    ResetTimeout     time.Duration // Time in open state before half-open (default: 30s)
    HalfOpenMax      int           // Max probes in half-open before closing (default: 1)
}

// Breaker is a thread-safe circuit breaker.
type Breaker struct { ... }

// NewBreaker creates a circuit breaker with the given config.
func NewBreaker(name string, cfg Config) *Breaker

// Execute runs fn if the circuit is closed or half-open.
// Returns ErrCircuitOpen if the circuit is open.
func (b *Breaker) Execute(fn func() error) error

// State returns the current breaker state.
func (b *Breaker) State() State

// Metrics returns failure/success counts for observability.
func (b *Breaker) Metrics() Metrics
```

**Behavior:**
- Closed: all calls pass through. Track consecutive failures.
- After `FailureThreshold` consecutive failures → transition to Open.
- Open: all calls immediately return `ErrCircuitOpen`. After `ResetTimeout` → Half-Open.
- Half-Open: allow `HalfOpenMax` calls. If any succeeds → Closed. If any fails → Open.
- Thread-safe via `sync.Mutex` (not `sync/atomic` — state transitions need atomicity across
  multiple fields).
- Prometheus counter/gauge for state transitions (optional, via `otel` integration).

**Files:**
- `shared/chamicore-lib/resilience/breaker.go` (new)
- `shared/chamicore-lib/resilience/breaker_test.go` (new, 100% coverage)

#### 6.2 — Integrate circuit breaker into SMD sync loops

**What:** Wrap the SMD client call in BSS and Cloud-Init sync loops with a circuit breaker.
When SMD is confirmed down (5 consecutive failures), stop hammering it every 5 minutes and
instead fail fast, logging the open-circuit state. Probe periodically via half-open.

**Integration point (BSS syncer.go, Cloud-Init syncer.go):**

```go
type Syncer struct {
    smd     client.Client
    breaker *resilience.Breaker  // new field
    // ...
}

func (s *Syncer) SyncOnce(ctx context.Context) error {
    err := s.breaker.Execute(func() error {
        return s.syncFromSMD(ctx)
    })
    if errors.Is(err, resilience.ErrCircuitOpen) {
        s.log.Warn().Msg("SMD circuit open, skipping sync attempt")
        return err
    }
    return err
}
```

**Config defaults for sync loops:**
- `FailureThreshold: 3` (3 consecutive failures = ~15 minutes of SMD downtime at 5-min interval)
- `ResetTimeout: 60s` (probe once per minute in open state, not every 5 minutes)

**Files touched:**
- `services/chamicore-bss/internal/sync/syncer.go` (add breaker)
- `services/chamicore-bss/internal/sync/syncer_test.go` (test breaker integration)
- `services/chamicore-cloud-init/internal/sync/syncer.go` (same)
- `services/chamicore-cloud-init/internal/sync/syncer_test.go` (same)

#### 6.3 — Integrate circuit breaker into Redfish calls

**What:** Add a per-endpoint circuit breaker to the Redfish client in `chamicore-lib`.
When a specific BMC endpoint is unreachable after N attempts, stop trying it and fail
fast for subsequent calls. This prevents a fleet scan from spending retry time on known-dead
BMCs.

**Integration point (redfish/client.go):**

```go
type Client struct {
    http       *http.Client
    breakers   map[string]*resilience.Breaker  // keyed by normalized endpoint
    breakerMu  sync.Mutex
    breakerCfg resilience.Config
    // ...
}
```

**Config defaults for Redfish:**
- `FailureThreshold: 3` (3 failed attempts = BMC is down)
- `ResetTimeout: 120s` (BMC recovery is slow — wait 2 minutes before probing)

**Files touched:**
- `shared/chamicore-lib/redfish/client.go` (add per-endpoint breaker map)
- `shared/chamicore-lib/redfish/client_test.go` (test breaker behavior)

#### 6.4 — Fix transport-error retries in httputil/client

**What:** The base HTTP client currently only retries on HTTP status codes 429/502/503/504.
Transport-level errors (connection refused, DNS failure, TLS handshake failure, TCP timeout)
cause immediate failure with no retry. This is the highest-impact gap: a service that's
still starting up causes all callers to fail instantly.

**Change:**

```go
// client.go — in the retry loop

resp, err := c.http.Do(req)
if err != nil {
    // Transport error — check if retryable
    if isRetryableTransportError(err) && attempt < maxAttempts-1 {
        lastErr = fmt.Errorf("transport error (attempt %d/%d): %w", attempt+1, maxAttempts, err)
        continue  // retry
    }
    return fmt.Errorf("executing request: %w", err)
}
```

```go
// isRetryableTransportError returns true for transient transport failures.
func isRetryableTransportError(err error) bool {
    if err == nil {
        return false
    }
    // Connection refused, reset, timeout
    var netErr *net.OpError
    if errors.As(err, &netErr) {
        return true
    }
    // DNS temporary failure
    var dnsErr *net.DNSError
    if errors.As(err, &dnsErr) && dnsErr.Temporary() {
        return true
    }
    // Context deadline exceeded (per-request timeout, not parent context)
    if errors.Is(err, context.DeadlineExceeded) {
        return true
    }
    // TLS handshake timeout
    if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
        return true
    }
    return false
}
```

**Important:** Do NOT retry if the parent context is cancelled (`ctx.Err() != nil`). Only
retry transport errors when the caller's context is still alive.

**Files touched:**
- `shared/chamicore-lib/httputil/client/client.go` (add transport retry + `isRetryableTransportError`)
- `shared/chamicore-lib/httputil/client/client_test.go` (test transport retry scenarios)

#### 6.5 — Fix Kea-Sync context.Background() usage

**What:** Kea-Sync's sync loop uses `context.Background()` for sync operations instead of
propagating the parent context. This means sync operations can't be cancelled during
graceful shutdown, potentially leaking goroutines or delaying process exit.

**Change:** Pass the parent context from `Start()` through to `SyncOnce()`.

**Files touched:**
- `services/chamicore-kea-sync/internal/sync/syncer.go` (propagate context)
- `services/chamicore-kea-sync/internal/sync/syncer_test.go` (verify cancellation)

### Acceptance Criteria

- Circuit breaker package has 100% test coverage
- Breaker state transitions are correct: closed → open after N failures, open → half-open
  after timeout, half-open → closed on success, half-open → open on failure
- BSS/Cloud-Init sync loops fail fast when SMD circuit is open (verify via test)
- Redfish client fails fast for known-dead BMC endpoints (verify via test)
- HTTP client retries transport errors (connection refused, DNS failure) with backoff
- HTTP client does NOT retry when parent context is cancelled
- Kea-Sync sync operations respect context cancellation
- All existing tests continue to pass
- No new external dependencies added

---

## Implementation Order

```
WS4 (Store Interfaces) ─────────────────────────────>
  4.1 auth store split  →  4.2 smd store split  →  4.3 AGENTS.md update

WS5 (HA PostgreSQL)    ─────────────────────────────────────────>
  5.1 ADR-018  →  5.2 Helm chart external mode  →  5.3 backup runbook

WS6 (Circuit Breakers) ──────────────────────────────────────────────────>
  6.1 lib breaker pkg  →  6.2 sync loop integration  →  6.3 redfish integration
                        →  6.4 transport retry fix    →  6.5 kea-sync context fix
```

- **WS4, WS5, and WS6 are independent** and can proceed in parallel
- Within WS4: auth (4.1) and smd (4.2) are independent; AGENTS.md update (4.3) follows both
- Within WS5: ADR (5.1) should be written first; Helm (5.2) and runbook (5.3) follow
- Within WS6: breaker package (6.1) blocks 6.2 and 6.3; transport fix (6.4) and context
  fix (6.5) are independent of 6.1 and can start immediately

### Dependencies on High-Priority Plan

- **WS4 (store split) depends on WS2 (bulk endpoints):** The bulk endpoint work in the
  high-priority plan adds new methods to the SMD store interface. Split the interface after
  bulk methods are added, not before, to avoid rework.
- **WS5 (HA PostgreSQL) depends on WS3 Phase B (soft delete):** Soft delete adds migrations
  and changes query patterns. Document the HA strategy after schema changes are finalized.
- **WS6 (circuit breakers) is fully independent** and can start any time.

### Estimated Effort

| Task | Effort | New/Modified Files |
|------|--------|--------------------|
| 4.1 Auth store split | Medium | 6 files |
| 4.2 SMD store split | Medium | 7 files |
| 4.3 AGENTS.md update | Small | 1 file |
| 5.1 ADR-018 | Medium | 1 new |
| 5.2 Helm chart external mode | Medium | 4 files |
| 5.3 Backup runbook | Small | 1 new |
| 6.1 Circuit breaker package | Medium | 2 new |
| 6.2 Sync loop integration | Medium | 4 files |
| 6.3 Redfish breaker integration | Medium | 2 files |
| 6.4 Transport retry fix | Small | 2 files |
| 6.5 Kea-Sync context fix | Small | 2 files |

---

## Open Questions

1. **Store split granularity:** Should `CreateComponentWithInterfaces` live on `ComponentStore`
   (it touches both components and interfaces in a transaction) or on a separate
   `ComponentWithInterfacesStore`? Recommendation: keep it on `ComponentStore` since the
   transaction is component-centric.
2. **Circuit breaker observability:** Should breaker state transitions emit OTel events or
   just increment Prometheus counters? Prometheus counters are simpler and sufficient for
   alerting.
3. **PostgreSQL operator preference:** Should the ADR recommend a specific operator
   (CloudNativePG, Zalando, CrunchyData) or stay operator-agnostic? Recommendation:
   document CloudNativePG as the primary example (CNCF project, most actively maintained)
   but keep the Helm chart operator-agnostic.
4. **Transport retry scope:** Should `isRetryableTransportError` be exported from
   `httputil/client` for use by the Redfish client, or should each client define its own
   retry policy? Recommendation: export it — consistent retry semantics across all HTTP clients.
