# ADR-019: PostgreSQL High Availability Strategy

## Status

Proposed

## Date

2026-03-03

## Context

All Chamicore services depend on PostgreSQL for state, but current deployment defaults use
one chart-managed PostgreSQL instance without replication or automated failover.

Current reality:

- Docker Compose uses one `postgres:16-alpine` container.
- Helm default deploys one PostgreSQL `Deployment` with one PVC.
- RPO/RTO targets are not defined across environments.
- Backup/restore procedures are not centrally documented.

Production HPC operations need explicit, testable HA tiers with clear trade-offs and
operational expectations.

## Decision

Adopt a three-tier PostgreSQL HA strategy and keep service code unchanged by using DSN
configuration (`CHAMICORE_<SERVICE>_DB_DSN`) as the abstraction boundary.

### Tier 0: Development (default)

- Single PostgreSQL instance (Compose or Helm internal mode).
- No replication or automatic failover.
- RPO: last successful manual/scheduled backup.
- RTO: manual restore time.
- Target use: local development, CI, demos.

### Tier 1: Standard Production

- Primary + synchronous standby with automated failover managed by an external PostgreSQL
  operator (for example CloudNativePG, Zalando postgres-operator, or CrunchyData PGO).
- PgBouncer in front of the cluster for connection pooling and failover insulation.
- WAL archiving to object storage (S3/MinIO) for PITR.
- RPO target: 0 (synchronous replication).
- RTO target: < 30 seconds (automated failover path).
- Target use: on-prem production clusters.

### Tier 2: Managed PostgreSQL

- Cloud managed PostgreSQL (for example AWS RDS, Azure Database for PostgreSQL,
  Google Cloud SQL) with Multi-AZ and provider-managed backups/failover.
- Chamicore connects to provider endpoint DSN.
- RPO/RTO follows provider SLA (commonly RPO=0 and sub-minute failover).
- Target use: cloud-hosted production clusters.

### Helm Support

The `chamicore` chart supports:

- `postgresql.mode=internal` (default): deploy chart-managed PostgreSQL.
- `postgresql.mode=external`: do not deploy PostgreSQL and use external connection
  parameters + credentials secret.

This enables Tier 0 and Tier 1/2 from one chart without service code changes.

### Application-Level Considerations

- All services remain DSN-driven; no application code changes per tier.
- For Tier 1, service DSNs should typically point to PgBouncer rather than direct
  PostgreSQL nodes.
- For Tier 2, pool sizing in `dbutil.Connect()` may require tuning to provider limits.
- Boot-path services (BSS/Cloud-Init) can continue serving cached data during transient
  dependency outages, but startup sync and writes still need DB availability.
- Auth is the most availability-sensitive service because token/signing-key operations
  depend directly on DB access.

## Consequences

### Positive

- Defines explicit HA targets and environment-appropriate deployment choices.
- Removes ambiguity around production database topology and failover expectations.
- Keeps service code stable by using DSN indirection and deployment-level controls.

### Negative

- Tier 1 introduces operational complexity (operator lifecycle, failover drills,
  PgBouncer tuning, backup verification).
- Tier 2 introduces cloud vendor dependency and SLA coupling.

### Neutral

- Existing per-service schemas and migration behavior are unchanged.
- Internal single-node PostgreSQL remains the default for developer ergonomics.
