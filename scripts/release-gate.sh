#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

THRESHOLDS_FILE="${QUALITY_THRESHOLDS_FILE:-quality/thresholds.txt}"
MUTATION_SCORES_FILE="${QUALITY_MUTATION_SCORES:-quality/mutation-scores.txt}"
REQUIRE_MUTATION="${QUALITY_REQUIRE_MUTATION:-0}"
REPORT_DIR="${QUALITY_REPORT_DIR:-quality/reports}"
RELEASE_TAG="${RELEASE_TAG:-}"

if ! command -v sha256sum >/dev/null 2>&1; then
    echo "sha256sum is required for release report signing" >&2
    exit 1
fi

echo "==> Running quality gate"
make quality-gate \
    QUALITY_THRESHOLDS_FILE="$THRESHOLDS_FILE" \
    QUALITY_MUTATION_SCORES="$MUTATION_SCORES_FILE" \
    QUALITY_REQUIRE_MUTATION="$REQUIRE_MUTATION"

echo "==> Running database quality gate"
make quality-db

echo "==> Running mutation threshold gate"
make quality-mutation \
    QUALITY_THRESHOLDS_FILE="$THRESHOLDS_FILE" \
    QUALITY_MUTATION_SCORES="$MUTATION_SCORES_FILE" \
    QUALITY_REQUIRE_MUTATION="$REQUIRE_MUTATION"

commit_sha="$(git rev-parse HEAD)"
generated_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

mkdir -p "$REPORT_DIR"
report_path="$REPORT_DIR/$commit_sha.json"
signature_path="$report_path.sha256"

cat > "$report_path" <<EOF
{
  "commit": "$commit_sha",
  "generated_at": "$generated_at",
  "quality_gate": "pass",
  "quality_db": "pass",
  "quality_mutation": "pass",
  "thresholds_file": "$THRESHOLDS_FILE",
  "mutation_scores_file": "$MUTATION_SCORES_FILE",
  "require_mutation": $REQUIRE_MUTATION
}
EOF

(
    cd "$(dirname "$report_path")"
    sha256sum "$(basename "$report_path")" > "$(basename "$signature_path")"
)

sha256sum -c "$signature_path" >/dev/null

commit_ts="$(git show -s --format=%ct "$commit_sha")"
report_ts="$(stat -c %Y "$report_path")"
if [[ "$report_ts" -lt "$commit_ts" ]]; then
    echo "stale report detected: $report_path predates commit $commit_sha" >&2
    exit 1
fi

echo "release report written: $report_path"
echo "release report signature: $signature_path"

if [[ -n "$RELEASE_TAG" ]]; then
    if git rev-parse -q --verify "refs/tags/$RELEASE_TAG" >/dev/null; then
        echo "tag already exists: $RELEASE_TAG" >&2
        exit 1
    fi

    if ! grep -q "\"commit\": \"$commit_sha\"" "$report_path"; then
        echo "refusing to create tag: report does not match current commit" >&2
        exit 1
    fi

    git tag -a "$RELEASE_TAG" -m "Release $RELEASE_TAG (quality report: $report_path)"
    echo "created annotated tag: $RELEASE_TAG"
fi
