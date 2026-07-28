import { useEffect, useMemo, useState } from "react";
import { backend } from "./backend";
import type { BootstrapData, SessionState } from "./types";

type View = "overview" | "connections" | "network" | "logs";

const emptySession: SessionState = {
  phase: "idle",
  context: "",
  namespace: "default",
  message: "未连接",
  updatedAt: new Date().toISOString(),
};

const phaseLabels: Record<SessionState["phase"], string> = {
  idle: "未连接",
  checking: "检查权限",
  "installing-gateway": "安装 Gateway",
  "discovering-network": "发现网络",
  "ready-for-tunnel": "网络已发现",
  error: "连接失败",
};

function App() {
  const [data, setData] = useState<BootstrapData>({
    contexts: [],
    namespaces: ["default"],
    session: emptySession,
  });
  const [contextName, setContextName] = useState("");
  const [namespace, setNamespace] = useState("default");
  const [view, setView] = useState<View>("overview");
  const [loading, setLoading] = useState(true);
  const [uiError, setUIError] = useState("");

  useEffect(() => {
    let active = true;
    const unsubscribe = backend.onSession((session) => {
      if (active) setData((current) => ({ ...current, session }));
    });
    backend
      .bootstrap()
      .then((initial) => {
        if (!active) return;
        setData(initial);
        const selected = initial.contexts.find((item) => item.current)?.name ?? initial.contexts[0]?.name ?? "";
        setContextName(selected);
        setNamespace(initial.namespaces.includes("default") ? "default" : (initial.namespaces[0] ?? "default"));
      })
      .catch((error: Error) => setUIError(error.message))
      .finally(() => setLoading(false));
    return () => {
      active = false;
      unsubscribe();
    };
  }, []);

  const busy = data.session.phase === "checking" || data.session.phase === "discovering-network";
  const ready = data.session.phase === "ready-for-tunnel";
  const discovery = data.session.discovery;
  const statusClass = data.session.phase === "error" ? "danger" : ready ? "success" : busy ? "working" : "";
  const contextLabel = useMemo(
    () => data.contexts.find((item) => item.name === contextName)?.cluster ?? "未选择集群",
    [contextName, data.contexts],
  );

  async function changeContext(next: string) {
    setContextName(next);
    setUIError("");
    try {
      const namespaces = await backend.namespaces(next);
      setData((current) => ({ ...current, namespaces }));
      setNamespace(namespaces.includes("default") ? "default" : (namespaces[0] ?? "default"));
    } catch (error) {
      setUIError((error as Error).message);
    }
  }

  async function toggleConnection() {
    setUIError("");
    try {
      if (busy || ready) {
        await backend.disconnect();
      } else {
        await backend.connect(contextName, namespace);
      }
    } catch (error) {
      setUIError((error as Error).message);
    }
  }

  return (
    <div className="app-shell">
      <header>
        <div className="brand"><span className="brand-mark">KC</span><strong>Kube Clash</strong><span className="tag">M1</span></div>
        <span className={`top-status ${statusClass}`}>{phaseLabels[data.session.phase]}</span>
      </header>

      <div className="layout">
        <nav>
          {(["overview", "connections", "network", "logs"] as View[]).map((item) => (
            <button key={item} className={view === item ? "active" : ""} onClick={() => setView(item)}>
              <span className="nav-icon">{item === "overview" ? "◉" : item === "connections" ? "↔" : item === "network" ? "⌘" : "≡"}</span>
              {item === "overview" ? "概览" : item === "connections" ? "连接" : item === "network" ? "网络" : "日志"}
            </button>
          ))}
        </nav>

        <main>
          {view === "overview" && (
            <>
              <div className="selectors">
                <label> Kubernetes Context
                  <select disabled={busy || ready} value={contextName} onChange={(event) => void changeContext(event.target.value)}>
                    {data.contexts.map((item) => <option key={item.name} value={item.name}>{item.name}</option>)}
                  </select>
                </label>
                <label>DNS Namespace
                  <select disabled={busy || ready} value={namespace} onChange={(event) => setNamespace(event.target.value)}>
                    {data.namespaces.map((item) => <option key={item}>{item}</option>)}
                  </select>
                </label>
              </div>
              <section className="hero">
                <div className={`orb ${statusClass}`}>{busy ? "···" : ready ? "✓" : "⏻"}</div>
                <h1>{loading ? "正在读取 kubeconfig" : data.session.message}</h1>
                <p>{contextName ? `${contextName} · ${contextLabel} · ${namespace}` : "没有发现 Kubernetes Context"}</p>
                <button className="primary" disabled={loading || !contextName} onClick={() => void toggleConnection()}>
                  {busy ? "取消" : ready ? "断开" : "连接"}
                </button>
                {(uiError || data.session.error) && <div className="error">{uiError || data.session.error}</div>}
              </section>
              {discovery && (
                <div className="metrics">
                  <article><span>Pod 网络</span><strong>{discovery.podCIDRs[0] ?? "精确路由"}</strong><small>{discovery.pods} 个 Pod</small></article>
                  <article><span>Service</span><strong>{discovery.serviceIPs.length}</strong><small>ClusterIP</small></article>
                  <article><span>集群 DNS</span><strong>{discovery.dnsServer || "未发现"}</strong><small>cluster.local</small></article>
                </div>
              )}
            </>
          )}

          {view === "connections" && <Empty title="活动连接" text="Mihomo TUN 启动后，这里会显示真实 TCP 和 UDP 会话。" />}
          {view === "network" && (
            <section className="page">
              <h1>集群网络</h1>
              <p>从 Kubernetes API 实时发现，不依赖本机 kubectl。</p>
              <table><thead><tr><th>类型</th><th>目标</th><th>状态</th></tr></thead>
                <tbody>
                  {(discovery?.podCIDRs ?? []).map((item) => <tr key={item}><td>Pod CIDR</td><td><code>{item}</code></td><td>已发现</td></tr>)}
                  {discovery?.dnsServer && <tr><td>CoreDNS</td><td><code>{discovery.dnsServer}</code></td><td>已发现</td></tr>}
                  {!discovery && <tr><td colSpan={3}>连接集群后显示网络信息</td></tr>}
                </tbody>
              </table>
            </section>
          )}
          {view === "logs" && <Empty title="运行日志" text={data.session.error || data.session.message} />}
        </main>
      </div>
    </div>
  );
}

function Empty({ title, text }: { title: string; text: string }) {
  return <section className="empty"><h1>{title}</h1><p>{text}</p></section>;
}

export default App;
