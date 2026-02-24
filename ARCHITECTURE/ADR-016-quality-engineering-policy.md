# ADR-016: Quality Engineering Policy and Database Drift Control

## Status

Accepted

## Date

2026-02-24

## Context

Chamicore is developed rapidly with AI-assisted workflows across multiple services in a
submodule monorepo. This speed is valuable, but it increases the risk of:

- Superficial test coverage that misses important negative paths.
- Regressions landing without early detection.
- Inconsistent quality standards across services and teams.
- Database schema drift as migrations and store logic evolve.

Existing ADRs define API conventions (ADR-007), observability (ADR-009), and performance
testing (ADR-012), but they do not define a single mandatory quality policy spanning unit
test strength, regression prevention, and database quality controls.

To claim production-grade readiness with stakeholders, quality expectations must be explicit,
measurable, and enforced in CI/CD.

## Decision

Adopt a repository-wide quality engineering policy with mandatory automated gates. These gates
apply to every service unless explicitly waived via a documented ADR or approved exception.

### 1. Required CI Quality Gates

Every merge request to `main` must pass all of the following:

1. Formatting and static checks:
   - `go fmt` / `goimports` clean.
   - `golangci-lint run` with zero findings.
2. Unit test reliability:
   - `go test -race ./...`
   - `go test -shuffle=on ./...`
3. Coverage requirements:
   - Repository target remains 100% unless a temporary exception is documented.
   - Per-package minimums may be enforced where 100% is not yet practical.
   - Changed-line coverage must stay high (target >= 90%).
4. Integration checks:
   - `go test -tags integration ./...` for affected services.
5. OpenAPI and contract consistency for changed APIs:
   - Handlers, clients, and `api/openapi.yaml` must stay aligned.

### 2. Test Strength Policy

Coverage is necessary but not sufficient. Tests must demonstrate behavioral strength:

1. For each public handler/store method:
   - Happy path
   - Validation failure
   - Not found / conflict paths where applicable
   - Dependency/store failure path
   - Auth/scope failure path where applicable
2. Race and flake prevention:
   - No known data races.
   - Flaky tests are treated as blocking defects.
3. Fuzz tests:
   - Add fuzz coverage for parsers, request decoding, and patch/merge logic.
4. Mutation testing:
   - Run mutation testing on critical packages (nightly or pre-release).
   - Surviving mutants are triaged as test-strength gaps.

### 3. Database Quality and Drift Controls

Database quality is a first-class release gate:

1. Migration discipline:
   - Released migrations are immutable.
   - Every migration has both `.up.sql` and `.down.sql`.
   - Migrations are idempotent and apply cleanly from an empty database.
2. Drift detection:
   - CI must verify migration status is clean after apply.
   - Schema inspection checks (indexes, constraints, column types) must match expected state.
   - Integration tests should fail on missing/incorrect schema objects.
3. Query quality:
   - Query plans for critical paths are reviewed when queries change.
   - New indexes must include justification in migration or review notes.
4. Safety checks:
   - Backup/restore validation is exercised before production rollout.
   - Destructive migration patterns require explicit review and rollback strategy.

### 4. Regression Prevention and Release Governance

1. Branch protection:
   - Required CI gates cannot be bypassed for `main`.
2. Release criteria:
   - No open Sev-1 defects.
   - Smoke tests pass against deployment target.
   - Observability signals (health, readiness, metrics, traces) are present.
3. Quality evidence:
   - Each release includes test, coverage, and integration artifacts for auditability.

### 5. Ownership and Review Cadence

1. Quality gates are owned by maintainers of each service and shared platform tooling.
2. The policy is reviewed at least once per quarter or after major incident learnings.
3. Temporary exceptions must include:
   - Scope
   - Expiry date
   - Named owner
   - Remediation plan

## Consequences

### Positive

- Production-readiness claims are backed by objective evidence.
- Regressions are detected earlier and less likely to reach production.
- Database quality remains stable as schemas and features evolve.
- Teams share one explicit quality contract across the monorepo.

### Negative

- CI pipelines may take longer and require more infrastructure.
- Short-term delivery speed can decrease while test depth and drift checks are added.
- Mutation/fuzz testing introduces additional tooling and maintenance.

### Neutral

- Thresholds and tooling can evolve over time; the policy defines outcomes and minimum gates.
- Some legacy areas may require temporary exceptions while convergence work completes.
