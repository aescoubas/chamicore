# High-Priority Remediation Plan

**Date:** 2026-03-03
**Source:** [Codebase Quality Report](../2026-03-03_codebase_report.md) — High Priority Recommendations
**Scope:** 3 work streams addressing coverage gaps, missing bulk endpoints, and hard-delete risk

---

## Work Stream 1: Close the Test Coverage Gap

### Problem

AGENTS.md mandates 100% coverage. `quality/thresholds.txt` enforces it. Four modules were
identified with shortfalls:

| Module | Reported | Target | Status |
|--------|----------|--------|--------|
| chamicore-smd | 85.9% | 100% | Gap — untested metrics pkg, incomplete conditional-GET client tests |
| chamicore-auth authz | 48% unit (misleading) | 100% | Actual unit is ~93%; remaining gap is Casbin error recovery paths |
| chamicore-lib redfish | 85.9% | 100% | Gap — crypto/rand, context cancellation, retry exhaustion paths |
| chamicore-lib dbutil | 62% (historical) | 100% | **Resolved** — now 100% |

### Decision: Invest in Covering vs Relax Policy

**Recommendation: Cover the remaining paths.** The total effort is ~180 lines of test code
across 3 modules. This is tractable and keeps the 100% policy honest.

### Tasks

#### 1.1 — chamicore-smd: Test the metrics package (~40 LoC)

**What:** `internal/metrics/resources.go` has 6 functions at 0% coverage — it's an entire
untested Prometheus collector.

**How:**
- Create `internal/metrics/resources_test.go`
- Use a test database (testcontainers or sqlmock) with known row counts
- Test `Describe()` sends expected metric descriptors
- Test `Collect()` returns correct gauge values for components, interfaces, groups
- Test `collectMetric()` error path when SQL query fails (mock a closed DB)
- Test `RegisterResourceCollector()` double-registration handling

**Files touched:**
- `services/chamicore-smd/internal/metrics/resources_test.go` (new)

#### 1.2 — chamicore-smd: Complete conditional-GET client tests (~50 LoC)

**What:** `pkg/client/client.go` has `listComponentsConditional()` at 92% and
`listInterfacesConditional()` at 68%. Missing: request creation failure, malformed JSON
response, context cancellation.

**How:**
- Add table-driven test cases to existing `pkg/client/client_test.go`
- Test with `httptest.NewServer` returning malformed JSON → expect typed error
- Test with cancelled context → expect context error
- Mirror component tests for interface conditional list

**Files touched:**
- `services/chamicore-smd/pkg/client/client_test.go` (edit)

#### 1.3 — chamicore-lib redfish: Error injection tests (~80 LoC)

**What:** `redfish/client.go` has 5+ functions below 80%. The uncovered paths are defensive:
`crypto/rand` failures, context cancellation mid-sleep, retry exhaustion, URL edge cases.

**How:**
- `randDuration()`: inject a broken `io.Reader` via a test seam or refactor to accept
  a rand source (prefer minimal refactor: extract `randSource io.Reader` field on client)
- `sleepWithJitter()`: pass a pre-cancelled context → verify early return
- `nextBackoff()`: test max-backoff clamping with large retry counts
- `joinPath()`: table-driven tests for trailing slashes, empty segments, scheme-prefixed inputs
- `doJSON()`: httptest server returning truncated body, non-JSON content-type
- `contextErr()`: wrap known context errors in `fmt.Errorf` and verify detection

**Files touched:**
- `shared/chamicore-lib/redfish/client.go` (minor: add `randSource` field or similar test seam)
- `shared/chamicore-lib/redfish/client_test.go` (edit)

#### 1.4 — chamicore-auth authz: Casbin error recovery tests (~60 LoC)

**What:** Unit coverage is ~93% but `seedDefaults()`, `SavePolicy()`, and `RemovePolicy()`
have untested error branches when Casbin or the database returns unexpected errors.

**How:**
- Mock the Casbin enforcer to return errors on `AddPolicy()`, `HasPolicy()`, `AddGroupingPolicy()`
- Test `seedDefaults()` when `AddRoleMember()` fails partway through
- Test `SavePolicy()` transaction failure at commit time
- Test `RemoveFilteredPolicy()` when no policies match

**Files touched:**
- `services/chamicore-auth/internal/authz/casbin_test.go` (edit)
- `services/chamicore-auth/internal/authz/adapter_test.go` (edit)

### Acceptance Criteria

- `go test -coverprofile=coverage.out ./...` reports 100% in all four modules
- `scripts/quality/check-coverage-thresholds.sh` passes green for all entries in
  `quality/thresholds.txt`
- No test marked `t.Skip()` — all paths genuinely exercised

---

## Work Stream 2: Implement Bulk Endpoints

### Problem

AGENTS.md specifies `<resource>/bulk` endpoints with `207 Multi-Status`, but zero services
implement them. For an HPC platform managing thousands of nodes, single-item CRUD is a
bottleneck for fleet operations (initial registration, mass state changes, boot parameter
updates).

### Priority Targets

1. **SMD — Component bulk create/update** (highest value: fleet onboarding)
2. **BSS — Boot parameter bulk set** (boot storm preparation)
3. **Cloud-Init — Payload bulk set** (optional, lower priority)

### Design

#### Response Format (new in chamicore-lib/httputil)

```json
{
  "kind": "BulkResult",
  "apiVersion": "hsm/v2",
  "metadata": { "total": 100, "succeeded": 98, "failed": 2 },
  "items": [
    { "id": "node-001", "status": 201 },
    { "id": "node-002", "status": 201 },
    { "id": "node-bad", "status": 422, "error": { "type": "about:blank", "title": "Unprocessable Entity", "status": 422, "detail": "invalid type: Foobar" } }
  ]
}
```

- Each item processed independently (not transactional across items)
- Per-item status: `201` (created), `200` (updated), `409` (conflict), `422` (validation)
- Overall HTTP status: `207 Multi-Status`
- Body limit: 10 MB (`httputil.BodyLimit(10 << 20)` on bulk routes)
- Max items: 10,000 (validated server-side)

### Tasks

#### 2.1 — chamicore-lib: Add multi-status response helpers

**What:** New types and response helper in `httputil/`.

**Types:**
- `BulkResultItem` — `{ID, Status, Error *Problem}`
- `BulkResult[T]` — envelope with `Metadata{Total, Succeeded, Failed}` + `Items []BulkResultItem`
- `RespondBulkResult(w, r, items []BulkResultItem)` — writes `207` with envelope

**Files touched:**
- `shared/chamicore-lib/httputil/bulk.go` (new)
- `shared/chamicore-lib/httputil/bulk_test.go` (new)

#### 2.2 — chamicore-smd: Bulk component create/update

**What:** `POST /hsm/v2/State/Components/bulk` — accepts array of component specs,
returns `207` with per-item results.

**Behavior:**
- Each component validated independently (ID format, type enum, required fields)
- Valid components inserted/updated; invalid ones return per-item error
- Events emitted per successful item via transactional outbox
- Store method: `CreateOrUpdateComponentsBulk(ctx, []model.Component) ([]BulkItemResult, error)`
- Implementation: single transaction with `ON CONFLICT (id) DO UPDATE` for upsert semantics
  (items are not independent at the DB level — use a single TX for performance, but report
  per-item validation errors before hitting the DB)

**Files touched:**
- `services/chamicore-smd/internal/store/store.go` (add interface method)
- `services/chamicore-smd/internal/store/postgres_component.go` (implement bulk method)
- `services/chamicore-smd/internal/server/handlers_component.go` (add `handleBulkCreateComponents`)
- `services/chamicore-smd/internal/server/server.go` (register route)
- `services/chamicore-smd/api/openapi.yaml` (add path + schemas)
- `services/chamicore-smd/pkg/client/client.go` (add `BulkCreateComponents` method)
- Tests for all layers

#### 2.3 — chamicore-bss: Bulk boot parameter set

**What:** `POST /boot/v1/bootparams/bulk` — accepts array of boot param specs,
returns `207` with per-item results.

**Behavior:**
- Each boot param validated independently
- Upsert semantics (create or replace by component ID)
- Store method: `SetBootParamsBulk(ctx, []model.BootParam) ([]BulkItemResult, error)`

**Files touched:**
- `services/chamicore-bss/internal/store/store.go` (add interface method)
- `services/chamicore-bss/internal/store/postgres_bootparam.go` (implement bulk method)
- `services/chamicore-bss/internal/server/handlers_bootparam.go` (add `handleBulkSetBootParams`)
- `services/chamicore-bss/internal/server/server.go` (register route)
- `services/chamicore-bss/api/openapi.yaml` (add path + schemas)
- `services/chamicore-bss/pkg/client/client.go` (add `BulkSetBootParams` method)
- Tests for all layers

#### 2.4 — chamicore-cli: Add bulk subcommands

**What:** CLI commands that read from file/stdin and call bulk endpoints.

```bash
chamicore-cli smd components bulk create --file nodes.json
chamicore-cli bss bootparams bulk set --file params.json
cat nodes.json | chamicore-cli smd components bulk create --stdin
```

**Files touched:**
- `services/chamicore-cli/cmd/smd_bulk.go` (new)
- `services/chamicore-cli/cmd/bss_bulk.go` (new)

### Acceptance Criteria

- `POST /hsm/v2/State/Components/bulk` with 100 valid items → `207` with 100 `status: 201`
- Same request with 2 invalid items → `207` with 98 `201` + 2 `422` with RFC 9457 errors
- Body > 10 MB → `413 Payload Too Large`
- Body with > 10,000 items → `422` with detail explaining the limit
- All endpoints documented in OpenAPI specs
- Client SDKs expose typed bulk methods
- 100% test coverage on new code
- Smoke test added to `tests/smoke/` exercising bulk endpoints

---

## Work Stream 3: Soft Delete + Audit Logging for Critical Resources

### Problem

All delete operations are permanent (`DELETE FROM`). No audit trail, no recoverability.
For an HPC platform where accidental deletion of node records disrupts operations, this is
a safety risk. The codebase report recommends soft deletes for critical resources and
audit logging at minimum.

### Approach: Phased Implementation

**Phase A — Audit logging (low risk, immediate value):** Log deleted resource state before
removal. This is additive — no schema changes, no query changes.

**Phase B — Soft delete for critical resources (schema migration):** Add `deleted_at` column
to high-value tables, modify queries to filter on `deleted_at IS NULL`, add purge endpoint
for permanent removal.

### Scope

| Resource | Phase A (audit log) | Phase B (soft delete) |
|----------|--------------------|-----------------------|
| SMD components | Yes | Yes |
| SMD ethernet interfaces | Yes | Cascade from component |
| SMD groups/partitions | Yes | No (low risk, easy to recreate) |
| BSS boot params | Yes | Yes |
| Cloud-Init payloads | Yes | No (derived data, regenerable) |
| Auth policies | Yes | No (Casbin manages lifecycle) |

### Tasks

#### 3.1 — Phase A: Audit logging on delete handlers

**What:** Before executing the delete, fetch the current resource and log it at `info` level
with a structured `"audit"` field. This provides a recovery path via log aggregation
(Loki, ELK, etc.) without any schema changes.

**Pattern:**
```go
func (s *Server) handleDeleteComponent(w http.ResponseWriter, r *http.Request) {
    id := chi.URLParam(r, "id")
    // Fetch before delete for audit
    existing, err := s.store.GetComponent(r.Context(), id)
    if errors.Is(err, store.ErrNotFound) {
        httputil.Problem(w, r, http.StatusNotFound, "Component %s not found", id)
        return
    }
    // ... (handle other errors)

    // Audit log: capture full state before deletion
    s.log.Info().
        Str("audit_action", "delete").
        Str("resource_kind", "Component").
        Str("resource_id", id).
        RawJSON("resource_state", mustMarshal(existing)).
        Str("actor", auth.SubjectFromContext(r.Context())).
        Str("request_id", httputil.RequestIDFromContext(r.Context())).
        Msg("resource deleted")

    if err := s.store.DeleteComponent(r.Context(), id); err != nil {
        // ... error handling
    }
    httputil.RespondNoContent(w)
}
```

**Files touched:**
- `services/chamicore-smd/internal/server/handlers_component.go` (edit delete handlers)
- `services/chamicore-smd/internal/server/handlers_interface.go` (edit delete handler)
- `services/chamicore-smd/internal/server/handlers_group.go` (edit delete handler)
- `services/chamicore-bss/internal/server/handlers_bootparam.go` (edit delete handler)
- `services/chamicore-cloud-init/internal/server/handlers_payload.go` (edit delete handler)
- Update tests for all modified handlers

#### 3.2 — Phase B: Soft delete schema migration (SMD components)

**What:** Add `deleted_at` column, partial index, modify all queries.

**Migration:**
```sql
-- 000009_soft_delete.up.sql
ALTER TABLE smd.components ADD COLUMN deleted_at TIMESTAMPTZ;
CREATE INDEX idx_components_active ON smd.components (id) WHERE deleted_at IS NULL;
-- Existing unique constraints need WHERE deleted_at IS NULL
-- Cascade: interfaces follow component soft-delete state

-- 000009_soft_delete.down.sql
DELETE FROM smd.components WHERE deleted_at IS NOT NULL;
DROP INDEX IF EXISTS smd.idx_components_active;
ALTER TABLE smd.components DROP COLUMN deleted_at;
```

**Store changes:**
- All `SELECT` queries add `WHERE deleted_at IS NULL` (active records only)
- `DeleteComponent()` becomes `UPDATE ... SET deleted_at = NOW() WHERE id = $1 AND deleted_at IS NULL`
- New method: `PurgeComponent(ctx, id)` — permanent `DELETE FROM` (admin only)
- New method: `RestoreComponent(ctx, id)` — `UPDATE ... SET deleted_at = NULL`
- New method: `ListDeletedComponents(ctx, opts)` — list soft-deleted records

**API changes:**
- `DELETE /hsm/v2/State/Components/{id}` — soft delete (returns `204` as before)
- `DELETE /hsm/v2/State/Components/{id}/purge` — permanent delete (new, admin scope)
- `POST /hsm/v2/State/Components/{id}/restore` — restore soft-deleted (new, admin scope)
- `GET /hsm/v2/State/Components?includeDeleted=true` — optional filter (new query param)

**Files touched:**
- `services/chamicore-smd/migrations/postgres/000009_soft_delete.up.sql` (new)
- `services/chamicore-smd/migrations/postgres/000009_soft_delete.down.sql` (new)
- `services/chamicore-smd/internal/model/component.go` (add `DeletedAt *time.Time`)
- `services/chamicore-smd/internal/store/store.go` (add new interface methods)
- `services/chamicore-smd/internal/store/postgres_component.go` (modify all queries)
- `services/chamicore-smd/internal/server/handlers_component.go` (add purge/restore handlers)
- `services/chamicore-smd/internal/server/server.go` (register new routes)
- `services/chamicore-smd/api/openapi.yaml` (add new paths + query param)
- `services/chamicore-smd/pkg/client/client.go` (add new client methods)
- `services/chamicore-smd/pkg/types/component.go` (add `DeletedAt` to public types)
- Tests for all layers

#### 3.3 — Phase B: Soft delete schema migration (BSS boot params)

**What:** Same pattern as 3.2 but for `bss.boot_params`.

**Files touched:** Mirror of 3.2 in `services/chamicore-bss/`.

#### 3.4 — SMD delete event: Include resource snapshot

**What:** Currently `chamicore.smd.components.deleted` events carry only the component ID.
Change to include the full resource snapshot before deletion. This makes the event stream
a complete audit trail.

**Files touched:**
- `services/chamicore-smd/internal/store/events_component.go` (pass snapshot to delete event)
- `services/chamicore-smd/internal/store/events_component_test.go` (update tests)

### Acceptance Criteria

**Phase A (audit logging):**
- Every delete handler logs the full resource state at `info` level before deletion
- Log entries include: `audit_action`, `resource_kind`, `resource_id`, `resource_state`,
  `actor`, `request_id`
- Existing delete behavior unchanged (still returns `204`)

**Phase B (soft delete):**
- `DELETE` on component → record has `deleted_at` set, no longer appears in list/get
- `GET /.../{id}` on soft-deleted record → `404`
- `GET /...?includeDeleted=true` → includes soft-deleted records with `deletedAt` field
- `POST /.../{id}/restore` → clears `deleted_at`, record reappears
- `DELETE /.../{id}/purge` → permanent removal (admin scope only)
- Cascade: soft-deleting a component soft-deletes its ethernet interfaces
- Migration is reversible (`down.sql` purges soft-deleted records and removes column)
- 100% test coverage on new code

---

## Implementation Order

```
WS1 (Coverage)  ─────────────────────────────────────────────────>
  1.1 smd metrics tests
  1.2 smd client tests
  1.3 lib redfish tests
  1.4 auth authz tests

WS2 (Bulk)       ──────────────────────────────────────────────────────────>
  2.1 lib bulk helpers  →  2.2 smd bulk  →  2.3 bss bulk  →  2.4 cli bulk

WS3 (Soft Delete) ───────────────────────────────────────────────────────────────>
  3.1 audit logging (all services)  →  3.2 smd soft delete  →  3.3 bss soft delete
                                       3.4 smd event snapshot
```

- **WS1** and **WS2** are independent and can proceed in parallel
- **WS3 Phase A** (task 3.1) is independent and can start immediately
- **WS3 Phase B** (tasks 3.2-3.4) should follow WS2 to avoid migration conflicts with bulk
- **WS2.1** (lib bulk helpers) blocks WS2.2 and WS2.3
- **WS2.2** (smd bulk) blocks WS2.4 (cli needs the endpoint to exist)

### Estimated Effort

| Task | Effort | New/Modified Files |
|------|--------|--------------------|
| 1.1 SMD metrics tests | Small | 1 new |
| 1.2 SMD client tests | Small | 1 edit |
| 1.3 Lib redfish tests | Small | 1-2 edits |
| 1.4 Auth authz tests | Small | 2 edits |
| 2.1 Lib bulk helpers | Medium | 2 new |
| 2.2 SMD bulk endpoints | Large | 7+ files |
| 2.3 BSS bulk endpoints | Large | 7+ files |
| 2.4 CLI bulk commands | Medium | 2 new |
| 3.1 Audit logging | Medium | 5 edits |
| 3.2 SMD soft delete | Large | 10+ files |
| 3.3 BSS soft delete | Large | 8+ files |
| 3.4 SMD event snapshot | Small | 2 edits |

---

## Open Questions

1. **Soft-delete retention policy:** How long should soft-deleted records be retained before
   automatic purge? Options: 30 days, 90 days, indefinite (admin purge only).
2. **Bulk endpoint auth scope:** Should bulk operations require a separate scope
   (e.g., `smd:components:bulk:write`) or reuse the existing write scope?
3. **Audit log destination:** Phase A uses structured logging (stdout → log aggregator).
   Should Phase B add a dedicated `audit_log` table in PostgreSQL for queryable history?
4. **Cascade depth for soft delete:** Should soft-deleting a component also soft-delete
   BSS boot params and Cloud-Init payloads referencing it (cross-service cascade via events)?
