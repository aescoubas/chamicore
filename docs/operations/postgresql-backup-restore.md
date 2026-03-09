# PostgreSQL Backup and Restore Runbook

This runbook covers backup and restore for all Chamicore PostgreSQL deployment tiers.

## Scope

- Database: `chamicore`
- Schemas: `auth`, `smd`, `bss`, `cloudinit`, `discovery`, `power`
- Restore objective: preserve service schema isolation while enabling full-cluster recovery.

## Tier 0: Single Instance (Development)

### Backup

Full logical backup:

```bash
pg_dump \
  --dbname="postgres://chamicore:${PGPASSWORD}@localhost:5432/chamicore?sslmode=disable" \
  --format=custom \
  --file=/var/backups/chamicore/chamicore_$(date +%F_%H%M%S).dump
```

Example cron (daily 02:00):

```bash
0 2 * * * PGPASSWORD='<password>' pg_dump --dbname='postgres://chamicore@localhost:5432/chamicore?sslmode=disable' --format=custom --file=/var/backups/chamicore/chamicore_$(date +\%F_\%H\%M\%S).dump
```

### Restore

```bash
dropdb --if-exists chamicore
createdb chamicore
pg_restore --clean --if-exists --no-owner --dbname=chamicore /var/backups/chamicore/<backup>.dump
```

## Tier 1: Operator-Managed HA + PgBouncer

### Backup (Base + WAL for PITR)

Recommended pattern:

1. Use operator-native base backup scheduling (CloudNativePG/PGO/Zalando).
2. Enable WAL archiving to object storage (S3/MinIO).
3. Periodically test recovery in a staging namespace.

For operator-agnostic logical exports:

```bash
pg_dump \
  --dbname="postgres://<user>:<password>@<pgbouncer-host>:6432/chamicore?sslmode=require" \
  --format=custom \
  --file=/backups/chamicore_$(date +%F_%H%M%S).dump
```

### PITR Restore

1. Provision a new PostgreSQL cluster from last base backup.
2. Replay WAL to target time (`recovery_target_time`).
3. Validate recovered data.
4. Re-point PgBouncer/service DNS to recovered primary.

Failover/switchover commands are operator-specific. Use the operator runbook for:

- controlled switchover
- forced failover
- replica rebuild

## Tier 2: Managed PostgreSQL

Use provider-native automated backups and PITR.

- AWS RDS: restore to point in time and cut over endpoint.
- Azure Database for PostgreSQL: PITR restore to new server.
- Google Cloud SQL: point-in-time clone and cutover.

Also keep periodic logical exports for portability/compliance when required.

## Schema-Aware Restore

Restore only one service schema without affecting others:

```bash
pg_restore \
  --dbname="postgres://<user>:<password>@<host>:5432/chamicore?sslmode=require" \
  --schema=smd \
  --clean --if-exists \
  /backups/chamicore_<timestamp>.dump
```

Examples:

- `--schema=auth` for auth-only recovery
- `--schema=bss` for boot-param data recovery

## Post-Restore Verification

1. Run service readiness checks (`/readiness`) for all services.
2. Verify representative API reads and writes.
3. Confirm background syncers have resumed and are ready.
4. Confirm auth token issuance and JWKS endpoint health.

## Migrations After Restore

Services run migrations on startup. If a restore lands on an older migration version,
services automatically apply forward migrations during boot.

Operational guidance:

- Start `chamicore-auth` and `chamicore-smd` first.
- Start dependent services after core readiness is green.
