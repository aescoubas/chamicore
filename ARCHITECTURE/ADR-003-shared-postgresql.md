# ADR-003: Shared PostgreSQL

## Status

Accepted

## Date

2025-02-18

## Context

Upstream OpenCHAMI services use a variety of storage backends:

- **SMD**: PostgreSQL
- **BSS**: etcd (with PostgreSQL as an alternative)
- **Cloud-Init**: DuckDB
- **OPAAL** (auth): SQLite

This diversity creates operational complexity: each backend requires different expertise,
monitoring, backup procedures, and failure modes. Some choices (DuckDB, SQLite) are
inappropriate for production multi-node deployments.

## Decision

All Chamicore services will use **PostgreSQL** as their sole database backend.

- One PostgreSQL instance (or cluster) with a single database (e.g., `chamicore`).
- Each service gets its own **schema** within the database, providing logical isolation.
- Services **never** access another service's schema directly; inter-service communication
  uses HTTP APIs.
- Connection strings are provided via environment variables.
- Migrations use [golang-migrate](https://github.com/golang-migrate/migrate) with numbered SQL files.
- Each service sets `search_path` to its own schema on connection.
- Connection pooling is handled at the application level using Go's `database/sql` pool.

### Schema Naming

All schemas live in a single `chamicore` database:

```
chamicore (database)
  smd            # State Management Daemon schema
  bss            # Boot Script Service schema
  cloudinit      # Cloud-Init schema
  auth           # Authentication and authorization schema
```

Each service's migrations create and operate within its own schema:

```sql
CREATE SCHEMA IF NOT EXISTS smd;
SET search_path TO smd;
```

## Consequences

### Positive

- Single database technology to deploy, monitor, backup, and maintain.
- PostgreSQL is battle-tested, well-documented, and widely supported.
- Rich ecosystem of tools (pgAdmin, pg_dump, logical replication, etc.).
- Consistent migration tooling and patterns across all services.
- Simplifies development environment (one `docker run postgres` for all services).
- Horizontal scaling via read replicas if needed.

### Negative

- Potential resource contention between services sharing a PostgreSQL instance.
  - Mitigated by per-service connection pool limits and separate schemas.
- Single point of failure if not deployed with high availability.
  - Mitigated by standard PostgreSQL HA patterns (streaming replication, Patroni).
- PostgreSQL may be overkill for services with very simple storage needs.
  - Accepted trade-off for operational simplicity.

### Neutral

- Services have isolated schemas but share the same database instance and connection endpoint.
- Performance characteristics may differ from upstream's original backend choices.
- Migration from upstream databases requires data export/import tooling.
