#!/usr/bin/env bash
# Build kubeloop-helper before the desktop application so it can be embedded.
# At runtime the desktop app materializes it under ~/.kubeloop/helper/.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"

VERSION="${1:-dev}"
OUT_DIR="${2:-build/embedded}"
mkdir -p "${OUT_DIR}"

HELPER_NAME="kubeloop-helper"
if [[ "$(go env GOOS)" == "windows" ]]; then
  HELPER_NAME="kubeloop-helper.exe"
fi

echo "==> Building ${HELPER_NAME} (version=${VERSION})"
go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
  -o "${OUT_DIR}/${HELPER_NAME}" \
  ./cmd/kubeloop-helper
chmod 755 "${OUT_DIR}/${HELPER_NAME}"
echo "==> Ready to embed ${OUT_DIR}/${HELPER_NAME}"
