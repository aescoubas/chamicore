#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
THRESHOLDS_FILE_REL="${1:-quality/thresholds.txt}"
THRESHOLDS_FILE="$ROOT_DIR/$THRESHOLDS_FILE_REL"

if [[ ! -f "$THRESHOLDS_FILE" ]]; then
    echo "threshold file not found: $THRESHOLDS_FILE_REL" >&2
    exit 1
fi

status=0

while read -r metric scope threshold; do
    [[ "$metric" == "coverage" ]] || continue

    module_dir="$ROOT_DIR/$scope"
    if [[ ! -d "$module_dir" ]]; then
        echo "coverage threshold references missing directory: $scope" >&2
        status=1
        continue
    fi

    if [[ ! -f "$module_dir/go.mod" ]]; then
        echo "skipping coverage threshold for non-Go module: $scope"
        continue
    fi

    coverage_file="$module_dir/coverage.out"
    if [[ ! -f "$coverage_file" ]]; then
        echo "coverage.out missing in $scope, generating"
        (
            cd "$module_dir"
            go test -covermode=atomic -coverprofile=coverage.out ./...
        )
    fi

    total="$(
        cd "$module_dir"
        go tool cover -func=coverage.out | awk '/^total:/ { gsub("%", "", $3); print $3 }'
    )"
    if [[ -z "$total" ]]; then
        echo "failed to read total coverage from $coverage_file" >&2
        status=1
        continue
    fi

    if awk -v actual="$total" -v min="$threshold" 'BEGIN { exit !(actual + 0 >= min + 0) }'; then
        echo "coverage OK for $scope: $total% >= $threshold%"
    else
        echo "coverage FAIL for $scope: $total% < $threshold%" >&2
        status=1
    fi
done < <(awk '/^[[:space:]]*#/ || NF == 0 { next } { print $1, $2, $3 }' "$THRESHOLDS_FILE")

exit "$status"
