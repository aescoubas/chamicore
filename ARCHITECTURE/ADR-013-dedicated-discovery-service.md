# ADR-013: Dedicated Discovery Service (chamicore-discovery)

## Status

Accepted (amends [ADR-002](ADR-002-microservice-selection.md))

## Date

2026-02-18

## Context

ADR-002 consolidated the upstream OpenCHAMI `magellan` discovery tool into SMD, making SMD
responsible for both hardware inventory management and active Redfish BMC discovery. This
mirrors how upstream CSM/OpenCHAMI operates: SMD is the single service that crawls the
network for BMCs, interrogates them via Redfish, and registers the discovered components
into its own database.

This coupling has several problems observed in production CSM/OpenCHAMI deployments:

1. **Mixed operational profiles**: Inventory management is a steady-state, low-latency CRUD
   workload. Discovery is bursty, long-running, and network-bound (Redfish calls to thousands
   of BMCs with timeouts and retries). Combining them in one process means discovery bursts
   can degrade inventory query performance.

2. **Scaling mismatch**: Discovery load is proportional to the number of BMCs being scanned
   and is heaviest during initial deployment or hardware additions. Inventory load is
   proportional to the number of API consumers (BSS, Cloud-Init, UI, CLI) and is constant
   during steady-state operation. These need independent scaling.

3. **Credential management in the wrong place**: Discovery requires BMC credentials
   (Redfish/IPMI usernames and passwords). Storing device credentials in the inventory
   service expands its security surface unnecessarily.

4. **Single discovery method**: Baking Redfish discovery into SMD makes it difficult to
   support alternative discovery mechanisms (IPMI, SNMP for switches/PDUs, LLDP from
   switch ports, bulk CSV/JSON import from asset databases, or passive network discovery
   via SSDP/mDNS).

5. **Complexity and testability**: SMD's codebase becomes harder to reason about and test
   when it contains both HTTP CRUD handlers and network-crawling discovery logic with
   retry loops, connection pools to remote BMCs, and hardware-specific Redfish response
   parsing.

6. **Blast radius**: A bug in discovery code (e.g., a goroutine leak from hung Redfish
   connections) can take down the entire inventory service, affecting the boot path for
   all nodes.

The `chamicore-kea-sync` service already establishes a pattern for this project: a dedicated
service that watches one system (SMD) and pushes state to another (Kea DHCP). Discovery is
the same pattern in reverse: watch the network and push state to SMD.

## Decision

Extract hardware discovery from SMD into a new dedicated service: **`chamicore-discovery`**.

### Architecture

```
  ┌───────────────────────────────────────────────────┐
  │              chamicore-discovery                   │
  │                                                   │
  │  ┌─────────────┐    ┌───────────────────────────┐ │
  │  │   Scanner    │    │     Driver Registry       │ │
  │  │   Engine     │───▶│                           │ │
  │  │             │    │  ┌─────────┐ ┌──────────┐ │ │
  │  │  - Targets   │    │  │ Redfish │ │   IPMI   │ │ │
  │  │  - Schedules │    │  └─────────┘ └──────────┘ │ │
  │  │  - Jobs      │    │  ┌─────────┐ ┌──────────┐ │ │
  │  └─────────────┘    │  │  SNMP   │ │   LLDP   │ │ │
  │                      │  └─────────┘ └──────────┘ │ │
  │                      │  ┌─────────┐ ┌──────────┐ │ │
  │                      │  │  CSV    │ │  Manual  │ │ │
  │                      │  │ Import  │ │   API    │ │ │
  │                      │  └─────────┘ └──────────┘ │ │
  │                      └───────────────────────────┘ │
  └────────────────────────┬──────────────────────────┘
                           │
                           │ POST /hsm/v2/State/Components
                           │ PATCH /hsm/v2/State/Components/{id}
                           │ (register/update via SMD HTTP API)
                           ▼
                  ┌─────────────────┐
                  │  chamicore-smd   │
                  │  (pure inventory │
                  │   + state CRUD)  │
                  └─────────────────┘
                           │
                           │ (consumed by)
                           ▼
              BSS, Cloud-Init, Kea-Sync, UI, CLI
```

### Core Principles

1. **SMD becomes a pure inventory service.** It accepts component registrations via its
   HTTP API and manages state, groups, roles, and network interfaces. It does not know
   or care how hardware was discovered. All discovery logic is removed from SMD.

2. **Discovery is not in the critical boot path.** If chamicore-discovery is down, existing
   inventory and the entire boot pipeline (BSS, Cloud-Init, Kea-Sync) continue to function.
   Discovery is a setup/maintenance operation, not a runtime dependency.

3. **One-way data flow via HTTP API.** Discovery finds hardware and registers it into SMD
   using SMD's public HTTP API (`pkg/client/`). No shared database, no internal package
   imports. The contract is the OpenAPI spec.

4. **Pluggable drivers.** Different sites discover hardware differently. The service
   supports multiple discovery backends through a driver interface.

5. **Credentials managed via chamicore-auth.** BMC and device credentials are stored and
   retrieved through chamicore-auth's service account and secrets management. Discovery
   authenticates to chamicore-auth to obtain scoped credentials for target devices. This
   keeps all credential management in one place with consistent access control and audit
   logging.

6. **Dual-mode binary: service + sysadmin CLI.** The `chamicore-discovery` binary runs
   as both a long-running HTTP service and a standalone CLI tool. Sysadmins can perform
   ad-hoc scans, probe individual BMCs, and bulk-import hardware inventories directly
   from the command line without needing the full service stack running.

### Dual-Mode Operation

The `chamicore-discovery` binary is built with Cobra and operates in three modes:

#### Server Mode

```bash
chamicore-discovery serve
```

Starts the long-running HTTP API server with database-backed target management,
scheduled scans, job tracking, and metrics. This is the mode used in production
deployments (Docker, Kubernetes).

#### Standalone CLI Mode

These commands work **without the discovery service running**. They use drivers
directly and push results to SMD's HTTP API. This is for sysadmins doing ad-hoc
work from a management node:

```bash
# Scan a single BMC
chamicore-discovery scan 10.0.0.1 --driver redfish --username admin --password <secret>

# Scan an IP range
chamicore-discovery scan 10.0.0.0/24 --driver redfish --cred-id bmc-default

# Scan multiple targets
chamicore-discovery scan 10.0.0.1,10.0.0.2,10.0.0.3 --driver redfish --cred-id rack12

# Probe a BMC without registering (inspect only)
chamicore-discovery probe 10.0.0.1 --driver redfish --username admin --password <secret>

# Import from CSV file
chamicore-discovery import nodes.csv --format csv

# Import from JSON file
chamicore-discovery import inventory.json --format json

# List available drivers
chamicore-discovery drivers
```

Key design points for standalone mode:
- **No database required.** Standalone commands do not need PostgreSQL or the discovery
  service's database. They operate purely in memory.
- **Credentials inline or from chamicore-auth.** Credentials can be passed directly via
  `--username`/`--password` flags, read from environment variables
  (`CHAMICORE_DISCOVERY_BMC_USERNAME`, `CHAMICORE_DISCOVERY_BMC_PASSWORD`), or fetched
  from chamicore-auth by reference (`--cred-id bmc-default`).
- **SMD URL is required.** Standalone `scan` and `import` commands need the SMD API
  endpoint to register discovered components (`--smd-url` flag or
  `CHAMICORE_DISCOVERY_SMD_URL` env var). The `probe` command does not need SMD.
- **Output to terminal.** Standalone commands print results in human-readable table
  format by default, with `--output json` and `--output yaml` options for scripting.
- **Dry-run support.** All mutating commands support `--dry-run` to show what would
  be discovered and registered without actually writing to SMD.

#### Service Client Mode

These commands talk to the running discovery service API for managing persistent
targets and viewing scan history:

```bash
# Target management
chamicore-discovery targets list
chamicore-discovery targets create --name "rack12-bmcs" --driver redfish \
  --addresses 10.0.12.0/24 --cred-id rack12 --schedule "0 */6 * * *"
chamicore-discovery targets update <id> --enabled=false
chamicore-discovery targets delete <id>
chamicore-discovery targets scan <id>          # Trigger scan for a specific target

# Scan job management
chamicore-discovery scans list
chamicore-discovery scans status <id>
chamicore-discovery scans cancel <id>
```

Service client commands require `--service-url` (or `CHAMICORE_DISCOVERY_SERVICE_URL`)
pointing to the running discovery service.

#### Mode Summary

| Command | Needs DB? | Needs Discovery Service? | Needs SMD? | Needs Auth? |
|---------|-----------|--------------------------|------------|-------------|
| `serve` | Yes | N/A (is the service) | Yes (to register) | Yes (JWKS + creds) |
| `scan` | No | No | Yes (to register) | Optional (for `--cred-id`) |
| `probe` | No | No | No | Optional (for `--cred-id`) |
| `import` | No | No | Yes (to register) | No |
| `drivers` | No | No | No | No |
| `targets *` | No | Yes (API client) | No | Yes (JWT) |
| `scans *` | No | Yes (API client) | No | Yes (JWT) |

This dual-mode design means sysadmins can:
- Discover hardware **before** the full Chamicore stack is deployed (bootstrap scenario).
- Run quick one-off scans from a laptop or jump host without any infrastructure.
- Script bulk imports during initial cluster setup.
- Probe a single BMC to verify it is reachable and inspect its Redfish inventory.
- Still use the full service for production-grade scheduled scans with tracking and history.

### Discovery Drivers

Each driver implements a common interface for discovering components from a specific
source:

```go
// Driver discovers hardware from a specific source type.
type Driver interface {
    // Name returns the driver's unique identifier (e.g., "redfish", "ipmi").
    Name() string

    // Discover scans the given targets and returns discovered components.
    // Results are streamed via the channel to support large-scale scans.
    Discover(ctx context.Context, targets []Target, creds CredentialProvider) (<-chan DiscoveryResult, error)

    // Probe checks if a single endpoint is reachable and identifies its capabilities.
    Probe(ctx context.Context, target Target, creds CredentialProvider) (*ProbeResult, error)
}
```

| Driver | Source | Use Case | Priority |
|--------|--------|----------|----------|
| **Redfish** | BMC Redfish API | Modern server BMCs (iLO, iDRAC, OpenBMC) | v1 (required) |
| **Manual/API** | HTTP API | Operator-driven registration, scripts, automation | v1 (required) |
| **CSV/JSON Import** | File upload | Bulk registration from asset databases or spreadsheets | v1 (required) |
| **IPMI** | IPMI over LAN | Legacy BMCs without Redfish | v2 |
| **SNMP** | SNMP walks | Network switches, PDUs | v2 |
| **LLDP** | Switch LLDP tables | Passive topology discovery from switch port data | v2 |
| **SSDP/mDNS** | Multicast | Passive discovery of announcing devices | Future |

### Discovery Targets

A target defines what to scan and how:

```go
type Target struct {
    ID           string   `json:"id"`
    Name         string   `json:"name"`
    Driver       string   `json:"driver"`         // "redfish", "ipmi", etc.
    Addresses    []string `json:"addresses"`       // IPs, CIDR ranges, hostnames
    CredentialID string   `json:"credentialId"`    // Reference to credential in chamicore-auth
    Schedule     string   `json:"schedule"`        // Cron expression (empty = one-shot)
    Enabled      bool     `json:"enabled"`
}
```

Targets support both individual addresses and CIDR ranges. The scanner engine expands
ranges and manages concurrent connections with configurable parallelism.

### Scan Jobs

Scans are tracked as jobs with lifecycle state:

| State | Description |
|-------|-------------|
| `pending` | Job created, waiting to execute |
| `running` | Actively scanning targets |
| `completed` | Scan finished successfully |
| `failed` | Scan terminated with errors |
| `cancelled` | Scan cancelled by operator |

Each job tracks:
- Targets scanned, components discovered, components created/updated in SMD
- Errors encountered (per-target, not global failure)
- Start/end timestamps, duration
- Driver used

### Credential Management

Discovery delegates all credential management to chamicore-auth:

1. Operators create credential sets in chamicore-auth via the API or CLI:
   ```
   chamicore auth credentials create --name "bmc-default" --type device \
     --username admin --password <secret>
   ```

2. Credential sets are scoped and tagged (e.g., by rack, by BMC vendor, by network range).

3. Discovery targets reference credential sets by ID. At scan time, chamicore-discovery
   requests the actual credentials from chamicore-auth using its service account token.

4. Credentials are never stored in chamicore-discovery's database. They are fetched
   on-demand and held only in memory for the duration of a scan.

5. All credential access is audit-logged by chamicore-auth.

This approach extends chamicore-auth with a **device credential store** (new endpoints):

| Endpoint | Purpose | Auth |
|----------|---------|------|
| `POST /auth/v1/credentials` | Store a device credential set | Admin |
| `GET /auth/v1/credentials` | List credential sets (metadata only, no secrets) | Admin |
| `GET /auth/v1/credentials/{id}` | Retrieve credential (requires `read:credentials` scope) | Service account |
| `PUT /auth/v1/credentials/{id}` | Update credential | Admin |
| `DELETE /auth/v1/credentials/{id}` | Delete credential | Admin |

### State Change Notifications

When discovery detects changes (new components, state changes on known BMCs, components
going offline), it takes the following approach:

**Phase 1 (initial implementation):**
- Discovery updates SMD directly via HTTP API calls (POST for new, PATCH for changes).
- SMD records the state change in its database (e.g., a component's state changes from
  `Ready` to `Off`).
- Consumers that need to react to changes poll SMD (as they already do).

**Phase 2 (future - event-driven):**
- Introduce an event bus (likely NATS or similar lightweight messaging).
- Discovery publishes discovery events (`component.discovered`, `component.stateChanged`,
  `component.unreachable`).
- SMD and other services subscribe to relevant events.
- This decouples discovery from SMD and enables reactive workflows (e.g., automatic
  re-provisioning when a replaced node is discovered).
- The event-driven pattern is intentionally deferred until justified by operational
  requirements. The HTTP-based phase 1 approach is sufficient for initial deployments.

### API Endpoints

| Endpoint | Purpose | Auth |
|----------|---------|------|
| `POST /discovery/v1/scans` | Trigger a new scan (one-shot or against targets) | `write:discovery` |
| `GET /discovery/v1/scans` | List scan jobs with status and summary | `read:discovery` |
| `GET /discovery/v1/scans/{id}` | Get detailed scan status and results | `read:discovery` |
| `DELETE /discovery/v1/scans/{id}` | Cancel a running scan | `write:discovery` |
| `POST /discovery/v1/targets` | Create a discovery target | `write:discovery` |
| `GET /discovery/v1/targets` | List configured targets | `read:discovery` |
| `GET /discovery/v1/targets/{id}` | Get target details | `read:discovery` |
| `PUT /discovery/v1/targets/{id}` | Update target configuration | `write:discovery` |
| `DELETE /discovery/v1/targets/{id}` | Remove target | `write:discovery` |
| `POST /discovery/v1/targets/{id}/scan` | Trigger scan for a specific target | `write:discovery` |
| `GET /discovery/v1/drivers` | List available discovery drivers | `read:discovery` |
| `GET /health` | Liveness probe | None |
| `GET /readiness` | Readiness probe | None |
| `GET /version` | Build info | None |
| `GET /metrics` | Prometheus metrics | None |
| `GET /api/docs` | Swagger UI | None |
| `GET /api/openapi.yaml` | OpenAPI spec | None |

### Service-Specific Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `discovery_scans_total` | Counter | Total scans executed (by driver, status) |
| `discovery_scan_duration_seconds` | Histogram | Scan duration (by driver) |
| `discovery_components_discovered_total` | Counter | Components found (by driver, type) |
| `discovery_components_registered_total` | Counter | Components pushed to SMD (by action: create, update) |
| `discovery_targets_total` | Gauge | Configured targets (by driver, enabled/disabled) |
| `discovery_errors_total` | Counter | Discovery errors (by driver, error type) |
| `discovery_bmc_response_duration_seconds` | Histogram | BMC response time (by driver, vendor) |
| `discovery_active_scans` | UpDownCounter | Currently running scans |

### Repository Layout

```
chamicore-discovery/
  cmd/chamicore-discovery/main.go   # Cobra root command
  internal/
    cli/                            # CLI subcommands (Cobra commands)
      serve.go                      # `serve` - start HTTP API server
      scan.go                       # `scan` - standalone one-shot scan
      probe.go                      # `probe` - inspect a single endpoint
      import.go                     # `import` - bulk CSV/JSON import
      drivers.go                    # `drivers` - list available drivers
      targets.go                    # `targets` - CRUD via service API
      scans.go                      # `scans` - job management via service API
      output.go                     # Output formatting (table, JSON, YAML)
    config/config.go                # Configuration (env vars, CLI flags)
    server/server.go                # Chi router, middleware, handler registration
    server/handlers.go              # HTTP handlers for scans, targets
    store/store.go                  # Store interface (targets, scan jobs)
    store/postgres.go               # PostgreSQL implementation
    model/model.go                  # Domain types (Target, ScanJob, DiscoveryResult)
    scanner/scanner.go              # Scan engine (job scheduling, concurrency control)
    driver/driver.go                # Driver interface
    driver/redfish/redfish.go       # Redfish discovery driver
    driver/manual/manual.go         # Manual/API registration pass-through
    driver/csv/csv.go               # CSV/JSON bulk import driver
  pkg/
    client/client.go                # Typed HTTP client for this service's API
    types/types.go                  # Public request/response types
  api/openapi.yaml
  migrations/postgres/
    000001_init.up.sql
    000001_init.down.sql
  Dockerfile
  Makefile
  go.mod
  .goreleaser.yml
  .gitlab-ci.yml
  .golangci.yml
  README.md
  LICENSE
```

### Database Schema (discovery schema)

```sql
CREATE SCHEMA IF NOT EXISTS discovery;

CREATE TABLE discovery.targets (
    id            VARCHAR(255) PRIMARY KEY,
    name          VARCHAR(255) NOT NULL,
    driver        VARCHAR(63)  NOT NULL,
    addresses     JSONB        NOT NULL DEFAULT '[]',
    credential_id VARCHAR(255) NOT NULL DEFAULT '',
    schedule      TEXT         NOT NULL DEFAULT '',
    enabled       BOOLEAN      NOT NULL DEFAULT true,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE TABLE discovery.scan_jobs (
    id                 VARCHAR(255) PRIMARY KEY,
    target_id          VARCHAR(255) REFERENCES discovery.targets(id),
    driver             VARCHAR(63)  NOT NULL,
    state              VARCHAR(32)  NOT NULL DEFAULT 'pending',
    components_found   INT          NOT NULL DEFAULT 0,
    components_created INT          NOT NULL DEFAULT 0,
    components_updated INT          NOT NULL DEFAULT 0,
    errors             JSONB        NOT NULL DEFAULT '[]',
    started_at         TIMESTAMPTZ,
    completed_at       TIMESTAMPTZ,
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_scan_jobs_state ON discovery.scan_jobs (state);
CREATE INDEX idx_scan_jobs_target ON discovery.scan_jobs (target_id);
```

### Configuration

```bash
CHAMICORE_DISCOVERY_LISTEN_ADDR=:27776
CHAMICORE_DISCOVERY_DB_DSN=postgres://user:pass@localhost:5432/chamicore?sslmode=disable&search_path=discovery
CHAMICORE_DISCOVERY_LOG_LEVEL=info
CHAMICORE_DISCOVERY_JWKS_URL=http://localhost:3333/.well-known/jwks.json
CHAMICORE_DISCOVERY_SMD_URL=http://localhost:27779
CHAMICORE_DISCOVERY_AUTH_URL=http://localhost:3333
CHAMICORE_DISCOVERY_DEV_MODE=false
CHAMICORE_DISCOVERY_SCAN_CONCURRENCY=50          # Max concurrent BMC connections per scan
CHAMICORE_DISCOVERY_SCAN_TIMEOUT=30s             # Per-BMC timeout
CHAMICORE_DISCOVERY_SCAN_RETRY_MAX=3             # Retries per BMC on transient failure
CHAMICORE_DISCOVERY_METRICS_ENABLED=true
CHAMICORE_DISCOVERY_TRACES_ENABLED=true
CHAMICORE_DISCOVERY_PROMETHEUS_LISTEN_ADDR=:9090
```

### Updated Inter-Service Dependency Order

```
PostgreSQL
  -> chamicore-auth (needs DB)
    -> chamicore-smd (needs chamicore-auth for JWKS, needs DB)
      -> chamicore-bss (needs SMD + DB)
      -> chamicore-cloud-init (needs SMD + DB)
      -> chamicore-kea-sync (needs SMD API + Kea control agent)
      -> chamicore-ui (needs all service APIs + chamicore-auth)
    -> chamicore-discovery (needs chamicore-auth for JWKS + creds, needs SMD API)
```

Note that chamicore-discovery depends on chamicore-auth (for credentials and its own
JWT validation) and on chamicore-smd (to register discovered components), but nothing
depends on chamicore-discovery. It sits outside the critical boot path entirely.

## Consequences

### Positive

- **SMD becomes dramatically simpler.** Removing discovery logic (Redfish crawling, retry
  management, BMC connection pooling, hardware-specific response parsing) reduces SMD to a
  clean inventory CRUD service. Easier to test, maintain, and reason about.

- **Independent scaling.** Discovery can be scaled horizontally for large-scale scan
  operations without affecting inventory query performance. During steady-state, discovery
  can be scaled down to minimal resources.

- **No impact on boot path.** Discovery downtime does not affect booting. Existing inventory
  in SMD, boot parameters in BSS, and cloud-init payloads continue to function.

- **Pluggable discovery methods.** Sites can use whichever combination of drivers matches
  their hardware: Redfish for modern BMCs, IPMI for legacy, SNMP for switches, CSV import
  for asset database integration. New drivers can be added without modifying any other service.

- **Centralized credential management.** Device credentials stored in chamicore-auth benefit
  from the same access control, audit logging, and encryption that user and service credentials
  receive. No credential sprawl across services.

- **Follows established patterns.** The kea-sync service already proves the "watch one
  system, push to another" pattern works well. Discovery is the same pattern in reverse.

- **Clean contract.** Discovery communicates with SMD exclusively via SMD's public HTTP API.
  Any tool or script that can call SMD's API can register components, making the system
  open to third-party and custom discovery integrations.

- **Testability.** Discovery and inventory can be tested independently. SMD tests don't need
  mock BMCs. Discovery tests don't need a full inventory database - only a mock SMD HTTP
  endpoint.

- **Sysadmin-friendly CLI.** The dual-mode binary gives sysadmins a convenient command-line
  tool for day-to-day hardware management. Quick ad-hoc scans, BMC probes, and CSV imports
  work from any management node without requiring the full service stack. This is critical
  for initial cluster bootstrap (before Chamicore is fully deployed) and for troubleshooting.
  Standalone commands with `--dry-run` and human-readable output make discovery approachable
  for operators who prefer the terminal over APIs.

### Negative

- **Additional service to deploy.** One more container in the stack.
  - Mitigated: Discovery is optional. Sites that register components manually or via scripts
    don't need to deploy it. The Helm chart and Docker Compose will include it but it can be
    disabled.

- **Latency for discovery-to-registration.** An extra HTTP hop (discovery -> SMD) compared
  to in-process registration.
  - Mitigated: This latency is irrelevant. Discovery is a background operation, not a
    latency-sensitive path. A few milliseconds of HTTP overhead per component is negligible
    compared to the seconds-per-BMC Redfish crawl time.

- **Credential indirection.** Fetching credentials from chamicore-auth adds a dependency
  and round-trip.
  - Mitigated: Credentials can be cached in memory for the duration of a scan. The auth
    service is already a dependency for JWT validation. The security benefit of centralized
    credential management outweighs the minor complexity.

- **Amends ADR-002.** The service count increases from 9 to 10 repositories.
  - Accepted: The architectural benefit of separation of concerns justifies the additional
    repository. The service follows all established patterns (template, conventions,
    shared library) so the maintenance cost is incremental, not multiplicative.

### Neutral

- The Redfish driver implementation is comparable in scope to what would have been built
  inside SMD. The work is not duplicated; it is relocated.
- The event-driven notification pattern (phase 2) is a natural extension point. The service
  architecture supports adding an event bus later without redesigning the core.
- chamicore-deploy (Helm charts, Docker Compose) needs to add the new service. This follows
  the existing pattern for adding services.
- The CLI gains a new subcommand group: `chamicore discovery scans`, `chamicore discovery targets`.
- Load testing gains new scenarios: bulk discovery under load, concurrent scans.
