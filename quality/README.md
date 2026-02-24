# Quality CLI Workflow

The repository exposes local quality gates as CLI-first Make targets.

## Commands

```bash
make quality-gate
```

Runs:

1. Threshold ratchet validation (`quality/thresholds.txt` cannot decrease).
2. Lint (`make lint`).
3. Unit tests with race detector (`make test`).
4. Unit tests with package shuffle (`make test-shuffle`).
5. Coverage run (`make test-cover`).
6. Per-module coverage threshold checks (`make quality-coverage`).
7. Integration tests (`make test-integration`).

```bash
make quality-db
```

Runs database quality checks against an ephemeral PostgreSQL container:

1. Migration lifecycle for each service schema (`up -> down -> up`).
2. Expected table/index/constraint assertions.
3. Critical query plan assertions (index usage checks).

```bash
make release-gate
```

Runs `quality-gate`, `quality-db`, and mutation threshold validation, then writes a
signed report:

- `quality/reports/<git-sha>.json`
- `quality/reports/<git-sha>.json.sha256`

Optional:

```bash
RELEASE_TAG=vX.Y.Z make release-gate
```

This creates an annotated Git tag only after report generation and validation.

## Threshold Ratcheting

Thresholds are defined in `quality/thresholds.txt`:

- `coverage <module-path> <percent>`
- `mutation <package-scope> <percent>`

`make quality-ratchet` compares the current file to `HEAD` and fails if any existing
threshold is reduced or removed.

## Mutation Scores

Provide scores in `quality/mutation-scores.txt` using:

```txt
<scope> <score-percent>
```

Use `quality/mutation-scores.example.txt` as a template.
