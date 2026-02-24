# Architecture Decision Records

This directory contains Architecture Decision Records (ADRs) for the Chamicore project.
ADRs document significant technical decisions, their context, and their consequences.

## Format

All ADRs follow the template in [TEMPLATE.md](TEMPLATE.md). Each ADR has:

- **Status**: Proposed, Accepted, Deprecated, or Superseded
- **Date**: When the decision was made
- **Context**: The problem or situation that prompted the decision
- **Decision**: What was decided
- **Consequences**: Trade-offs and implications (positive, negative, neutral)

## How to Add a New ADR

1. Copy `TEMPLATE.md` to `ADR-NNN-short-title.md` (use the next available number).
2. Fill in all sections.
3. Add an entry to the index below.
4. Submit a merge request for review.

## Index

| ADR | Title | Status | Date |
|-----|-------|--------|------|
| [ADR-001](ADR-001-clean-room-rewrite.md) | Clean-Room Rewrite | Accepted | 2025-02-18 |
| [ADR-002](ADR-002-microservice-selection.md) | Microservice Selection | Accepted | 2025-02-18 |
| [ADR-003](ADR-003-shared-postgresql.md) | Shared PostgreSQL | Accepted | 2025-02-18 |
| [ADR-004](ADR-004-go-chi-framework.md) | Go + go-chi Framework | Accepted | 2025-02-18 |
| [ADR-005](ADR-005-submodule-monorepo.md) | Submodule-Based Monorepo | Accepted | 2025-02-18 |
| [ADR-006](ADR-006-authentication-oidc-jwt.md) | Authentication (OIDC/JWT) | Superseded by ADR-011 | 2025-02-18 |
| [ADR-007](ADR-007-api-design-conventions.md) | API Design Conventions | Accepted | 2025-02-18 |
| [ADR-008](ADR-008-deployment-strategy.md) | Deployment Strategy | Accepted | 2025-02-18 |
| [ADR-009](ADR-009-opentelemetry-observability.md) | OpenTelemetry Observability | Accepted | 2025-02-18 |
| [ADR-010](ADR-010-component-identifiers.md) | Component Identifiers (Flat IDs) | Accepted | 2025-02-18 |
| [ADR-011](ADR-011-consolidated-auth-service.md) | Consolidated Auth Service | Accepted | 2025-02-18 |
| [ADR-012](ADR-012-performance-testing-strategy.md) | Performance Testing Strategy | Accepted | 2025-02-18 |
| [ADR-013](ADR-013-dedicated-discovery-service.md) | Dedicated Discovery Service | Accepted | 2026-02-18 |
| [ADR-014](ADR-014-boot-path-data-flow.md) | Boot-Path Data Flow and Service Self-Sufficiency | Accepted | 2026-02-18 |
| [ADR-015](ADR-015-event-driven-architecture.md) | Event-Driven Architecture (NATS JetStream) | Proposed | 2026-02-18 |
| [ADR-016](ADR-016-quality-engineering-policy.md) | Quality Engineering Policy and Database Drift Control | Accepted | 2026-02-24 |
| [ADR-017](ADR-017-power-control-service.md) | Power Control Service (PCS-Compatible) and Shared Redfish Library | Accepted | 2026-02-24 |
