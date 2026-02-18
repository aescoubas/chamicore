# ADR-012: Performance Testing Strategy

## Status

Accepted

## Date

2025-02-18

## Context

Chamicore must support HPC-scale deployments where **tens of thousands of nodes boot
concurrently**. A power-on event at a large site (e.g., 50,000 nodes) triggers a storm
of simultaneous DHCP requests, boot script fetches, and cloud-init payload downloads.
If Chamicore cannot handle this burst, nodes fail to boot and the site is unusable.

Without performance testing, latency regressions and throughput ceilings are discovered
only in production — when it is too late and too expensive to fix. We need a systematic
approach to measure, track, and enforce performance characteristics as the codebase
evolves.

## Decision

Implement a **four-tier testing strategy** with smoke and load tests added alongside
the existing unit, integration, and system tiers. Performance metrics are collected
via OpenTelemetry, stored in Prometheus, and tracked over time in Grafana dashboards.

### Test Tiers

| Tier | Location | Build Tag | Trigger | Duration |
|------|----------|-----------|---------|----------|
| Unit | Per-service `*_test.go` | (none) | Every commit | Seconds |
| Integration | Per-service `*_test.go` | `integration` | Every commit | Minutes |
| System | `tests/` | `system` | Every merge to main | Minutes |
| Smoke | `tests/smoke/` | `smoke` | Every deployment | < 30 seconds |
| Load | `tests/load/` | N/A (k6 scripts) | Nightly / on-demand | 15-30 minutes |

### Smoke Tests

Quick verification that a deployed stack is functional:

- Each service is reachable (`GET /health` returns 200).
- One happy-path operation per service succeeds.
- Run before load tests; failures skip the load phase.
- Written in Go with the `smoke` build tag.

### Load Tests

Simulates realistic boot-storm conditions using [k6](https://k6.io/).

#### Target Scale

| Parameter | Value |
|-----------|-------|
| Registered components | 50,000 |
| Concurrent booting nodes (sustained) | 10,000 |
| Concurrent booting nodes (spike) | 20,000 |
| Sustained load duration | 10 minutes |

#### Key Scenarios

1. **Boot storm**: 10,000+ concurrent requests to BSS for boot scripts.
2. **Cloud-init storm**: 10,000+ concurrent requests for cloud-init payloads.
3. **Inventory at scale**: CRUD operations against SMD with 50,000 components.
4. **Bulk registration**: Register 10,000 components as fast as possible.
5. **DHCP sync burst**: 10,000 component changes, measure Kea-Sync propagation time.
6. **Auth under load**: Token exchange under concurrent boot-storm conditions.
7. **Mixed boot path**: End-to-end flow from registration to boot script served.
8. **Database stress**: Concurrent reads and writes across all schemas.

#### Performance Targets

| Metric | Target |
|--------|--------|
| Single-resource GET p50 | < 10ms |
| Single-resource GET p95 | < 50ms |
| Single-resource GET p99 | < 100ms |
| Full boot path p99 (register → boot script) | < 200ms |
| Per-service throughput | > 10,000 req/s |
| Bulk registration rate | > 1,000/s |
| Error rate under sustained load | < 0.1% |
| Database query p99 | < 50ms |
| DB connection pool utilization | < 80% |
| Container memory | Stable (no leaks over 30min) |

### Load Test Execution

```
Phase 1: Seed (pre-populate 50,000 components, boot params, cloud-init payloads)
Phase 2: Ramp-up (0 → 10,000 VUs over 2 minutes)
Phase 3: Sustained (10,000 VUs for 10 minutes)
Phase 4: Spike (20,000 VUs for 2 minutes)
Phase 5: Cooldown (ramp to 0 over 1 minute, verify no lingering issues)
```

### Metrics Collection

All performance data flows through the existing OpenTelemetry pipeline:

```
Services (OTel SDK) ──OTLP──> OTel Collector ──> Prometheus
k6 ──remote-write──────────────────────────────> Prometheus
                                                      │
                                                  Grafana
                                                 (dashboards)
```

- **Service metrics**: Collected automatically by the OTel HTTP middleware already
  present in every service (see ADR-009).
- **k6 metrics**: Exported to Prometheus via k6's Prometheus remote-write extension.
- **Custom metrics**: Boot path duration, sync latency, bulk registration rate.
- **Infrastructure metrics**: Container CPU/memory via cAdvisor or Prometheus node-exporter.

### Baselines and Regression Detection

- Performance baselines are stored in `tests/load/baselines.json`.
- Each load test run compares results against baselines.
- If any metric exceeds its threshold (e.g., p99 latency 20% above baseline), the
  nightly pipeline fails and the team is alerted.
- Baselines are updated **manually** when intentional optimizations or architecture
  changes land — they are never auto-updated.

### Tooling

| Tool | Purpose |
|------|---------|
| [k6](https://k6.io/) | HTTP load generation with scripted scenarios |
| [k6 Prometheus remote-write](https://k6.io/docs/results-output/real-time/prometheus-remote-write/) | Export k6 metrics to Prometheus |
| Prometheus | Metrics storage and querying |
| Grafana | Dashboards for real-time and historical performance visualization |
| Go test harness | Seed data generation, smoke tests, custom scenario orchestration |

### CI Integration

| Test | Trigger | Failure Action |
|------|---------|---------------|
| Smoke | Every deployment / every merge to main | Block deployment |
| Load (full) | Nightly on dedicated runner | Alert + fail pipeline |
| Load (quick) | On-demand / pre-release | Developer feedback |

### Grafana Dashboards

Pre-built dashboards shipped in `chamicore-deploy`:

1. **Boot Storm Overview**: Request rate, latency percentiles, error rate across BSS
   and Cloud-Init during simulated boot events.
2. **Service Performance**: Per-service request duration, throughput, active requests,
   error breakdown.
3. **Database Performance**: Query latency, connection pool utilization, pool wait times,
   per-schema breakdown.
4. **Load Test Comparison**: Side-by-side view of current run vs. baseline, highlighting
   regressions.

## Consequences

### Positive

- **Regressions caught early**: Performance baselines prevent accidental degradation
  from landing unnoticed.
- **Scale confidence**: Boot-storm tests prove the system works at target scale before
  production deployment.
- **Data-driven optimization**: Metrics and dashboards identify bottlenecks precisely
  (slow query, connection exhaustion, contention) rather than guessing.
- **Leverages existing infrastructure**: OTel metrics + Prometheus + Grafana are already
  in place for observability (ADR-009); load testing reuses the same pipeline.
- **Smoke tests as deployment gate**: Prevents deploying a broken stack.

### Negative

- **Infrastructure cost**: Load tests require a runner with sufficient CPU, memory, and
  network to generate 10,000+ concurrent connections.
  - Mitigated: Run full load tests nightly, not on every commit. Quick mode for
    development feedback.
- **k6 is not Go**: Load test scripts are JavaScript, adding a second language.
  - Accepted: k6 is the industry standard for HTTP load testing. Its scripting model
    (ramping VUs, thresholds, scenarios) is far more expressive than Go test loops.
    Go is still used for smoke tests and seed data generation.
- **Baseline maintenance**: Baselines must be updated when performance characteristics
  intentionally change.
  - Mitigated: Manual updates force deliberate review of performance impacts.

### Neutral

- k6 can be extended with xk6 modules for custom protocols if needed later.
- Load test scenarios will evolve as new features are added.
- The same Grafana dashboards serve both production monitoring and load test analysis.
