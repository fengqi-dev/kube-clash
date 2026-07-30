# KubeLoop 桌面客户端设计

[English](design.md) | [简体中文](design.zh-CN.md)

> 状态：Draft v0.2（产品边界已确认）
> 目标：让开发者像连接 VPN 一样连接 Kubernetes 集群，并从本机透明访问 Pod IP、Service IP 和集群域名。

## 0. 已确认的产品决策

- 产品目标为 macOS、Windows、Linux 多平台，优先交付 macOS；
- Gateway 由桌面客户端自动检查、安装和升级；
- 第一阶段只完成 Pod、Service 和集群 DNS 的透明访问；
- M2 支持用本地服务替换集群 Service（TCP/UDP），断开时完整恢复 Endpoints。
- 使用 sing-box 作为 TUN、DNS 和规则路由内核。

## 1. 产品定位

KubeLoop 是桌面网络客户端，而不是命令行工具。用户不需要理解路由、TUN、端口转发或
Kubernetes 网络细节，只需要选择集群并点击连接。

它提供熟悉的桌面网络客户端体验：

- 常驻系统托盘；
- 一键连接和断开；
- TUN 透明接管，无需给每个应用配置代理；
- 实时展示连接状态、路由、请求和错误；
- 自动检测网段冲突；
- 设置开机启动和自动重连。

它解决的是 Kubernetes 开发网络问题，而不是公网代理问题：

- 访问 Pod IP；
- 访问 ClusterIP Service；
- 解析并访问 `*.svc.cluster.local`；
- 将本地进程映射为集群内 Service 的目标（Service Local Intercept）。

## 2. 首版范围

### 2.1 MVP 包含

1. 读取本机 kubeconfig，并展示 Context、集群和 Namespace。
2. 通过 Kubernetes API 获取 Pod CIDR、Service IP 和集群 DNS 信息。
3. 创建系统 TUN 设备，只接管目标集群网段。
4. 自动安装或复用集群内 Gateway。
5. 通过 Kubernetes API Server 的 port-forward 通道连接 Gateway，不暴露公网端口。
6. 支持 TCP 和 UDP。
7. 将 `cluster.local` DNS 查询转发给集群 CoreDNS。
8. 展示连接时长、上下行流量、活动连接和诊断信息。
9. 从 UI 完成连接、断开、Gateway 安装和卸载。

### 2.2 M2：Service Local Intercept

在已连接会话中，用户可将现有 ClusterIP Service 替换为本地进程：

1. 桌面通过控制通道在 Gateway 上注册唯一 listen 端口（TCP/UDP）；
2. 清空 Service selector，写入托管 EndpointSlice，后端指向 Gateway Pod IP:listenPort；
3. 集群客户端仍使用原 ClusterIP / DNS；kube-proxy 将流量送到 Gateway；
4. Gateway 发出 `InboundReady`，桌面 `Accept` 后转发到 `127.0.0.1`（或指定本地地址）；
5. 停止拦截或断开连接时恢复 selector、删除托管 EndpointSlice，并注销 Gateway 监听。

Gateway 仍无 kube API、无特权、无 hostNetwork。EndpointSlice 变更由桌面 kubeconfig 完成。

### 2.3 MVP 暂不包含

- 全局公网代理和规则订阅；
- 多集群同时连接；
- ICMP/Ping 的完整语义；
- Headless / ExternalName Service 拦截；
- Service Mesh 流量身份模拟；
- Windows 客户端。

产品架构从第一天保持跨平台，交付顺序为：

1. macOS；
2. Windows；
3. Linux。

核心网络栈、Kubernetes 访问、隧道协议和 Gateway 必须跨平台复用。系统网络配置由平台适配
层实现，不允许平台逻辑进入通用连接流程。

## 3. 用户体验

### 3.1 首页

```text
┌──────────────────────────────────────────────────────┐
│ KubeLoop                                设置  —  □ │
├──────────────────────────────────────────────────────┤
│                                                      │
│             ● 已连接                                  │
│             dev-cluster / default                    │
│             00:42:18                                 │
│                                                      │
│               [ 断开连接 ]                            │
│                                                      │
│  Pod 网络         Service         DNS                │
│  10.244.0.0/16    32 个 IP        cluster.local      │
│                                                      │
│  ↓ 18.2 MB        ↑ 4.7 MB        12 个活动连接       │
├──────────────────────────────────────────────────────┤
│  概览        连接        网络        日志              │
└──────────────────────────────────────────────────────┘
```

未连接状态下，主区域展示：

1. kubeconfig 选择；
2. Context 选择；
3. Namespace 选择；
4. “连接”主按钮；
5. 首次连接时的 Gateway 安装提示。

### 3.2 连接状态

状态机必须对用户可解释：

```text
未连接
  → 检查 kubeconfig
  → 检查集群权限
  → 检查/安装 Gateway
  → 发现 Pod 与 Service 网络
  → 检查本地网段冲突
  → 请求系统网络权限
  → 创建安全通道
  → 已连接
```

任何一步失败时，UI 展示：

- 人能读懂的错误原因；
- 受影响的能力；
- “重试”按钮；
- 可复制的诊断详情。

### 3.3 系统托盘

托盘菜单提供：

- 当前连接状态；
- 最近使用的集群；
- 连接/断开；
- 打开主窗口；
- 退出。

关闭主窗口不等于断开连接，退出应用时需要明确提示。

## 4. 总体架构

```text
┌──────────────── macOS Desktop ─────────────────┐
│                                                │
│  ┌─────────────┐     ┌──────────────────────┐  │
│  │ Desktop UI  │────▶│ Core Service         │  │
│  │ React       │     │ Go                   │  │
│  └─────────────┘     │ - kubeconfig/context │  │
│                      │ - cluster discovery  │  │
│                      │ - session lifecycle  │  │
│                      │ - traffic metrics    │  │
│                      └──────────┬───────────┘  │
│                                 │ local IPC     │
│                      ┌──────────▼───────────┐  │
│                      │ Privileged Helper    │  │
│                      │ - utun               │  │
│                      │ - routes             │  │
│                      │ - split DNS          │  │
│                      └──────────┬───────────┘  │
│                                 │ packets       │
│                      ┌──────────▼───────────┐  │
│                      │ sing-box Core          │  │
│                      │ TUN / DNS / Rules    │  │
│                      └──────────┬───────────┘  │
└─────────────────────────────────┼──────────────┘
                                  │ encrypted Kubernetes
                                  │ port-forward channel
                         ┌────────▼─────────┐
                         │ In-cluster      │
                         │ Gateway         │
                         │ TCP/UDP dialer  │
                         └────────┬─────────┘
                                  │
                         Pod / Service / CoreDNS
```

### 4.1 桌面 UI

推荐使用 Wails + React：

- Go 适合 Kubernetes 客户端、网络控制和并发任务；
- UI 可以保持现代桌面客户端体验；
- 相比 Electron，安装包和常驻内存更小；
- 核心逻辑可以复用于 Windows 和 Linux。

UI 进程不直接持有 root 权限。

### 4.2 Core Service

运行在普通用户权限下，负责：

- kubeconfig 与 Context 管理；
- 使用 Kubernetes API 做资源发现；
- 安装、升级和检查 Gateway；
- 建立 port-forward；
- 管理连接状态机；
- 运行用户态 TCP/IP 栈；
- 汇总指标和结构化日志；
- 通过本地 RPC 向 UI 推送状态。

首版不调用外部 `kubectl`，直接使用 Kubernetes client-go，避免用户机器上的版本差异和
命令行窗口闪烁。

### 4.3 sing-box Core 与 Privileged Helper

sing-box 作为客户端托管的独立进程随平台安装包分发，负责：

- 创建 TUN 和接收目标集群流量；
- DNS 劫持与 `cluster.local` nameserver policy；
- 根据 Pod CIDR 和 Service IP 执行规则路由；
- 将集群流量发送到本地 `KUBERNETES` SOCKS5 桥；
- 让所有非集群流量保持 `DIRECT`。

桌面客户端生成最小配置，不接受代理订阅，也不接管公网流量。sing-box 通过只监听
`127.0.0.1` 的 External Controller 接受健康检查，Controller Secret 每个 Session 随机
生成。

Privileged Helper 是独立、最小权限的系统服务。本机 IPC 使用 Token 认证，只接受字段受限
的 Session 描述，不接受命令或文件路径，负责：

- 下载固定版本的官方 sing-box 压缩包，校验内置 SHA-256 后安装到系统保护目录；
- 在受保护的 Session 目录内重新生成 sing-box 配置；
- 创建和销毁 utun/TUN；
- 添加和删除明确的 Pod/Service 路由；
- 配置 `cluster.local` split DNS；
- 崩溃恢复时清理残留网络配置。

Helper 不读取 kubeconfig，也不持有 Kubernetes 凭证；仅当系统保护目录尚未安装内核时，
联网获取固定版本且经过校验的 sing-box Release。

sing-box 使用 GPLv3。分发安装包时必须同时保留许可证、版权声明，并按许可证要求提供对应
源码。sing-box 保持为独立进程，不修改其源码；项目仍需在发布流程中生成第三方许可证和源码
获取说明。

平台实现：

| 平台 | TUN | 权限服务 | DNS |
| --- | --- | --- | --- |
| macOS | Network Extension 或 utun | LaunchDaemon + Authorization Services | DNS Settings / split DNS |
| Windows | Wintun | Windows Service | NRPT / DNS 配置 |
| Linux | `/dev/net/tun` | systemd service 或 polkit | systemd-resolved |

用户只在首次安装或升级 Helper 时授权，而不是每次连接都输入密码。

macOS 原型阶段可以使用 utun 快速验证数据通路；正式发布前优先评估 Packet Tunnel Network
Extension，以获得更稳定的系统生命周期和签名分发体验。

### 4.4 In-cluster Gateway

Gateway 是一个普通 Deployment：

- 默认 1 个副本；
- 不创建公网 LoadBalancer；
- 只通过 Kubernetes API Server port-forward 访问；
- 接收多路复用的 TCP/UDP 会话；
- 在 Pod 网络内连接目标 Pod、Service 或 CoreDNS；
- 暴露健康检查和协议版本；
- 不需要 `hostNetwork`、`privileged` 或 `NET_ADMIN`。

这比在集群内创建 TUN 并修改 iptables 更安全，也更容易被企业集群接受。

### 4.5 跨平台边界

通用模块：

- Kubernetes API 与 kubeconfig；
- Gateway 安装器；
- 连接状态机；
- 用户态 TCP/IP 栈；
- 隧道协议；
- 路由规划与冲突检测；
- 指标、日志与诊断。

平台模块仅实现以下接口：

```text
EnsureHelper()
CreateTunnel(configuration)
ApplyRoutes(routes)
ConfigureSplitDNS(domains, server)
WatchNetworkChanges()
RestoreSystemNetwork()
```

平台模块返回结构化错误，UI 不直接解析系统命令输出。

## 5. 数据通路

### 5.1 Pod IP / Service IP

1. 应用向 Pod IP 或 ClusterIP 发起连接。
2. 系统路由将数据包送入 KubeLoop 的 TUN。
3. sing-box 根据动态生成的 IP-CIDR 规则选择 `KUBERNETES` outbound。
4. sing-box 将 TCP/UDP 会话发送到本地 SOCKS5 桥。
5. SOCKS5 桥把 TCP 和 UDP 都封装进可靠的多路复用流。
6. 流量通过 API Server port-forward 到 Gateway。
7. Gateway 在集群内连接真实目标。
8. 返回流量沿原通道和 sing-box TUN 返回应用。

使用 sing-box 网络栈的好处：

- 集群 Gateway 不需要网络管理权限；
- 不依赖集群 CNI 的回程路由能力；
- 本机真实 IP 不会泄漏到 Pod 网络；
- TCP/UDP 的错误和超时可以正确返回给本地应用。

### 5.2 DNS

客户端使用 split DNS，只接管：

- `cluster.local`；
- `svc.cluster.local`；
- 用户显式配置的集群域。

查询通过现有隧道转发到 kube-system 中的 CoreDNS Service。其他域名继续使用用户原来的
DNS，不受 KubeLoop 影响。

短名称如 `my-service` 存在 Namespace 语义，首版采用当前 Namespace 生成搜索域：

```text
<namespace>.svc.cluster.local
svc.cluster.local
cluster.local
```

UI 需要明确展示当前 DNS Namespace。

## 6. 集群网络发现

### 6.1 Pod 网络

优先读取 Node：

- `spec.podCIDR`；
- `spec.podCIDRs`（双栈）。

如果 CNI 不写 Node PodCIDR，则回退为读取现有 Pod IP 并安装精确路由。UI 会提示这种模式
无法自动覆盖尚未创建的新 Pod。

### 6.2 Service 网络

Kubernetes API 通常不直接暴露 Service CIDR。首版采用两级策略：

1. 获取所有 Service 的 `clusterIPs`，安装精确 `/32` 或 `/128` 路由；
2. 监听 Service 变更并增量更新路由。

如果用户或集群元数据提供 Service CIDR，则可以直接安装网段路由。

必须忽略：

- Headless Service（`clusterIP: None`）；
- ExternalName；
- 空地址；
- 用户明确排除的 Namespace。

### 6.3 网段冲突

连接前比较目标路由与本机：

- LAN 路由；
- VPN 路由；
- Docker/虚拟机网络；
- 其他已连接集群路由。

发现冲突时不应静默覆盖。UI 展示冲突双方、可能影响，并提供：

- 取消连接；
- 只添加精确 Pod/Service IP 路由；
- 用户确认后的强制优先路由。

## 7. 隧道协议

协议运行在单个可靠字节流上，使用多路复用减少 port-forward 数量。

### 7.1 握手

客户端发送：

- 协议版本；
- 客户端版本；
- 集群 Session ID；
- 支持的能力：TCP、UDP、IPv6、DNS；
- 最大帧长度。

Gateway 返回协商后的能力和限制。不兼容时返回明确的升级信息。

### 7.2 帧类型

- `OPEN_TCP`
- `TCP_DATA`
- `OPEN_UDP`
- `UDP_DATA`
- `CLOSE`
- `RESET`
- `PING` / `PONG`
- `DNS_QUERY` / `DNS_RESPONSE`
- `WINDOW_UPDATE`

每个流包含独立 Stream ID。TCP 必须有流量控制，避免单个大下载阻塞全部连接。UDP 会话按
源地址、目标地址和空闲时间管理。

协议负载不自行加密，因为底层经过 Kubernetes API Server 的 TLS 通道，但每个会话仍需
随机令牌，防止 Gateway Pod 内其他进程复用监听端口。

## 8. Kubernetes 权限

连接前客户端用 `SelfSubjectAccessReview` 探测能力，并按结果降级：

| 能力 | 缺失时 |
| --- | --- |
| Gateway 安装（`kubeloop-system` Deployment） | 只查找预装 Gateway；没有则展示可复制管理员 YAML |
| Gateway `pods/portforward` | **硬失败**（无法建 TUN） |
| list nodes / kube-dns / ServiceCIDR 源 | Overview 允许手动填写 Pod/Service CIDR 与 CoreDNS，按 Context 持久化 |
| 全集群 list pods/services | 降级为可见 Namespace 列表（单/多 ns） |
| Service update + EndpointSlice | Exchange 禁用 |
| Service create + EndpointSlice | Preview 禁用 |

### 8.0 管理员预装 + 开发者最小权限示例

Gateway **始终**安装在 `kubeloop-system`（不按业务 Namespace 拆分）。开发者至少需要对 Gateway Pod `get/list` 与 `pods/portforward`。

开发者（单业务 Namespace，例如 `dev`）大致需要：

```yaml
# Gateway 通道（集群级或 kubeloop-system Role）
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "list"]
- apiGroups: [""]
  resources: ["pods/portforward"]
  verbs: ["create"]
# 业务 Namespace
- apiGroups: [""]
  resources: ["pods", "services"]
  verbs: ["get", "list", "watch"]
# 强烈建议（否则需在 Overview 手动填 CIDR/DNS）
- apiGroups: [""]
  resources: ["nodes"]
  verbs: ["list"]
- apiGroups: [""]
  resources: ["services"]
  resourceNames: ["kube-dns", "coredns"]
  verbs: ["get"]
  # 作用域：kube-system
# Exchange / Preview 另加 services update/create 与 endpointslices *
```

客户端硬依赖（建连）：

- 读取 Gateway Pod；
- 对 Gateway Pod 创建 port-forward。

自动网络发现（有则填 Overview，无则手动）：

- 读取 Node（Pod CIDR）；
- 读取 `kube-system` 的 kube-dns/coredns Service；
- 读取 ServiceCIDR 源（如 servicecidrs / kubeadm-config）。

业务清单：

- 读取/监听允许 Namespace 内的 Pod 和 Service。

Service Local Intercept / Preview 额外需要目标 Namespace：

- 更新或创建 Service；
- 创建、更新、删除 EndpointSlice。

安装 Gateway 还需要在 `kubeloop-system` 创建 Deployment / ServiceAccount / Role / RoleBinding。
企业环境可由管理员预装；无安装权限的账号连接时复用已有 Gateway。

Gateway 本身不需要读取 Kubernetes API，因此默认 ServiceAccount 不授予额外权限。

### 8.1 Gateway 自动安装流程

1. 客户端检查 `kubeloop-system` Namespace；
2. 使用 server-side apply 提交带版本标签的资源；
3. 等待 Deployment Available；
4. 校验镜像 digest、协议版本和健康状态；
5. 建立 port-forward；
6. 客户端升级时先判断协议兼容性，再决定是否滚动升级 Gateway。

自动安装必须是幂等的。客户端只管理带以下标识的资源：

```text
app.kubernetes.io/managed-by: kube-loop
app.kubernetes.io/part-of: kube-loop
```

如果用户没有安装权限，UI 展示缺少的 RBAC 权限和可复制的管理员安装清单，但不会降级为
执行外部命令。

卸载 Gateway 是独立的设置操作。断开连接不会删除 Gateway，以便下次快速连接。

## 9. 安全设计

- kubeconfig 凭证只由 Core Service 在内存中读取，不传给 UI、Helper 或 Gateway；
- 日志默认脱敏 token、证书和 kubeconfig 内容；
- Gateway 不暴露 NodePort、LoadBalancer 或 Ingress；
- Helper IPC 校验调用进程签名和用户身份；
- Helper 只允许操作 KubeLoop 自己创建的接口、路由和 DNS 配置；
- 网络配置写入恢复日志，应用异常退出后自动回滚；
- Gateway 镜像固定 digest，并在 UI 中展示版本；
- 支持管理员禁用任意目标访问，限制到集群 CIDR。

## 10. 故障恢复

客户端要处理：

- 电脑睡眠与唤醒；
- Wi-Fi 切换；
- API Server 短暂断线；
- Gateway 重建；
- kubeconfig 凭证刷新；
- 应用崩溃；
- Helper 或 Core Service 版本不一致。

断线时先保留会话并指数退避重连。超过阈值后移除 TUN 路由，避免应用流量持续黑洞。

## 11. 可观测性

UI 中展示：

- 当前阶段和连接时长；
- Gateway 版本与延迟；
- Pod/Service 路由数量；
- TCP/UDP 活动连接；
- 上下行字节数；
- DNS 成功率和最近错误；
- 重连次数。

诊断包仅包含：

- 脱敏后的客户端日志；
- 网络路由快照；
- 版本和平台信息；
- Gateway 状态；
- 权限检查结果。

默认不包含流量内容、DNS 查询明细或 Kubernetes Secret。

## 12. 版本里程碑

### M0：交互原型

- 首页、集群选择、连接状态、网络与日志页面；
- 使用模拟数据验证产品流程；
- 确定品牌、信息层级和错误体验。

### M1：开发者预览版

- macOS arm64；
- 单集群；
- Pod/Service IPv4；
- TCP、UDP 和 cluster.local DNS；
- Gateway 自动安装；
- 基础诊断和自动恢复。

### M2：可试用版

- macOS amd64；
- IPv6/双栈；
- 系统托盘、开机启动、自动更新；
- 企业预装 Gateway；
- 性能和稳定性优化。

### M3：Windows

- Windows 10/11；
- Wintun 与 Windows Service；
- NRPT split DNS；
- 与 macOS 共用协议和 Gateway。

### M4：Linux

- 主流桌面发行版；
- `/dev/net/tun`；
- systemd-resolved；
- deb/rpm/AppImage 分发评估。

多集群和规则路由放在三平台基础访问能力稳定之后。Service Local Intercept 见 §2.2。

## 13. MVP 验收标准

在一个标准 Kubernetes 集群中，用户可从桌面 UI：

1. 选择 Context 并完成连接；
2. 用浏览器或本地应用访问 Pod IP；
3. 访问 ClusterIP Service；
4. 访问 `service.namespace.svc.cluster.local`；
5. 断开后系统路由和 DNS 完整恢复；
6. 应用被强制结束后，Helper 能清理残留配置；
7. 整个过程不打开终端、不要求用户安装 kubectl、不暴露集群公网端口。

性能初始目标：

- 新建 TCP 连接额外延迟低于 30 ms（不含集群基础网络延迟）；
- 单连接吞吐达到 100 Mbps；
- 空闲常驻内存低于 150 MB；
- 1000 条并发 TCP 连接下客户端保持可操作。

## 14. 后续决策

以下问题不阻塞交互原型，但需要在 M1 开发前确认：

1. 是否需要兼容多个 kubeconfig 文件以及 `KUBECONFIG` 合并规则；
2. macOS 正式版采用 Network Extension 还是独立 utun Helper；
3. Gateway 镜像仓库和签名/供应链方案；
4. 是否需要提供企业管理员离线安装清单；
5. Service 数量很大时，使用精确路由还是要求管理员提供 Service CIDR；
6. 客户端自动更新和代码签名渠道。
