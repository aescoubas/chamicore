# ADR-015: Event-Driven Architecture via NATS JetStream

## Status

Proposed (implements Phase 2 of [ADR-014](ADR-014-boot-path-data-flow.md))

## Date

2026-02-18

## Context

ADR-014 established a two-phase change propagation strategy for Chamicore:

- **Phase 1 (current)**: Services poll SMD for changes using ETags. Maximum propagation
  delay equals the poll interval (default 30 seconds). This is sufficient for initial
  deployments where provisioning happens well before boot storms.

- **Phase 2 (this ADR)**: Event-driven notifications for sub-second propagation.

Phase 1 has known limitations that will become operational pain points as deployments
mature:

1. **Propagation delay on hardware replacement.** When an operator swaps a failed blade
   (new MAC address), three sync loops must fire before the replacement is bootable:
   BSS (update MAC in boot params), Cloud-Init (update metadata), and Kea-Sync (push
   new DHCP reservation). At 30-second intervals, worst case is ~90 seconds. Operators
   must remember to trigger force sync on each service or wait.

2. **No zero-touch provisioning.** When Discovery finds a new node, an operator must
   manually configure boot params in BSS and payloads in Cloud-Init. There is no mechanism
   for services to react automatically to new components.

3. **No real-time UI.** The Web UI must poll service APIs for updates. Dashboard views
   are stale by seconds to minutes. Operators refreshing the page to see changes is a
   poor experience.

4. **Token revocation delay.** The in-memory revocation cache (ADR-014) is refreshed every
   10 seconds. A revoked token remains usable for up to that window.

5. **No audit trail.** There is no unified log of all state changes across services.
   Answering "what happened to this node at 3pm?" requires correlating logs from multiple
   services.

6. **Polling overhead at scale.** With 50,000+ components, each sync cycle fetches a
   substantial payload from SMD even when nothing has changed. ETags help (304 responses
   are cheap), but the sync loop still consumes CPU cycles, network bandwidth, and
   scheduler attention for zero-value work most of the time.

### Why NATS JetStream

Several messaging systems were considered:

| System | Pros | Cons |
|--------|------|------|
| **NATS JetStream** | Single Go binary (~20MB), Go-native client, persistence + replay, subject-based routing, consumer groups, simple operations | Less mature than Kafka for very high throughput |
| Apache Kafka | Battle-tested at extreme scale, strong ordering | JVM-based, heavy operational burden (ZooKeeper/KRaft), overkill for our event volume |
| Redis Streams | Simple, widely deployed | No native Go client for streams, persistence less robust, memory-bound |
| RabbitMQ | Mature, flexible routing | Erlang runtime, more complex topology, heavier than NATS |
| PostgreSQL LISTEN/NOTIFY | Zero new infrastructure | No persistence, no replay, lost if no listener is connected, no consumer groups |

NATS JetStream is the best fit because:
- It is a single statically-linked binary with no runtime dependencies (like our Go services).
- The Go client library is first-party and idiomatic.
- JetStream provides persistent streams with configurable retention, replay from any
  sequence, durable consumer groups, and exactly-once delivery semantics.
- Subject-based routing with wildcards maps naturally to our event taxonomy.
- It is lightweight enough for small deployments (single instance) and scales to clustered
  mode for production (3-node RAFT consensus).
- Operational simplicity aligns with Chamicore's goal of reducing infrastructure burden
  compared to upstream OpenCHAMI.

## Decision

Introduce NATS JetStream as an event bus for inter-service change propagation, real-time
notifications, and audit logging. Events replace the polling sync loops from ADR-014
Phase 1 while preserving all existing HTTP APIs and the self-sufficient boot-path
architecture.

### Core Principles

1. **Events are not on the hot path.** Booting nodes still query BSS and Cloud-Init
   directly (local DB, no cross-service calls, no event bus involvement). Events handle
   background propagation between services. This principle from ADR-014 is unchanged.

2. **Events replace polling, not HTTP APIs.** All operator and management CRUD remains
   synchronous REST. Discovery still registers components in SMD via HTTP. Events are
   a notification sidecar, not a new transport for commands.

3. **Graceful degradation.** If NATS is unavailable, services fall back to polling sync
   loops (Phase 1 behavior). The event bus improves latency but is never a hard dependency.
   The boot path is completely unaffected by NATS availability.

4. **At-least-once delivery with idempotent consumers.** Events may be delivered more than
   once (network retries, consumer restarts). Every consumer must handle duplicates safely
   (typically via upserts keyed on component ID).

5. **Transactional outbox for reliable publishing.** Events are written to a database
   outbox table in the same transaction as the state change. A background publisher
   process reads the outbox and publishes to NATS. This guarantees no phantom events
   (event without DB commit) and no lost events (DB commit without event).

### Event Taxonomy

Every meaningful state change in the system maps to an event type. Events are organized
into domains matching their source service:

#### Discovery Domain

| Event Type | Payload | Produced When |
|-----------|---------|---------------|
| `chamicore.discovery.component.discovered` | Component snapshot (ID, type, MAC, BMC endpoint) | New hardware found during scan |
| `chamicore.discovery.component.rediscovered` | Component snapshot with changed fields | Known hardware re-scanned, attributes changed |
| `chamicore.discovery.component.unreachable` | Component ID, last-seen timestamp | BMC stopped responding to probes |
| `chamicore.discovery.scan.completed` | Scan ID, summary (found/created/updated/errors) | Scan job finished |
| `chamicore.discovery.scan.failed` | Scan ID, error details | Scan job failed |

#### SMD Domain (Inventory)

| Event Type | Payload | Produced When |
|-----------|---------|---------------|
| `chamicore.smd.component.created` | Full component snapshot | Component registered via API |
| `chamicore.smd.component.updated` | Component ID, changed fields, snapshot | Component fields modified |
| `chamicore.smd.component.deleted` | Component ID | Component removed |
| `chamicore.smd.group.membership.changed` | Group name, added/removed component IDs | Group membership modified |
| `chamicore.smd.interface.created` | Interface snapshot (ID, MAC, IP, component ID) | Network interface added |
| `chamicore.smd.interface.updated` | Interface ID, changed fields, snapshot | Network interface modified |
| `chamicore.smd.interface.deleted` | Interface ID, component ID | Network interface removed |

#### BSS Domain

| Event Type | Payload | Produced When |
|-----------|---------|---------------|
| `chamicore.bss.bootparams.created` | Component ID, boot params snapshot | Boot params configured |
| `chamicore.bss.bootparams.updated` | Component ID, changed fields, snapshot | Boot params modified |
| `chamicore.bss.bootparams.deleted` | Component ID | Boot params removed |
| `chamicore.boot.requested` | Component ID, MAC, timestamp | Node fetched a boot script |

#### Cloud-Init Domain

| Event Type | Payload | Produced When |
|-----------|---------|---------------|
| `chamicore.cloudinit.payload.created` | Component ID, payload metadata | Payload configured |
| `chamicore.cloudinit.payload.updated` | Component ID, changed fields | Payload modified |
| `chamicore.cloudinit.payload.deleted` | Component ID | Payload removed |
| `chamicore.cloudinit.requested` | Component ID, timestamp | Node fetched cloud-init data |

#### Auth Domain

| Event Type | Payload | Produced When |
|-----------|---------|---------------|
| `chamicore.auth.token.issued` | Subject, roles, scopes, expiry (no secret material) | JWT created |
| `chamicore.auth.token.revoked` | JTI, subject, expiry | JWT revoked by admin |
| `chamicore.auth.policy.changed` | Policy ID, action (create/update/delete) | Casbin policy modified |
| `chamicore.auth.credential.created` | Credential ID, name, type (no secrets) | Device credential stored |
| `chamicore.auth.credential.updated` | Credential ID, name (no secrets) | Device credential rotated |
| `chamicore.auth.credential.deleted` | Credential ID | Device credential removed |

#### Kea-Sync Domain

| Event Type | Payload | Produced When |
|-----------|---------|---------------|
| `chamicore.keasync.reservation.pushed` | Component ID, MAC, IP | DHCP reservation sent to Kea |
| `chamicore.keasync.reservation.failed` | Component ID, error | Push to Kea failed |

### Event Envelope

All events follow a consistent envelope compatible with the
[CloudEvents](https://cloudevents.io/) specification (v1.0):

```json
{
  "specversion": "1.0",
  "id": "evt-a1b2c3d4e5f6",
  "type": "chamicore.smd.component.updated",
  "source": "chamicore-smd",
  "time": "2026-02-18T14:30:00.123Z",
  "subject": "node-a1b2c3",
  "datacontenttype": "application/json",
  "traceparent": "00-abcdef1234567890abcdef1234567890-1234567890abcdef-01",
  "data": {
    "componentId": "node-a1b2c3",
    "changedFields": ["role", "state"],
    "snapshot": {
      "id": "node-a1b2c3",
      "type": "Node",
      "role": "Compute",
      "state": "Ready",
      "mac": "AA:BB:CC:DD:EE:FF",
      "nid": 1001
    }
  }
}
```

| Field | Description |
|-------|-------------|
| `specversion` | CloudEvents spec version (`"1.0"`) |
| `id` | Unique event identifier (UUID or prefixed random) |
| `type` | Dot-delimited event type from the taxonomy above |
| `source` | Service that produced the event |
| `time` | ISO 8601 timestamp of the state change |
| `subject` | The primary entity ID (component ID, scan ID, JTI, etc.) |
| `datacontenttype` | Always `application/json` |
| `traceparent` | W3C Trace Context header for distributed tracing correlation |
| `data.changedFields` | Array of field names that changed (for update events) |
| `data.snapshot` | Current state of the entity after the change |

Design choices:
- **`changedFields`** lets consumers skip irrelevant updates (e.g., BSS only cares about
  MAC and role changes, not state changes).
- **`snapshot`** carries enough state for consumers to act without calling back to the
  source service. This avoids a "thundering herd" of HTTP GETs after an event.
- **`traceparent`** propagates distributed tracing through the event bus, connecting
  the HTTP request that triggered the change to all downstream event processing.
- **No secret material** in events. Auth events include subject and scope metadata but
  never tokens, passwords, or credential values.

### NATS JetStream Configuration

#### Stream

A single persistent stream captures all Chamicore events:

```
Stream: CHAMICORE_EVENTS
  Subjects: chamicore.>
  Storage: File
  Retention: Limits (time-based)
  MaxAge: 7 days
  MaxBytes: 10 GB
  Replicas: 1 (single node) or 3 (clustered production)
  Discard: Old (oldest messages discarded when limits reached)
  Deduplication Window: 2 minutes (for publisher retries)
```

Seven-day retention provides sufficient replay window for:
- Services recovering from downtime (replay missed events).
- Debugging and incident investigation.
- Audit consumers that process events asynchronously.

#### Consumers

Each subscribing service creates a **durable consumer** with explicit acknowledgment:

| Consumer | Subscriptions | Delivery | Ack Policy | Purpose |
|----------|--------------|----------|------------|---------|
| `bss-sync` | `chamicore.smd.component.>`, `chamicore.smd.interface.>` | Pull, queue group | Explicit | BSS MAC/role sync |
| `cloudinit-sync` | `chamicore.smd.component.>` | Pull, queue group | Explicit | Cloud-Init metadata sync |
| `kea-sync` | `chamicore.smd.component.>`, `chamicore.smd.interface.>` | Pull, queue group | Explicit | Kea DHCP reservation sync |
| `auth-revocations` | `chamicore.auth.token.revoked` | Push, broadcast | Explicit | Token revocation propagation |
| `ui-realtime` | `chamicore.>` | Push, ephemeral | None | UI WebSocket push |
| `audit-logger` | `chamicore.>` | Pull, durable | Explicit | Audit trail persistence |

Key properties:
- **Queue groups** ensure that if BSS runs 3 replicas, each event is processed by exactly
  one replica (load-balanced), not all three.
- **`auth-revocations`** is broadcast (not queue group) because every service replica
  must update its local revocation cache.
- **`ui-realtime`** is ephemeral (not durable) because missed events while the UI is
  disconnected are not critical; the UI does a full fetch on reconnect.
- **`audit-logger`** is durable with explicit ack, ensuring no events are lost even if
  the audit service restarts.

#### Subject Hierarchy and Filtering

The dot-delimited subject hierarchy enables fine-grained subscriptions:

```
chamicore.>                                    # Everything (UI, audit)
chamicore.smd.>                                # All SMD events
chamicore.smd.component.>                      # All component events
chamicore.smd.component.created.>              # Only component creations
chamicore.smd.component.updated.node-a1b2c3    # Updates for a specific component
chamicore.auth.token.revoked.>                 # All token revocations
chamicore.discovery.scan.>                     # All scan lifecycle events
```

### Transactional Outbox Pattern

Services must guarantee that events are published if and only if the corresponding
database write committed. The transactional outbox pattern achieves this:

```
┌─────────────────────────────────────────────────┐
│ Service (e.g., SMD)                             │
│                                                 │
│  Handler: POST /hsm/v2/State/Components         │
│    │                                            │
│    ▼                                            │
│  BEGIN TRANSACTION                              │
│    INSERT INTO smd.components (...)             │
│    INSERT INTO smd.outbox (event_type, ...)     │
│  COMMIT                                         │
│                                                 │
│  Background Publisher Goroutine                 │
│    │                                            │
│    ├─ Polls outbox (or LISTEN/NOTIFY)           │
│    ├─ Publishes to NATS JetStream               │
│    ├─ On publish ack: DELETE FROM smd.outbox     │
│    └─ On NATS failure: retry with backoff        │
│        (events accumulate in outbox until NATS   │
│         recovers)                                │
└─────────────────────────────────────────────────┘
```

Each service schema includes an outbox table:

```sql
CREATE TABLE <schema>.outbox (
    id           BIGSERIAL   PRIMARY KEY,
    event_type   TEXT        NOT NULL,
    subject      TEXT        NOT NULL,
    data         JSONB       NOT NULL,
    trace_parent TEXT        NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at TIMESTAMPTZ
);

CREATE INDEX idx_outbox_unpublished ON <schema>.outbox (id)
    WHERE published_at IS NULL;
```

The background publisher:
1. Queries `SELECT * FROM outbox WHERE published_at IS NULL ORDER BY id LIMIT 100`.
2. Publishes each event to NATS with the message deduplication ID set to the outbox row ID.
3. On NATS publish acknowledgment, sets `published_at = NOW()`.
4. A cleanup job periodically deletes published rows older than 1 hour.

If NATS is unavailable, events accumulate in the outbox. When NATS recovers, the publisher
drains the backlog in order. The deduplication window (2 minutes) prevents duplicate
delivery during publisher retries.

For low-latency pickup, the publisher can use PostgreSQL `LISTEN/NOTIFY`:
```sql
-- Trigger on outbox INSERT
CREATE OR REPLACE FUNCTION notify_outbox() RETURNS trigger AS $$
BEGIN
    PERFORM pg_notify('outbox_' || TG_TABLE_SCHEMA, NEW.id::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER outbox_notify AFTER INSERT ON <schema>.outbox
    FOR EACH ROW EXECUTE FUNCTION notify_outbox();
```

This wakes the publisher goroutine immediately on insert instead of waiting for the next
poll cycle, reducing publish latency to milliseconds.

### Graceful Degradation

The event bus is an optimization, not a hard dependency:

```
NATS available:
  Events propagate in < 100ms
  Polling sync loops disabled (or reduced to infrequent reconciliation)

NATS unavailable:
  Outbox accumulates unpublished events
  Services detect NATS disconnection and activate polling sync loops
  Boot storm completely unaffected (events are not on the hot path)
  When NATS recovers:
    Outbox drains (events published in order)
    Consumers replay from last acknowledged sequence
    Polling sync loops deactivate

NATS + PostgreSQL available, SMD down:
  Boot storm still works (BSS/Cloud-Init serve from local data)
  Events from SMD stop (no state changes to propagate)
  Discovery events still flow (discovery → NATS, but SMD registration fails)
```

### Producer/Consumer Map

```
┌─────────────────┬──────────────────────────────────┬──────────────────────────────────────────────┐
│ Service         │ Produces                         │ Consumes                                     │
├─────────────────┼──────────────────────────────────┼──────────────────────────────────────────────┤
│ Discovery       │ component.discovered             │ (none — initiator)                           │
│                 │ component.rediscovered            │                                              │
│                 │ component.unreachable             │                                              │
│                 │ scan.completed, scan.failed       │                                              │
├─────────────────┼──────────────────────────────────┼──────────────────────────────────────────────┤
│ SMD             │ component.created/updated/deleted │ discovery.component.discovered (auto-register│
│                 │ group.membership.changed          │   if enabled)                                │
│                 │ interface.created/updated/deleted │ discovery.component.rediscovered (auto-update)│
├─────────────────┼──────────────────────────────────┼──────────────────────────────────────────────┤
│ BSS             │ bootparams.created/updated/deleted│ smd.component.created (apply role defaults)  │
│                 │ boot.requested                    │ smd.component.updated (MAC/role sync)        │
│                 │                                   │ smd.component.deleted (cleanup params)       │
│                 │                                   │ smd.interface.updated (MAC change)            │
├─────────────────┼──────────────────────────────────┼──────────────────────────────────────────────┤
│ Cloud-Init      │ payload.created/updated/deleted   │ smd.component.created (apply role defaults)  │
│                 │ cloudinit.requested               │ smd.component.updated (metadata sync)        │
│                 │                                   │ smd.component.deleted (cleanup payloads)     │
├─────────────────┼──────────────────────────────────┼──────────────────────────────────────────────┤
│ Kea-Sync        │ reservation.pushed/failed         │ smd.component.created (new reservation)      │
│                 │                                   │ smd.component.updated (MAC/IP change)        │
│                 │                                   │ smd.component.deleted (remove reservation)   │
│                 │                                   │ smd.interface.created/updated/deleted         │
├─────────────────┼──────────────────────────────────┼──────────────────────────────────────────────┤
│ Auth            │ token.issued, token.revoked       │ auth.policy.changed (self: hot-reload)       │
│                 │ policy.changed                    │                                              │
│                 │ credential.created/updated/deleted│                                              │
├─────────────────┼──────────────────────────────────┼──────────────────────────────────────────────┤
│ UI              │ (none — read-only consumer)       │ chamicore.> (all events for live dashboard)  │
├─────────────────┼──────────────────────────────────┼──────────────────────────────────────────────┤
│ Audit Logger    │ (none — archival consumer)        │ chamicore.> (all events for compliance log)  │
└─────────────────┴──────────────────────────────────┴──────────────────────────────────────────────┘
```

### Key Workflows Enabled

#### Zero-Touch Provisioning (Discovery → Bootable in < 1 Second)

When BSS and Cloud-Init have role-based default configurations, a newly discovered node
becomes fully bootable without operator intervention:

```
t=0.00s  Discovery finds new BMC at 10.0.0.42
         Publishes: component.discovered {type: Node, mac: AA:BB:..., role: Compute}

t=0.01s  SMD receives event, creates component node-a1b2c3
         Publishes: component.created {id: node-a1b2c3, role: Compute, mac: AA:BB:...}

t=0.02s  BSS receives component.created
         Role = "Compute" → applies default Compute boot params
         Inserts into bss.boot_params with MAC and kernel/initrd/cmdline

t=0.02s  Cloud-Init receives component.created (parallel with BSS)
         Role = "Compute" → applies default Compute payload template
         Inserts into cloudinit.payloads

t=0.02s  Kea-Sync receives component.created (parallel)
         Pushes DHCP reservation (MAC → IP) to Kea

t=0.50s  Node is fully bootable:
         ✓ DHCP reservation in Kea
         ✓ Boot params in BSS
         ✓ Cloud-init payload in Cloud-Init

         (vs. ~90s with polling: 3 sync loops × 30s worst case)
```

This workflow is **opt-in**: BSS and Cloud-Init only auto-apply defaults if role-based
default configurations exist. If no defaults are configured, the events are acknowledged
but no action is taken (the operator must still configure boot params manually).

#### Reactive Hardware Replacement

Operator swaps a failed blade. The new blade has a different MAC address:

```
t=0.00s  Discovery re-scans rack 12, detects new MAC on known BMC
         Publishes: component.rediscovered {id: node-a1b2c3, newMac: BB:CC:DD:...}

t=0.01s  SMD updates MAC for node-a1b2c3
         Publishes: component.updated {changedFields: ["mac"], snapshot: {mac: BB:CC:DD:...}}

t=0.02s  BSS updates MAC in bss.boot_params (boot params unchanged)
t=0.02s  Kea-Sync deletes old reservation, pushes new one to Kea
t=0.50s  Replacement blade is bootable
```

#### Instant Token Revocation

```
t=0.00s  Admin revokes token: POST /auth/v1/token/revoke {jti: "xyz"}
         Auth writes to revoked_tokens table
         Publishes: token.revoked {jti: "xyz", expiresAt: "..."}

t=0.01s  All service replicas receive event via broadcast consumer
         Each updates its in-memory revocation cache

t=0.02s  Next request with revoked token is rejected
         (vs. up to 10s with polling-based cache refresh)
```

#### Live UI Dashboard

```
UI backend subscribes to chamicore.> (ephemeral consumer)

Any event in the system:
  NATS delivers to UI backend
  UI backend transforms to frontend-friendly format
  Pushes to browser via WebSocket

Examples:
  Discovery scan completes → "42 new nodes discovered in rack 12"
  Node fetches boot script → boot counter increments on dashboard
  Component state changes → inventory view updates without page refresh
  Kea-Sync pushes reservation → DHCP status view updates
```

#### Audit Trail

```
Audit Logger subscribes to chamicore.> (durable consumer, explicit ack)
Persists every event to an audit table (or external log store)

Enables:
  "Show all changes to node-a1b2c3 in the last 24 hours"
  "Who changed the boot params for rack 12?"
  "How many nodes booted between 3am and 4am?"
  "List all policy changes this week"
  "When was this component first discovered?"
```

### What Changes vs. What Stays the Same

| Aspect | Before (Phase 1) | After (Phase 2) |
|--------|-------------------|------------------|
| BSS ← SMD sync | Polling every 30s | NATS event consumer (< 100ms) |
| Cloud-Init ← SMD sync | Polling every 30s | NATS event consumer (< 100ms) |
| Kea-Sync ← SMD sync | Polling loop | NATS event consumer (< 100ms) |
| Token revocation propagation | DB poll every 10s | NATS broadcast (< 10ms) |
| UI updates | Page refresh / API polling | WebSocket push via events |
| Audit trail | Scattered in per-service logs | Unified event log |
| Zero-touch provisioning | Not possible | Automatic via event chain |
| Boot storm hot path | BSS/Cloud-Init → local DB | **Unchanged** (no events on hot path) |
| Operator CRUD APIs | Synchronous HTTP REST | **Unchanged** |
| Discovery → SMD registration | HTTP POST | **Unchanged** (HTTP, publishes event after) |
| JWT validation | Cached JWKS, local | **Unchanged** |
| Database schemas | Per-service schemas | **Unchanged** (+ outbox table per schema) |

### Implementation in chamicore-lib

Event infrastructure lives in shared library packages:

| Package | Purpose |
|---------|---------|
| `events/` | Event envelope types (CloudEvents-compatible), marshaling, common helpers |
| `events/nats/` | NATS JetStream publisher, consumer, connection management, health checks |
| `events/outbox/` | Outbox table management, background publisher, LISTEN/NOTIFY integration |

```go
// Initialize in main.go:
pub, err := events.NewPublisher(events.PublisherConfig{
    NATSUrl:    cfg.NATSUrl,
    DB:         db,
    SchemaName: "smd",
    ServiceName: "chamicore-smd",
})
defer pub.Close()

// Publish in a handler (writes to outbox in the same transaction):
func (s *Server) handleCreateComponent(w http.ResponseWriter, r *http.Request) {
    tx, _ := s.db.BeginTx(ctx, nil)
    // ... insert component ...
    s.publisher.PublishTx(ctx, tx, events.Event{
        Type:    "chamicore.smd.component.created",
        Subject: component.ID,
        Data:    componentSnapshot,
    })
    tx.Commit()
}

// Subscribe in main.go:
sub, err := events.NewConsumer(events.ConsumerConfig{
    NATSUrl:      cfg.NATSUrl,
    StreamName:   "CHAMICORE_EVENTS",
    ConsumerName: "bss-sync",
    Subjects:     []string{"chamicore.smd.component.>", "chamicore.smd.interface.>"},
    QueueGroup:   "bss",
    Handler:      s.handleSMDEvent,
})
defer sub.Close()

func (s *Server) handleSMDEvent(ctx context.Context, evt events.Event) error {
    switch evt.Type {
    case "chamicore.smd.component.created":
        return s.onComponentCreated(ctx, evt)
    case "chamicore.smd.component.updated":
        return s.onComponentUpdated(ctx, evt)
    case "chamicore.smd.component.deleted":
        return s.onComponentDeleted(ctx, evt)
    }
    return nil // ignore unknown event types
}
```

### Event-Related Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `events_published_total` | Counter | Events published (by type, service) |
| `events_publish_duration_seconds` | Histogram | Time from handler to NATS ack |
| `events_consumed_total` | Counter | Events consumed (by type, consumer) |
| `events_consume_duration_seconds` | Histogram | Event handler processing time |
| `events_consume_errors_total` | Counter | Failed event processing attempts |
| `events_outbox_pending` | Gauge | Unpublished events in outbox |
| `events_outbox_lag_seconds` | Gauge | Age of oldest unpublished outbox entry |
| `nats_connection_status` | Gauge | 1 = connected, 0 = disconnected |

`events_outbox_pending > 0` for more than a few seconds indicates NATS connectivity
issues and should trigger an alert.

### Configuration

```bash
# NATS connection (all services)
CHAMICORE_NATS_URL=nats://localhost:4222
CHAMICORE_NATS_CREDS_FILE=/etc/chamicore/nats.creds  # optional: NATS auth

# Event publishing (services that produce events)
CHAMICORE_EVENTS_ENABLED=true                        # Master toggle (default: true if NATS_URL set)
CHAMICORE_EVENTS_OUTBOX_POLL_INTERVAL=100ms          # Outbox poll fallback (if LISTEN/NOTIFY unavailable)
CHAMICORE_EVENTS_OUTBOX_CLEANUP_INTERVAL=5m          # Delete published outbox rows older than 1h

# Event consumption (services that consume events)
CHAMICORE_BSS_EVENTS_ENABLED=true                    # Enable event-based sync (disables polling)
CHAMICORE_BSS_EVENTS_FALLBACK_POLL_INTERVAL=30s      # Polling interval when NATS is down
```

When `CHAMICORE_NATS_URL` is not set, the event system is completely disabled and services
use Phase 1 polling exclusively. This allows deployments to opt in to events incrementally.

### Deployment

NATS is added to the `chamicore-deploy` stack:

**Docker Compose (development):**

```yaml
nats:
  image: nats:2-alpine
  command: ["--jetstream", "--store_dir", "/data"]
  ports:
    - "4222:4222"   # Client connections
    - "8222:8222"   # HTTP monitoring
  volumes:
    - nats-data:/data
```

**Helm (production):**

The NATS Helm chart deploys a 3-node cluster with JetStream enabled. The stream and
consumer configurations are applied via a Kubernetes Job on install/upgrade.

NATS is an **optional component**. The Helm chart's `values.yaml` includes
`events.enabled: false` by default. Operators opt in when ready.

### Trigger Conditions for Adoption

Do not deploy the event bus until at least one of these conditions is met:

1. **Polling delay is causing operational pain.** Operators are routinely running force
   sync after changes and complaining about the delay.
2. **Zero-touch provisioning is required.** The site wants discovered nodes to become
   bootable automatically without operator intervention.
3. **The UI needs real-time updates.** Stakeholders want a live dashboard that reflects
   changes as they happen.
4. **Audit/compliance requirements** demand a unified event history across all services.
5. **Token revocation latency** (10 seconds) is unacceptable for the security posture.

Until then, Phase 1 polling (ADR-014) works correctly and requires no additional
infrastructure.

## Consequences

### Positive

- **Sub-second change propagation.** MAC changes, role updates, new component registrations
  propagate to all downstream services in under 100 milliseconds (vs. up to 90 seconds with
  polling).

- **Zero-touch provisioning.** Newly discovered nodes become bootable automatically when
  role-based defaults exist. Reduces operator toil for large-scale deployments.

- **Real-time UI.** Dashboard views update instantly via WebSocket, eliminating the need
  for manual page refreshes and periodic API polling.

- **Unified audit trail.** A single event stream captures every state change across all
  services, enabling comprehensive compliance reporting and incident investigation.

- **Instant token revocation.** Revoked tokens are blocked within milliseconds across all
  service replicas, eliminating the 10-second window from Phase 1.

- **Reliable event delivery.** The transactional outbox pattern guarantees no lost events
  (outbox persists through NATS outages) and no phantom events (outbox writes are
  transactional with state changes).

- **Graceful degradation.** NATS unavailability triggers automatic fallback to polling.
  The boot path is never affected. The system never hard-fails due to the event bus.

- **Incremental adoption.** Each service can be migrated from polling to events
  independently. The `EVENTS_ENABLED` toggle allows gradual rollout.

- **Observability.** Events carry `traceparent` for end-to-end distributed tracing.
  Event metrics (outbox lag, consumer lag, publish/consume rates) integrate with the
  existing OTel/Prometheus/Grafana stack.

### Negative

- **New infrastructure dependency.** NATS JetStream is a new component to deploy, monitor,
  and upgrade.
  - Mitigated: NATS is a single binary with minimal configuration. It is optional; the
    system works without it (polling fallback). The operational burden is low compared to
    alternatives (Kafka, RabbitMQ).

- **Outbox table overhead.** Every state-changing write now inserts an additional outbox
  row in the same transaction, adding a small amount of write amplification to PostgreSQL.
  - Mitigated: The outbox insert is a simple append to an indexed table. Published rows
    are cleaned up periodically. The overhead is negligible compared to the primary write.

- **At-least-once semantics require idempotent consumers.** Event handlers must safely
  handle duplicate delivery (e.g., use upserts, check for existing records).
  - Mitigated: This is standard practice. The store layer already uses `ON CONFLICT`
    clauses for upserts. All consumers are implemented with idempotency in mind.

- **Event schema evolution.** As the system evolves, event payloads may change. Consumers
  must handle unknown fields gracefully and support backward-compatible schema changes.
  - Mitigated: Events use JSON with explicit `type` fields. Unknown fields are ignored.
    Breaking changes require a new event type (e.g., `component.created.v2`), following
    the same versioning strategy as the HTTP APIs.

- **Debugging complexity.** Asynchronous event chains are harder to debug than synchronous
  HTTP calls.
  - Mitigated: The `traceparent` field connects events to the originating HTTP request
    in distributed traces. Event metrics surface consumer lag and processing errors.
    The NATS monitoring endpoint (`/streaming/channelsz`) provides stream-level visibility.

### Neutral

- The event envelope follows CloudEvents 1.0, a CNCF standard. This enables future
  integration with external event consumers without Chamicore-specific adapters.
- The choice of NATS JetStream can be revisited if requirements change dramatically.
  The `events/` abstraction in `chamicore-lib` decouples service code from the specific
  messaging implementation. Switching to a different backend (e.g., Redis Streams) would
  require reimplementing the `events/nats/` package without changing service code.
- The audit logger is a new optional component, not a modification to existing services.
  It can be a simple Go program or a NATS consumer writing to an external log store
  (Elasticsearch, Loki, S3).
- Boot-path performance characteristics are completely unchanged by this ADR. The
  performance targets from ADR-014 (10,000+ req/s, p99 < 100ms) remain valid and are
  achieved by the same mechanism (local DB reads, no cross-service calls).
