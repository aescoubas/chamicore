# ADR-001: Clean-Room Rewrite

## Status

Accepted

## Date

2025-02-18

## Context

OpenCHAMI is an open-source HPC system management platform that evolved from Cray/HPE's
CSM (Cray System Management) stack. It consists of approximately 54 repositories, many of
which carry significant legacy baggage:

- Deep dependencies on Cray-HPE `hms-*` libraries that are tightly coupled and poorly documented.
- Inconsistent coding styles, error handling, and API patterns across services.
- Multiple database backends (PostgreSQL, SQLite, DuckDB, etcd) without clear rationale.
- Legacy integrations (Vault, Kafka) that add operational complexity without clear benefit
  for our deployment scenarios.
- Some services contain dead code paths, unused features, or Cray-specific logic.

We need to decide whether to fork OpenCHAMI and incrementally refactor, or start fresh
with a clean-room rewrite using OpenCHAMI as a design reference.

## Decision

We will perform a **clean-room rewrite** of the essential OpenCHAMI services. We will use
OpenCHAMI's API contracts, data models, and architectural patterns as design references,
but all code will be written from scratch with consistent conventions.

Key principles:
- Reference OpenCHAMI's API specifications and behavior, not its implementation.
- No code is copied from upstream repositories.
- All services use the same libraries, patterns, and conventions (see ADR-004).
- Simplify where upstream is unnecessarily complex.
- Maintain API compatibility where it benefits users migrating from OpenCHAMI.

## Consequences

### Positive

- Freedom to establish consistent coding conventions from day one.
- No inherited technical debt from Cray-HPE legacy code.
- Simpler dependency tree without `hms-*` libraries.
- Unified database backend (PostgreSQL only) reduces operational complexity.
- Easier onboarding for new contributors with a clean, well-documented codebase.
- AI agents can work effectively with consistent, well-structured code.

### Negative

- Higher initial development effort compared to forking and refactoring.
- Risk of missing edge cases that upstream handles through years of production use.
- No automatic benefit from upstream bug fixes or improvements.
- API compatibility requires careful study of upstream behavior.

### Neutral

- We must explicitly decide which features to carry over and which to drop.
- Documentation of API contracts becomes critical for correctness.
- Testing strategy must be thorough to compensate for lack of production history.
