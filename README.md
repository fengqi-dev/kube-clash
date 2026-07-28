# Kube Clash

Kube Clash 是一个 Kubernetes 桌面网络客户端。它通过 TUN 模式让本机应用透明访问 Pod
IP、ClusterIP Service 和集群 DNS，不要求用户在终端中执行命令。

本项目使用 Mihomo（Clash Meta）作为网络内核。Mihomo 负责 TUN、DNS 劫持和规则匹配，
Kube Clash 负责 Kubernetes 网络发现、Gateway、port-forward 和本地 SOCKS5 桥接。

项目当前处于 M1 开发阶段，详见 [桌面客户端设计](docs/design.md)。

## 当前进度

已完成：

- Wails + React + TypeScript 跨平台桌面工程；
- 原生 macOS arm64 应用构建；
- kubeconfig 与 Context 读取；
- Namespace 查询；
- Node PodCIDR、ClusterIP、Pod 和 CoreDNS 自动发现；
- 根据集群网络动态生成 Mihomo TUN、DNS、SOCKS5 和路由规则；
- 自动安装无特权的集群 Gateway；
- 使用 client-go 原生建立 API Server port-forward；
- 本地 SOCKS5 TCP/UDP 到 Gateway 的数据桥；
- Go 连接状态机与前端实时事件；
- 概览、连接、网络和日志页面。

正在开发：

- Mihomo 内核下载、校验和进程生命周期；
- macOS TUN 提权与应用签名；
- Windows/Linux 平台打包；
- 连接统计和故障恢复。

当前客户端会在完成真实集群网络发现后停在“等待启动 Mihomo TUN”，不会把尚未建立的
数据通道显示为已连接。

## 本地开发

环境要求：

- Go 1.26 或更高版本；
- Node.js 22 或更高版本；
- Wails v2.13。

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@v2.13.0
wails dev
```

构建 macOS 应用：

```bash
wails build
```

## Minikube 集成测试

集成测试会在当前 Minikube 中创建 `kube-clash-system` Namespace 和 Gateway Deployment，
并验证以下真实数据路径：

- Gateway 自动安装和 Ready 检查；
- Kubernetes API Server port-forward；
- Gateway TCP 到 Kubernetes ClusterIP；
- Gateway UDP 到 CoreDNS；
- SOCKS5 TCP 和 UDP 完整链路。

先把本地 arm64 Gateway 镜像加载进 Minikube：

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build \
  -trimpath -ldflags="-s -w" \
  -o build/bin/kube-clash-gateway-linux-arm64 \
  ./cmd/kube-clash-gateway

minikube image build \
  -t kube-clash-gateway:dev \
  -f build/gateway.local.Dockerfile .
```

运行集成测试：

```bash
KUBE_CLASH_MINIKUBE_TEST=1 \
  go test -tags=integration ./internal/cluster \
  -run TestMinikubeGatewayTCPAndDNS -v -count=1
```
