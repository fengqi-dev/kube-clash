# KubeLoop Desktop Client Design

[English](design.md) | [简体中文](design.zh-CN.md)

> Status: Draft v0.2 (product boundaries confirmed)  
> Goal: Let developers connect to a Kubernetes cluster like a VPN, and transparently reach Pod IPs, Service IPs, and cluster DNS from the local machine.

## 0. Confirmed product decisions

- Target platforms are macOS, Windows, and Linux, with macOS first;
- The desktop client automatically checks, installs, and upgrades the Gateway;
- Phase one delivers transparent access to Pods, Services, and cluster DNS only;
- M2 supports replacing a cluster Service with a local process (TCP/UDP) and fully restores Endpoints on disconnect;
- Use sing-box as the TUN, DNS, and rule-routing core.

## 1. Product positioning

KubeLoop is a desktop network client, not a CLI. Users should not need to understand routes, TUN, port-forwarding, or Kubernetes networking details — they select a cluster and click Connect.

It aims for a familiar desktop network-client experience:

- Lives in the system tray;
- One-click connect and disconnect;
- Transparent TUN takeover — no per-app proxy configuration;
- Live connection status, routes, requests, and errors;
- Automatic CIDR conflict detection;
- Optional launch-at-login and auto-reconnect.

It solves Kubernetes development networking problems, not public proxy problems:

- Reach Pod IPs;
- Reach ClusterIP Services;
- Resolve and reach `*.svc.cluster.local`;
- Map a local process as the backend of a cluster Service (Service Local Intercept).

## 2. First-release scope

### 2.1 MVP includes

1. Read the local kubeconfig and show Context, cluster, and Namespace.
2. Discover Pod CIDR, Service IPs, and cluster DNS via the Kubernetes API.
3. Create a system TUN that only takes over the target cluster ranges.
4. Automatically install or reuse the in-cluster Gateway.
5. Reach the Gateway through Kubernetes API Server port-forward — no public ports.
6. Support TCP and UDP.
7. Forward `cluster.local` DNS queries to in-cluster CoreDNS.
8. Show connection duration, upload/download, active connections, and diagnostics.
9. Complete connect, disconnect, Gateway install, and uninstall from the UI.

### 2.2 M2: Service Local Intercept

While connected, users can replace an existing ClusterIP Service with a local process:

1. The desktop registers a unique listen port (TCP/UDP) on the Gateway over the control channel;
2. Clear the Service selector, snapshot and delete classic Endpoints, replace EndpointSlices with a managed slice pointing at Gateway Pod IP:listenPort;
3. Cluster clients keep the original ClusterIP / DNS; kube-proxy sends traffic to the Gateway;
4. The Gateway emits `InboundReady`; the desktop `Accept`s and forwards to `127.0.0.1` (or a configured local address);
5. On stop or disconnect, restore the selector, recreate Endpoints from the snapshot, delete the managed EndpointSlice, and unregister the Gateway listener.

The Gateway still has no kube API access, no privilege, and no hostNetwork. EndpointSlice changes are performed with the desktop kubeconfig.

### 2.2.1 Traffic Mirror

Mirror uses the same Service → Gateway hijack as Exchange, but the desktop datapath differs:

1. Dial the original Pod (from the Endpoints snapshot) as the **primary** path via Gateway outbound TCP/UDP, and return its response to the cluster client;
2. Tee a copy of the client request to a local TCP/UDP process; discard the local response;
3. If the local dial fails, primary traffic continues uninterrupted.

### 2.3 Out of MVP

- Global public proxying and rule subscriptions;
- Simultaneous multi-cluster connect;
- Full ICMP/Ping semantics;
- Headless / ExternalName Service intercept;
- Service Mesh traffic identity simulation;
- Windows client (in MVP delivery order).

Architecture stays cross-platform from day one. Delivery order:

1. macOS;
2. Windows;
3. Linux.

The core network stack, Kubernetes access, tunnel protocol, and Gateway must be shared across platforms. System network configuration lives in a platform adapter layer and must not enter the common connect flow.

## 3. User experience

### 3.1 Home

```text
┌──────────────────────────────────────────────────────┐
│ KubeLoop                              Settings  —  □ │
├──────────────────────────────────────────────────────┤
│                                                      │
│             ● Connected                              │
│             dev-cluster / default                    │
│             00:42:18                                 │
│                                                      │
│               [ Disconnect ]                         │
│                                                      │
│  Pod network      Service         DNS                │
│  10.244.0.0/16    32 IPs          cluster.local      │
│                                                      │
│  ↓ 18.2 MB        ↑ 4.7 MB        12 active conns    │
├──────────────────────────────────────────────────────┤
│  Overview    Connections    Network    Logs          │
└──────────────────────────────────────────────────────┘
```

When disconnected, the main area shows:

1. kubeconfig selection;
2. Context selection;
3. Namespace selection;
4. Primary Connect button;
5. First-connect Gateway install prompt.

### 3.2 Connection states

The state machine must be explainable to users:

```text
Disconnected
  → Check kubeconfig
  → Check cluster permissions
  → Check/install Gateway
  → Discover Pod and Service networks
  → Check local CIDR conflicts
  → Request system network permission
  → Create secure channel
  → Connected
```

On any failure, the UI shows:

- A human-readable reason;
- Which capabilities are affected;
- A Retry button;
- Copyable diagnostic details.

### 3.3 System tray

The tray menu provides:

- Current connection status;
- Recently used clusters;
- Connect / Disconnect;
- Open main window;
- Quit.

Closing the main window does not disconnect. Quitting the app must ask for confirmation.

## 4. Architecture

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

### 4.1 Desktop UI

Recommended stack: Wails + React:

- Go fits Kubernetes clients, network control, and concurrency;
- UI can stay a modern desktop experience;
- Smaller install size and resident memory than Electron;
- Core logic can be reused on Windows and Linux.

The UI process does not hold root privileges.

### 4.2 Core Service

Runs as a normal user and owns:

- kubeconfig and Context management;
- Kubernetes API resource discovery;
- Gateway install, upgrade, and health checks;
- port-forward setup;
- Connection state machine;
- Userspace TCP/IP stack orchestration;
- Metrics and structured logs;
- Status push to the UI over local RPC.

MVP does not invoke external `kubectl`; it uses client-go directly to avoid version skew and flashing terminal windows.

### 4.3 sing-box Core and Privileged Helper

sing-box ships as a managed independent process in the platform package and is responsible for:

- Creating the TUN and receiving target cluster traffic;
- DNS hijack / `cluster.local` nameserver policy;
- Rule routing from Pod CIDR and Service IPs;
- Sending cluster traffic to the local `KUBERNETES` SOCKS5 bridge;
- Keeping all non-cluster traffic `DIRECT`.

The desktop generates a minimal config — no proxy subscriptions and no takeover of public internet traffic. sing-box exposes a health External Controller on `127.0.0.1` only; the Controller Secret is random per session.

The Privileged Helper is a separate, least-privilege system service. Its
token-authenticated local IPC accepts a field-constrained session description,
not commands or filesystem paths. It:

- Runs the pinned sing-box binary shipped in the platform package in place
  (no runtime copy or download into protected storage);
- Regenerates the sing-box config in a protected per-session directory;
- Creates and destroys utun/TUN;
- Adds and removes explicit Pod/Service routes;
- Configures `cluster.local` split DNS;
- Cleans residual network config on crash recovery.

On Windows the package layout mirrors common sidecar apps: a flat
`Program Files\KubeLoop\` directory with `sing-box.exe` beside the app and
`resources\` containing `kubeloop-helper.exe` plus dedicated
`kubeloop-helper-install.exe` / `kubeloop-helper-uninstall.exe` tools used for
UAC elevation.

The Helper does not read kubeconfig or hold Kubernetes credentials.

sing-box is GPLv3. Distributions that bundle it must keep license and copyright notices and provide corresponding source as required. sing-box stays an unmodified separate process; the release pipeline still produces third-party notices and source-access instructions.

Platform mapping:

| Platform | TUN | Privilege service | DNS |
| --- | --- | --- | --- |
| macOS | Network Extension or utun | LaunchDaemon + Authorization Services | DNS Settings / split DNS |
| Windows | Wintun | Windows Service | NRPT / DNS config |
| Linux | `/dev/net/tun` | systemd service or polkit | systemd-resolved |

Users authorize only on first Helper install or upgrade, not on every Connect.

For the macOS prototype, utun is acceptable to validate the data path; before GA, prefer evaluating a Packet Tunnel Network Extension for a more stable system lifecycle and signed distribution.

### 4.4 In-cluster Gateway

The Gateway is a normal Deployment:

- One replica by default;
- No public LoadBalancer;
- Reached only via Kubernetes API Server port-forward;
- Accepts multiplexed TCP/UDP sessions;
- Dials target Pods, Services, or CoreDNS inside the Pod network;
- Exposes health checks and protocol version;
- Needs no `hostNetwork`, `privileged`, or `NET_ADMIN`.

This is safer than creating a TUN inside the cluster and rewriting iptables, and easier for enterprise clusters to accept.

### 4.5 Cross-platform boundary

Shared modules:

- Kubernetes API and kubeconfig;
- Gateway installer;
- Connection state machine;
- Userspace TCP/IP stack;
- Tunnel protocol;
- Route planning and conflict detection;
- Metrics, logs, and diagnostics.

Platform modules only implement:

```text
EnsureHelper()
CreateTunnel(configuration)
ApplyRoutes(routes)
ConfigureSplitDNS(domains, server)
WatchNetworkChanges()
RestoreSystemNetwork()
```

Platform modules return structured errors; the UI does not parse raw system command output.

## 5. Data path

### 5.1 Pod IP / Service IP

1. An app connects to a Pod IP or ClusterIP.
2. The system route sends packets into the KubeLoop TUN.
3. sing-box selects the `KUBERNETES` outbound from dynamically generated IP-CIDR rules.
4. sing-box sends the TCP/UDP session to the local SOCKS5 bridge.
5. The SOCKS5 bridge multiplexes TCP and UDP onto a reliable stream.
6. Traffic reaches the Gateway through API Server port-forward.
7. The Gateway dials the real target in-cluster.
8. Return traffic follows the same path back through sing-box TUN to the app.

Benefits of the sing-box network stack:

- The cluster Gateway needs no network-admin privileges;
- No dependency on CNI return-path behavior;
- The laptop’s real IP is not leaked into the Pod network;
- TCP/UDP errors and timeouts return correctly to local apps.

### 5.2 DNS

The client uses split DNS and only takes over:

- Configured cluster domains (always includes `cluster.local`, plus optional custom domains);
- Matching `svc.<domain>` / `<ns>.svc.<domain>` suffixes;
- Reverse zones derived from Pod/Service CIDRs (`*.in-addr.arpa` / `*.ip6.arpa`) for PTR.

Queries are forwarded through the existing tunnel to the kube-system CoreDNS Service. All other names keep using the user’s original DNS. The local DNS search proxy listens on UDP and TCP; sing-box DNS uses `prefer_ipv4` so AAAA answers are allowed when dual-stack routes exist.

Short names such as `my-service` are namespace-sensitive. Search suffixes come from the configurable DNS search Namespace:

```text
<namespace>.svc.<cluster-domain>
svc.<cluster-domain>
<cluster-domain>
```

The UI must clearly show the current DNS search Namespace and cluster domains.

#### Coexistence with other TUN / system-DNS clients

KubeLoop does **not** hijack the system default resolver. It installs selective split DNS only. Clients such as Clash Verge that take over TUN and/or force system DNS can prevent cluster names from reaching KubeLoop. After connect, KubeLoop probes `kubernetes.default.svc.<cluster-domain>` via its split-DNS port and surfaces a warning when the probe fails. Practical guidance: avoid running two TUN stacks at once, or disable the other client’s TUN/system DNS while KubeLoop is connected (connect KubeLoop last when both must run).

## 6. Cluster network discovery

### 6.1 Pod network

Prefer Node fields:

- `spec.podCIDR`;
- `spec.podCIDRs` (dual-stack).

If the CNI does not populate Node PodCIDR, fall back to reading existing Pod IPs and installing precise routes. The UI should warn that this mode cannot automatically cover Pods that do not exist yet.

### 6.2 Service network

The Kubernetes API often does not expose Service CIDR directly. MVP uses a two-level strategy:

1. Collect all Service `clusterIPs` and install precise `/32` or `/128` routes;
2. Watch Service changes and update routes incrementally.

If the user or cluster metadata provides a Service CIDR, install a range route instead.

Must ignore:

- Headless Services (`clusterIP: None`);
- ExternalName;
- Empty addresses;
- Namespaces the user explicitly excludes.

### 6.3 CIDR conflicts

Before connect, compare target routes with local:

- LAN routes;
- VPN routes;
- Docker / VM networks;
- Routes from other connected clusters.

Conflicts must not be overwritten silently. The UI shows both sides, likely impact, and offers:

- Cancel connect;
- Add only precise Pod/Service IP routes;
- Force preferred routes after explicit user confirmation.

## 7. Tunnel protocol

The protocol runs on a single reliable byte stream and multiplexes sessions to reduce port-forward count.

### 7.1 Handshake

The client sends:

- Protocol version;
- Client version;
- Cluster session ID;
- Capabilities: TCP, UDP, IPv6, DNS;
- Max frame length.

The Gateway returns negotiated capabilities and limits. Incompatible versions return a clear upgrade message.

### 7.2 Frame types

- `OPEN_TCP`
- `TCP_DATA`
- `OPEN_UDP`
- `UDP_DATA`
- `CLOSE`
- `RESET`
- `PING` / `PONG`
- `DNS_QUERY` / `DNS_RESPONSE`
- `WINDOW_UPDATE`

Each stream has an independent Stream ID. TCP must have flow control so one large download cannot stall everything. UDP sessions are keyed by source, destination, and idle timeout.

Payloads are not encrypted separately because the path already uses the API Server TLS channel, but each session still needs a random token so other processes inside the Gateway Pod cannot reuse the listen port.

## 8. Kubernetes permissions

Before connect, the client probes capabilities with `SelfSubjectAccessReview` and degrades accordingly:

| Capability | When missing |
| --- | --- |
| Gateway install (`kubeloop-system` Deployment) | Only look for a preinstalled Gateway; otherwise show copyable admin YAML |
| Gateway `pods/portforward` | **Hard fail** (cannot build TUN) |
| list nodes / kube-dns / ServiceCIDR sources | Overview allows manual Pod/Service CIDR and CoreDNS, persisted per Context |
| Cluster-wide list pods/services | Degrade to visible Namespace list (one or many) |
| Service update + EndpointSlice | Exchange disabled |
| Service create + EndpointSlice | Preview disabled |

### 8.0 Admin preinstall + minimal developer permissions

The Gateway is **always** installed in `kubeloop-system` (not split per app Namespace). Developers at least need `get/list` and `pods/portforward` on the Gateway Pod.

A developer scoped to one app Namespace (for example `dev`) roughly needs:

```yaml
# Gateway path (cluster-scoped or kubeloop-system Role)
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "list"]
- apiGroups: [""]
  resources: ["pods/portforward"]
  verbs: ["create"]
# App Namespace
- apiGroups: [""]
  resources: ["pods", "services"]
  verbs: ["get", "list", "watch"]
# Strongly recommended (otherwise fill CIDR/DNS manually on Overview)
- apiGroups: [""]
  resources: ["nodes"]
  verbs: ["list"]
- apiGroups: [""]
  resources: ["services"]
  resourceNames: ["kube-dns", "coredns"]
  verbs: ["get"]
  # Scope: kube-system
# Exchange / Preview additionally need services update/create, endpointslices *, and endpoints *
```

Hard client dependencies (connect):

- Read the Gateway Pod;
- Create port-forward to the Gateway Pod.

Automatic network discovery (fill Overview when present, else manual):

- Read Nodes (Pod CIDR);
- Read kube-dns/coredns Service in `kube-system`;
- Read ServiceCIDR sources (for example servicecidrs / kubeadm-config).

Inventory:

- List/watch Pods and Services in allowed Namespaces.

Service Local Intercept / Preview additionally need in the target Namespace:

- Update or create Services;
- Create, update, and delete EndpointSlices;
- Get, delete, and create Endpoints (Exchange snapshots and restores classic Endpoints).

Installing the Gateway also needs create rights for Deployment / ServiceAccount / Role / RoleBinding in `kubeloop-system`. Enterprises can preinstall; accounts without install rights reuse an existing Gateway.

The Gateway itself does not need Kubernetes API access, so its default ServiceAccount gets no extra permissions.

### 8.1 Gateway auto-install flow

1. Client checks the `kubeloop-system` Namespace;
2. Server-side apply version-labeled resources;
3. Wait until the Deployment is Available;
4. Verify image digest, protocol version, and health;
5. Establish port-forward;
6. On client upgrade, check protocol compatibility before rolling the Gateway.

Install must be idempotent. The client only manages resources with:

```text
app.kubernetes.io/managed-by: kube-loop
app.kubernetes.io/part-of: kube-loop
```

If the user lacks install rights, the UI shows missing RBAC and a copyable admin manifest, and never falls back to running external commands.

Uninstalling the Gateway is a separate Settings action. Disconnect does not delete the Gateway, so the next connect is fast.

## 9. Security design

- kubeconfig credentials are read only by Core Service in memory — never sent to UI, Helper, or Gateway;
- Logs redact tokens, certificates, and kubeconfig content by default;
- The Gateway is not published as NodePort, LoadBalancer, or Ingress;
- Helper IPC verifies caller process signature and user identity;
- Helper may only operate interfaces, routes, and DNS config created by KubeLoop;
- Network changes are written to a recovery log and rolled back after abnormal exit;
- Gateway images are pinned by digest and shown in the UI;
- Admins can disable arbitrary-target access and limit routes to cluster CIDRs.

## 10. Failure recovery

The client must handle:

- Sleep and wake;
- Wi-Fi changes;
- Brief API Server outages;
- Gateway recreation;
- kubeconfig credential refresh;
- App crashes;
- Helper or Core Service version mismatch.

On disconnect, keep the session and reconnect with exponential backoff first. After a threshold, remove TUN routes so application traffic does not black-hole forever.

## 11. Observability

The UI shows:

- Current phase and connection duration;
- Gateway version and latency;
- Pod/Service route counts;
- Active TCP/UDP connections;
- Upload/download bytes;
- DNS success rate and recent errors;
- Reconnect count.

Diagnostic bundles include only:

- Redacted client logs;
- Network route snapshots;
- Version and platform info;
- Gateway status;
- Permission check results.

By default they exclude traffic payloads, DNS query details, and Kubernetes Secrets.

## 12. Milestones

### M0: Interaction prototype

- Home, cluster selection, connection status, network and logs pages;
- Validate product flow with mock data;
- Settle brand, information hierarchy, and error UX.

### M1: Developer preview

- macOS arm64;
- Single cluster;
- Pod/Service IPv4;
- TCP, UDP, and cluster.local DNS;
- Automatic Gateway install;
- Basic diagnostics and auto-recovery.

### M2: Trialable release

- macOS amd64;
- IPv6 / dual-stack;
- System tray, launch-at-login, auto-update;
- Enterprise preinstalled Gateway;
- Performance and stability work.

### M3: Windows

- Windows 10/11;
- Wintun and Windows Service;
- NRPT split DNS;
- Shared protocol and Gateway with macOS.

### M4: Linux

- Mainstream desktop distributions;
- `/dev/net/tun`;
- systemd-resolved;
- Evaluate deb/rpm/AppImage distribution.

Multi-cluster and advanced rule routing come after the three platforms have stable basic access. Service Local Intercept is in §2.2.

## 13. MVP acceptance criteria

On a standard Kubernetes cluster, from the desktop UI a user can:

1. Select a Context and complete connect;
2. Reach a Pod IP from a browser or local app;
3. Reach a ClusterIP Service;
4. Reach `service.namespace.svc.cluster.local`;
5. Fully restore system routes and DNS after disconnect;
6. Have the Helper clean residual config if the app is force-quit;
7. Do all of the above without a terminal, without installing kubectl, and without exposing the cluster on the public internet.

Initial performance targets:

- Extra TCP connect latency under 30 ms (excluding baseline cluster latency);
- Single-connection throughput at least 100 Mbps;
- Idle resident memory under 150 MB;
- Client remains usable with 1000 concurrent TCP connections.

## 14. Open decisions

These do not block the interaction prototype, but should be confirmed before M1 development:

1. Whether to support multiple kubeconfig files and `KUBECONFIG` merge rules;
2. Whether macOS GA uses Network Extension or a standalone utun Helper;
3. Gateway image registry and signing / supply-chain plan;
4. Whether to ship an offline admin install manifest for enterprises;
5. Whether large Service counts should use precise routes or require an admin-provided Service CIDR;
6. Client auto-update and code-signing channels.
