(() => {
  "use strict";

  const instances = new Map();
  let registered = false;

  const COLORS = {
    req: "#0d7a4a",
    res: "#2f6fed",
    shadow: "#7a857c",
    userFill: "#f4f5f4",
    userStroke: "#cfd4cf",
    accentFill: "#e7f6ee",
    accentStroke: "rgba(13, 122, 74, 0.45)",
    accentInk: "#0a5c38",
    altFill: "#eef6f1",
    altStroke: "rgba(13, 122, 74, 0.28)",
    altInk: "#174d35",
    muted: "#5b655e",
    fg: "#0f1412",
  };

  const ROLE = {
    user: {
      fill: COLORS.userFill,
      stroke: COLORS.userStroke,
      labelFill: COLORS.fg,
    },
    accent: {
      fill: COLORS.accentFill,
      stroke: COLORS.accentStroke,
      labelFill: COLORS.accentInk,
    },
    alt: {
      fill: COLORS.altFill,
      stroke: COLORS.altStroke,
      labelFill: COLORS.altInk,
    },
  };

  const SIDE_PORTS = [
    { key: "rt", placement: [1, 0.32] },
    { key: "rb", placement: [1, 0.68] },
    { key: "lt", placement: [0, 0.32] },
    { key: "lb", placement: [0, 0.68] },
    { key: "top", placement: "top" },
    { key: "bottom", placement: "bottom" },
    // Offset ports so vertical request / response rails stay apart.
    { key: "topL", placement: [0.28, 0] },
    { key: "topR", placement: [0.72, 0] },
    { key: "bottomL", placement: [0.28, 1] },
    { key: "bottomR", placement: [0.72, 1] },
  ];

  function prefersReducedMotion() {
    return (
      typeof matchMedia === "function" &&
      matchMedia("(prefers-reduced-motion: reduce)").matches
    );
  }

  function ensureRegistered() {
    if (!window.G6) return false;
    if (registered) return true;
    const { Line, Polyline, register, ExtensionCategory } = window.G6;

    class FlowAntLine extends Line {
      onCreate() {
        const shape = this.shapeMap?.key;
        if (!shape || prefersReducedMotion()) return;
        shape.animate([{ lineDashOffset: -20 }, { lineDashOffset: 0 }], {
          duration: 2200,
          iterations: Infinity,
        });
      }
    }

    class FlowAntPolyline extends Polyline {
      onCreate() {
        const shape = this.shapeMap?.key;
        if (!shape || prefersReducedMotion()) return;
        shape.animate([{ lineDashOffset: -20 }, { lineDashOffset: 0 }], {
          duration: 2200,
          iterations: Infinity,
        });
      }
    }

    register(ExtensionCategory.EDGE, "flow-ant-line", FlowAntLine);
    register(ExtensionCategory.EDGE, "flow-ant-polyline", FlowAntPolyline);
    registered = true;
    return true;
  }

  function escapeHtml(value) {
    return String(value ?? "")
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }

  function makeNode(id, x, y, role, title, sub, size) {
    const theme = ROLE[role] || ROLE.alt;
    const [w, h] = size || [124, 64];
    const subHtml = sub
      ? `<span style="display:block;margin-top:2px;font-size:11px;font-weight:500;line-height:1.25;opacity:0.88">${escapeHtml(sub)}</span>`
      : "";
    return {
      id,
      type: "html",
      data: { role },
      style: {
        x,
        y,
        size: [w, h],
        dx: -w / 2,
        dy: -h / 2,
        ports: SIDE_PORTS,
        port: false,
        innerHTML: `<div style="box-sizing:border-box;width:100%;height:100%;display:flex;align-items:center;justify-content:center;padding:6px 8px;border-radius:12px;background:${theme.fill};border:1.5px solid ${theme.stroke};color:${theme.labelFill};font-weight:650;font-size:12px;font-family:'Geist Variable','Segoe UI',sans-serif;line-height:1.25;text-align:center"><div><span style="display:block">${escapeHtml(title)}</span>${subHtml}</div></div>`,
      },
    };
  }

  function zone(id, x, y, text, tone) {
    const fill =
      tone === "accent"
        ? COLORS.accentInk
        : tone === "k8s"
          ? COLORS.altInk
          : COLORS.muted;
    // HTML nodes paint above the canvas, so zone labels must stay in a clear top band.
    return {
      id,
      type: "rect",
      style: {
        x,
        y,
        size: [4, 4],
        fillOpacity: 0,
        strokeOpacity: 0,
        labelText: String(text || "").toUpperCase(),
        labelFill: fill,
        labelFontSize: 11,
        labelFontWeight: 600,
        labelFontFamily: "SF Mono, ui-monospace, monospace",
        labelLetterSpacing: 0.6,
        labelPlacement: "center",
        labelTextAlign: "center",
        labelTextBaseline: "middle",
        ports: [],
        port: false,
        zIndex: 20,
      },
    };
  }

  function note(id, x, y, text, tone) {
    return {
      id,
      type: "rect",
      style: {
        x,
        y,
        size: [4, 4],
        fillOpacity: 0,
        strokeOpacity: 0,
        labelText: text,
        labelFill: tone === "shadow" ? COLORS.shadow : COLORS.accentInk,
        labelFontSize: 11,
        labelFontWeight: 600,
        labelFontFamily: "Geist Variable, Segoe UI, sans-serif",
        labelPlacement: "center",
        labelTextAlign: "center",
        labelTextBaseline: "middle",
        ports: [],
        port: false,
        zIndex: 20,
      },
    };
  }

  function edge(id, source, target, dir, ports) {
    const isReq = dir === "in";
    const isShadow = dir === "shadow";
    const stroke = isShadow ? COLORS.shadow : isReq ? COLORS.req : COLORS.res;
    const style = {
      stroke,
      lineWidth: isShadow ? 1.75 : 2,
      lineDash: prefersReducedMotion() ? undefined : [7, 7],
      endArrow: true,
      endArrowSize: 7,
      endArrowFill: stroke,
      opacity: isShadow ? 0.9 : 1,
      zIndex: 1,
    };
    if (ports?.sourcePort) style.sourcePort = ports.sourcePort;
    if (ports?.targetPort) style.targetPort = ports.targetPort;
    if (ports?.router) style.router = ports.router;
    if (ports?.controlPoints) style.controlPoints = ports.controlPoints;

    return {
      id,
      source,
      target,
      type: ports?.polyline ? "flow-ant-polyline" : "flow-ant-line",
      data: { dir },
      style,
    };
  }

  function pair(prefix, a, b) {
    return [
      edge(`${prefix}-in`, a, b, "in", {
        sourcePort: "rt",
        targetPort: "lt",
      }),
      edge(`${prefix}-out`, b, a, "out", {
        sourcePort: "lb",
        targetPort: "rb",
      }),
    ];
  }

  // Peer sits below the hub; request flows upward on the left, response down on the right.
  function pairVerticalUp(prefix, hubId, peerId) {
    return [
      edge(`${prefix}-in`, peerId, hubId, "in", {
        sourcePort: "topL",
        targetPort: "bottomL",
      }),
      edge(`${prefix}-out`, hubId, peerId, "out", {
        sourcePort: "bottomR",
        targetPort: "topR",
      }),
    ];
  }

  function buildPortfwd(t) {
    return {
      height: 190,
      nodes: [
        zone("z-user", 90, 22, t("arch.features.actor.user"), "user"),
        zone("z-kl", 270, 22, "KubeLoop", "accent"),
        zone("z-k8s", 530, 22, t("arch.features.actor.k8s"), "k8s"),
        makeNode(
          "app",
          90,
          105,
          "user",
          t("arch.features.node.app"),
          "localhost",
        ),
        makeNode(
          "kl",
          270,
          105,
          "accent",
          "KubeLoop",
          t("arch.features.node.listen"),
          [140, 64],
        ),
        makeNode("gw", 450, 105, "alt", "Gateway", null, [120, 64]),
        makeNode(
          "pod",
          625,
          105,
          "alt",
          t("arch.features.node.podService"),
          null,
          [130, 64],
        ),
      ],
      edges: [
        ...pair("p1", "app", "kl"),
        ...pair("p2", "kl", "gw"),
        ...pair("p3", "gw", "pod"),
      ],
    };
  }

  function buildExchangeLike(t, midLabel) {
    // Top band y<40 reserved for zone labels.
    // USER         KUBERNETES                         KUBELOOP
    // User ──────→ Service → Gateway ───────────────→ KubeLoop → App
    //              Other Service
    const Y = 110;
    const Y_PEER = 220;
    const node = [118, 56];
    return {
      height: 280,
      nodes: [
        zone("z-user", 80, 16, t("arch.features.actor.user"), "user"),
        zone("z-k8s", 320, 16, t("arch.features.actor.k8s"), "k8s"),
        zone("z-kl", 560, 16, "KubeLoop", "accent"),
        makeNode(
          "callerUser",
          80,
          Y,
          "user",
          t("arch.features.node.callerUser"),
          t("arch.features.node.callerUserSub"),
          node,
        ),
        makeNode("svc", 250, Y, "alt", midLabel, null, node),
        makeNode("gw", 400, Y, "alt", "Gateway", null, node),
        makeNode(
          "kl",
          560,
          Y,
          "accent",
          "KubeLoop",
          "Accept → local",
          [128, 56],
        ),
        makeNode(
          "app",
          730,
          Y,
          "user",
          t("arch.features.node.app"),
          t("arch.features.node.localProcess"),
          node,
        ),
        makeNode(
          "callerSvc",
          250,
          Y_PEER,
          "alt",
          t("arch.features.node.callerSvc"),
          t("arch.features.node.callerSvcSub"),
          node,
        ),
      ],
      edges: [
        ...pair("e-user", "callerUser", "svc"),
        ...pairVerticalUp("e-peer", "svc", "callerSvc"),
        ...pair("e-gw", "svc", "gw"),
        ...pair("e-kl", "gw", "kl"),
        ...pair("e-app", "kl", "app"),
      ],
    };
  }

  function buildMirror(t) {
    // Top band y<40 reserved for zone / note labels.
    // USER         KUBERNETES              KUBELOOP
    // User ──────→ Gateway ──────────────→ KubeLoop → Pod
    //              Other Service         └→ App (shadow)
    const Y = 110;
    const Y_PEER = 220;
    const Y_APP = 220;
    const node = [118, 56];
    return {
      height: 290,
      nodes: [
        zone("z-user", 80, 16, t("arch.features.actor.user"), "user"),
        zone("z-k8s", 280, 16, t("arch.features.actor.k8s"), "k8s"),
        zone("z-kl", 500, 16, "KubeLoop", "accent"),
        note(
          "n-primary",
          720,
          16,
          t("arch.features.mirror.primary"),
          "primary",
        ),
        note(
          "n-shadow",
          560,
          170,
          t("arch.features.mirror.shadowOnly"),
          "shadow",
        ),
        makeNode(
          "callerUser",
          80,
          Y,
          "user",
          t("arch.features.node.callerUser"),
          t("arch.features.node.callerUserSub"),
          node,
        ),
        makeNode("gw", 250, Y, "alt", "Gateway", null, node),
        makeNode(
          "kl",
          430,
          Y,
          "accent",
          "KubeLoop",
          t("arch.features.node.tee"),
          [128, 56],
        ),
        makeNode(
          "pod",
          620,
          Y,
          "alt",
          t("arch.features.node.originalPod"),
          null,
          [140, 56],
        ),
        makeNode(
          "callerSvc",
          250,
          Y_PEER,
          "alt",
          t("arch.features.node.callerSvc"),
          t("arch.features.node.callerSvcSub"),
          node,
        ),
        makeNode(
          "app",
          560,
          Y_APP,
          "user",
          t("arch.features.node.app"),
          t("arch.features.mirror.shadow"),
          [140, 52],
        ),
      ],
      edges: [
        ...pair("m-user", "callerUser", "gw"),
        ...pairVerticalUp("m-peer", "gw", "callerSvc"),
        ...pair("m-kl", "gw", "kl"),
        ...pair("m-pod", "kl", "pod"),
        edge("m-shadow", "kl", "app", "shadow", {
          sourcePort: "bottom",
          targetPort: "top",
        }),
      ],
    };
  }

  function buildOverview(t) {
    const targetSize = [124, 48];
    return {
      height: 290,
      nodes: [
        zone("z-user", 80, 22, t("arch.features.actor.user"), "user"),
        zone("z-kl", 290, 22, "KubeLoop", "accent"),
        zone("z-k8s", 600, 22, t("arch.features.actor.k8s"), "k8s"),
        makeNode(
          "apps",
          80,
          100,
          "user",
          t("arch.n1.graph"),
          t("arch.n1.tag"),
          [112, 64],
        ),
        makeNode(
          "singbox",
          230,
          100,
          "accent",
          t("arch.n2.graph"),
          t("arch.n2.tag"),
          [128, 64],
        ),
        makeNode(
          "socks",
          380,
          100,
          "accent",
          t("arch.n3.graph"),
          t("arch.n3.tag"),
          [128, 64],
        ),
        makeNode(
          "api",
          540,
          100,
          "alt",
          t("arch.n4.graph"),
          t("arch.n4.tag"),
          [136, 64],
        ),
        makeNode(
          "gw",
          690,
          100,
          "alt",
          t("arch.n5.graph"),
          t("arch.n5.tag"),
          [128, 64],
        ),
        note("n-targets", 670, 168, t("arch.flow.targets"), "primary"),
        makeNode("pod", 540, 230, "alt", t("arch.t1"), null, targetSize),
        makeNode("svc", 670, 230, "alt", t("arch.t2"), null, targetSize),
        makeNode("dns", 800, 230, "alt", t("arch.t3"), null, targetSize),
      ],
      edges: [
        ...pair("o1", "apps", "singbox"),
        ...pair("o2", "singbox", "socks"),
        ...pair("o3", "socks", "api"),
        ...pair("o4", "api", "gw"),
        edge("o-pod", "gw", "pod", "in", {
          polyline: true,
          sourcePort: "bottom",
          targetPort: "top",
          router: { type: "orth" },
        }),
        edge("o-svc", "gw", "svc", "in", {
          polyline: true,
          sourcePort: "bottom",
          targetPort: "top",
          router: { type: "orth" },
        }),
        edge("o-dns", "gw", "dns", "in", {
          polyline: true,
          sourcePort: "rb",
          targetPort: "lt",
          router: { type: "orth" },
        }),
      ],
    };
  }

  function buildSpec(kind, t) {
    switch (kind) {
      case "overview":
        return buildOverview(t);
      case "portfwd":
        return buildPortfwd(t);
      case "exchange":
        return buildExchangeLike(t, "Service");
      case "preview":
        return buildExchangeLike(t, t("arch.features.node.previewSvc"));
      case "mirror":
        return buildMirror(t);
      default:
        return null;
    }
  }

  function destroyOne(key) {
    const entry = instances.get(key);
    if (!entry) return;
    try {
      entry.graph.destroy();
    } catch (_) {
      /* ignore */
    }
    instances.delete(key);
  }

  function destroyAll() {
    for (const key of [...instances.keys()]) destroyOne(key);
  }

  function containerWidth(el) {
    const w =
      el.clientWidth ||
      el.parentElement?.clientWidth ||
      el.closest(".flow-stage")?.clientWidth ||
      0;
    return Math.max(w, 320);
  }

  async function mountOne(el, index, t) {
    if (!window.G6 || !ensureRegistered()) return;
    const kind = el.getAttribute("data-flow-diagram");
    const key = el.getAttribute("data-flow-id") || `${kind}-${index}`;
    const spec = buildSpec(kind, t);
    if (!spec) return;

    destroyOne(key);
    el.innerHTML = "";
    el.classList.add("flow-g6");
    el.style.height = `${spec.height}px`;

    const width = containerWidth(el);
    const { Graph } = window.G6;
    const graph = new Graph({
      container: el,
      width,
      height: spec.height,
      autoFit: "view",
      padding: [8, 12, 8, 12],
      animation: false,
      data: {
        nodes: spec.nodes,
        edges: spec.edges,
      },
      node: {
        // Do not set a default type here — it overrides per-node `html` cards.
        style: {
          port: false,
          labelPlacement: "center",
          labelTextAlign: "center",
          labelTextBaseline: "middle",
        },
      },
      edge: {
        type: "flow-ant-line",
        animation: {
          enter: false,
          exit: false,
          update: false,
        },
        style: {
          sourcePort: (d) => d.style?.sourcePort,
          targetPort: (d) => d.style?.targetPort,
        },
      },
      behaviors: [],
      plugins: [],
    });

    instances.set(key, { graph, el, kind, height: spec.height });
    await graph.render();
  }

  async function mount(t) {
    if (!window.G6) {
      console.warn("[KubeLoop] @antv/g6 failed to load; flow diagrams skipped.");
      return;
    }
    ensureRegistered();
    destroyAll();
    const hosts = [...document.querySelectorAll("[data-flow-diagram]")];
    await Promise.all(hosts.map((el, index) => mountOne(el, index, t)));
  }

  function remount(t) {
    return mount(t);
  }

  function resizeVisible() {
    for (const entry of instances.values()) {
      const { graph, el, height } = entry;
      if (!el.offsetParent && el.getClientRects().length === 0) continue;
      const width = containerWidth(el);
      try {
        graph.setSize(width, height);
        graph.fitView();
      } catch (_) {
        /* ignore */
      }
    }
  }

  window.KubeLoopFlows = { mount, remount, resizeVisible, destroyAll };
})();
