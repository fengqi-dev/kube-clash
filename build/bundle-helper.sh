#!/usr/bin/env bash
# Build kubeloop-helper and place it next to the desktop app for packaging.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"

VERSION="${1:-dev}"
OUT_DIR="${2:-build/bin}"
mkdir -p "${OUT_DIR}"

HELPER_NAME="kubeloop-helper"
if [[ "$(go env GOOS)" == "windows" ]]; then
  HELPER_NAME="kubeloop-helper.exe"
fi

echo "==> Building ${HELPER_NAME} (version=${VERSION})"
go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
  -o "${OUT_DIR}/${HELPER_NAME}" \
  ./cmd/kubeloop-helper

# Nest into macOS .app bundle when present.
shopt -s nullglob
for app in "${OUT_DIR}"/*.app; do
  dest="${app}/Contents/Helpers"
  mkdir -p "${dest}"
  cp "${OUT_DIR}/${HELPER_NAME}" "${dest}/${HELPER_NAME}"
  chmod 755 "${dest}/${HELPER_NAME}"
  echo "==> Copied helper into ${dest}/"
done
