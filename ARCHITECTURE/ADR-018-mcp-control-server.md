# ADR-018: MCP Control Server for Agent-Driven Cluster Operations

## Status

Accepted

## Date

2026-02-25

## Context

Chamicore has strong per-service APIs and a CLI, but coding agents still interact with
the cluster through ad-hoc shell commands and direct client wiring. This creates three
practical problems:

1. No single, typed control surface for agent workflows across SMD, BSS, Cloud-Init,
   Discovery, and Power.
2. No built-in runtime policy split between safe read-only operation and read-write
   operation.
3. No consistent audit line per agent action with enough context for operator review.

The project needs a dedicated MCP server component that:
- follows existing Chamicore architecture conventions (service boundaries, auth model,
  deployment patterns, RFC 9457 error semantics),
- supports both local coding-agent usage and deployed access,
- starts with a limited V1 toolset and expands later.

## Decision

Introduce a new service, `chamicore-mcp`, as the agent control plane for Chamicore.

### 1. Service Role and Boundaries

- New component: `services/chamicore-mcp`.
- `chamicore-mcp` is an API aggregation layer, not a datastore owner.
- It never accesses service schemas directly and only talks to existing service APIs
  through typed clients.
- It does not replace existing APIs; it exposes curated MCP tools over them.

### 2. Transport Model (V1)

`chamicore-mcp` supports two transports in V1:

1. `stdio` for local agent execution.
2. HTTP/SSE for deployed agent-to-server sessions.

Both transports expose the same tool registry and policy semantics.

### 3. Mode Model and Safety Gates

Two runtime modes are defined:

- `read-only`: only tools tagged `read` are executable.
- `read-write`: both `read` and `write` tools are executable.

Safe defaults and write dual-control:

- default mode is `read-only`.
- `read-write` requires explicit enablement beyond mode selection.
- mode checks are centralized and applied before any tool handler runs.

### 4. Destructive Confirmation Policy (V1)

Certain operations require explicit per-call confirmation (`confirm=true`) in addition
to mode allowance:

- all `*.delete` tools,
- `power.transitions.abort`,
- `power.transitions.create` when the requested operation is one of:
  `ForceOff`, `GracefulShutdown`, `GracefulRestart`, `ForceRestart`, `Nmi`.

Requests that violate confirmation policy fail fast with validation errors.

### 5. V1 Tool Scope

V1 includes a subset of read/write tools across:
- cluster summary,
- SMD components/groups,
- BSS bootparams,
- Cloud-Init payloads,
- Discovery targets/scans/drivers,
- Power status/transitions/actions workflow.

Tool names and request/response schemas are contract-defined in:
- `services/chamicore-mcp/api/tools.yaml`.

### 6. Token and Auth Model (V1)

V1 supports broad admin tokens for rapid adoption while preserving a path to least
privilege:

- broad admin token usage is allowed,
- per-tool required scope metadata remains part of the tool contract for future
  tightening.

Token source strategy:

- environment-first token lookup,
- optional CLI config fallback only when explicitly allowed by configuration.

### 7. Audit and Observability (V1)

Every tool call emits one structured completion audit log to stdout including:
- request/session identity,
- tool name,
- mode,
- principal (when available),
- target summary,
- outcome and duration.

No database or event-bus audit sink is required in V1.

### 8. Error Mapping

Downstream service errors are normalized for MCP responses:
- preserve actionable HTTP/problem details from RFC 9457 when present,
- provide stable tool-level error envelopes for agent handling.

### 9. Deployment

`chamicore-mcp` is supported in both:
- local process mode for direct agent launch,
- deployed mode through Compose and Helm.

Deploy defaults are safety-first (`read-only` by default).

## Consequences

### Positive

- Adds a consistent, typed agent control surface across Chamicore services.
- Reduces accidental mutation risk through default read-only mode and dual-control write
  enablement.
- Makes destructive operations explicit and auditable per call.
- Preserves service ownership boundaries by reusing existing APIs and clients.
- Supports both local development and deployed operation with one contract.

### Negative

- Introduces another service to operate, configure, and monitor.
- Adds policy complexity (mode + confirmation + scope metadata).
- Initial V1 broad-token allowance is less strict than final least-privilege posture.

### Neutral

- V1 intentionally ships with a limited tool subset; additional tools are additive.
- Audit is stdout-only in V1; persistent audit backends can be added later without
  changing tool contracts.
