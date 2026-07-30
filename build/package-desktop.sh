#!/usr/bin/env bash
# Package Wails build/bin output into release artifacts under dist/.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "${ROOT}"

BIN_DIR="${ROOT}/build/bin"
DIST_DIR="${ROOT}/dist"
OS="$(go env GOOS)"
ARCH="$(go env GOARCH)"

version_raw="${VERSION:-${GITHUB_REF_NAME:-dev}}"
version="${version_raw#v}"
if [[ -z "${version}" ]]; then
  version="0.0.0"
fi

mkdir -p "${DIST_DIR}"

package_linux() {
  tar -C "${BIN_DIR}" -czf "${DIST_DIR}/kubeloop-linux-${ARCH}.tar.gz" .

  if [[ ! -x "${BIN_DIR}/KubeLoop" ]]; then
    echo "expected Linux binary at build/bin/KubeLoop" >&2
    exit 1
  fi

  if ! command -v nfpm >/dev/null 2>&1; then
    echo "nfpm is required to build deb/rpm packages" >&2
    exit 1
  fi

  export VERSION="${version}"
  export GOARCH="${ARCH}"

  nfpm package \
    --config "${ROOT}/build/nfpm.yaml" \
    --packager deb \
    --target "${DIST_DIR}/kubeloop_${version}_${ARCH}.deb"

  nfpm package \
    --config "${ROOT}/build/nfpm.yaml" \
    --packager rpm \
    --target "${DIST_DIR}/kubeloop-${version}-1.${ARCH}.rpm"
}

package_darwin() {
  local app_src app_stage stage dmg

  app_src="$(find "${BIN_DIR}" -maxdepth 1 -name '*.app' -print -quit)"
  if [[ -z "${app_src}" ]]; then
    echo "expected a macOS .app bundle under build/bin" >&2
    exit 1
  fi

  # Publish a stable bundle name regardless of wails.json "name".
  app_stage="${BIN_DIR}/KubeLoop.app"
  if [[ "${app_src}" != "${app_stage}" ]]; then
    rm -rf "${app_stage}"
    mv "${app_src}" "${app_stage}"
  fi

  tar -C "${BIN_DIR}" -czf "${DIST_DIR}/kubeloop-darwin-${ARCH}.tar.gz" KubeLoop.app

  stage="$(mktemp -d "${TMPDIR:-/tmp}/kubeloop-dmg.XXXXXX")"
  trap 'rm -rf "${stage}"' RETURN
  cp -R "${app_stage}" "${stage}/KubeLoop.app"
  ln -s /Applications "${stage}/Applications"

  dmg="${DIST_DIR}/kubeloop-darwin-${ARCH}.dmg"
  rm -f "${dmg}"
  hdiutil create \
    -volname "KubeLoop" \
    -srcfolder "${stage}" \
    -ov \
    -format UDZO \
    "${dmg}"
}

case "${OS}" in
  linux)
    package_linux
    ;;
  darwin)
    package_darwin
    ;;
  *)
    echo "package-desktop.sh does not package ${OS}; use the platform-specific release step" >&2
    exit 1
    ;;
esac

echo "Packaged artifacts in ${DIST_DIR}:"
ls -la "${DIST_DIR}"
