# ADR-014: Boot-Path Data Flow and Service Self-Sufficiency

## Status

Accepted

## Date

2026-02-18

## Context

Chamicore must support booting **tens of thousands of nodes concurrently** (boot storms).
During a boot storm, every node simultaneously:

1. Requests a DHCP lease from Kea (pre-populated by Kea-Sync).
2. Fetches an iPXE boot script from BSS (`GET /boot/v1/bootscript?mac=<mac>`).
3. Downloads kernel/initrd (external, not Chamicore's concern).
4. Fetches cloud-init payloads from Cloud-Init (`GET /cloud-init/<id>/user-data`).

The existing architecture (AGENTS.md, ADR-002, ADR-003) defines per-service PostgreSQL
schemas and declares inter-service dependencies, but it does not explicitly specify:

- Whether BSS or Cloud-Init make synchronous HTTP calls to SMD at boot-script-serve time.
- How BSS resolves a MAC address to boot parameters (does it store MACs locally or call SMD?).
- How downstream services learn about changes in SMD (new components, updated MACs, role
  changes).
- How PostgreSQL connection pools should be sized across services sharing one instance.
- Whether boot endpoints require JWT authentication.
- How auth token revocation is checked without adding a database round-trip per request.

These are critical architectural decisions that determine whether the system can meet its
10,000+ req/s boot storm targets. Without explicit rules, implementers may inadvertently
introduce cross-service calls on the hot path, coupling failure domains and creating
bottlenecks.

### The Fan-Out Problem

If BSS calls SMD on every `GET /bootscript?mac=<mac>` to resolve MAC→component ID, then
during a 10,000-node boot storm, SMD receives 10,000 synchronous requests from BSS alone,
plus 10,000 from Cloud-Init (if it also fans out), plus any operator/UI queries. SMD becomes
the system-wide bottleneck, and a brief SMD latency spike fails the entire boot. This
violates the principle established in ADR-013: services should not create cascading failure
dependencies.

### The Consistency Problem

If services do NOT call SMD at request time, they must store their own copies of data that
originates in SMD (MAC addresses, component roles, etc.). This introduces a cache coherency
problem: when an operator changes a MAC address or role in SMD, downstream services are
stale until they sync. The architecture must define how and when propagation happens, and
what staleness window is acceptable.

## Decision

### Principle: Self-Sufficient Services on the Hot Path

**During a boot storm, every service serves requests from its own database schema. No
cross-service HTTP calls on the read path.** This is the foundational rule for Chamicore's
boot-time performance.

The data flow splits into three distinct regimes with different performance characteristics
and different acceptable trade-offs:

```
REGIME 1: PROVISIONING (slow path, operator-driven, consistency matters)
─────────────────────────────────────────────────────────────────────────
  Discovery ──scan──> BMCs
       │
       ▼
  SMD (register components, MACs, roles, network interfaces)
       │
       ├──> Kea-Sync ──poll──> Kea (push DHCP reservations)
       │
       ├──> BSS sync loop ──poll──> BSS local tables (update MACs, roles)
       │
       └──> Cloud-Init sync loop ──poll──> Cloud-Init local tables (update metadata)

  Operator/CLI
       ├──> BSS API: set boot params per node or role
       └──> Cloud-Init API: set payloads per node or role

REGIME 2: BOOT STORM (hot path, 10k+ concurrent, latency matters)
──────────────────────────────────────────────────────────────────
  Node ──DHCP──> Kea (reads local reservation table)
       │
       ├──GET /boot/v1/bootscript?mac=──> BSS ──> bss schema (PostgreSQL)
       │
       └──GET /cloud-init/<id>/user-data──> Cloud-Init ──> cloudinit schema (PostgreSQL)

  Services NOT involved at request time:
    SMD, Auth, Discovery, Kea-Sync

REGIME 3: STEADY-STATE OPERATIONS (humans, moderate load, correctness matters)
──────────────────────────────────────────────────────────────────────────────
  UI/CLI ──> Any service (all requests authenticated via cached JWKS)
```

### Boot Endpoints Are Unauthenticated

Boot-time endpoints (`GET /boot/v1/bootscript`, `GET /cloud-init/<id>/*`) are served
**without JWT authentication**. Booting nodes do not carry JWTs - they are bare iPXE
clients and cloud-init `curl` requests.

These endpoints are registered outside the JWT middleware group, alongside `/health`,
`/readiness`, and `/metrics`:

```go
// Unauthenticated routes (boot path, health, operational)
r.Group(func(r chi.Router) {
    r.Get("/health", s.handleHealth)
    r.Get("/readiness", s.handleReady)
    r.Get("/boot/v1/bootscript", s.handleGetBootScript)  // Boot storm hot path
})

// Authenticated routes (operator/management API)
r.Group(func(r chi.Router) {
    r.Use(auth.JWTMiddleware(cfg))
    r.Post("/boot/v1/bootparams", s.handleCreateBootParam)
    // ...
})
```

Security for boot endpoints relies on network-level controls (management VLAN isolation),
not application-level JWT tokens. This is consistent with how PXE/iPXE booting works
in production HPC environments.

### BSS Data Model: MAC Stored Locally

BSS stores everything it needs to serve a boot script in its own `bss` PostgreSQL schema.
The MAC address and component role are **denormalized** from SMD at provisioning time, so
BSS never needs to call SMD at boot-request time:

```sql
CREATE TABLE bss.boot_params (
    id            VARCHAR(255) PRIMARY KEY,
    component_id  VARCHAR(255) NOT NULL,
    mac           TEXT,
    role          VARCHAR(32)  NOT NULL DEFAULT '',
    kernel_uri    TEXT         NOT NULL,
    initrd_uri    TEXT         NOT NULL,
    cmdline       TEXT         NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_boot_params_mac ON bss.boot_params (mac);
CREATE INDEX idx_boot_params_component ON bss.boot_params (component_id);
CREATE INDEX idx_boot_params_role ON bss.boot_params (role);
```

Boot script resolution:
1. Node sends `GET /boot/v1/bootscript?mac=AA:BB:CC:DD:EE:FF`.
2. BSS queries `SELECT * FROM bss.boot_params WHERE mac = $1`.
3. If no match by MAC, BSS falls back to role-based params:
   `SELECT * FROM bss.boot_params WHERE role = $1 AND mac IS NULL` (default params for
   that role).
4. BSS renders the iPXE script and returns it.

One indexed point lookup. No HTTP calls. p99 target: < 100ms.

### DHCP Boot Option Synthesis in Kea-Sync

Kea-Sync is responsible for programming DHCP reservation boot directives so nodes can
reach BSS without manual Kea edits. In addition to MAC/IP/hostname, synchronized
reservations include:

- `boot-file-name` (DHCP option 67, or URL for HTTP boot)
- `next-server` (DHCP next-server / option 66 equivalent)

Boot option synthesis is configurable:

- `direct-http` strategy:
  - `boot-file-name` is rendered from a template (for example
    `http://<entrypoint>/boot/v1/bootscript?mac=__mac__`).
  - `next-server` is optional.
- `ipxe-chain` strategy:
  - `boot-file-name` is a chainloader filename (for example `undionly.kpxe` or `ipxe.efi`).
  - `next-server` is required in most environments.

Supported template variables are `{{component_id}}`/`__component_id__`,
`{{mac}}`/`__mac__`, `{{ip}}`/`__ip__`, and `{{hostname}}`/`__hostname__`.
This keeps DHCP boot metadata aligned with SMD inventory and avoids out-of-band Kea
configuration drift.

### Cloud-Init Data Model: Component Metadata Stored Locally

Cloud-Init similarly stores payloads and any component metadata it needs for template
rendering in its own `cloudinit` schema:

```sql
CREATE TABLE cloudinit.payloads (
    id            VARCHAR(255) PRIMARY KEY,
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

Payload resolution:
1. Node sends `GET /cloud-init/<component-id>/user-data`.
2. Cloud-Init queries `SELECT user_data FROM cloudinit.payloads WHERE component_id = $1`.
3. If the payload includes template variables (e.g., `{{ .Role }}`, `{{ .NID }}`), Cloud-Init
   renders them using the locally cached component metadata.
4. Returns the rendered payload.

One indexed point lookup plus optional template rendering. No HTTP calls. p99 target: < 100ms.

### Change Propagation: Two-Phase Strategy

Changes in SMD (new components, updated MACs, role changes, deletions) must propagate to
downstream services. The propagation strategy has two phases:

#### Phase 1: Polling with ETags (Initial Implementation)

Each downstream service runs a **background sync loop** that polls SMD for changes using
conditional requests:

```go
// Background sync loop in BSS (and similarly in Cloud-Init, Kea-Sync)
func (s *Syncer) Run(ctx context.Context) {
    ticker := time.NewTicker(s.cfg.SyncInterval) // default: 30s
    defer ticker.Stop()

    var lastETag string
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            s.sync(ctx, &lastETag)
        case <-s.forceSyncCh:  // triggered by operator via API
            s.sync(ctx, &lastETag)
        }
    }
}

func (s *Syncer) sync(ctx context.Context, lastETag *string) {
    components, etag, err := s.smdClient.ListComponents(ctx, smdclient.ListOpts{
        Fields: "id,mac,role,nid,state",
        IfNoneMatch: *lastETag,
    })
    if errors.Is(err, smdclient.ErrNotModified) {
        return // 304 Not Modified — nothing changed
    }
    if err != nil {
        s.log.Error().Err(err).Msg("sync failed")
        return
    }

    *lastETag = etag
    s.reconcile(ctx, components) // diff and update local tables
}
```

Key characteristics:
- **Efficient in steady state**: 304 Not Modified responses are tiny and fast. The common
  case (nothing changed) costs one HTTP round-trip with an empty body.
- **Bounded staleness**: Maximum propagation delay equals the poll interval (default 30s).
  This is acceptable for boot-storm scenarios because provisioning happens minutes or hours
  before the boot storm.
- **Force sync**: Operators can trigger an immediate sync via the service's API
  (`POST /boot/v1/sync` or `POST /cloud-init/sync`) or via the CLI. This is useful after
  bulk provisioning changes.
- **Reconciliation, not full replacement**: The sync loop diffs the SMD snapshot against
  local state and applies only the changes (inserts, updates, deletes). This minimizes
  database writes.
- **Startup sync**: Services perform a full sync on startup before marking themselves as
  ready (the `/readiness` endpoint returns 503 until the first sync completes).

Sync configuration per service:

```bash
CHAMICORE_BSS_SYNC_INTERVAL=30s          # Poll interval (default: 30s)
CHAMICORE_BSS_SYNC_FIELDS=id,mac,role    # Which SMD fields to sync
CHAMICORE_BSS_SYNC_ON_STARTUP=true       # Full sync before accepting traffic
```

#### Phase 2: Event-Driven Notifications (Future)

When polling latency becomes unacceptable (e.g., real-time component state tracking,
automatic re-provisioning on hardware replacement), introduce an event bus:

```
SMD ──publishes──> NATS JetStream
                       │
                       ├──> BSS subscriber (component.created, component.updated, component.deleted)
                       ├──> Cloud-Init subscriber (component.updated, component.deleted)
                       ├──> Kea-Sync subscriber (component.created, component.updated, component.deleted)
                       └──> (future consumers)

Discovery ──publishes──> NATS JetStream
                             │
                             └──> (component.discovered events for monitoring/alerting)
```

Design constraints for the event bus:
- **Not on the hot path**: Booting nodes still query BSS/Cloud-Init directly. Events are
  for **propagation**, not for request serving.
- **At-least-once delivery**: Services must be idempotent when processing events (duplicate
  events must not corrupt state).
- **Catch-up on reconnect**: If a service was down and missed events, it must reconcile
  from SMD on startup (same as the polling full-sync).
- **Graceful degradation**: If the event bus is unavailable, services fall back to polling.
  The event bus improves latency but is not a hard dependency.
- **Not added until justified**: The polling approach is sufficient for initial deployments.
  The event bus is added when a concrete use case demands sub-second propagation.

#### Propagation Latency by Phase

| Change Type | Phase 1 (polling) | Phase 2 (events) |
|------------|-------------------|-------------------|
| New component registered | ≤ sync interval (30s) | < 1s |
| MAC address changed | ≤ sync interval (30s) | < 1s |
| Role changed | ≤ sync interval (30s) | < 1s |
| Component deleted | ≤ sync interval (30s) | < 1s |
| Force sync (operator) | Immediate | Immediate |
| Full reconciliation | Startup | Startup + reconnect |

### PostgreSQL Connection Pool Strategy

All services share one PostgreSQL instance (per ADR-003). Connection pools must be sized
to prevent contention during boot storms while leaving headroom for other services:

```
Total PostgreSQL max_connections: 500

Per-service pool allocation:
  ┌──────────────────┬──────┬───────────────────────────────────────────┐
  │ Service          │ Pool │ Rationale                                 │
  ├──────────────────┼──────┼───────────────────────────────────────────┤
  │ BSS              │  100 │ Boot storm hot path, 10k+ concurrent reqs │
  │ Cloud-Init       │  100 │ Boot storm hot path, 10k+ concurrent reqs │
  │ SMD              │   80 │ Operator queries, discovery writes         │
  │ Auth             │   40 │ Revocation checks, policy queries          │
  │ Discovery        │   30 │ Scan job tracking, target management       │
  │ Kea-Sync         │   10 │ Background sync, low concurrency           │
  │ Reserve/headroom │   40 │ Migrations, monitoring, spikes             │
  ├──────────────────┼──────┼───────────────────────────────────────────┤
  │ Total            │  400 │ 80% of max_connections (leaves headroom)   │
  └──────────────────┴──────┴───────────────────────────────────────────┘
```

Configuration:

```bash
# PostgreSQL server
max_connections = 500

# Per-service (in each service's environment)
CHAMICORE_BSS_DB_MAX_OPEN_CONNS=100
CHAMICORE_BSS_DB_MAX_IDLE_CONNS=20
CHAMICORE_BSS_DB_CONN_MAX_LIFETIME=5m
CHAMICORE_BSS_DB_CONN_MAX_IDLE_TIME=1m
```

#### Connection Pooling with PgBouncer (Scale-Out)

For deployments exceeding a single PostgreSQL instance's connection capacity, deploy
[PgBouncer](https://www.pgbouncer.org/) in **transaction pooling mode** between services
and PostgreSQL:

```
Services (400 app connections)
    │
    ▼
PgBouncer (transaction pooling)
    │
    ▼
PostgreSQL (50-100 backend connections)
```

PgBouncer multiplexes many application connections over fewer PostgreSQL backend
connections. A connection is assigned to a backend only for the duration of a transaction,
then returned to the pool. This is effective because Chamicore queries are short (simple
indexed lookups) and do not hold connections open for long.

PgBouncer is optional for initial deployments but recommended for production at scale.
The `chamicore-deploy` Helm chart includes PgBouncer as an opt-in component.

### Auth Token Revocation: In-Memory Cache

JWT signature validation is local (cached JWKS keys, CPU-only, no network call). However,
checking whether a token has been revoked requires looking up its JTI in the
`auth.revoked_tokens` table. A database round-trip per authenticated request is unnecessary
overhead.

Instead, services cache the revocation list in memory:

```go
// RevocationCache maintains an in-memory set of revoked JTIs.
// Refreshed periodically from the auth database.
type RevocationCache struct {
    mu       sync.RWMutex
    revoked  map[string]time.Time // jti -> expires_at
    interval time.Duration        // refresh interval (default: 10s)
}

func (c *RevocationCache) IsRevoked(jti string) bool {
    c.mu.RLock()
    defer c.mu.RUnlock()
    _, ok := c.revoked[jti]
    return ok
}
```

Refresh loop:
1. Every 10 seconds, query `SELECT jti, expires_at FROM auth.revoked_tokens WHERE expires_at > NOW()`.
2. Replace the in-memory map.
3. Expired entries (past `expires_at`) are automatically excluded.

Trade-off: a revoked token remains valid for up to 10 seconds after revocation. This is
acceptable because:
- Chamicore JWTs are short-lived (15-60 minutes). Revocation is a safety net, not the
  primary expiry mechanism.
- Active attacks are mitigated by network-level controls and short token lifetimes.
- The 10-second window can be reduced to 1-2 seconds if needed (at the cost of more
  frequent DB queries, which are still cheap since the revocation table is small).

For services that do not share a database with auth (or in deployments where auth runs on
a separate PostgreSQL instance), the revocation cache fetches the list via chamicore-auth's
API:

```
GET /auth/v1/revocations?active=true
```

This endpoint returns only currently-active revocations (JTIs where `expires_at > NOW()`).
Services poll it with If-None-Match for efficiency.

### Complete Boot-Storm Request Flow

For clarity, here is the exact sequence for a single node during a boot storm, with every
service interaction annotated:

```
1. Node powers on

2. DHCP (Kea, external to Chamicore)
   Node ──DHCPDISCOVER──> Kea
   Kea looks up MAC in local reservation table (pre-populated by Kea-Sync)
   Kea ──DHCPOFFER──> Node (IP address, next-server = BSS, filename = iPXE)

3. iPXE boot script (BSS, unauthenticated, single DB query)
   Node ──GET /boot/v1/bootscript?mac=AA:BB:CC:DD:EE:FF──> BSS
   BSS:
     SELECT kernel_uri, initrd_uri, cmdline
     FROM bss.boot_params
     WHERE mac = 'AA:BB:CC:DD:EE:FF'
   BSS renders iPXE script:
     #!ipxe
     kernel http://images.example.com/vmlinuz <cmdline>
     initrd http://images.example.com/initramfs.img
     boot
   BSS ──200 OK──> Node
   Time budget: p99 < 100ms

4. Kernel/initrd download (external HTTP server, not Chamicore)

5. Cloud-init payload (Cloud-Init, unauthenticated, single DB query)
   Node ──GET /cloud-init/node-a1b2c3/user-data──> Cloud-Init
   Cloud-Init:
     SELECT user_data
     FROM cloudinit.payloads
     WHERE component_id = 'node-a1b2c3'
   Cloud-Init renders template (if applicable) and returns payload.
   Cloud-Init ──200 OK──> Node
   Time budget: p99 < 100ms

Total Chamicore involvement: 2 HTTP requests, 2 PostgreSQL point lookups.
No cross-service calls. No authentication overhead.
```

### Sync Endpoints

Each service that syncs from SMD exposes a sync management API for operators:

| Service | Endpoint | Purpose |
|---------|----------|---------|
| BSS | `POST /boot/v1/sync` | Trigger immediate sync from SMD |
| BSS | `GET /boot/v1/sync/status` | Last sync time, ETag, component count, errors |
| Cloud-Init | `POST /cloud-init/sync` | Trigger immediate sync from SMD |
| Cloud-Init | `GET /cloud-init/sync/status` | Last sync time, ETag, component count, errors |
| Kea-Sync | (existing design) | Already polls SMD continuously |

These endpoints require authentication (`write:sync` scope for POST, `read:sync` for GET).

### Sync-Related Metrics

| Metric | Type | Service | Description |
|--------|------|---------|-------------|
| `sync_last_success_timestamp` | Gauge | BSS, Cloud-Init | Unix timestamp of last successful sync |
| `sync_duration_seconds` | Histogram | BSS, Cloud-Init | Time taken per sync cycle |
| `sync_components_total` | Gauge | BSS, Cloud-Init | Number of components in local cache |
| `sync_changes_applied_total` | Counter | BSS, Cloud-Init | Inserts + updates + deletes per sync |
| `sync_errors_total` | Counter | BSS, Cloud-Init | Failed sync attempts |
| `sync_not_modified_total` | Counter | BSS, Cloud-Init | 304 Not Modified responses (no changes) |
| `auth_revocation_cache_size` | Gauge | All | Number of active revoked JTIs in memory |
| `auth_revocation_cache_refresh_duration` | Histogram | All | Time to refresh revocation cache |

### Observability: Boot Storm Dashboard

The Grafana dashboard in `chamicore-deploy` should include a **boot storm panel** showing:

- BSS request rate and p50/p95/p99 latency (the primary boot-storm indicator)
- Cloud-Init request rate and p50/p95/p99 latency
- PostgreSQL connection pool utilization per service (active/idle/waiting)
- PostgreSQL query latency per schema (bss, cloudinit)
- Sync lag: time since last successful SMD sync per service
- Kea-Sync reservation push rate and lag

## Consequences

### Positive

- **Predictable boot-storm performance.** With no cross-service calls on the hot path,
  BSS and Cloud-Init performance is determined entirely by their own code and their own
  PostgreSQL queries. A latency spike in SMD, Auth, or Discovery cannot affect boot times.

- **Independent failure domains.** If SMD goes down during a boot storm, booting continues
  uninterrupted. Nodes use the data that was synced before the outage. This is a critical
  operational benefit: the inventory service can be upgraded, restarted, or briefly
  unavailable without impacting thousands of actively booting nodes.

- **Efficient sync via ETags.** The polling approach reuses the ETag infrastructure already
  defined in ADR-007 and implemented in `chamicore-lib/httputil`. No new protocols or
  infrastructure. The common case (nothing changed) is a single lightweight 304 response.

- **Clear upgrade path to event-driven.** Phase 1 polling and Phase 2 events solve the
  same problem with different latency characteristics. Services can be migrated one at a
  time. The event bus is additive, not a rewrite.

- **Explicit connection pool budget.** Documenting pool sizes prevents the common failure
  mode where services compete for PostgreSQL connections under load and all degrade together.

- **Boot endpoints skip auth overhead.** No JWT parsing, no JWKS lookup, no revocation
  check for the highest-throughput endpoints in the system. Consistent with HPC network
  security models where the management VLAN is already a trust boundary.

### Negative

- **Data denormalization in BSS and Cloud-Init.** MAC addresses and roles are stored in
  two places (SMD and BSS/Cloud-Init). If the sync loop fails or is misconfigured, data
  diverges.
  - Mitigated: The `/sync/status` endpoint and `sync_last_success_timestamp` metric make
    sync health observable. Readiness probes fail if the initial sync has not completed.
    Alerts fire if sync lag exceeds a threshold.

- **Eventual consistency.** After an SMD change, downstream services are stale for up to
  the sync interval (default 30s). An operator who changes a MAC in SMD and immediately
  reboots the affected node may see the old boot params.
  - Mitigated: Force sync (`POST /boot/v1/sync`) resolves immediately. The CLI can chain
    this automatically: `chamicore smd component update ... && chamicore bss sync`.

- **Unauthenticated boot endpoints.** A host on the management VLAN can request any node's
  boot script or cloud-init payload.
  - Accepted: This is the standard security model for PXE boot environments. The management
    network is isolated. Cloud-init payloads should not contain secrets (use Vault or
    similar for secret injection post-boot).

- **Connection pool tuning is manual.** Operators must size pools based on their deployment
  scale.
  - Mitigated: The documented defaults work for clusters up to ~50,000 nodes. The Helm
    chart exposes pool sizes as values. PgBouncer is available for larger deployments.

### Neutral

- The sync loop is a well-understood pattern (Kea-Sync already implements it). Extending
  it to BSS and Cloud-Init is incremental, not novel.
- The revocation cache trades a bounded staleness window (≤ 10s) for eliminating a
  per-request database round-trip. The window is configurable.
- Phase 2 (event bus) requires choosing a messaging system. NATS JetStream is the current
  preference for its simplicity, persistence, and Go-native client. The decision can be
  deferred until Phase 2 is actually needed.
- Template rendering in Cloud-Init (e.g., injecting hostname, NID, role into user-data)
  uses locally cached metadata, not live SMD queries. The template variables available
  are limited to what the sync loop fetches.
