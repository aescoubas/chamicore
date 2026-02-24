# ADR-017: Power Control Service (PCS-Compatible) and Shared Redfish Library

## Status

Accepted

## Date

2026-02-24

## Context

Chamicore currently has discovery, inventory, boot, and auth services, but no dedicated
power-control service for node power operations (on/off/reboot/reset) through Redfish.

This creates three concrete issues:

1. Operators cannot run power workflows through Chamicore APIs/CLI.
2. Redfish logic exists only in discovery internals (`chamicore-discovery/internal/driver/redfish`),
   so a new power service would duplicate transport/auth logic if implemented directly.
3. Local libvirt validation needs a Redfish control-plane simulator so VM power operations
   can be tested end-to-end in development.

Additionally, the project wants compatibility with OpenCHAMI PCS endpoint style while
remaining aligned with Chamicore architecture conventions:
- service-local PostgreSQL schema
- envelope + RFC 9457 API style where applicable
- JWT scopes via chamicore-auth
- event publication via outbox + NATS
- no cross-service DB access

## Decision

Introduce a new service `chamicore-power` (API prefix `/power/v1`) that provides
PCS-style transition/status semantics backed by Redfish, and extract shared Redfish
transport and operation primitives into `shared/chamicore-lib/redfish`.

### 1. Service and API Shape

- New service: `chamicore-power`.
- Canonical service prefix: `/power/v1`.
- PCS-compatible primary endpoints (shape and behavior):
  - `POST /transitions` (start async transition task)
  - `GET /transitions` (list transition tasks)
  - `GET /transitions/{transitionID}` (transition status/details)
  - `DELETE /transitions/{transitionID}` (abort in-progress transition)
  - `GET /power-status` (current power state)
- V1 convenience endpoints are also added:
  - `POST /actions/on`
  - `POST /actions/off`
  - `POST /actions/reboot`
  - `POST /actions/reset`
- V1 scope is power operations only (no virtual media / boot override / OEM extensions).

### 2. Supported Operations (V1)

V1 supports:
- `On`
- `ForceOff`
- `GracefulShutdown`
- `GracefulRestart`
- `ForceRestart`
- `Nmi`

Request operation aliases are case-insensitive and normalized internally.

### 3. Async Job Model, Bulk, and Result Semantics

- Transition APIs are asynchronous.
- `POST /transitions` returns a task/transition identifier immediately.
- Per-node execution results are tracked and returned in transition details.
- Bulk operations are supported in V1.
- Default max nodes per request: `20`, configurable via env var.
- Bulk failures are per-node, not all-or-nothing.

### 4. Verification, Retry, and Concurrency Policy

- Final-state verification is mandatory after issuing a Redfish action.
- Default verification window: `90s`, configurable.
- Recommended retry policy for transient errors:
  - max attempts: `3`
  - exponential backoff with jitter (base `250ms`)
  - retry only transport/timeouts and retryable HTTP responses
- Concurrency defaults:
  - global worker limit: `20` (configurable)
  - per-BMC operation limit: `1` (serialized per BMC, configurable)

### 5. Source of Truth, Mapping, and Missing Mapping Behavior

- SMD remains the source of truth for hardware topology.
- Node -> BMC mapping is derived from SMD component relationships and interfaces.
- `chamicore-power` keeps a local denormalized mapping cache in its own schema, synchronized
  from SMD (polling + ETag, event-driven updates when available).
- If mapping is missing for a requested node, V1 fails fast with explicit per-node errors.
  V1 does not auto-trigger discovery scans.

### 6. Credential and TLS Policy

- Device credentials are referenced by ID and sourced from `chamicore-auth`.
- Credential binding is per BMC (not per node) in V1.
- `chamicore-power` fetches credential material from auth at execution time.
- TLS verification defaults to enabled.
- Per-BMC insecure override is supported for known environments.
- Global insecure override is allowed only as explicit configuration (primarily dev/test).
- Custom CA bundles are out of scope for V1.

### 7. SMD State Sync and Event Publication

- On successful verified transitions, `chamicore-power` patches SMD component state.
- Transition lifecycle and per-node outcomes are emitted to NATS using transactional outbox.
- Event emission follows existing CloudEvents-compatible conventions in `chamicore-lib/events`.

### 8. AuthZ and Audit Requirements

- New scopes:
  - `read:power` for transition/status reads
  - `write:power` for transition/action mutation endpoints
  - `admin:power` for power service administrative resources (mapping/endpoint config)
- Every transition and node task records structured audit fields:
  - actor subject/type
  - request ID and trace context
  - operation type
  - node ID + resolved BMC ID/endpoint
  - dry-run flag
  - attempt count
  - timestamps (queued/started/completed)
  - latency
  - final status + error detail (if any)

### 9. CLI

- Add `power` command group to `chamicore-cli`.
- Primary user shape:
  - `chamicore power on|off|reboot|reset`
  - `chamicore power status`
  - `chamicore power transition list|get|abort|wait`
- Group operations are first-class (`--group <name>`), expanding members from SMD.
- Dry-run supported for command planning without issuing Redfish actions.

### 10. Local Virtualization (libvirt)

- Use one shared Sushy Tools dynamic emulator instance for all local libvirt domains.
- Integrate Sushy startup into `make compose-vm-up`.
- `compose-vm-up` brings up compose stack, Sushy emulator, and VM resources.
- This is the default local validation path for power operations.

## Consequences

### Positive

- Adds missing operational capability: node power control via Chamicore API/CLI.
- Avoids Redfish code duplication by extracting shared library logic.
- Preserves architecture boundaries: SMD as source of truth, auth-managed credentials,
  service-local schema, outbox events.
- Enables realistic local end-to-end testing via libvirt + Sushy.
- Supports scale-oriented operations with async jobs, bulk, retry, and concurrency controls.

### Negative

- Introduces a new microservice and schema, increasing system complexity.
- Requires careful mapping synchronization and reconciliation logic with SMD.
- Adds additional operational tuning surface (timeouts, retries, workers, per-BMC limits).
- PCS compatibility while adding Chamicore conventions may require adapter logic in API docs/CLI.

### Neutral

- V1 intentionally excludes power-cap, virtual media, and OEM vendor extensions.
- Missing-mapping behavior is explicit fail-fast in V1; discovery-trigger automation can be
  introduced later as an additive feature.
