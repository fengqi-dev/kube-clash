const dictionary = {
  en: {
    "nav.docs": "Docs",
    "nav.github": "GitHub",
    "nav.releases": "Releases",
    "nav.menu": "Menu",
    "nav.overview": "Overview",
    "nav.getStarted": "Get started",
    "nav.product": "Product",
    "nav.workflows": "Workflows",
    "nav.architecture": "Architecture",
    "nav.design": "Design notes",
    "sidebar.group.start": "Start",
    "sidebar.group.guides": "Guides",
    "sidebar.group.reference": "Reference",
    "cta.download": "Download releases",
    "cta.github": "View on GitHub",
    "cta.design": "Design notes",
    "cta.designHref":
      "https://github.com/fengqi-dev/kube-loop/blob/main/docs/design.md",
    "cta.getStarted": "Get started",
    "footer.copy": "© KubeLoop contributors. Apache License 2.0.",

    "overview.title": "Welcome to KubeLoop",
    "overview.desc":
      "KubeLoop is a desktop client that connects your laptop to Kubernetes like a VPN — so local apps can reach Pod IPs, ClusterIP Services, and cluster DNS without port-forwards.",
    "overview.start.title": "Get started",
    "overview.start.quick.title": "Quickstart",
    "overview.start.quick.body": "Download a build, pick a Context, and connect.",
    "overview.start.clusters.title": "Manage clusters",
    "overview.start.clusters.body": "Add kubeconfig files, probe APIs, switch contexts.",
    "overview.start.design.title": "Design notes",
    "overview.start.design.body": "Architecture, permissions, and security boundaries.",
    "overview.guides.title": "Guides",
    "overview.guides.product.title": "Product capabilities",
    "overview.guides.product.body":
      "TUN, split DNS, Host Aliases, Exchange, Mirror, Preview, and Port Forward.",
    "overview.guides.workflows.title": "Everyday workflows",
    "overview.guides.workflows.body":
      "Open Services, map custom domains, debug Pods, exchange, mirror, or preview locally.",
    "overview.guides.arch.title": "Architecture",
    "overview.guides.arch.body": "How traffic flows from your apps to the Gateway.",
    "overview.more.title": "Also useful",
    "overview.more.releases.title": "Releases",
    "overview.more.releases.body": "Desktop packages and Gateway images.",
    "overview.more.github.title": "GitHub",
    "overview.more.github.body": "Source, issues, and contribution entry points.",
    "overview.callout.title": "Early access (M1)",
    "overview.callout.body":
      "Core connect / disconnect, TUN networking, and the privileged helper are available. Signed installers are still being polished.",

    "started.title": "Get started",
    "started.desc": "Connect your machine to a cluster in a few minutes.",
    "started.steps.title": "Connect once",
    "started.step1.title": "Install KubeLoop",
    "started.step1.body":
      "Download a desktop build from GitHub Releases (or build from source with Wails).",
    "started.step2.title": "Confirm kubeconfig access",
    "started.step2.body":
      "Make sure this machine can reach the cluster API with a normal kubeconfig.",
    "started.step3.title": "Select a Context and Connect",
    "started.step3.body":
      "Open the Clusters page, pick a Context, optionally probe the API, then connect.",
    "started.step4.title": "Approve the helper once",
    "started.step4.body":
      "On first use, approve the virtual network service. Later connects should not ask again.",
    "started.after.title": "After you are connected",
    "started.after.body":
      "Use Overview for traffic and status, Workload / Network for Port Forward, Exchange, Mirror, and Preview, and Host Aliases for custom domain → IP maps. Short names like mysql.default resolve via Kubernetes search domains.",

    "product.title": "Product",
    "product.desc":
      "Transparent cluster networking plus the tools you reach for every day.",
    "product.core.title": "Core capabilities",
    "product.core.tun.title": "Transparent TUN",
    "product.core.tun.body": "Managed sing-box routes only discovered Pod and Service traffic.",
    "product.core.dns.title": "Cluster DNS",
    "product.core.dns.body":
      "Split DNS with search domains — mysql.default and *.svc.cluster.local both work.",
    "product.core.hosts.title": "Host Aliases",
    "product.core.hosts.body":
      "Per-context domain → IPv4 maps via local DNS while connected. Reconnect to apply; cleared on disconnect.",
    "product.core.clusters.title": "Cluster management",
    "product.core.clusters.body": "Add kubeconfig files, list Contexts, probe and switch.",
    "product.tools.title": "Developer tools",
    "product.tools.exchange.title": "Exchange",
    "product.tools.exchange.body": "Replace a ClusterIP Service with a process on your machine.",
    "product.tools.mirror.title": "Mirror",
    "product.tools.mirror.body":
      "Keep cluster Pods as the primary path and tee a copy of TCP requests to a local process.",
    "product.tools.preview.title": "Preview",
    "product.tools.preview.body": "Expose a local app as a temporary ClusterIP Service.",
    "product.tools.portfwd.title": "Port Forward",
    "product.tools.portfwd.body": "Forward a local port to a Pod or Service — even when TUN is off.",
    "product.gateway.title": "Minimal Gateway",
    "product.gateway.body":
      "Unprivileged in-cluster Deployment reached only via API Server port-forward. Works with scoped RBAC and admin-preinstalled Gateways.",

    "workflows.title": "Workflows",
    "workflows.desc":
      "Browse, debug, map domains, exchange, mirror, and preview without opening a terminal for every Service.",
    "workflows.list.title": "Everyday paths",
    "workflows.w1.label": "Internal API",
    "workflows.w1.title": "Open a Service in the browser",
    "workflows.w1.body": "Connect, then use a ClusterIP or Service DNS name.",
    "workflows.w1.hint": "mysql.default.svc",
    "workflows.w2.label": "Pod debug",
    "workflows.w2.title": "Hit a real Pod IP",
    "workflows.w2.body": "Pod CIDR is routed locally after Connect.",
    "workflows.w2.hint": "10.244.x.x",
    "workflows.w3.label": "Port forward",
    "workflows.w3.title": "Skip kubectl port-forward",
    "workflows.w3.body": "Start from Workload or Network with a Namespace picker.",
    "workflows.w3.hint": "localhost:8080",
    "workflows.w4.label": "Exchange",
    "workflows.w4.title": "Run a local process as a Service",
    "workflows.w4.body": "Exchange keeps ClusterIP / DNS while traffic lands locally.",
    "workflows.w4.hint": "Exchange",
    "workflows.w5.label": "Mirror",
    "workflows.w5.title": "Debug without replacing the Service",
    "workflows.w5.body":
      "Cluster Pods keep answering clients; a copy of each TCP request is sent to your local process.",
    "workflows.w5.hint": "Mirror",
    "workflows.w6.label": "Host alias",
    "workflows.w6.title": "Map a custom domain to a cluster IP",
    "workflows.w6.body":
      "On Host Aliases, bind app.dev to a Service or Pod IP. Reconnect so split DNS picks it up.",
    "workflows.w6.hint": "app.dev → 10.96.x.x",

    "arch.title": "Architecture",
    "arch.desc":
      "Local apps enter through sing-box. Only cluster destinations cross the SOCKS bridge into the Gateway.",
    "arch.flow.title": "Traffic path",
    "arch.n1.tag": "Desktop",
    "arch.n1.title": "Your apps",
    "arch.n1.body": "Browsers, IDEs, CLIs, and SDKs — no SOCKS settings per app.",
    "arch.n2.tag": "sing-box",
    "arch.n2.title": "TUN / DNS / rules",
    "arch.n2.body":
      "Split DNS and focused routes for Pod CIDR, Service CIDR, cluster.local, and optional Host Aliases.",
    "arch.n3.tag": "Bridge",
    "arch.n3.title": "Local SOCKS5 bridge",
    "arch.n3.body": "TCP and UDP sessions multiplexed toward the cluster path.",
    "arch.n4.tag": "API",
    "arch.n4.title": "Kubernetes API Server",
    "arch.n4.body": "port-forward only — no NodePort, LoadBalancer, or public ingress.",
    "arch.n5.tag": "Gateway",
    "arch.n5.title": "In-cluster Gateway",
    "arch.n5.body": "Unprivileged dialer into Pods, Services, and CoreDNS.",
    "arch.t1": "Pod IP",
    "arch.t2": "ClusterIP Service",
    "arch.t3": "CoreDNS",
    "arch.more.title": "Read more",
    "arch.more.body": "Protocol, RBAC, and recovery details live in the design notes.",
  },
  "zh-CN": {
    "nav.docs": "文档",
    "nav.github": "GitHub",
    "nav.releases": "下载",
    "nav.menu": "菜单",
    "nav.overview": "概览",
    "nav.getStarted": "快速开始",
    "nav.product": "产品能力",
    "nav.workflows": "使用场景",
    "nav.architecture": "架构",
    "nav.design": "设计文档",
    "sidebar.group.start": "开始",
    "sidebar.group.guides": "指南",
    "sidebar.group.reference": "参考",
    "cta.download": "下载 Release",
    "cta.github": "查看 GitHub",
    "cta.design": "设计文档",
    "cta.designHref":
      "https://github.com/fengqi-dev/kube-loop/blob/main/docs/design.zh-CN.md",
    "cta.getStarted": "快速开始",
    "footer.copy": "© KubeLoop 贡献者。Apache License 2.0。",

    "overview.title": "欢迎使用 KubeLoop",
    "overview.desc":
      "KubeLoop 是一款桌面客户端：像连 VPN 一样连上 Kubernetes，让本机应用直接访问 Pod IP、ClusterIP Service 与集群 DNS，无需 port-forward。",
    "overview.start.title": "开始使用",
    "overview.start.quick.title": "快速开始",
    "overview.start.quick.body": "下载构建、选择 Context，然后连接。",
    "overview.start.clusters.title": "管理集群",
    "overview.start.clusters.body": "添加 kubeconfig、探测 API、切换 Context。",
    "overview.start.design.title": "设计文档",
    "overview.start.design.body": "架构、权限与安全边界说明。",
    "overview.guides.title": "指南",
    "overview.guides.product.title": "产品能力",
    "overview.guides.product.body":
      "TUN、分流 DNS、主机别名、Exchange、Mirror、Preview 与端口转发。",
    "overview.guides.workflows.title": "使用场景",
    "overview.guides.workflows.body":
      "打开 Service、映射自定义域名、调试 Pod，以及 Exchange / Mirror / Preview。",
    "overview.guides.arch.title": "架构",
    "overview.guides.arch.body": "流量如何从本机应用到达 Gateway。",
    "overview.more.title": "更多",
    "overview.more.releases.title": "Release",
    "overview.more.releases.body": "桌面安装包与 Gateway 镜像。",
    "overview.more.github.title": "GitHub",
    "overview.more.github.body": "源码、Issue 与贡献入口。",
    "overview.callout.title": "早期体验版（M1）",
    "overview.callout.body":
      "连接编排、TUN 网络与特权 Helper 已可用；签名安装包仍在打磨。",

    "started.title": "快速开始",
    "started.desc": "几分钟内把本机连上集群。",
    "started.steps.title": "连接一次",
    "started.step1.title": "安装 KubeLoop",
    "started.step1.body": "从 GitHub Releases 下载桌面构建（或用 Wails 从源码构建）。",
    "started.step2.title": "确认 kubeconfig 可用",
    "started.step2.body": "确保本机可用普通 kubeconfig 访问集群 API。",
    "started.step3.title": "选择 Context 并连接",
    "started.step3.body": "打开「集群」页，选择 Context，可先探测 API，然后连接。",
    "started.step4.title": "首次批准 Helper",
    "started.step4.body": "首次使用时批准虚拟网卡服务；之后连接通常不再要求授权。",
    "started.after.title": "连接之后",
    "started.after.body":
      "在概览查看流量与状态；在工作负载 / 网络使用端口转发、Exchange、Mirror 与 Preview；在主机别名配置域名 → IP。mysql.default 等短名通过 Kubernetes 搜索域解析。",

    "product.title": "产品能力",
    "product.desc": "透明集群网络，加上你每天都会用到的工具。",
    "product.core.title": "核心能力",
    "product.core.tun.title": "透明 TUN",
    "product.core.tun.body": "托管 sing-box，仅路由已发现的 Pod 与 Service 流量。",
    "product.core.dns.title": "集群 DNS",
    "product.core.dns.body":
      "分流 DNS 与搜索域——mysql.default 与 *.svc.cluster.local 都可用。",
    "product.core.hosts.title": "主机别名",
    "product.core.hosts.body":
      "按 Context 配置域名 → IPv4；连接期间经本地 DNS 生效。改后需重连；断开时清理。",
    "product.core.clusters.title": "集群管理",
    "product.core.clusters.body": "添加 kubeconfig、列出 Context、探测并切换。",
    "product.tools.title": "开发者工具",
    "product.tools.exchange.title": "Exchange",
    "product.tools.exchange.body": "用本机进程替换现有 ClusterIP Service。",
    "product.tools.mirror.title": "Mirror",
    "product.tools.mirror.body": "集群原 Pod 继续响应客户端，同时将 TCP 请求拷贝一份到本机进程。",
    "product.tools.preview.title": "Preview",
    "product.tools.preview.body": "把本机应用临时暴露为 ClusterIP Service。",
    "product.tools.portfwd.title": "端口转发",
    "product.tools.portfwd.body": "将本地端口转发到 Pod 或 Service——未开 TUN 也能用。",
    "product.gateway.title": "最小化 Gateway",
    "product.gateway.body":
      "无特权集群内 Deployment，仅经 API Server port-forward 访问；支持受限 RBAC 与管理员预装。",

    "workflows.title": "使用场景",
    "workflows.desc": "浏览、排查、映射域名、Exchange / Mirror / Preview，不必为每个 Service 再开终端。",
    "workflows.list.title": "日常路径",
    "workflows.w1.label": "内部 API",
    "workflows.w1.title": "在浏览器打开 Service",
    "workflows.w1.body": "连接后使用 ClusterIP 或 Service DNS 名称。",
    "workflows.w1.hint": "mysql.default.svc",
    "workflows.w2.label": "Pod 排查",
    "workflows.w2.title": "直连真实 Pod IP",
    "workflows.w2.body": "连接后本机已路由 Pod 网段。",
    "workflows.w2.hint": "10.244.x.x",
    "workflows.w3.label": "端口转发",
    "workflows.w3.title": "告别 kubectl port-forward",
    "workflows.w3.body": "在工作负载或网络页选择 Namespace 启动。",
    "workflows.w3.hint": "localhost:8080",
    "workflows.w4.label": "Exchange",
    "workflows.w4.title": "本机进程充当 Service",
    "workflows.w4.body": "Exchange 保留 ClusterIP / DNS，流量落到本机。",
    "workflows.w4.hint": "Exchange",
    "workflows.w5.label": "Mirror",
    "workflows.w5.title": "不替换 Service 也能调试",
    "workflows.w5.body": "集群原 Pod 继续响应客户端，同时将 TCP 请求拷贝到本机进程。",
    "workflows.w5.hint": "Mirror",
    "workflows.w6.label": "主机别名",
    "workflows.w6.title": "把自定义域名指到集群 IP",
    "workflows.w6.body":
      "在「主机别名」将 app.dev 映射到 Service 或 Pod IP，重新连接后分流 DNS 生效。",
    "workflows.w6.hint": "app.dev → 10.96.x.x",

    "arch.title": "架构",
    "arch.desc": "本地应用经 sing-box 进入。只有集群目标会穿过 SOCKS 桥到达 Gateway。",
    "arch.flow.title": "流量路径",
    "arch.n1.tag": "桌面",
    "arch.n1.title": "你的应用",
    "arch.n1.body": "浏览器、IDE、CLI、SDK——无需逐个配置 SOCKS。",
    "arch.n2.tag": "sing-box",
    "arch.n2.title": "TUN / DNS / 规则",
    "arch.n2.body":
      "为 Pod CIDR、Service CIDR、cluster.local 以及可选的主机别名做分流 DNS 与定向路由。",
    "arch.n3.tag": "桥",
    "arch.n3.title": "本机 SOCKS5 桥",
    "arch.n3.body": "TCP / UDP 会话复用后送向集群路径。",
    "arch.n4.tag": "API",
    "arch.n4.title": "Kubernetes API Server",
    "arch.n4.body": "仅 port-forward——无 NodePort、LoadBalancer 或公网 Ingress。",
    "arch.n5.tag": "Gateway",
    "arch.n5.title": "集群内 Gateway",
    "arch.n5.body": "无特权拨号到 Pod、Service 与 CoreDNS。",
    "arch.t1": "Pod IP",
    "arch.t2": "ClusterIP Service",
    "arch.t3": "CoreDNS",
    "arch.more.title": "延伸阅读",
    "arch.more.body": "协议、RBAC 与故障恢复细节见设计文档。",
  },
};

const titles = {
  en: {
    overview: "Overview — KubeLoop Docs",
    "get-started": "Get started — KubeLoop Docs",
    product: "Product — KubeLoop Docs",
    workflows: "Workflows — KubeLoop Docs",
    architecture: "Architecture — KubeLoop Docs",
  },
  "zh-CN": {
    overview: "概览 — KubeLoop 文档",
    "get-started": "快速开始 — KubeLoop 文档",
    product: "产品能力 — KubeLoop 文档",
    workflows: "使用场景 — KubeLoop 文档",
    architecture: "架构 — KubeLoop 文档",
  },
};

const storageKey = "kubeloop.site.language";

function pageId() {
  return document.body.getAttribute("data-page") || "overview";
}

function mountShell() {
  const page = pageId();
  const header = document.getElementById("site-header");
  const sidebar = document.getElementById("site-sidebar");
  if (header) {
    header.innerHTML = `
      <div class="topbar-inner">
        <a class="brand" href="index.html">
          <img src="assets/appicon.svg" width="26" height="26" alt="" />
          <span>KubeLoop</span>
          <span class="badge" data-i18n="nav.docs">Docs</span>
        </a>
        <div class="top-actions">
          <button type="button" class="menu-btn" id="menu-toggle" data-i18n="nav.menu">Menu</button>
          <a href="https://github.com/fengqi-dev/kube-loop/releases" data-i18n="nav.releases">Releases</a>
          <a href="https://github.com/fengqi-dev/kube-loop" data-i18n="nav.github">GitHub</a>
          <div class="lang-switch" role="group" aria-label="Language">
            <button type="button" class="lang-btn" data-lang="en" aria-pressed="true">EN</button>
            <button type="button" class="lang-btn" data-lang="zh-CN" aria-pressed="false">中文</button>
          </div>
        </div>
      </div>`;
  }
  if (sidebar) {
    sidebar.innerHTML = `
      <div class="sidebar-group">
        <h2 data-i18n="sidebar.group.start">Start</h2>
        <a href="index.html" data-nav="overview" data-i18n="nav.overview">Overview</a>
        <a href="get-started.html" data-nav="get-started" data-i18n="nav.getStarted">Get started</a>
      </div>
      <div class="sidebar-group">
        <h2 data-i18n="sidebar.group.guides">Guides</h2>
        <a href="product.html" data-nav="product" data-i18n="nav.product">Product</a>
        <a href="workflows.html" data-nav="workflows" data-i18n="nav.workflows">Workflows</a>
        <a href="architecture.html" data-nav="architecture" data-i18n="nav.architecture">Architecture</a>
      </div>
      <div class="sidebar-group">
        <h2 data-i18n="sidebar.group.reference">Reference</h2>
        <a href="https://github.com/fengqi-dev/kube-loop/blob/main/docs/design.md" data-design-link data-i18n="nav.design">Design notes</a>
        <a href="https://github.com/fengqi-dev/kube-loop/releases" data-i18n="nav.releases">Releases</a>
      </div>`;
    sidebar.querySelectorAll("[data-nav]").forEach((link) => {
      if (link.getAttribute("data-nav") === page) {
        link.setAttribute("aria-current", "page");
      }
    });
  }

  const toggle = document.getElementById("menu-toggle");
  const backdrop = document.getElementById("sidebar-backdrop");
  const close = () => document.body.classList.remove("sidebar-open");
  toggle?.addEventListener("click", () => {
    document.body.classList.toggle("sidebar-open");
  });
  backdrop?.addEventListener("click", close);
  sidebar?.querySelectorAll("a").forEach((link) => {
    link.addEventListener("click", close);
  });
}

function applyLanguage(lang) {
  const table = dictionary[lang] || dictionary.en;
  const page = pageId();
  document.documentElement.lang = lang;
  document.title = (titles[lang] || titles.en)[page] || titles.en.overview;
  document.querySelectorAll("[data-i18n]").forEach((node) => {
    const key = node.getAttribute("data-i18n");
    const value = table[key];
    if (typeof value === "string") node.textContent = value;
  });
  const designHref =
    table["cta.designHref"] ||
    "https://github.com/fengqi-dev/kube-loop/blob/main/docs/design.md";
  document.querySelectorAll("[data-design-link]").forEach((node) => {
    node.setAttribute("href", designHref);
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

mountShell();
initLanguage();
