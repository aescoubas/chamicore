#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
THRESHOLDS_FILE_REL="${1:-quality/thresholds.txt}"
MUTATION_FILE_REL="${2:-quality/mutation-scores.txt}"
REQUIRE_MUTATION="${3:-0}"

THRESHOLDS_FILE="$ROOT_DIR/$THRESHOLDS_FILE_REL"
MUTATION_FILE="$ROOT_DIR/$MUTATION_FILE_REL"

if [[ ! -f "$THRESHOLDS_FILE" ]]; then
    echo "threshold file not found: $THRESHOLDS_FILE_REL" >&2
    exit 1
fi

if ! awk '/^[[:space:]]*#/ || NF == 0 { next } $1 == "mutation" { found = 1 } END { exit !found }' "$THRESHOLDS_FILE"; then
    echo "no mutation thresholds configured, skipping mutation check"
    exit 0
fi

if [[ ! -f "$MUTATION_FILE" ]]; then
    if [[ "$REQUIRE_MUTATION" == "1" ]]; then
        echo "mutation scores file missing and required: $MUTATION_FILE_REL" >&2
        exit 1
    fi
    echo "mutation scores file missing, skipping mutation check: $MUTATION_FILE_REL"
    exit 0
fi

if ! awk '
    BEGIN { ok = 1 }
    /^[[:space:]]*#/ || NF == 0 { next }
    NF != 2 {
        printf("invalid mutation score line (%s:%d): expected 2 columns\n", FILENAME, NR) > "/dev/stderr"
        ok = 0
        next
    }
    $2 !~ /^[0-9]+(\.[0-9]+)?$/ {
        printf("invalid mutation score value (%s:%d): %s\n", FILENAME, NR, $2) > "/dev/stderr"
        ok = 0
    }
    END { exit !ok }
' "$MUTATION_FILE"; then
    exit 1
fi

status=0
while read -r metric scope threshold; do
    [[ "$metric" == "mutation" ]] || continue

    score="$(awk -v target="$scope" '$1 == target { print $2 }' "$MUTATION_FILE")"
    if [[ -z "$score" ]]; then
        echo "missing mutation score for $scope" >&2
        status=1
        continue
    fi

    if awk -v actual="$score" -v min="$threshold" 'BEGIN { exit !(actual + 0 >= min + 0) }'; then
        echo "mutation OK for $scope: $score% >= $threshold%"
    else
        echo "mutation FAIL for $scope: $score% < $threshold%" >&2
        status=1
    fi
done < <(awk '/^[[:space:]]*#/ || NF == 0 { next } { print $1, $2, $3 }' "$THRESHOLDS_FILE")

exit "$status"
