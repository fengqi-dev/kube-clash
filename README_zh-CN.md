# KubeLoop

[![CI](https://github.com/fengqi-dev/kube-loop/actions/workflows/ci.yml/badge.svg)](https://github.com/fengqi-dev/kube-loop/actions/workflows/ci.yml)
[![Release](https://github.com/fengqi-dev/kube-loop/actions/workflows/release.yml/badge.svg)](https://github.com/fengqi-dev/kube-loop/actions/workflows/release.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

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

1. 从 [GitHub Releases](https://github.com/fengqi-dev/kube-loop/releases) 下载对应平台压缩包
   （darwin / windows / linux × amd64 / arm64；或按下方说明自行构建）。
   - **macOS**：解压后打开 `KubeLoop.app`。若被 Gatekeeper 拦截，可右键 → **打开**，
     或执行 `xattr -cr KubeLoop.app`。
   - **Windows**：解压 zip。若出现 SmartScreen，选择 **更多信息** → **仍要运行**。
   - **Linux**：解压后直接运行。
2. 确认本机 kubeconfig 能正常访问目标集群 API。
3. 打开 KubeLoop，选择 **Context**，点击 **连接**。
4. 首次使用时批准一次 **虚拟网卡服务**（特权 Helper）。之后连接通常不再要求授权；
   可在 **设置** 中安装或卸载该服务。

连接成功后，可在概览查看流量与状态，在网络页使用发现。端口转发、流量交换、流量镜像与预览各自
带有 Namespace 选择器。Exchange 用本机进程替换 Service；Mirror 保留集群 Pod 为主路径，并将 TCP/UDP 请求拷贝到本机。

## 安全设计

- kubeconfig 凭证留在桌面进程内，不会交给 Gateway，也不会作为独立密钥库交给 UI。
- Gateway 不使用 `privileged`、`hostNetwork`、`NET_ADMIN`，也不挂载 ServiceAccount
  Token；不以 Service / Ingress 对外发布。
- 路由仅覆盖已发现的 Pod / Service 网段；非集群流量保持直连。
- 流量交换 / 镜像 / 预览对 Service、Endpoints、EndpointSlice 的修改，在停止或断开时始终恢复。
- 特权 Helper 只接受本机 IPC，用于在 `~/.kubeloop` 会话目录下启停 TUN；它不访问
  Kubernetes API。

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
wails dev
```

```bash
# VERSION 会同时注入 Go、前端、Helper，以及 Gateway 镜像/二进制
VERSION=v0.1.0
VITE_APP_VERSION="$VERSION" wails build -ldflags "-X main.version=${VERSION}"
./build/bundle-helper.sh "$VERSION"   # 将 kubeloop-helper 放到应用旁或 .app 内
# Gateway 镜像（发版 CI）：docker build --build-arg VERSION=$VERSION -f build/gateway.Dockerfile .
```

开发常用覆盖：

```bash
# 使用本地 sing-box / Gateway 镜像
KUBELOOP_SINGBOX_PATH=/path/to/sing-box \
KUBELOOP_GATEWAY_IMAGE=kube-loop-gateway:dev \
wails dev

# 不装系统 Helper，改为每次连接提权
KUBELOOP_HELPER=0 wails dev
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

KubeLoop 源代码使用 [Apache License 2.0](LICENSE)。

sing-box 是独立托管的 GPLv3 程序。分发包含 sing-box 的安装包时须自行履行其许可证义务，
详见[第三方软件声明](THIRD_PARTY_NOTICES.md)。
