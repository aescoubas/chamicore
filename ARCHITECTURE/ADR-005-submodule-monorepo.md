# ADR-005: Submodule-Based Monorepo

## Status

Accepted

## Date

2025-02-18

## Context

OpenCHAMI's 54 separate repositories create significant friction:
- Difficult to make cross-service changes.
- No unified view of the system.
- Dependency management across repos is manual.
- CI/CD pipelines are fragmented.

We need to decide how to organize our reduced set of repositories. Options considered:
1. **Fully separate repos** (status quo) - Independent repos, no umbrella.
2. **True monorepo** (single Go module) - All code in one repo.
3. **Submodule-based monorepo** - Umbrella repo with submodules.

## Decision

We will use a **submodule-based monorepo** structure:

- `chamicore/` is the umbrella repository containing documentation, ADRs, Makefile, and
  submodule references.
- Each service is its own Git repository with its own `go.mod`, CI pipeline, and release cycle.
- Service repos are added as Git submodules under `services/` and `shared/`.

```
chamicore/
  services/
    chamicore-smd/     (submodule -> git.cscs.ch/openchami/chamicore-smd)
    chamicore-bss/     (submodule -> git.cscs.ch/openchami/chamicore-bss)
    ...
  shared/
    chamicore-lib/     (submodule -> git.cscs.ch/openchami/chamicore-lib)
```

### Submodule Workflow

- Clone with `--recurse-submodules` for the full checkout.
- Develop within individual service directories (each is a full Git repo).
- The umbrella repo pins submodule commits; update to advance.

## Consequences

### Positive

- Each service maintains independent versioning and release cycles.
- Unified view of the entire system from the umbrella repo.
- Developers can clone just the service they need, or the full monorepo.
- Cross-service documentation and shared tooling live in one place.
- CI/CD can run per-service (on submodule push) or system-wide (on umbrella push).
- `go.mod` per service avoids dependency conflicts between services.

### Negative

- Git submodules have a learning curve and can confuse developers.
  - Mitigated: Clear documentation in AGENTS.md with common workflows.
- Submodule references can become stale if not updated regularly.
  - Mitigated: Makefile targets for `submodule update`.
- Two-step commit process for cross-service changes (commit in submodule, update reference).
  - Accepted trade-off for independent versioning.

### Neutral

- GitLab handles submodules well in the web UI and CI.
- Developers primarily work in individual service repos day-to-day.
- The umbrella repo is mainly for documentation, integration, and CI orchestration.
