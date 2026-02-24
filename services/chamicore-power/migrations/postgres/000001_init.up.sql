CREATE SCHEMA IF NOT EXISTS power;
SET search_path TO power;

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE IF NOT EXISTS power.transitions (
    id                 TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    request_id         TEXT NOT NULL DEFAULT '',
    operation          TEXT NOT NULL,
    state              TEXT NOT NULL DEFAULT 'pending',
    dry_run            BOOLEAN NOT NULL DEFAULT FALSE,
    requested_by       TEXT NOT NULL DEFAULT '',
    target_count       INT NOT NULL DEFAULT 0,
    success_count      INT NOT NULL DEFAULT 0,
    failure_count      INT NOT NULL DEFAULT 0,
    queued_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at         TIMESTAMPTZ,
    completed_at       TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS power.transition_tasks (
    id                    TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    transition_id         TEXT NOT NULL REFERENCES power.transitions(id) ON DELETE CASCADE,
    node_id               TEXT NOT NULL,
    bmc_id                TEXT NOT NULL DEFAULT '',
    bmc_endpoint          TEXT NOT NULL DEFAULT '',
    operation             TEXT NOT NULL,
    state                 TEXT NOT NULL DEFAULT 'pending',
    dry_run               BOOLEAN NOT NULL DEFAULT FALSE,
    attempt_count         INT NOT NULL DEFAULT 0,
    final_power_state     TEXT NOT NULL DEFAULT '',
    error_detail          TEXT NOT NULL DEFAULT '',
    queued_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at            TIMESTAMPTZ,
    completed_at          TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (transition_id, node_id)
);

CREATE TABLE IF NOT EXISTS power.bmc_endpoints (
    bmc_id                TEXT PRIMARY KEY,
    endpoint              TEXT NOT NULL,
    credential_id         TEXT NOT NULL,
    insecure_skip_verify  BOOLEAN NOT NULL DEFAULT FALSE,
    source                TEXT NOT NULL DEFAULT 'smd',
    last_synced_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS power.node_bmc_links (
    node_id               TEXT PRIMARY KEY,
    bmc_id                TEXT NOT NULL REFERENCES power.bmc_endpoints(bmc_id) ON DELETE CASCADE,
    source                TEXT NOT NULL DEFAULT 'smd',
    last_synced_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_transitions_state ON power.transitions (state);
CREATE INDEX IF NOT EXISTS idx_transitions_operation ON power.transitions (operation);
CREATE INDEX IF NOT EXISTS idx_transition_tasks_transition_id ON power.transition_tasks (transition_id);
CREATE INDEX IF NOT EXISTS idx_transition_tasks_node_id ON power.transition_tasks (node_id);
CREATE INDEX IF NOT EXISTS idx_transition_tasks_state ON power.transition_tasks (state);
CREATE INDEX IF NOT EXISTS idx_node_bmc_links_bmc_id ON power.node_bmc_links (bmc_id);
