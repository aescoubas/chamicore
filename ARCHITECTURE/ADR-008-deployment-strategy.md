# ADR-008: Deployment Strategy

## Status

Accepted

## Date

2025-02-18

## Context

Chamicore needs deployment tooling for two primary scenarios:

1. **Production**: Kubernetes clusters at HPC sites, requiring reproducible,
   configurable deployments with proper secrets management and scaling.
2. **Development**: Local machines where developers need to spin up the full
   stack quickly for testing and development.

Upstream OpenCHAMI uses a mix of Docker Compose and Helm charts across multiple
repositories (deployment-recipes, quickstart, etc.), which can be confusing.

## Decision

We will centralize all deployment tooling in the **chamicore-deploy** repository:

### Helm Charts (Production)

```
chamicore-deploy/
  charts/
    chamicore/                    # Umbrella chart
      Chart.yaml
      values.yaml
      charts/
        chamicore-smd/
        chamicore-bss/
        chamicore-cloud-init/
        chamicore-auth/
        chamicore-ui/
        chamicore-kea-sync/
        postgresql/               # Bitnami PostgreSQL subchart
```

- Umbrella Helm chart deploys the full stack.
- Individual service charts can be installed separately.
- Configuration via `values.yaml` overrides.
- Secrets managed via Kubernetes Secrets (not Vault).

### Docker Compose (Development)

```
chamicore-deploy/
  compose/
    docker-compose.yml            # Full stack
    docker-compose.dev.yml        # Dev overrides (hot reload, debug ports)
    .env.example                  # Example environment variables
```

- Single `docker-compose up` brings up all services + PostgreSQL.
- Dev override adds volume mounts for live code reload.
- Services start in dependency order (PostgreSQL -> chamicore-auth -> SMD -> BSS, etc.).

### Container Images

- Each service has a `Dockerfile` in its repository.
- Multi-stage builds: build stage (Go compiler) -> runtime stage (distroless/static).
- Images tagged with: `latest`, `vX.Y.Z`, and Git commit SHA.
- Container registry: GitLab Container Registry (`registry.cscs.ch/openchami/`).

### CI/CD (GitLab)

Each service repository has a `.gitlab-ci.yml` that:
1. Runs linting and tests.
2. Builds the Go binary.
3. Builds and pushes the Docker image.
4. (On tag) Creates a release with GoReleaser.

The umbrella repo has a pipeline that:
1. Runs integration tests using Docker Compose.
2. Validates Helm chart templates.
3. Publishes Helm charts to the chart registry.

## Consequences

### Positive

- Single repository for all deployment artifacts reduces confusion.
- Developers get a working full stack with one command (`docker-compose up`).
- Helm charts follow Kubernetes best practices for production deployment.
- GitLab CI/CD integrates natively with GitLab Container Registry and Helm.
- Consistent image tagging enables reproducible deployments.

### Negative

- Helm chart maintenance requires Kubernetes expertise.
  - Mitigated: Start simple, iterate as requirements emerge.
- Docker Compose dev environment may diverge from production Helm deployment.
  - Mitigated: Use the same container images and environment variable conventions.
- CI/CD is GitLab-specific.
  - Accepted: We host on GitLab (git.cscs.ch); other CI systems can be added later.

### Neutral

- GoReleaser handles binary releases and changelogs automatically.
- Container image scanning can be added to CI later.
- Helm chart testing (helm unittest, ct lint) can be added incrementally.
