# KubeLoop

[![CI](https://github.com/fengqi-dev/kube-loop/actions/workflows/ci.yml/badge.svg)](https://github.com/fengqi-dev/kube-loop/actions/workflows/ci.yml)
[![Release](https://github.com/fengqi-dev/kube-loop/actions/workflows/release.yml/badge.svg)](https://github.com/fengqi-dev/kube-loop/actions/workflows/release.yml)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

[English](README.md) | [简体中文](README_zh-CN.md)

**[Website](https://fengqi-dev.github.io/kube-loop/)** · **[Releases](https://github.com/fengqi-dev/kube-loop/releases)** · **[Design](docs/design.md)** · **[设计文档](docs/design.zh-CN.md)**

KubeLoop is a desktop client that connects your laptop to a Kubernetes cluster
like a VPN — so local apps can reach Pod IPs, ClusterIP Services, and
`*.cluster.local` without port-forwards, proxy env vars, or per-app setup.

---

## What you get

- **One-click cluster network** — Pick a kubeconfig context and namespace, click
  Connect. KubeLoop discovers Pod / Service CIDRs and cluster DNS, then brings
  up a focused tunnel.
- **Transparent access** — Browsers, IDEs, CLIs, and SDKs talk to cluster
  addresses as if they were on the same network. No SOCKS settings in each app.
- **Cluster traffic only** — Only Kubernetes destinations go through the tunnel.
  Everything else stays on your normal route.
- **Works offline of the public internet path** — The in-cluster Gateway is
  reached through the Kubernetes API Server (port-forward). No NodePort,
  LoadBalancer, or public ingress for the data path.
- **macOS, Windows, and Linux** — Same desktop workflow across platforms.

## Everyday workflows

| Need | How KubeLoop helps |
| --- | --- |
| Open a Service in the browser / call an internal API | Connect, then use the ClusterIP or `*.svc.cluster.local` name |
| Debug against a real Pod IP | Pod CIDR is routed locally after Connect |
| `kubectl port-forward` without the terminal | **Port Forward** in the Network page |
| Run a local process *as* a cluster Service | **Exchange** (Service Local Intercept): cluster clients keep the same ClusterIP / DNS; traffic lands on your machine |
| Expose a local process as a new ClusterIP | **Preview** creates a temporary Service that points at your local app |

## How it works (short)

```text
Your apps  →  TUN + split DNS  →  local bridge  →  API Server  →  in-cluster Gateway
                                                                    ├─ Pods
                                                                    ├─ Services
                                                                    └─ CoreDNS
```

Under the hood KubeLoop manages a pinned [sing-box](https://github.com/SagerNet/sing-box)
core for TUN / DNS / rules, and a small unprivileged Gateway Deployment in the
cluster. You do not need `kubectl` installed locally.

## Get started

### Install KubeLoop

**macOS / Linux** (downloads the latest release package for your CPU):

```bash
curl -fsSL https://raw.githubusercontent.com/fengqi-dev/kube-loop/main/scripts/install.sh | bash
```

**Windows** (PowerShell; runs the latest NSIS installer when available):

```powershell
irm https://raw.githubusercontent.com/fengqi-dev/kube-loop/main/scripts/install.ps1 | iex
```

Other options:

- **macOS (Homebrew)**:
  ```bash
  brew tap kube-loop/kubeloop https://github.com/fengqi-dev/kube-loop
  brew install --cask kubeloop
  ```
- **macOS (manual)**: download the `.dmg` from [GitHub Releases](https://github.com/fengqi-dev/kube-loop/releases)
  and drag `KubeLoop.app` into Applications (or extract the `.tar.gz`). If Gatekeeper
  blocks it, right-click → **Open**, or run `xattr -cr KubeLoop.app`.
- **Windows**: run the NSIS installer (`kubeloop-*-windows-*-installer.exe`), or extract the
  portable zip into a folder (flat layout like `Program Files\KubeLoop\` with
  `sing-box.exe` and `resources\` helper tools). If SmartScreen appears, choose
  **More info** → **Run anyway**.
- **Linux**: install the `.deb` / `.rpm`, or extract the `.tar.gz` and run `KubeLoop`.
- Or build from source — see below.

Then:

1. Ensure your machine can reach the cluster API with a normal kubeconfig.
2. Open KubeLoop, choose a **Context**, click **Connect**.
3. On first use, approve the **virtual network service** (privileged helper)
   once. Later connects should not ask again. You can install or remove it under
   **Settings**.

After you are connected, open Overview for traffic and status, or Network for
discovery. Port Forward, Exchange, and Preview each have their own Namespace
picker.

### Limited RBAC / single-namespace accounts

KubeLoop supports developer kubeconfigs that cannot install the Gateway or list
the whole cluster:

1. **Admin preinstalls Gateway** in `kubeloop-system` (copy YAML from Overview when
   install is forbidden and no Gateway is found).
2. Grant the user `get/list` + `pods/portforward` on the Gateway Pod.
3. Scope Pod/Service list/watch to the allowed Namespace(s).
4. If the user cannot list Nodes / read CoreDNS, enter **Pod CIDR**, **Service
   CIDR**, and **Cluster DNS** on Overview (saved per context; reconnect to apply).

Exchange / Mirror / Preview stay disabled when Service / EndpointSlice / Endpoints write is missing.
**Exchange** replaces a Service with a local process; **Mirror** keeps cluster Pods as the
primary path and tees TCP/UDP requests to a local process.
See [docs/design.md](docs/design.md) §8 (or [中文](docs/design.zh-CN.md)) for example Roles.

## MCP (Cursor / AI agents)

KubeLoop can expose its desktop control plane as a local [MCP](https://modelcontextprotocol.io) server (Streamable HTTP on `127.0.0.1`). Tools cover connect/disconnect, discovery, Port Forward, Exchange, Mirror, Preview, and helper install.

1. Start KubeLoop, open the **MCP** tab.
2. Choose a client (Claude Code / Codex / Cursor / VS Code) and click **Install MCP server**
   (writes the user-scoped config and enables the local endpoint if needed).
3. Restart or refresh MCP in that client.

You can also **Copy config** for the selected client. MCP is **off by default** until enabled
or installed; it binds only to localhost. Bearer token auth is optional and **off by default**.

## Security posture

KubeLoop is built so cluster access stays scoped and recoverable:

- kubeconfig credentials stay in the desktop process — not sent to the Gateway
  or the UI layer as a separate secret store.
- The Gateway runs without `privileged`, `hostNetwork`, `NET_ADMIN`, or a
  mounted ServiceAccount token, and is not published as a Service / Ingress.
- Routing is limited to discovered Pod and Service ranges; non-cluster traffic
  remains direct.
- Exchange / Mirror / Preview changes to Services, Endpoints, and EndpointSlices are always
  restored on stop or disconnect.
- The privileged helper accepts authenticated local IPC with a field-constrained
  session description — never caller-supplied commands, executable paths, or config
  paths. It regenerates config and manages the verified core under protected system
  storage, and never talks to the Kubernetes API.
- The optional MCP server listens on `127.0.0.1` only; Bearer token auth is optional (off by default).

## Platform notes

| | |
| --- | --- |
| **UI** | Light / dark (system-aware), English and 简体中文 |
| **Data** | App state under `~/.kubeloop`; helper state under system-protected storage |
| **Helper** | Install once for TUN / DNS / routes; uninstall anytime in Settings (Windows: `resources\kubeloop-helper-*.exe`) |
| **Core** | Packages ship pinned sing-box beside the app; helper runs it in place (no copy/download) |
| **Updates** | Checks GitHub Releases on startup; open the download page from Settings |

## For developers

Requirements: Go 1.26+, Node.js 22+, [Wails](https://wails.io) v2.13.

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@v2.13.0
npm install --prefix frontend
wails dev # automatically builds and embeds the platform helper
```

```bash
# VERSION is injected into Go, the Vite frontend, helper, and Gateway image/binary
VERSION=v0.1.0
VITE_APP_VERSION="$VERSION" wails build -ldflags "-X main.version=${VERSION}"
# Platform packages (after wails build):
#   macOS DMG / tar.gz:  VERSION=$VERSION ./build/package-desktop.sh
#   Linux deb/rpm/tar:   go install github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.46.3
#                        # Debian/Ubuntu also needs: apt install rpm
#                        VERSION=$VERSION ./build/package-desktop.sh
#   Windows installer:   VITE_APP_VERSION="$VERSION" wails build -nsis -ldflags "-X main.version=${VERSION}"
# Gateway image (release CI): docker build --build-arg VERSION=$VERSION -f build/gateway.Dockerfile .
```

Useful overrides while developing:

```bash
# Use a local Gateway image
KUBELOOP_GATEWAY_IMAGE=kube-loop-gateway:dev wails dev
```

```bash
go test ./...
./e2e/run.sh                # Minikube end-to-end (see e2e/)
```

### Local end-to-end tests

Local E2E requires Go, Docker, `kubectl`, a running Kubernetes cluster, and
permission to install the privileged KubeLoop Helper. The scripts build all
artifacts, install or update the Helper, run the TUN and platform test suites,
print a per-test summary, and clean temporary resources.

**Windows** (defaults to the `docker-desktop` context):

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass `
  -File .\e2e\scripts\run-local-windows.ps1 `
  -Context docker-desktop `
  -Timeout 25m
```

**macOS** (defaults to the `docker-desktop` context):

```bash
chmod +x e2e/scripts/run-local-macos.sh
./e2e/scripts/run-local-macos.sh --timeout 25m
```

**Linux** (defaults to the `minikube` context):

```bash
chmod +x e2e/scripts/run-local-linux.sh
./e2e/scripts/run-local-linux.sh --timeout 25m
```

Common options are `--context`, `--gateway-image`, `--timeout`,
`--skip-build`, `--skip-platform`, `--keep-resources`, and
`--ignore-network-preflight`. PowerShell uses the equivalent single-dash
parameter names such as `-Context` and `-SkipPlatform`.

Test output is written to `e2e-local.log`; privileged platform test output is
written to `e2e-platform.log`.

Tag `v*` to cut a release (desktop packages, Gateway binaries + GHCR image).

## Documentation

- [Project website](https://fengqi-dev.github.io/kube-loop/)
- [Desktop design (English)](docs/design.md)
- [桌面客户端设计（简体中文）](docs/design.zh-CN.md)
- [Third-party notices](THIRD_PARTY_NOTICES.md)

## License

KubeLoop is licensed under the [MIT License](LICENSE).

sing-box is a separately licensed (GPLv3) managed dependency. Distributions that
bundle it must meet its license obligations — see
[Third-party notices](THIRD_PARTY_NOTICES.md).
