#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
K6_BIN="${K6_BIN:-k6}"
K6_DOCKER_IMAGE="${K6_DOCKER_IMAGE:-grafana/k6:0.49.0}"

if command -v "$K6_BIN" >/dev/null 2>&1; then
	exec "$K6_BIN" "$@"
fi

if command -v docker >/dev/null 2>&1; then
	exec docker run --rm \
		--network host \
		-e K6_PROMETHEUS_RW_SERVER_URL \
		-v "$ROOT_DIR:/work" \
		-w /work \
		"$K6_DOCKER_IMAGE" \
		"$@"
fi

echo "k6 not found in PATH and docker is unavailable; cannot run load tests" >&2
exit 1
