#!/usr/bin/env bash
# Run the Venom suites — the integration tests.
#
#   bash test/run-venom.sh local              all suites
#   bash test/run-venom.sh local 01-mcp-surface.yml   just one
set -euo pipefail

VENOM_VERSION="v1.2.0"
TEST_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(dirname "$TEST_DIR")"
VENOM_DIR="${TEST_DIR}/venom"
VENOM_BIN="${VENOM_DIR}/bin/venom"
export VENOM_LOG_DIR="${VENOM_DIR}/log"

ENVIRONMENT="${1:-}"
SUITE="${2:-}"

if [ -z "$ENVIRONMENT" ]; then
  echo "usage: bash test/run-venom.sh <environment> [suite.yml]"
  echo
  echo "environments:"
  for f in "${TEST_DIR}"/environments/*.yml; do
    echo "  $(basename "$f" .yml)"
  done
  exit 2
fi

ENV_FILE="${TEST_DIR}/environments/${ENVIRONMENT}.yml"
if [ ! -f "$ENV_FILE" ]; then
  echo "no such environment: ${ENVIRONMENT}" >&2
  exit 2
fi

# The runner is pinned and kept inside the repository, so changing its version
# is a commit rather than a migration on every machine.
if [ ! -x "$VENOM_BIN" ]; then
  mkdir -p "$(dirname "$VENOM_BIN")"
  OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
  ARCH="$(uname -m)"
  case "$ARCH" in
    x86_64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
  esac
  URL="https://github.com/ovh/venom/releases/download/${VENOM_VERSION}/venom.${OS}-${ARCH}"
  echo "downloading venom ${VENOM_VERSION} for ${OS}-${ARCH}"
  curl -fsSL -o "$VENOM_BIN" "$URL"
  chmod +x "$VENOM_BIN"
fi

# The binary under test must exist; the suites drive it directly.
if [ ! -x "${REPO_DIR}/build/gocc2cc" ]; then
  echo "build/gocc2cc is missing — run 'make build' first" >&2
  exit 2
fi

rm -rf "$VENOM_LOG_DIR"
mkdir -p "$VENOM_LOG_DIR"

if [ -n "$SUITE" ]; then
  SUITES=("${TEST_DIR}/${SUITE}")
else
  SUITES=("${TEST_DIR}"/[0-9][0-9]-*.yml)
fi

# The run is wrapped so the post-processing below happens even when tests fail —
# which is exactly when its output is wanted.
set +e
# Venom runs exec steps from the suite's own directory, so the binary is passed
# as an absolute path rather than one relative to the repository root.
"$VENOM_BIN" run "${SUITES[@]}" \
  --var-from-file "$ENV_FILE" \
  --var "binary=${REPO_DIR}/build/gocc2cc" \
  -vv \
  --output-dir "$VENOM_LOG_DIR"
VENOM_EXIT=$?
set -e

# Turn the dumps into something readable. When a suite fails this is where the
# time actually goes.
TRACE="${VENOM_LOG_DIR}/traces.log"
: > "$TRACE"
if command -v jq >/dev/null 2>&1; then
  for dump in "$VENOM_LOG_DIR"/*.dump.json; do
    [ -e "$dump" ] || continue
    {
      echo "===== $(basename "$dump") ====="
      jq -r '.. | objects | select(has("systemout")) | ">>> stdout >>>\n" + .systemout' "$dump" 2>/dev/null || true
      echo
    } >> "$TRACE"
  done
  echo "traces: $TRACE"
fi

exit "$VENOM_EXIT"
