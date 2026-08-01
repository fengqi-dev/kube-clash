#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
KUBELOOP_LOCAL_OS=linux exec bash "${SCRIPT_DIR}/run-local-unix.sh" "$@"
