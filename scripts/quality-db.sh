#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

DB_IMAGE="${QUALITY_DB_IMAGE:-postgres:16-alpine}"
DB_USER="${QUALITY_DB_USER:-chamicore}"
DB_PASSWORD="${QUALITY_DB_PASSWORD:-chamicore}"
DB_NAME="${QUALITY_DB_NAME:-chamicore}"
CONTAINER_NAME="chamicore-quality-db-$$"

MIGRATION_DIRS=(
    "$ROOT_DIR/services/chamicore-auth/migrations/postgres"
    "$ROOT_DIR/services/chamicore-bss/migrations/postgres"
    "$ROOT_DIR/services/chamicore-cloud-init/migrations/postgres"
    "$ROOT_DIR/services/chamicore-discovery/migrations/postgres"
    "$ROOT_DIR/services/chamicore-smd/migrations/postgres"
)

EXPECTED_TABLES=(
    "auth.signing_keys"
    "auth.service_accounts"
    "auth.revoked_tokens"
    "auth.casbin_rule"
    "auth.device_credentials"
    "bss.boot_params"
    "cloudinit.payloads"
    "discovery.targets"
    "discovery.scan_jobs"
    "smd.components"
    "smd.ethernet_interfaces"
    "smd.groups"
    "smd.group_members"
    "smd.outbox"
)

EXPECTED_INDEXES=(
    "auth.idx_signing_keys_active"
    "auth.idx_service_accounts_name"
    "auth.idx_revoked_tokens_expires_at"
    "auth.idx_casbin_rule_ptype"
    "auth.idx_device_credentials_tags"
    "bss.idx_boot_params_mac"
    "bss.idx_boot_params_component"
    "cloudinit.idx_payloads_component"
    "discovery.idx_scan_jobs_state"
    "discovery.idx_scan_jobs_target"
    "smd.idx_components_type"
    "smd.idx_ethernet_interfaces_component_id"
    "smd.idx_group_members_component_id"
    "smd.idx_outbox_unsent"
)

if ! command -v docker >/dev/null 2>&1; then
    echo "docker is required for quality-db checks" >&2
    exit 1
fi

cleanup() {
    docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "==> Starting ephemeral PostgreSQL container: $CONTAINER_NAME"
docker run -d --name "$CONTAINER_NAME" \
    -e POSTGRES_USER="$DB_USER" \
    -e POSTGRES_PASSWORD="$DB_PASSWORD" \
    -e POSTGRES_DB="$DB_NAME" \
    "$DB_IMAGE" >/dev/null

echo "==> Waiting for PostgreSQL readiness"
for _ in $(seq 1 60); do
    if docker exec "$CONTAINER_NAME" pg_isready -U "$DB_USER" -d "$DB_NAME" >/dev/null 2>&1; then
        break
    fi
    sleep 1
done

if ! docker exec "$CONTAINER_NAME" pg_isready -U "$DB_USER" -d "$DB_NAME" >/dev/null 2>&1; then
    echo "PostgreSQL did not become ready in time" >&2
    exit 1
fi

psql_exec() {
    local sql="$1"
    docker exec -i "$CONTAINER_NAME" psql -v ON_ERROR_STOP=1 -U "$DB_USER" -d "$DB_NAME" -Atqc "$sql"
}

psql_apply_file() {
    local file="$1"
    docker exec -i "$CONTAINER_NAME" psql -v ON_ERROR_STOP=1 -U "$DB_USER" -d "$DB_NAME" < "$file" >/dev/null
}

apply_sorted() {
    local direction="$1"
    local dir="$2"
    mapfile -t files < <(find "$dir" -maxdepth 1 -type f -name "*.${direction}.sql" | sort)
    if [[ "${#files[@]}" -eq 0 ]]; then
        echo "no ${direction} migrations found in $dir" >&2
        exit 1
    fi

    for file in "${files[@]}"; do
        echo "    ${direction}: $(basename "$file")"
        psql_apply_file "$file"
    done
}

apply_reverse_down() {
    local dir="$1"
    mapfile -t files < <(find "$dir" -maxdepth 1 -type f -name "*.down.sql" | sort -r)
    if [[ "${#files[@]}" -eq 0 ]]; then
        echo "no down migrations found in $dir" >&2
        exit 1
    fi

    for file in "${files[@]}"; do
        echo "    down: $(basename "$file")"
        psql_apply_file "$file"
    done
}

echo "==> Running migration lifecycle checks (up -> down -> up)"
for dir in "${MIGRATION_DIRS[@]}"; do
    if [[ ! -d "$dir" ]]; then
        echo "migration directory missing: $dir" >&2
        exit 1
    fi
    echo "--> $dir"
    apply_sorted "up" "$dir"
    apply_reverse_down "$dir"
    apply_sorted "up" "$dir"
done

echo "==> Verifying expected tables"
for table in "${EXPECTED_TABLES[@]}"; do
    exists="$(psql_exec "SELECT CASE WHEN to_regclass('$table') IS NULL THEN 0 ELSE 1 END;")"
    if [[ "$exists" != "1" ]]; then
        echo "missing table: $table" >&2
        exit 1
    fi
done

echo "==> Verifying expected indexes"
for index in "${EXPECTED_INDEXES[@]}"; do
    exists="$(psql_exec "SELECT CASE WHEN to_regclass('$index') IS NULL THEN 0 ELSE 1 END;")"
    if [[ "$exists" != "1" ]]; then
        echo "missing index: $index" >&2
        exit 1
    fi
done

echo "==> Verifying critical constraints"
checks=(
    "SELECT COUNT(*) FROM pg_constraint c JOIN pg_class t ON c.conrelid = t.oid JOIN pg_namespace n ON t.relnamespace = n.oid WHERE n.nspname = 'smd' AND t.relname = 'ethernet_interfaces' AND c.conname = 'uq_ethernet_interfaces_mac_addr' AND c.contype = 'u';"
    "SELECT COUNT(*) FROM pg_constraint c JOIN pg_class t ON c.conrelid = t.oid JOIN pg_namespace n ON t.relnamespace = n.oid WHERE n.nspname = 'discovery' AND t.relname = 'scan_jobs' AND c.contype = 'f' AND pg_get_constraintdef(c.oid) LIKE '%REFERENCES discovery.targets(id)%';"
    "SELECT COUNT(*) FROM pg_constraint c JOIN pg_class t ON c.conrelid = t.oid JOIN pg_namespace n ON t.relnamespace = n.oid WHERE n.nspname = 'smd' AND t.relname = 'group_members' AND c.contype = 'f' AND pg_get_constraintdef(c.oid) LIKE '%REFERENCES smd.components(id)%';"
    "SELECT COUNT(*) FROM pg_constraint c JOIN pg_class t ON c.conrelid = t.oid JOIN pg_namespace n ON t.relnamespace = n.oid WHERE n.nspname = 'cloudinit' AND t.relname = 'payloads' AND c.contype = 'u' AND pg_get_constraintdef(c.oid) LIKE '%(component_id)%';"
)

for sql in "${checks[@]}"; do
    count="$(psql_exec "$sql")"
    if ! awk -v n="$count" 'BEGIN { exit !(n + 0 > 0) }'; then
        echo "constraint validation failed: $sql" >&2
        exit 1
    fi
done

echo "==> Seeding minimal data for query plan checks"
psql_exec "INSERT INTO smd.components (id, type, state, role) VALUES ('x0c0s0b0n0', 'Node', 'Ready', 'Compute') ON CONFLICT (id) DO NOTHING;"
psql_exec "INSERT INTO smd.ethernet_interfaces (id, component_id, mac_addr) VALUES ('if0', 'x0c0s0b0n0', '02:00:00:00:00:01') ON CONFLICT (id) DO NOTHING;"
psql_exec "INSERT INTO bss.boot_params (id, component_id, mac, role, kernel_uri, initrd_uri, cmdline) VALUES ('bp0', 'x0c0s0b0n0', '02:00:00:00:00:01', 'Compute', 'http://kernel', 'http://initrd', 'console=ttyS0') ON CONFLICT (id) DO NOTHING;"
psql_exec "INSERT INTO cloudinit.payloads (id, component_id, role, user_data) VALUES ('ci0', 'x0c0s0b0n0', 'Compute', '#cloud-config') ON CONFLICT (id) DO NOTHING;"
psql_exec "INSERT INTO auth.service_accounts (name, secret_hash, scopes, enabled) VALUES ('cli', 'hash', 'admin', true) ON CONFLICT (name) DO NOTHING;"
psql_exec "INSERT INTO discovery.targets (id, name, driver, addresses, credential_id, schedule, enabled) VALUES ('t0', 'rack-a', 'redfish', '[\"192.0.2.10\"]'::jsonb, '', '', true) ON CONFLICT (id) DO NOTHING;"
psql_exec "INSERT INTO discovery.scan_jobs (id, target_id, driver, state) VALUES ('j0', 't0', 'redfish', 'pending') ON CONFLICT (id) DO NOTHING;"

plan_contains() {
    local label="$1"
    local query="$2"
    local expected_pattern="$3"

    plan="$(
        docker exec -i "$CONTAINER_NAME" \
            psql -v ON_ERROR_STOP=1 -U "$DB_USER" -d "$DB_NAME" -Atqc \
            "SET enable_seqscan = off; EXPLAIN ${query}; RESET enable_seqscan;" \
            | grep -Ev '^(SET|RESET)$'
    )"

    if ! grep -Eq "$expected_pattern" <<<"$plan"; then
        echo "query plan check failed: $label" >&2
        echo "expected pattern: $expected_pattern" >&2
        echo "plan:" >&2
        echo "$plan" >&2
        exit 1
    fi
}

echo "==> Verifying critical query plans"
plan_contains "BSS lookup by MAC uses MAC index" \
    "SELECT * FROM bss.boot_params WHERE mac = '02:00:00:00:00:01'" \
    "idx_boot_params_mac"
plan_contains "SMD component by type uses type index" \
    "SELECT * FROM smd.components WHERE type = 'Node'" \
    "idx_components_type"
plan_contains "Cloud-Init payload by component uses component index/unique constraint" \
    "SELECT * FROM cloudinit.payloads WHERE component_id = 'x0c0s0b0n0'" \
    "(idx_payloads_component|payloads_component_id_key)"
plan_contains "Auth service account lookup uses name index/unique constraint" \
    "SELECT * FROM auth.service_accounts WHERE name = 'cli'" \
    "(idx_service_accounts_name|service_accounts_name_key)"
plan_contains "Discovery scan jobs by state uses state index" \
    "SELECT * FROM discovery.scan_jobs WHERE state = 'pending'" \
    "idx_scan_jobs_state"

echo "quality-db checks passed"
