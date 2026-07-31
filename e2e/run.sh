#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"

CONTEXT="${KUBELOOP_E2E_CONTEXT:-minikube}"
IMAGE="${KUBELOOP_GATEWAY_IMAGE:-kube-loop-gateway:dev}"
ARCH="$(go env GOARCH)"

echo "==> Building Gateway binary (linux/${ARCH})"
mkdir -p build/bin
CGO_ENABLED=0 GOOS=linux GOARCH="${ARCH}" go build \
  -trimpath -ldflags="-s -w" \
  -o build/bin/kube-loop-gateway \
  ./cmd/kubeloop-gateway

echo "==> Loading Gateway image into Minikube (${IMAGE})"
if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
  if command -v minikube >/dev/null 2>&1; then
    eval "$(minikube docker-env --shell bash)"
  fi
  docker build -t "${IMAGE}" -f build/gateway.e2e.Dockerfile .
else
  echo "host Docker unavailable; building inside minikube"
  minikube cp build/bin/kube-loop-gateway /tmp/kube-loop-gateway
  minikube cp build/gateway.e2e.Dockerfile /tmp/Dockerfile.e2e
  minikube ssh -- 'sudo mkdir -p /tmp/gwbuild/build/bin && sudo cp /tmp/kube-loop-gateway /tmp/gwbuild/build/bin/kube-loop-gateway && sudo cp /tmp/Dockerfile.e2e /tmp/gwbuild/Dockerfile && sudo chmod 755 /tmp/gwbuild/build/bin/kube-loop-gateway'
  minikube ssh -- "cd /tmp/gwbuild && sudo docker build -t ${IMAGE} -f Dockerfile ."
fi

echo "==> Restarting Gateway Pods to pick up image"
kubectl --context="${CONTEXT}" -n kubeloop-system delete pod \
  -l app.kubernetes.io/name=kubeloop-gateway \
  --ignore-not-found=true --wait=false || true
echo "==> Waiting for Gateway Deployment to be ready"
kubectl --context="${CONTEXT}" -n kubeloop-system rollout status \
  deploy/kubeloop-gateway --timeout=180s
# rollout status can return before the new Pod accepts port-forward.
kubectl --context="${CONTEXT}" -n kubeloop-system wait \
  --for=condition=Ready pod \
  -l app.kubernetes.io/name=kubeloop-gateway \
  --timeout=120s
sleep 2

echo "==> Running e2e against context ${CONTEXT}"
KUBELOOP_E2E=1 \
KUBELOOP_E2E_CONTEXT="${CONTEXT}" \
KUBELOOP_GATEWAY_IMAGE="${IMAGE}" \
  go test -tags=e2e ./e2e -count=1 -timeout=20m -v "$@"
