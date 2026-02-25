#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
THRESHOLDS_FILE_REL="${1:-quality/thresholds.txt}"
THRESHOLDS_FILE="$ROOT_DIR/$THRESHOLDS_FILE_REL"

if [[ ! -f "$THRESHOLDS_FILE" ]]; then
    echo "threshold file not found: $THRESHOLDS_FILE_REL" >&2
    exit 1
fi

normalize_thresholds() {
    local file="$1"
    awk '
        BEGIN { ok = 1 }
        /^[[:space:]]*#/ || NF == 0 { next }
        NF != 3 {
            printf("invalid threshold line (%s:%d): expected 3 columns, got %d\n", FILENAME, NR, NF) > "/dev/stderr"
            ok = 0
            next
        }
        $1 !~ /^(coverage|mutation)$/ {
            printf("invalid metric (%s:%d): %s\n", FILENAME, NR, $1) > "/dev/stderr"
            ok = 0
            next
        }
        $3 !~ /^[0-9]+(\.[0-9]+)?$/ {
            printf("invalid threshold value (%s:%d): %s\n", FILENAME, NR, $3) > "/dev/stderr"
            ok = 0
            next
        }
        {
            key = $1 "|" $2
            if (seen[key]++) {
                printf("duplicate threshold key (%s:%d): %s %s\n", FILENAME, NR, $1, $2) > "/dev/stderr"
                ok = 0
            }
            printf("%s\t%s\t%s\n", $1, $2, $3)
        }
        END {
            if (!ok) {
                exit 1
            }
        }
    ' "$file" | sort
}

NEW_TMP="$(mktemp)"
OLD_TMP="$(mktemp)"
trap 'rm -f "$NEW_TMP" "$OLD_TMP"' EXIT

normalize_thresholds "$THRESHOLDS_FILE" > "$NEW_TMP"

if [[ "$THRESHOLDS_FILE_REL" == "quality/thresholds.txt" ]]; then
    while IFS=$'\t' read -r metric scope value; do
        [[ "$metric" == "coverage" ]] || continue
        if ! awk -v threshold="$value" 'BEGIN { exit !(threshold + 0 >= 100.0) }'; then
            echo "policy violation: coverage threshold for $scope is below strict minimum: $value" >&2
            exit 1
        fi
    done < "$NEW_TMP"
fi

if ! git -C "$ROOT_DIR" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    echo "not a git repository, skipping ratchet check"
    exit 0
fi

if ! git -C "$ROOT_DIR" cat-file -e "HEAD:$THRESHOLDS_FILE_REL" 2>/dev/null; then
    echo "no baseline threshold file in HEAD; ratchet check skipped"
    exit 0
fi

git -C "$ROOT_DIR" show "HEAD:$THRESHOLDS_FILE_REL" > "$OLD_TMP"
normalize_thresholds "$OLD_TMP" > "$OLD_TMP.normalized"
mv "$OLD_TMP.normalized" "$OLD_TMP"

status=0
while IFS=$'\t' read -r metric scope old_value; do
    new_value="$(awk -F '\t' -v m="$metric" -v s="$scope" '$1 == m && $2 == s { print $3 }' "$NEW_TMP")"
    if [[ -z "$new_value" ]]; then
        echo "ratchet violation: removed threshold entry $metric $scope" >&2
        status=1
        continue
    fi

    if ! awk -v old="$old_value" -v new="$new_value" 'BEGIN { exit !(new + 0 >= old + 0) }'; then
        echo "ratchet violation: $metric $scope reduced from $old_value to $new_value" >&2
        status=1
    fi
done < "$OLD_TMP"

if [[ "$status" -ne 0 ]]; then
    exit "$status"
fi

echo "threshold ratchet check passed: no decreases detected"
