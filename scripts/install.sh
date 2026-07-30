#!/usr/bin/env bash
# Download the latest KubeLoop desktop release (macOS DMG or Linux tarball).
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/fengqi-dev/kube-loop/main/scripts/install.sh | bash
#   VERSION=v1.1.0 ./scripts/install.sh
#
# macOS: downloads the .dmg into DEST (default: $PWD)
# Linux: downloads and extracts the .tar.gz into DEST (default: $PWD)
set -euo pipefail

REPO="${REPO:-fengqi-dev/kube-loop}"
DEST="${DEST:-$PWD}"
TAG="${VERSION:-${TAG:-}}"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "${os}" in
  linux) os=linux ;;
  darwin) os=darwin ;;
  *)
    echo "unsupported OS: $(uname -s) (use install.ps1 on Windows)" >&2
    exit 1
    ;;
esac

arch="$(uname -m)"
case "${arch}" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *)
    echo "unsupported arch: $(uname -m)" >&2
    exit 1
    ;;
esac

api="https://api.github.com/repos/${REPO}/releases"
if [[ -n "${TAG}" ]]; then
  json="$(curl -fsSL "${api}/tags/${TAG}")"
else
  json="$(curl -fsSL "${api}/latest")"
  TAG="$(printf '%s' "${json}" | sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1)"
fi
if [[ -z "${TAG}" ]]; then
  echo "could not resolve release tag" >&2
  exit 1
fi

ver="${TAG#v}"

asset_exists() {
  printf '%s' "${json}" | grep -Fq "\"name\": \"${1}\""
}

pick_asset() {
  local name
  for name in "$@"; do
    if asset_exists "${name}"; then
      printf '%s' "${name}"
      return 0
    fi
  done
  return 1
}

download_asset() {
  local asset="$1"
  local out="$2"
  local url="https://github.com/${REPO}/releases/download/${TAG}/${asset}"
  echo "Downloading ${asset} (${TAG})..."
  curl -fsSL -o "${out}" "${url}"
}

mkdir -p "${DEST}"

if [[ "${os}" == "darwin" ]]; then
  asset="$(pick_asset \
    "kubeloop-${ver}-darwin-${arch}.dmg" \
    "kubeloop-darwin-${arch}.dmg" || true)"
  if [[ -z "${asset}" ]]; then
    echo "no DMG for macOS/${arch} in ${TAG}" >&2
    exit 1
  fi
  out="${DEST}/${asset}"
  download_asset "${asset}" "${out}"
  echo "Saved ${out}"
  echo "Open the DMG and drag KubeLoop.app into Applications."
  exit 0
fi

asset="$(pick_asset \
  "kubeloop-${ver}-linux-${arch}.tar.gz" \
  "kubeloop-linux-${arch}.tar.gz" || true)"
if [[ -z "${asset}" ]]; then
  echo "no matching linux/${arch} tarball in ${TAG}" >&2
  exit 1
fi

tmp="$(mktemp "${TMPDIR:-/tmp}/kubeloop.XXXXXX")"
cleanup() { rm -f "${tmp}"; }
trap cleanup EXIT
download_asset "${asset}" "${tmp}"
echo "Extracting into ${DEST}..."
tar -xzf "${tmp}" -C "${DEST}"
trap - EXIT
cleanup

if [[ -x "${DEST}/KubeLoop" ]]; then
  echo "Installed binary: ${DEST}/KubeLoop"
else
  echo "Extracted into ${DEST}"
fi
