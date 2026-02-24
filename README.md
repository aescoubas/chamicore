# Chamicore

Chamicore is a clean-room rewrite of the [OpenCHAMI](https://github.com/OpenCHAMI) HPC system management platform. It consolidates the essential services into a submodule-based monorepo with consistent patterns, unified PostgreSQL storage, and simplified deployment.

## Architecture

```
              +-----------+         +-----------+
              | CLI       |         | Web UI    |
              | (ochami)  |         | :8080     |
              +-----+-----+         +-----+-----+
                    |                      |
          +-+------++-------+-------+-----++-------+
          | |       |       |       |      |       |
      +---v-v-+  +-v---+  +v------+  +---v---+  +-v--------+
      | Auth  |  | SMD |  | BSS   |  | Cloud |  | Disc-    |
      | :3333 |  |:27779| |:27778 |  | Init  |  | overy    |
      | Casbin|  |      | |       |  |:27777 |  | :27776   |
      +---+---+  +--+---+ +--+----+  +---+---+  +----+-----+
          |          |        |           |            |
          +-----+----+--------+-----------+       (not in
                |                              boot path)
         +------v------+         +----------+
         | PostgreSQL  |         +----------+
         | (shared)    |         | Kea-Sync |
         +-------------+         |    |     |
                                 +----+-----+
                                      |
                                 +----v-----+
                                 | Kea DHCP |
                                 | (PXE/    |
                                 |  iPXE)   |
                                 +----------+
```

## Services

| Service | Port | Description |
|---------|------|-------------|
| [chamicore-smd](services/chamicore-smd/) | 27779 | State Management Daemon - central inventory and hardware state |
| [chamicore-bss](services/chamicore-bss/) | 27778 | Boot Script Service - iPXE boot scripts, kernel/initrd management |
| [chamicore-cloud-init](services/chamicore-cloud-init/) | 27777 | Cloud-Init - per-node cloud-init payloads |
| [chamicore-kea-sync](services/chamicore-kea-sync/) | N/A | Syncs SMD inventory to Kea DHCP server for PXE/iPXE boot |
| [chamicore-discovery](services/chamicore-discovery/) | 27776 | Hardware discovery - service + sysadmin CLI (Redfish, IPMI, SNMP, CSV import) |
| [chamicore-auth](services/chamicore-auth/) | 3333 | AuthN/AuthZ (OIDC, Casbin) + device credential store |
| [chamicore-ui](services/chamicore-ui/) | 8080 | Web management UI (Go backend + Vue.js frontend) |
| [chamicore-cli](services/chamicore-cli/) | N/A | CLI client: per-service commands + composite multi-service workflows |
| [chamicore-deploy](shared/chamicore-deploy/) | N/A | Helm charts (production) and Docker Compose (development) |
| [chamicore-lib](shared/chamicore-lib/) | N/A | Shared Go library (auth middleware, DB, HTTP utils, identity) |

## Quick Start

### Development Environment

```bash
# Clone with all submodules
git clone --recurse-submodules git@git.cscs.ch:openchami/chamicore.git
cd chamicore

# Start the full stack (requires Docker)
make compose-up

# Optional: use the user-facing gateway endpoint for CLI
export CHAMICORE_ENDPOINT=http://localhost:8080
# export CHAMICORE_TOKEN=<jwt>
# ./bin/chamicore smd components list --limit 5

# Start the full stack and boot a libvirt VM (requires libvirt tooling)
make compose-vm-up
# Optional: use a libvirt network instead of user-mode networking
# CHAMICORE_VM_NETWORK=default make compose-vm-up

# Run tests
make test

# Stop everything
make compose-down
# Or tear down stack + VM
make compose-vm-down
```

### Quality Gates (CLI-first)

```bash
# Full local quality gate
make quality-gate

# Database migration/schema/query-plan gate
make quality-db

# Release gate + signed quality report
make release-gate
# Optional tag after successful gate:
# RELEASE_TAG=v0.1.0 make release-gate
```

See `quality/README.md` for details.

### Prerequisites

- Go 1.24+
- Node.js 20+ and npm (for chamicore-ui frontend)
- Docker and Docker Compose
- libvirt + `virsh` + `virt-install` + `qemu-img` (optional, for `make compose-vm-up`)
- Make
- Git (with submodule support)
- [k6](https://k6.io/) (for load testing, optional)

## Repository Structure

```
chamicore/
  AGENTS.md               # AI-assisted development guide
  ARCHITECTURE/            # Architecture Decision Records (ADRs)
  tests/                   # Cross-service system integration tests
  services/                # Service submodules
    chamicore-smd/
    chamicore-bss/
    chamicore-cloud-init/
    chamicore-kea-sync/
    chamicore-discovery/
    chamicore-auth/
    chamicore-ui/
    chamicore-cli/
  shared/
    chamicore-lib/         # Shared Go library
    chamicore-deploy/      # Helm charts + Docker Compose
```

## Key Decisions

All significant technical decisions are documented as Architecture Decision Records in [`ARCHITECTURE/`](ARCHITECTURE/README.md):

- **[ADR-001](ARCHITECTURE/ADR-001-clean-room-rewrite.md)**: Clean-room rewrite (not a fork)
- **[ADR-002](ARCHITECTURE/ADR-002-microservice-selection.md)**: Why these 7 services + UI + CLI
- **[ADR-003](ARCHITECTURE/ADR-003-shared-postgresql.md)**: Shared PostgreSQL backend
- **[ADR-004](ARCHITECTURE/ADR-004-go-chi-framework.md)**: Go + go-chi + zerolog + jwx
- **[ADR-005](ARCHITECTURE/ADR-005-submodule-monorepo.md)**: Submodule-based monorepo
- **[ADR-006](ARCHITECTURE/ADR-006-authentication-oidc-jwt.md)**: ~~OIDC/JWT authentication~~ (superseded by ADR-011)
- **[ADR-007](ARCHITECTURE/ADR-007-api-design-conventions.md)**: REST API conventions
- **[ADR-008](ARCHITECTURE/ADR-008-deployment-strategy.md)**: Helm + Docker Compose deployment
- **[ADR-009](ARCHITECTURE/ADR-009-opentelemetry-observability.md)**: OpenTelemetry metrics and tracing
- **[ADR-010](ARCHITECTURE/ADR-010-component-identifiers.md)**: Flat opaque IDs replacing xnames
- **[ADR-011](ARCHITECTURE/ADR-011-consolidated-auth-service.md)**: Consolidated auth service (replaces OPAAL + Hydra)
- **[ADR-012](ARCHITECTURE/ADR-012-performance-testing-strategy.md)**: Performance testing strategy (boot-storm at 10k+ nodes)
- **[ADR-013](ARCHITECTURE/ADR-013-dedicated-discovery-service.md)**: Dedicated discovery service (decoupled from SMD)
- **[ADR-014](ARCHITECTURE/ADR-014-boot-path-data-flow.md)**: Boot-path data flow (self-sufficient services, no cross-service calls on hot path)
- **[ADR-015](ARCHITECTURE/ADR-015-event-driven-architecture.md)**: Event-driven architecture via NATS JetStream (Phase 2 change propagation)

## Tech Stack

| Component | Technology |
|-----------|-----------|
| Language | Go 1.24+ (backend), TypeScript (frontend) |
| HTTP Framework | go-chi/chi/v5 |
| Frontend | Vue.js 3 + Vite + Pinia |
| Database | PostgreSQL (shared instance, per-service schemas) |
| Auth | OIDC federation + Casbin RBAC/ABAC via chamicore-auth |
| Logging | rs/zerolog |
| Deployment | Helm (prod) / Docker Compose (dev) |
| CI/CD | GitLab CI |
| Hosting | git.cscs.ch (GitLab) |

## Contributing

This project is built entirely by AI coding agents following strict conventions:

- **[AGENTS.md](AGENTS.md)** — Architecture, coding conventions, mandatory patterns, anti-patterns, verification checklists
- **[IMPLEMENTATION.md](IMPLEMENTATION.md)** — Phased implementation plan with task breakdown and acceptance criteria
- **[templates/](templates/)** — Reference code templates that every service is built from

## License

[MIT](LICENSE)
