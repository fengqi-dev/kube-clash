# KubeLoop

[![CI](https://github.com/fengqi-dev/kube-loop/actions/workflows/ci.yml/badge.svg)](https://github.com/fengqi-dev/kube-loop/actions/workflows/ci.yml)
[![Release](https://github.com/fengqi-dev/kube-loop/actions/workflows/release.yml/badge.svg)](https://github.com/fengqi-dev/kube-loop/actions/workflows/release.yml)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

[English](README.md) | [简体中文](README_zh-CN.md)

**[官网](https://fengqi-dev.github.io/kube-loop/)** · **[下载](https://github.com/fengqi-dev/kube-loop/releases)** · **[设计文档](docs/design.zh-CN.md)** · **[Design](docs/design.md)**

KubeLoop 是一款桌面客户端：像连 VPN 一样连上 Kubernetes 集群，让本机应用直接访问
Pod IP、ClusterIP Service 和 `*.cluster.local`——不必再为每个服务做端口转发，也不用给
每个应用配代理。

---

## 你能得到什么

- **一键接入集群网络** — 选择 kubeconfig Context 与 Namespace，点击连接。KubeLoop
  自动发现 Pod / Service 网段与集群 DNS，并拉起定向隧道。
- **对应用透明** — 浏览器、IDE、CLI、SDK 直接访问集群地址，无需在每个软件里配置
  SOCKS / HTTP 代理。
- **只接管集群流量** — 仅 Kubernetes 相关目标走隧道，其余流量仍走你原来的网络。
- **不依赖公网暴露** — 集群内 Gateway 经 Kubernetes API Server（port-forward）到达，
  不用 NodePort、LoadBalancer 或对外 Ingress。
- **macOS / Windows / Linux** — 同一套桌面使用体验。

## 常见场景

| 你想做的事 | KubeLoop 怎么帮你 |
| --- | --- |
| 浏览器打开集群内 Service / 调内部 API | 连接后使用 ClusterIP 或 `*.svc.cluster.local` |
| 直接访问真实 Pod IP 做排查 | 连接后本机已路由 Pod 网段 |
| 不想敲 `kubectl port-forward` | 网络页的 **端口转发** |
| 用本机进程顶替某个集群 Service | **流量交换**（Service Local Intercept）：集群侧仍用原 ClusterIP / DNS，流量落到本机 |
| 把本机进程临时暴露成新的 ClusterIP | **预览**：创建临时 Service 指向本地应用 |

## 工作原理（简版）

```text
本机应用  →  TUN + 分流 DNS  →  本地桥  →  API Server  →  集群内 Gateway
                                                         ├─ Pod
                                                         ├─ Service
                                                         └─ CoreDNS
```

KubeLoop 托管固定版本的 [sing-box](https://github.com/SagerNet/sing-box) 负责
TUN / DNS / 规则，并在集群中部署轻量、无特权的 Gateway。本机不必安装 `kubectl`。

## 快速开始

### 安装 KubeLoop

**macOS / Linux**（按 CPU 架构下载最新 Release 包）：

```bash
curl -fsSL https://raw.githubusercontent.com/fengqi-dev/kube-loop/main/scripts/install.sh | bash
```

**Windows**（PowerShell；优先运行最新 NSIS 安装包）：

```powershell
irm https://raw.githubusercontent.com/fengqi-dev/kube-loop/main/scripts/install.ps1 | iex
```

其他方式：

- **macOS（Homebrew）**：
  ```bash
  brew tap fengqi-dev/kube-loop https://github.com/fengqi-dev/kube-loop
  brew install --cask kubeloop
  ```
- **macOS（手动）**：从 [GitHub Releases](https://github.com/fengqi-dev/kube-loop/releases)
  下载 `.dmg`，将 `KubeLoop.app` 拖入 Applications（或解压 `.tar.gz`）。
  若被 Gatekeeper 拦截，可右键 → **打开**，或执行 `xattr -cr KubeLoop.app`。
- **Windows**：运行 NSIS 安装包（`kubeloop-*-windows-*-installer.exe`），或解压便携 zip。
  若出现 SmartScreen，选择 **更多信息** → **仍要运行**。
- **Linux**：安装 `.deb` / `.rpm`，或解压 `.tar.gz` 后运行 `KubeLoop`。
- 或按下方说明自行构建。

然后：

1. 确认本机 kubeconfig 能正常访问目标集群 API。
2. 打开 KubeLoop，选择 **Context**，点击 **连接**。
3. 首次使用时批准一次 **虚拟网卡服务**（特权 Helper）。之后连接通常不再要求授权；
   可在 **设置** 中安装或卸载该服务。

连接成功后，可在概览查看流量与状态，在网络页使用发现。端口转发、流量交换、流量镜像与预览各自
带有 Namespace 选择器。Exchange 用本机进程替换 Service；Mirror 保留集群 Pod 为主路径，并将 TCP/UDP 请求拷贝到本机。

## 安全设计

- kubeconfig 凭证留在桌面进程内，不会交给 Gateway，也不会作为独立密钥库交给 UI。
- Gateway 不使用 `privileged`、`hostNetwork`、`NET_ADMIN`，也不挂载 ServiceAccount
  Token；不以 Service / Ingress 对外发布。
- 路由仅覆盖已发现的 Pod / Service 网段；非集群流量保持直连。
- 流量交换 / 镜像 / 预览对 Service、Endpoints、EndpointSlice 的修改，在停止或断开时始终恢复。
- 特权 Helper 只接受带认证的本机 IPC 和字段受限的 Session 描述，不接受调用方传入的
  命令、可执行文件路径或配置路径。它在系统保护目录内重新生成配置并管理校验后的内核，
  且不访问 Kubernetes API。

## 平台说明

| | |
| --- | --- |
| **界面** | 浅色 / 深色（跟随系统），英文与简体中文 |
| **数据** | 状态与内核位于 `~/.kubeloop` |
| **Helper** | 安装一次即可管理 TUN / DNS / 路由；可在设置中随时卸载 |
| **更新** | 启动时检查 GitHub Releases；可在设置中打开下载页 |

## 开发者

环境：Go 1.26+、Node.js 22+、[Wails](https://wails.io) v2.13。

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@v2.13.0
npm install --prefix frontend
wails dev # 自动构建并内嵌当前平台的 Helper
```

```bash
# VERSION 会同时注入 Go、前端、Helper，以及 Gateway 镜像/二进制
VERSION=v0.1.0
VITE_APP_VERSION="$VERSION" wails build -ldflags "-X main.version=${VERSION}"
# 平台安装包（在 wails build 之后）：
#   macOS DMG / tar.gz:  VERSION=$VERSION ./build/package-desktop.sh
#   Linux deb/rpm/tar:   go install github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.46.3
#                        # Debian/Ubuntu 还需: apt install rpm
#                        VERSION=$VERSION ./build/package-desktop.sh
#   Windows 安装包:      VITE_APP_VERSION="$VERSION" wails build -nsis -ldflags "-X main.version=${VERSION}"
# Gateway 镜像（发版 CI）：docker build --build-arg VERSION=$VERSION -f build/gateway.Dockerfile .
```

开发常用覆盖：

```bash
# 使用本地 Gateway 镜像
KUBELOOP_GATEWAY_IMAGE=kube-loop-gateway:dev wails dev
```

```bash
go test ./...
./e2e/run.sh                # Minikube 端到端（见 e2e/）
```

推送 `v*` 标签即可发布（桌面包、Gateway 二进制与 GHCR 镜像）。

## 文档

- [项目网站](https://fengqi-dev.github.io/kube-loop/)
- [桌面客户端设计（简体中文）](docs/design.zh-CN.md)
- [Desktop design (English)](docs/design.md)
- [第三方软件声明](THIRD_PARTY_NOTICES.md)

## 许可证

KubeLoop 源代码使用 [MIT License](LICENSE)。

sing-box 是独立托管的 GPLv3 程序。分发包含 sing-box 的安装包时须自行履行其许可证义务，
详见[第三方软件声明](THIRD_PARTY_NOTICES.md)。
