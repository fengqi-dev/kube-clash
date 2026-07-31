#!/usr/bin/env bash
# Run TUN e2e packages and print a failed-case summary at the end.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT}"

TIMEOUT="${KUBELOOP_E2E_TIMEOUT:-30m}"
LOG="$(mktemp "${TMPDIR:-/tmp}/kubeloop-e2e.XXXXXX")"
trap 'rm -f "${LOG}"' EXIT

echo "==> Running TUN e2e (log: ${LOG})"
set +e
go test -tags=e2e ./e2e/... -count=1 -timeout="${TIMEOUT}" -parallel=1 -p 1 -v "$@" 2>&1 | tee "${LOG}"
EXIT_CODE=${PIPESTATUS[0]}
set -e

echo
echo "==> e2e summary"
FAILED="$(grep -E '^--- FAIL: ' "${LOG}" | sed -E 's/^--- FAIL: ([^ ]+).*/\1/' || true)"
PKG_FAIL="$(grep -E '^FAIL[[:space:]]' "${LOG}" || true)"

if [[ -z "${FAILED}" && "${EXIT_CODE}" -eq 0 ]]; then
  echo "All e2e tests passed."
  exit 0
fi

if [[ -n "${FAILED}" ]]; then
  echo "Failed tests:"
  while IFS= read -r name; do
    [[ -n "${name}" ]] || continue
    echo "  - ${name}"
  done <<<"${FAILED}"
else
  echo "No per-test FAIL lines parsed (exit=${EXIT_CODE})."
fi

if [[ -n "${PKG_FAIL}" ]]; then
  echo "Failed packages:"
  while IFS= read -r line; do
    [[ -n "${line}" ]] || continue
    echo "  - ${line#FAIL	}"
  done <<<"${PKG_FAIL}"
fi

# Print a short error snippet under each failed test name.
if [[ -n "${FAILED}" ]]; then
  echo
  echo "Failure details:"
  while IFS= read -r name; do
    [[ -n "${name}" ]] || continue
    echo "--- ${name}"
    # From RUN to FAIL for this test; keep the last diagnostic lines.
    awk -v name="${name}" '
      $0 ~ ("=== RUN   " name "$") { buf=""; capturing=1 }
      capturing { buf = buf $0 ORS }
      $0 ~ ("--- FAIL: " name " ") {
        n = split(buf, lines, ORS)
        start = n > 12 ? n - 12 : 1
        for (i = start; i <= n; i++) if (lines[i] != "") print lines[i]
        capturing=0
      }
    ' "${LOG}"
    echo
  done <<<"${FAILED}"
fi

exit "${EXIT_CODE}"
