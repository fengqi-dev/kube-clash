# Kube Clash

[![CI](https://github.com/fengqi-dev/kube-clash/actions/workflows/ci.yml/badge.svg)](https://github.com/fengqi-dev/kube-clash/actions/workflows/ci.yml)
[![Release](https://github.com/fengqi-dev/kube-clash/actions/workflows/release.yml/badge.svg)](https://github.com/fengqi-dev/kube-clash/actions/workflows/release.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

[English](README.md) | [简体中文](README_zh-CN.md)

Kube Clash is a cross-platform Kubernetes desktop network client. It uses a
managed Mihomo core and TUN routing to let local applications transparently
access Pod IPs, ClusterIP Services, and cluster DNS without per-application
proxy settings or terminal commands.

> [!WARNING]
> Kube Clash is under active M1 development. The complete connection
> orchestration and managed Mihomo lifecycle are implemented; production TUN
> privilege handling and signed installers are still in progress.

## Why Kube Clash?

Accessing private Kubernetes workloads from a developer workstation typically
requires port-forwarding each Service, changing application proxy settings, or
installing a privileged VPN component. Kube Clash provides a desktop-first
workflow:

1. Select a kubeconfig Context and DNS Namespace.
2. Click **Connect**.
3. Kube Clash discovers the cluster network and installs a minimal Gateway.
4. Mihomo routes only Kubernetes traffic into the tunnel.
5. Local applications access Pods, Services, and `*.cluster.local` directly.

All non-cluster traffic stays `DIRECT`.

## Architecture

```text
Local application
      │
      ▼
Mihomo TUN / DNS / rules
      │  SOCKS5 TCP + UDP
      ▼
Kube Clash local bridge
      │  Kubernetes API Server port-forward
      ▼
In-cluster Gateway
      │
      ├── Pod IP
      ├── ClusterIP Service
      └── CoreDNS
```

- **Desktop UI:** Wails, React, TypeScript, and Tailwind CSS.
- **Kubernetes integration:** client-go; no local `kubectl` dependency.
- **Network core:** managed [Mihomo](https://github.com/MetaCubeX/mihomo)
  process for TUN, DNS interception, and rule matching.
- **Local bridge:** SOCKS5 TCP/UDP to the Kube Clash tunnel protocol.
- **Gateway:** an unprivileged, non-root Deployment reached only through the
  Kubernetes API Server.

## Current status

Implemented:

- Cross-platform Wails desktop project and modern Tailwind UI.
- Frameless system-aware light/dark UI with English (default) and Simplified
  Chinese language options.
- kubeconfig, Context, and Namespace discovery.
- Pod CIDR, ClusterIP, Pod, and CoreDNS discovery.
- Dynamic Mihomo TUN, DNS, SOCKS5, and routing configuration.
- macOS administrator authorization for starting the managed Mihomo TUN, with
  startup verification that rejects failed TUN initialization.
- Automatic download and SHA-256 verification of pinned Mihomo binaries.
- Idempotent installation of an unprivileged cluster Gateway.
- Native client-go API Server port-forwarding.
- Local SOCKS5 TCP/UDP bridge and Gateway tunnel protocol.
- End-to-end Connect/Disconnect lifecycle with reverse-order cleanup.
- Minikube integration tests for ClusterIP TCP and CoreDNS UDP.
- Live Mihomo connection, traffic, memory, network, and diagnostic views.
- Automatic startup check for newer stable GitHub Releases with a manual
  refresh and download link.

In progress:

- macOS production TUN authorization, privileged Helper, and application signing.
- Windows and Linux packaging.
- Automatic recovery and in-place update installation.

## Releases

Pushing a tag matching `v*` (for example, `v0.1.0`) builds desktop packages
for macOS, Windows, and Linux, builds Gateway binaries for Linux amd64 and
arm64, publishes the multi-architecture Gateway image to GHCR, generates
`SHA256SUMS`, and publishes everything to GitHub Releases.

```bash
git tag v0.1.0
git push origin v0.1.0
```

## Development

### Requirements

- Go 1.26+
- Node.js 22+
- Wails v2.13
- A Kubernetes cluster for integration tests

### Run locally

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@v2.13.0
npm install --prefix frontend
wails dev
```

Kube Clash downloads the pinned Mihomo core on first connection. For local
development, either use the automatic download or override the core and
Gateway image:

```bash
KUBE_CLASH_MIHOMO_PATH=/absolute/path/to/mihomo \
KUBE_CLASH_GATEWAY_IMAGE=kube-clash-gateway:dev \
wails dev
```

The current macOS preview starts Mihomo directly. TUN route creation therefore
requires launching the development build with sufficient permissions until the
signed privileged Helper is implemented.

### Build

```bash
wails build
```

### Unit tests

```bash
go test ./...
npm run build --prefix frontend
```

## Minikube integration test

The integration test creates a `kube-clash-system` Namespace and Gateway
Deployment in the current Minikube cluster. It verifies:

- automatic Gateway installation and readiness;
- Kubernetes API Server port-forwarding;
- TCP access to the Kubernetes ClusterIP;
- UDP access to CoreDNS;
- the complete SOCKS5 TCP and UDP path.

Build and load the local arm64 Gateway image:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build \
  -trimpath -ldflags="-s -w" \
  -o build/bin/kube-clash-gateway-linux-arm64 \
  ./cmd/kube-clash-gateway

minikube image build \
  -t kube-clash-gateway:dev \
  -f build/gateway.local.Dockerfile .
```

Run the integration test:

```bash
KUBE_CLASH_MINIKUBE_TEST=1 \
  go test -tags=integration ./internal/cluster \
  -run TestMinikubeGatewayTCPAndDNS -v -count=1
```

## Security

- kubeconfig credentials stay in the desktop core process.
- The Gateway does not use `privileged`, `hostNetwork`, `NET_ADMIN`, or a
  mounted ServiceAccount token.
- The Gateway is not exposed by a Service, NodePort, Ingress, or LoadBalancer.
- Public, loopback, link-local, and multicast Gateway targets are rejected.
- Mihomo only receives discovered Pod and Service routes; other traffic remains
  direct.

## Documentation

- [Desktop client design](docs/design.md)
- [Third-party notices](THIRD_PARTY_NOTICES.md)

## License

Kube Clash source code is licensed under the
[Apache License 2.0](LICENSE).

Mihomo is a separate managed program licensed under GPLv3. Distributions that
bundle Mihomo must independently comply with its license and corresponding
source requirements. See [Third-party notices](THIRD_PARTY_NOTICES.md).
