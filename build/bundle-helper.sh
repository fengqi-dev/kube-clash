#!/usr/bin/env bash
# Build kubeloop-helper (and Windows install/uninstall tools) before the desktop
# application so they can be embedded / packaged.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"

VERSION="${1:-dev}"
PLATFORM="$(go env GOOS)/$(go env GOARCH)"
export VITE_APP_VERSION="${VERSION}"
go run ./build/helper-prebuild.go "${PLATFORM}"
