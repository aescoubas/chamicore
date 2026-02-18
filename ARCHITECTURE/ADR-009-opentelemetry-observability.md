# ADR-009: OpenTelemetry Observability

## Status

Accepted

## Date

2025-02-18

## Context

HPC system management platforms are operationally critical. When something goes wrong
during node boot, hardware discovery, or authentication, operators need to quickly
identify what failed, where, and why. This requires consistent, correlated observability
across all Chamicore services.

Upstream OpenCHAMI has minimal observability: basic structured logging, no distributed
tracing, and no standardized metrics. Each service rolls its own approach (or none at all).

We need a unified observability strategy that covers:
- **Metrics**: Request rates, latencies, error rates, resource usage, business metrics.
- **Traces**: Request flows across services (e.g., CLI -> SMD -> DB, BSS -> SMD).
- **Log correlation**: Connecting log entries to traces and requests.

The industry is converging on [OpenTelemetry](https://opentelemetry.io/) (OTel) as the
vendor-neutral standard for telemetry data collection. OTel provides SDKs, APIs, and a
collector agent that can export to any backend (Prometheus, Grafana, Jaeger, Datadog, etc.).

## Decision

All Chamicore services will use **OpenTelemetry** for metrics and distributed tracing,
with a Prometheus scrape endpoint for pull-based metric collection.

### Metrics

- Use the OTel Metrics SDK with OTLP gRPC exporter (push to OTel Collector).
- Additionally expose a `GET /metrics` Prometheus endpoint per service for pull-based scraping.
- Standard HTTP metrics on every service:
  - `http_server_request_duration_seconds` (histogram)
  - `http_server_requests_total` (counter)
  - `http_server_active_requests` (up/down counter)
  - `http_server_request_size_bytes` / `http_server_response_size_bytes` (histograms)
- Database metrics: pool stats, query durations.
- Service-specific business metrics (e.g., components registered, boot scripts served).
- Consistent attribute names following OTel semantic conventions.

### Traces

- Use the OTel Tracing SDK with OTLP gRPC exporter.
- W3C Trace Context propagation (`traceparent` / `tracestate` headers).
- Automatic span creation for incoming HTTP requests (via middleware).
- Automatic trace context propagation on outgoing HTTP requests (via base client).
- Database query spans with `db.statement` and `db.operation` attributes.
- Trace IDs included in structured log output for correlation.

### Implementation

- Centralized OTel initialization in `chamicore-lib/otel/` package.
- HTTP middleware (`otelutil.HTTPMetrics()`, `otelutil.HTTPTracing()`) applied as the
  outermost layer in the middleware stack to capture the full request lifecycle.
- Database instrumentation via `otelutil.InstrumentDB()` wrapping `*sql.DB`.
- Clean shutdown via `otelutil.Init()` returning a shutdown function for graceful drain.

### Deployment

- Dev environment (Docker Compose) includes OTel Collector, Prometheus, Grafana, and Jaeger.
- Production uses OTel Collector as a sidecar or DaemonSet, exporting to the site's
  preferred backends.

### Configuration

- OTel SDK standard environment variables (`OTEL_SERVICE_NAME`, `OTEL_EXPORTER_OTLP_ENDPOINT`, etc.).
- Chamicore-specific toggles: `CHAMICORE_<SERVICE>_METRICS_ENABLED`, `CHAMICORE_<SERVICE>_TRACES_ENABLED`.
- Metrics and traces are enabled by default; can be disabled per-service if needed.

## Consequences

### Positive

- Vendor-neutral: OTel exports to any backend (Prometheus, Grafana Cloud, Datadog, etc.).
- Consistent metrics and traces across all services with no per-service custom code.
- Distributed tracing connects requests across services, critical for debugging boot flows.
- Prometheus endpoint provides compatibility with existing monitoring infrastructure.
- Log-trace correlation speeds up incident investigation.
- OTel Collector decouples services from backend choice; swap backends without code changes.
- Standard OTel semantic conventions mean dashboards and alerts are portable.

### Negative

- OTel SDK adds dependencies and a small runtime overhead.
  - Mitigated: Overhead is minimal; metrics/traces can be disabled per-service.
- OTel ecosystem is large; teams need to learn the SDK and conventions.
  - Mitigated: `chamicore-lib/otel/` abstracts the SDK; services call `Init()` + middleware.
- Collector adds an operational component to deploy and manage.
  - Mitigated: Simple configuration; well-documented Helm chart available.

### Neutral

- Metric names and attributes should follow OTel semantic conventions and be reviewed
  as new metrics are added.
- Trace sampling strategy (head-based vs tail-based) will depend on production volume.
- Grafana dashboards and alerting rules are part of `chamicore-deploy`, not individual services.
