#!/usr/bin/env bash
# Run Go tests with coverage and fail if total statements coverage is below MIN_COVER.
set -euo pipefail

MIN_COVER="${MIN_COVER:-80}"
MODULE_DIR="${1:-.}"

cd "$MODULE_DIR"

PKGS="$(go list ./... | grep -vE '/callback/v1$' || true)"
if [[ -z "${PKGS}" ]]; then
  echo "no packages to test in ${MODULE_DIR}"
  exit 1
fi

COVER_FILE="$(mktemp)"
trap 'rm -f "$COVER_FILE"' EXIT

# shellcheck disable=SC2086
go test ${PKGS} -count=1 -covermode=atomic "-coverprofile=${COVER_FILE}"

TOTAL_LINE="$(go tool cover -func="$COVER_FILE" | tail -n 1)"
# Example: total: (statements) 80.7%
PCT="$(echo "$TOTAL_LINE" | awk '{print $NF}' | tr -d '%')"

if [[ -z "$PCT" ]]; then
  echo "failed to parse coverage from: $TOTAL_LINE"
  exit 1
fi

echo "coverage total: ${PCT}% (minimum ${MIN_COVER}%) in ${MODULE_DIR}"

awk -v pct="$PCT" -v min="$MIN_COVER" 'BEGIN {
  if (pct+0 < min+0) {
    printf "coverage %.1f%% is below minimum %s%%\n", pct, min > "/dev/stderr"
    exit 1
  }
}'
