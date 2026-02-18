# ADR-002: Microservice Selection

## Status

Accepted

## Date

2025-02-18

## Context

OpenCHAMI has approximately 54 repositories. Many are forks of Cray-HPE libraries, helper
utilities, deprecated experiments, or single-purpose tools. We need to identify the essential
services required for a functional HPC system management platform and exclude the rest.

The core workflow that must be supported:
1. Discover hardware via Redfish BMC endpoints.
2. Register and track hardware inventory and state.
3. Serve DHCP/DNS for PXE booting.
4. Generate and serve iPXE boot scripts.
5. Serve cloud-init payloads for node configuration.
6. Authenticate and authorize API access.

## Decision

We will implement **7 core services + 1 Web UI + 1 CLI + 1 deployment repo + 1 shared library**:

| Repository | Purpose |
|------------|---------|
| chamicore-smd | State Management Daemon - central inventory and hardware state |
| chamicore-bss | Boot Script Service - iPXE boot script generation |
| chamicore-cloud-init | Cloud-Init - per-node cloud-init payloads |
| chamicore-kea-sync | Sync daemon: SMD inventory -> Kea DHCP reservations for PXE/iPXE |
| chamicore-discovery | Hardware discovery - dual-mode service + sysadmin CLI (see [ADR-013](ADR-013-dedicated-discovery-service.md)) |
| chamicore-auth | AuthN (OIDC federation, token exchange) + AuthZ (Casbin RBAC/ABAC) + device credential store |
| chamicore-ui | Web management UI (Go backend + Vue.js frontend) |
| chamicore-cli | CLI client: per-service commands + composite multi-service workflows |
| chamicore-deploy | Helm charts and Docker Compose files |
| chamicore-lib | Shared Go library |

### What Was Excluded

| Upstream Repo | Reason for Exclusion |
|---------------|---------------------|
| hms-* libraries | Replaced by chamicore-lib |
| magellan | Replaced by chamicore-discovery (see [ADR-013](ADR-013-dedicated-discovery-service.md)) |
| configurator | Functionality absorbed into cloud-init and CLI |
| image-builder | Out of scope; use existing tools (Packer, etc.) |
| node-orchestrator | Out of scope for initial release |
| coresmd (CoreDHCP/CoreDNS plugins) | Replaced by Kea DHCP + chamicore-kea-sync; Kea is a production-grade DHCP server |
| Various forks | No longer needed with clean-room approach |

## Consequences

### Positive

- Dramatically reduced repository count (54 -> 11). Uses Kea DHCP externally instead of custom CoreDHCP plugins.
- Clear ownership and boundaries for each service.
- Every repository has a well-defined purpose.
- Manageable scope for initial implementation.

### Negative

- Some upstream features are intentionally excluded and must be added later if needed.
- Users of excluded services need migration paths or alternatives.

### Neutral

- Future services can be added following the established pattern.
- The shared library (chamicore-lib) must be carefully designed to avoid becoming a monolith.
