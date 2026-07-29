const dictionary = {
  en: {
    "nav.how": "How it works",
    "nav.features": "Features",
    "nav.arch": "Architecture",
    "hero.eyebrow": "Desktop Kubernetes client",
    "hero.lead": "Transparent Kubernetes network access from your desktop.",
    "hero.support":
      "Connect once. Reach Pod IPs, ClusterIP Services, and cluster DNS without port-forwards or per-application proxy settings.",
    "cta.download": "Download releases",
    "cta.github": "View on GitHub",
    "cta.design": "Read the design",
    "mock.overview": "Overview",
    "mock.connections": "Connections",
    "mock.network": "Network",
    "mock.logs": "Logs",
    "mock.traffic": "Traffic statistics",
    "mock.trafficSub": "Live upload / download through the cluster tunnel",
    "mock.upload": "Upload",
    "mock.download": "Download",
    "mock.active": "Active",
    "how.title": "Connect once. Reach the cluster.",
    "how.support":
      "KubeLoop discovers your cluster network, installs a minimal Gateway, and routes only Kubernetes traffic through a managed sing-box TUN.",
    "how.step1.title": "Select context",
    "how.step1.body":
      "Pick a kubeconfig Context and DNS Namespace. Credentials stay on this device.",
    "how.step2.title": "Click Connect",
    "how.step2.body":
      "KubeLoop installs the Gateway, discovers Pods, Services, and CoreDNS, then starts the tunnel.",
    "how.step3.title": "Use cluster names",
    "how.step3.body":
      "Local apps talk to Pod IPs, ClusterIPs, and *.cluster.local directly. Everything else stays DIRECT.",
    "features.title": "Built for developer workstations",
    "features.support":
      "A desktop-first workflow that replaces port-forward rituals and proxy env vars with one transparent tunnel.",
    "features.f1.label": "Transparent TUN",
    "features.f1.body":
      "Managed sing-box routes only discovered Pod and Service traffic.",
    "features.f2.label": "Cluster DNS",
    "features.f2.body":
      "Resolve Service names and cluster.local domains from local apps.",
    "features.f3.label": "Exchange",
    "features.f3.body":
      "Replace a ClusterIP Service with a process on your machine.",
    "features.f4.label": "Port Forward",
    "features.f4.body":
      "Forward a local port to a Pod or Service without leaving the UI.",
    "features.f5.label": "Minimal Gateway",
    "features.f5.body":
      "Unprivileged in-cluster Deployment, reached only via API Server port-forward.",
    "features.f6.label": "Cross-platform",
    "features.f6.body":
      "macOS, Windows, and Linux desktop builds from the same Wails app.",
    "arch.title": "How traffic flows",
    "arch.support":
      "Local applications enter through sing-box. Only cluster destinations cross the SOCKS bridge into the Gateway.",
    "arch.diagram": `Local application
      │
      ▼
sing-box TUN / DNS / rules
      │  SOCKS5 TCP + UDP
      ▼
KubeLoop local bridge
      │  API Server port-forward
      ▼
In-cluster Gateway
      │
      ├── Pod IP
      ├── ClusterIP Service
      └── CoreDNS`,
    "band.title": "Try the latest desktop build",
    "band.support":
      "KubeLoop is under active M1 development. Grab a release, connect to your cluster, and open a Service by name.",
    "footer.copy": "© KubeLoop contributors. Apache License 2.0.",
    documentTitle: "KubeLoop — Transparent Kubernetes network access",
  },
  "zh-CN": {
    "nav.how": "工作方式",
    "nav.features": "能力",
    "nav.arch": "架构",
    "hero.eyebrow": "Kubernetes 桌面网络客户端",
    "hero.lead": "在桌面端透明访问 Kubernetes 集群网络。",
    "hero.support":
      "连接一次即可访问 Pod IP、ClusterIP Service 与集群 DNS，无需逐个 port-forward，也不必配置应用代理。",
    "cta.download": "下载 Release",
    "cta.github": "查看 GitHub",
    "cta.design": "阅读设计文档",
    "mock.overview": "概览",
    "mock.connections": "连接",
    "mock.network": "网络",
    "mock.logs": "日志",
    "mock.traffic": "流量统计",
    "mock.trafficSub": "集群隧道中的实时上传 / 下载",
    "mock.upload": "上传",
    "mock.download": "下载",
    "mock.active": "活跃",
    "how.title": "连接一次，直达集群。",
    "how.support":
      "KubeLoop 发现集群网络、安装最小化 Gateway，并通过托管的 sing-box TUN 只路由 Kubernetes 流量。",
    "how.step1.title": "选择 Context",
    "how.step1.body":
      "选择 kubeconfig Context 与 DNS Namespace。凭证只保留在本机。",
    "how.step2.title": "点击连接",
    "how.step2.body":
      "自动安装 Gateway，发现 Pod、Service 与 CoreDNS，然后启动隧道。",
    "how.step3.title": "使用集群名称",
    "how.step3.body":
      "本地应用可直接访问 Pod IP、ClusterIP 与 *.cluster.local。其余流量保持 DIRECT。",
    "features.title": "为开发者工作站而设计",
    "features.support":
      "用一个透明隧道替代繁琐的 port-forward 和代理环境变量。",
    "features.f1.label": "透明 TUN",
    "features.f1.body": "托管 sing-box，仅路由已发现的 Pod 与 Service 流量。",
    "features.f2.label": "集群 DNS",
    "features.f2.body": "本地应用可解析 Service 名称与 cluster.local 域名。",
    "features.f3.label": "Exchange",
    "features.f3.body": "用本机进程替换现有 ClusterIP Service。",
    "features.f4.label": "端口转发",
    "features.f4.body": "在界面中将本地端口转发到 Pod 或 Service。",
    "features.f5.label": "最小化 Gateway",
    "features.f5.body":
      "无特权集群内 Deployment，仅通过 API Server port-forward 访问。",
    "features.f6.label": "跨平台",
    "features.f6.body": "同一套 Wails 应用构建 macOS、Windows 与 Linux 桌面端。",
    "arch.title": "流量如何流动",
    "arch.support":
      "本地应用经 sing-box 进入。只有集群目标会穿过 SOCKS 桥到达 Gateway。",
    "arch.diagram": `本地应用
      │
      ▼
sing-box TUN / DNS / 规则
      │  SOCKS5 TCP + UDP
      ▼
KubeLoop 本地桥
      │  API Server port-forward
      ▼
集群内 Gateway
      │
      ├── Pod IP
      ├── ClusterIP Service
      └── CoreDNS`,
    "band.title": "试用最新桌面构建",
    "band.support":
      "KubeLoop 仍处于活跃的 M1 开发阶段。下载 Release，连接集群，然后直接用 Service 名称访问。",
    "footer.copy": "© KubeLoop 贡献者。Apache License 2.0。",
    documentTitle: "KubeLoop — 透明访问 Kubernetes 集群网络",
  },
};

const storageKey = "kubeloop.site.language";

function applyLanguage(lang) {
  const table = dictionary[lang] || dictionary.en;
  document.documentElement.lang = lang;
  document.title = table.documentTitle;
  document.querySelectorAll("[data-i18n]").forEach((node) => {
    const key = node.getAttribute("data-i18n");
    const value = table[key];
    if (typeof value === "string") node.textContent = value;
  });
  document.querySelectorAll(".lang-btn").forEach((button) => {
    button.setAttribute(
      "aria-pressed",
      button.getAttribute("data-lang") === lang ? "true" : "false",
    );
  });
  localStorage.setItem(storageKey, lang);
}

function initLanguage() {
  const saved = localStorage.getItem(storageKey);
  const preferred =
    saved === "zh-CN" || saved === "en"
      ? saved
      : navigator.language?.toLowerCase().startsWith("zh")
        ? "zh-CN"
        : "en";
  applyLanguage(preferred);
  document.querySelectorAll(".lang-btn").forEach((button) => {
    button.addEventListener("click", () => {
      applyLanguage(button.getAttribute("data-lang") || "en");
    });
  });
}

initLanguage();
