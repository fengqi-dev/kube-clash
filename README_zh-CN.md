# KubeLoop

[![CI](https://github.com/fengqi-dev/kube-loop/actions/workflows/ci.yml/badge.svg)](https://github.com/fengqi-dev/kube-loop/actions/workflows/ci.yml)
[![Release](https://github.com/fengqi-dev/kube-loop/actions/workflows/release.yml/badge.svg)](https://github.com/fengqi-dev/kube-loop/actions/workflows/release.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

[English](README.md) | [简体中文](README_zh-CN.md)

KubeLoop 是一个跨平台 Kubernetes 桌面网络客户端。它通过托管的 sing-box 内核和 TUN
路由，让本地应用透明访问 Pod IP、ClusterIP Service 和集群 DNS，无需逐个配置应用代理，
也不要求用户在终端中执行命令。

> [!WARNING]
> KubeLoop 当前处于 M1 开发阶段。完整连接编排和 sing-box 托管生命周期已经实现；
> 生产环境 TUN 提权和签名安装包仍在开发中。

## 为什么需要 KubeLoop？

开发者从本机访问 Kubernetes 私有网络时，通常需要为每个 Service 建立端口转发、修改应用
代理设置，或者安装高权限 VPN 组件。KubeLoop 提供以桌面客户端为中心的流程：

1. 选择 kubeconfig Context 和 DNS Namespace。
2. 点击**连接**。
3. 客户端自动发现集群网络并安装最小化 Gateway。
4. sing-box 只将 Kubernetes 目标流量送入隧道。
5. 本地应用直接访问 Pod、Service 和 `*.cluster.local`。

所有非集群流量保持 `DIRECT`。

## 架构

```text
本地应用
   │
   ▼
sing-box TUN / DNS / 规则
   │  SOCKS5 TCP + UDP
   ▼
KubeLoop 本地桥
   │  Kubernetes API Server port-forward
   ▼
集群内 Gateway
   │
   ├── Pod IP
   ├── ClusterIP Service
   └── CoreDNS
```

- **桌面界面：** Wails、React、TypeScript 和 Tailwind CSS。
- **Kubernetes 集成：** 使用 client-go，不依赖本机 `kubectl`。
- **网络内核：** 托管的 [sing-box](https://github.com/SagerNet/sing-box) 独立进程，
  负责 TUN、DNS 劫持和规则匹配。
- **本地网络桥：** 将 SOCKS5 TCP/UDP 转换为 KubeLoop 隧道协议。
- **集群 Gateway：** 无特权、非 root 的 Deployment，只能通过 Kubernetes API Server
  访问。

## 当前进度

已经完成：

- 跨平台 Wails 桌面工程和现代 Tailwind UI。
- 无边框且跟随系统浅色/深色外观，支持英文（默认）和简体中文界面。
- kubeconfig、Context 和 Namespace 读取。
- Pod CIDR、ClusterIP、Pod 和 CoreDNS 自动发现。
- 动态生成 sing-box TUN、DNS、SOCKS5 和路由配置。
- 在 macOS 上自动请求管理员授权启动 sing-box TUN，并验证 TUN 是否真正初始化成功。
- 自动下载固定版本 sing-box，并执行 SHA-256 校验。
- 幂等安装无特权集群 Gateway。
- 使用 client-go 原生建立 API Server port-forward。
- 本地 SOCKS5 TCP/UDP 桥和 Gateway 隧道协议。
- Connect/Disconnect 完整生命周期和逆序资源清理。
- ClusterIP TCP 和 CoreDNS UDP 的 Minikube 集成测试。
- sing-box 实时连接、流量、网络详情和诊断页面。
- 启动时自动检查最新稳定版 GitHub Release，支持手动刷新和打开下载页面。

正在开发：

- macOS 生产环境 TUN 授权、特权 Helper 和应用签名。
- Windows 和 Linux 平台打包。
- 故障自动恢复和应用内更新安装。

## 发布版本

推送匹配 `v*` 的标签（例如 `v0.1.0`）后，GitHub Actions 会构建 macOS、Windows
和 Linux 桌面包，构建 Linux amd64、arm64 Gateway 二进制，将多架构 Gateway
镜像发布到 GHCR，生成 `SHA256SUMS`，并将全部产物发布到 GitHub Releases。

```bash
git tag v0.1.0
git push origin v0.1.0
```

## 本地开发

### 环境要求

- Go 1.26+
- Node.js 22+
- Wails v2.13
- 用于集成测试的 Kubernetes 集群

### 启动开发环境

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@v2.13.0
npm install --prefix frontend
wails dev
```

首次连接时，KubeLoop 会自动下载固定版本的 sing-box。开发环境既可以使用自动下载，
也可以覆盖 sing-box 路径和 Gateway 镜像：

```bash
KUBELOOP_SINGBOX_PATH=/absolute/path/to/sing-box \
KUBELOOP_GATEWAY_IMAGE=kube-loop-gateway:dev \
wails dev
```

当前 macOS 预览版会直接启动 sing-box。在签名特权 Helper 完成前，创建 TUN 路由仍要求
以具备相应权限的方式启动开发版本。

### 构建

```bash
wails build
```

### 单元测试

```bash
go test ./...
npm run build --prefix frontend
```

## Minikube 集成测试

集成测试会在当前 Minikube 中创建 `kubeloop-system` Namespace 和 Gateway Deployment，
并验证：

- Gateway 自动安装和 Ready 检查；
- Kubernetes API Server port-forward；
- TCP 访问 Kubernetes ClusterIP；
- UDP 访问 CoreDNS；
- SOCKS5 TCP 和 UDP 完整链路。

构建并加载本地 arm64 Gateway 镜像：

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build \
  -trimpath -ldflags="-s -w" \
  -o build/bin/kube-loop-gateway-linux-arm64 \
  ./cmd/kubeloop-gateway

minikube image build \
  -t kube-loop-gateway:dev \
  -f build/gateway.local.Dockerfile .
```

运行集成测试：

```bash
KUBELOOP_MINIKUBE_TEST=1 \
  go test -tags=integration ./internal/cluster \
  -run TestMinikubeGatewayTCPAndDNS -v -count=1
```

## 安全设计

- kubeconfig 凭证只保留在桌面客户端核心进程中。
- Gateway 不使用 `privileged`、`hostNetwork`、`NET_ADMIN`，也不挂载
  ServiceAccount Token。
- Gateway 不通过 Service、NodePort、Ingress 或 LoadBalancer 暴露。
- Gateway 拒绝公网、回环、链路本地和组播目标。
- sing-box 只接收自动发现的 Pod 和 Service 路由，其余流量保持直连。

## 文档

- [桌面客户端设计](docs/design.md)
- [第三方软件声明](THIRD_PARTY_NOTICES.md)

## 许可证

KubeLoop 源代码使用 [Apache License 2.0](LICENSE)。

sing-box 是使用 GPLv3 的独立托管程序。发布包含 sing-box 的安装包时，必须独立履行其许可证
和对应源码提供义务，详见[第三方软件声明](THIRD_PARTY_NOTICES.md)。
