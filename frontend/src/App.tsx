import { useEffect, useMemo, useState } from "react";
import {
  Activity,
  Boxes,
  Check,
  CheckCircle2,
  ChevronDown,
  CircleAlert,
  CircleDot,
  Copy,
  Download,
  ExternalLink,
  Gauge,
  Globe2,
  LoaderCircle,
  Maximize2,
  Minus,
  Network,
  Orbit,
  Power,
  RefreshCw,
  Route,
  ScrollText,
  Server,
  Settings2,
  ShieldCheck,
  Waypoints,
  Wifi,
  WifiOff,
  X,
  type LucideIcon,
} from "lucide-react";
import { backend } from "./backend";
import { useI18n, type Language, type TranslationKey } from "./i18n";
import type { BootstrapData, Metrics, SessionState, UpdateInfo } from "./types";
import { Quit, WindowMinimise, WindowToggleMaximise } from "../wailsjs/runtime/runtime";

type View = "overview" | "connections" | "network" | "logs" | "settings";

const emptySession: SessionState = {
  phase: "idle",
  context: "",
  namespace: "default",
  message: "Disconnected",
  updatedAt: new Date().toISOString(),
};

const emptyUpdate: UpdateInfo = {
  currentVersion: "dev",
  available: false,
  url: "https://github.com/fengqi-dev/kube-clash/releases",
};

const navigation: Array<{ id: Exclude<View, "settings">; icon: LucideIcon }> = [
  { id: "overview", icon: Gauge },
  { id: "connections", icon: Waypoints },
  { id: "network", icon: Network },
  { id: "logs", icon: ScrollText },
];

const navKeys: Record<View, TranslationKey> = {
  overview: "nav.overview",
  connections: "nav.connections",
  network: "nav.network",
  logs: "nav.logs",
  settings: "nav.settings",
};

const headerKeys: Record<View, TranslationKey> = {
  overview: "header.overview",
  connections: "header.connections",
  network: "header.network",
  logs: "header.logs",
  settings: "header.settings",
};

const phaseKeys: Record<SessionState["phase"], TranslationKey> = {
  idle: "phase.idle",
  checking: "phase.checking",
  "installing-gateway": "phase.installing-gateway",
  "discovering-network": "phase.discovering-network",
  "starting-tunnel": "phase.starting-tunnel",
  connected: "phase.connected",
  error: "phase.error",
};

function App() {
  const { t } = useI18n();
  const [data, setData] = useState<BootstrapData>({
    contexts: [],
    namespaces: ["default"],
    session: emptySession,
    update: emptyUpdate,
  });
  const [contextName, setContextName] = useState("");
  const [namespace, setNamespace] = useState("default");
  const [view, setView] = useState<View>("overview");
  const [loading, setLoading] = useState(true);
  const [uiError, setUIError] = useState("");
  const [updateBusy, setUpdateBusy] = useState(false);

  useEffect(() => {
    let active = true;
    const unsubscribe = backend.onSession((session) => {
      if (active) setData((current) => ({ ...current, session }));
    });
    const unsubscribeUpdate = backend.onUpdate((update) => {
      if (active) setData((current) => ({ ...current, update }));
    });
    backend
      .bootstrap()
      .then((initial) => {
        if (!active) return;
        setData(initial);
        const selected =
          initial.contexts.find((item) => item.current)?.name ??
          initial.contexts[0]?.name ??
          "";
        setContextName(selected);
        setNamespace(
          initial.namespaces.includes("default")
            ? "default"
            : (initial.namespaces[0] ?? "default"),
        );
      })
      .catch((error: Error) => setUIError(error.message))
      .finally(() => setLoading(false));
    return () => {
      active = false;
      unsubscribe();
      unsubscribeUpdate();
    };
  }, []);

  const session = data.session;
  const busy =
    session.phase === "checking" ||
    session.phase === "installing-gateway" ||
    session.phase === "discovering-network" ||
    session.phase === "starting-tunnel";
  const ready = session.phase === "connected";
  const discovery = session.discovery;
  const currentContext = useMemo(
    () => data.contexts.find((item) => item.name === contextName),
    [contextName, data.contexts],
  );

  async function changeContext(next: string) {
    setContextName(next);
    setUIError("");
    try {
      const namespaces = await backend.namespaces(next);
      setData((current) => ({ ...current, namespaces }));
      setNamespace(
        namespaces.includes("default")
          ? "default"
          : (namespaces[0] ?? "default"),
      );
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

  async function checkForUpdates() {
    setUpdateBusy(true);
    try {
      const update = await backend.checkForUpdates();
      setData((current) => ({ ...current, update }));
    } catch (error) {
      setUIError((error as Error).message);
    } finally {
      setUpdateBusy(false);
    }
  }

  async function openUpdatePage() {
    try {
      await backend.openUpdatePage();
    } catch (error) {
      setUIError((error as Error).message);
    }
  }

  return (
    <div className="relative flex h-screen min-h-[580px] overflow-hidden bg-[var(--app-bg)] text-[var(--text-primary)]">
      <div className="pointer-events-none absolute inset-0 bg-[radial-gradient(circle_at_76%_-12%,rgba(20,184,166,0.13),transparent_34%),radial-gradient(circle_at_18%_100%,rgba(59,130,246,0.08),transparent_30%)]" />

      <aside className="relative z-10 flex w-[218px] shrink-0 flex-col border-r border-[color:var(--border)] bg-[var(--sidebar-bg)] px-3 py-4 backdrop-blur-2xl">
        <div
          className="window-drag flex h-10 items-center gap-3 px-2"
        >
          <div className="grid size-9 place-items-center rounded-xl border border-brand-300/20 bg-brand-400/10 text-brand-300 shadow-[0_0_24px_rgba(45,212,191,0.12)]">
            <Orbit size={20} strokeWidth={1.8} />
          </div>
          <div>
            <div className="text-[14px] font-semibold tracking-tight text-[var(--text-strong)]">Kube Clash</div>
            <div className="mt-0.5 text-[10px] font-medium tracking-[0.12em] text-[var(--text-muted)] uppercase">
              Network client
            </div>
          </div>
        </div>

        <div className="mt-8 px-2 text-[10px] font-semibold tracking-[0.14em] text-[var(--text-subtle)] uppercase">
          Workspace
        </div>
        <nav className="mt-2 space-y-1">
          {navigation.map(({ id, icon: Icon }) => (
            <button
              key={id}
              type="button"
              onClick={() => setView(id)}
              className={`group flex w-full items-center gap-3 rounded-xl px-3 py-2.5 text-left text-[13px] font-medium transition ${
                view === id
                  ? "bg-[var(--surface-hover)] text-[var(--text-strong)] shadow-sm ring-1 ring-[color:var(--border)]"
                  : "text-[var(--text-muted)] hover:bg-[var(--surface-2)] hover:text-[var(--text-primary)]"
              }`}
            >
              <Icon
                size={17}
                strokeWidth={1.8}
                className={view === id ? "text-brand-300" : "text-[var(--text-subtle)] transition group-hover:text-[var(--text-secondary)]"}
              />
              {t(navKeys[id])}
              {id === "connections" && ready && (
                <span className="ml-auto size-1.5 rounded-full bg-emerald-400 shadow-[0_0_8px_rgba(52,211,153,0.75)]" />
              )}
            </button>
          ))}
        </nav>

        <div className="mt-auto">
          <div className="mb-3 rounded-xl border border-[color:var(--border)] bg-[var(--surface-1)] p-3">
            <div className="flex items-center gap-2 text-[11px] font-medium text-[var(--text-secondary)]">
              <ShieldCheck size={14} className="text-brand-300" />
              Mihomo Core
            </div>
            <div className="mt-2 flex items-center justify-between">
              <span className="text-[11px] text-[var(--text-subtle)]">{session.coreVersion ?? "v1.19.28"}</span>
              <span className={`rounded-full px-2 py-0.5 text-[9px] font-semibold ${
                ready ? "bg-emerald-400/10 text-emerald-300" : "bg-[var(--surface-2)] text-[var(--text-muted)]"
              }`}>
                {ready ? t("core.running") : t("core.onDemand")}
              </span>
            </div>
          </div>
          <button
            type="button"
            onClick={() => setView("settings")}
            className={`flex w-full items-center gap-3 rounded-xl px-3 py-2.5 text-[13px] font-medium transition ${
              view === "settings"
                ? "bg-[var(--surface-hover)] text-[var(--text-strong)] ring-1 ring-[color:var(--border)]"
                : "text-[var(--text-muted)] hover:bg-[var(--surface-2)] hover:text-[var(--text-primary)]"
            }`}
          >
            <Settings2 size={17} strokeWidth={1.8} className={view === "settings" ? "text-brand-300" : ""} />
            {t("nav.settings")}
            {data.update.available && <span className="ml-auto size-1.5 rounded-full bg-brand-300 shadow-[0_0_8px_rgba(45,212,191,0.8)]" />}
          </button>
        </div>
      </aside>

      <div className="relative z-10 flex min-w-0 flex-1 flex-col">
        <header
          className="window-drag flex h-[66px] shrink-0 items-center justify-between border-b border-[color:var(--border)] px-7"
          onDoubleClick={() => WindowToggleMaximise()}
        >
          <div>
            <h1 className="text-[15px] font-semibold tracking-tight text-[var(--text-strong)]">
              {t(navKeys[view])}
            </h1>
            <p className="mt-0.5 text-[11px] text-[var(--text-subtle)]">
              {t(headerKeys[view])}
            </p>
          </div>
          <div className="flex items-center gap-2">
            <StatusBadge phase={session.phase} />
            <button
              type="button"
              aria-label={t("nav.settings")}
              onClick={() => setView("settings")}
              className="grid size-9 place-items-center rounded-xl border border-[color:var(--border)] bg-[var(--surface-1)] text-[var(--text-muted)] transition hover:bg-[var(--surface-hover)] hover:text-[var(--text-primary)]"
            >
              <Settings2 size={16} />
            </button>
            <WindowControls />
          </div>
        </header>

        <main className="min-h-0 flex-1 overflow-y-auto px-7 py-6">
          {view === "overview" && (
            <Overview
              contexts={data.contexts}
              namespaces={data.namespaces}
              contextName={contextName}
              namespace={namespace}
              clusterName={currentContext?.cluster ?? ""}
              session={session}
              discovery={discovery}
              loading={loading}
              error={uiError || session.error || ""}
              busy={busy}
              ready={ready}
              onContextChange={(value) => void changeContext(value)}
              onNamespaceChange={setNamespace}
              onToggle={() => void toggleConnection()}
            />
          )}
          {view === "connections" && <Connections ready={ready} metrics={session.metrics} />}
          {view === "network" && <NetworkView discovery={discovery} ready={ready} />}
          {view === "logs" && <Logs session={session} error={uiError || session.error || ""} />}
          {view === "settings" && (
            <Settings
              update={data.update}
              checking={updateBusy}
              onCheck={() => void checkForUpdates()}
              onOpen={() => void openUpdatePage()}
            />
          )}
        </main>
      </div>
    </div>
  );
}

function WindowControls() {
  const { t } = useI18n();
  return (
    <div
      className="window-no-drag ml-1 flex items-center overflow-hidden rounded-xl border border-[color:var(--border)] bg-[var(--surface-1)]"
    >
      <button
        type="button"
        aria-label={t("window.minimise")}
        title={t("window.minimise")}
        onClick={() => WindowMinimise()}
        className="grid size-9 place-items-center text-[var(--text-muted)] transition hover:bg-[var(--surface-hover)] hover:text-[var(--text-strong)]"
      >
        <Minus size={15} strokeWidth={1.8} />
      </button>
      <button
        type="button"
        aria-label={t("window.maximise")}
        title={t("window.maximise")}
        onClick={() => WindowToggleMaximise()}
        className="grid size-9 place-items-center border-x border-[color:var(--border)] text-[var(--text-muted)] transition hover:bg-[var(--surface-hover)] hover:text-[var(--text-strong)]"
      >
        <Maximize2 size={13} strokeWidth={1.8} />
      </button>
      <button
        type="button"
        aria-label={t("window.close")}
        title={t("window.close")}
        onClick={() => Quit()}
        className="grid size-9 place-items-center text-[var(--text-muted)] transition hover:bg-rose-500 hover:text-white"
      >
        <X size={15} strokeWidth={1.8} />
      </button>
    </div>
  );
}

interface OverviewProps {
  contexts: BootstrapData["contexts"];
  namespaces: string[];
  contextName: string;
  namespace: string;
  clusterName: string;
  session: SessionState;
  discovery?: SessionState["discovery"];
  loading: boolean;
  error: string;
  busy: boolean;
  ready: boolean;
  onContextChange(value: string): void;
  onNamespaceChange(value: string): void;
  onToggle(): void;
}

function Overview(props: OverviewProps) {
  const { t } = useI18n();
  const {
    contexts,
    namespaces,
    contextName,
    namespace,
    clusterName,
    session,
    discovery,
    loading,
    error,
    busy,
    ready,
    onContextChange,
    onNamespaceChange,
    onToggle,
  } = props;
  const disabled = busy || ready;

  return (
    <div className="mx-auto max-w-[1040px]">
      <section className="flex flex-wrap items-end justify-between gap-4">
        <div className="flex min-w-0 flex-1 flex-wrap gap-3">
          <SelectField
            label="Kubernetes Context"
            value={contextName}
            disabled={disabled}
            onChange={onContextChange}
            options={contexts.map((item) => ({ value: item.name, label: item.name }))}
            className="min-w-[260px] flex-[1.2]"
          />
          <SelectField
            label="DNS Namespace"
            value={namespace}
            disabled={disabled}
            onChange={onNamespaceChange}
            options={namespaces.map((item) => ({ value: item, label: item }))}
            className="min-w-[200px] flex-1"
          />
        </div>
        <div className="mb-0.5 hidden items-center gap-2 text-[11px] text-[var(--text-subtle)] xl:flex">
          <ShieldCheck size={14} className="text-emerald-400/80" />
          {t("overview.localCredentials")}
        </div>
      </section>

      <section className="relative mt-5 overflow-hidden rounded-[22px] border border-[color:var(--border)] bg-[var(--surface-1)] shadow-[0_22px_80px_rgba(0,0,0,0.22)] backdrop-blur-xl">
        <div className="pointer-events-none absolute inset-x-0 top-0 h-40 bg-[radial-gradient(ellipse_at_top,rgba(45,212,191,0.1),transparent_68%)]" />
        <div className="relative flex flex-col items-center px-8 pb-9 pt-10 text-center">
          <ConnectionOrb phase={session.phase} />
          <div className="mt-5 flex items-center gap-2">
            <h2 className="text-xl font-semibold tracking-[-0.025em] text-[var(--text-strong)]">
              {loading ? t("overview.loadingKubeconfig") : t(phaseKeys[session.phase])}
            </h2>
          </div>
          <div className="mt-2 flex flex-wrap items-center justify-center gap-2 text-[12px] text-[var(--text-muted)]">
            <span>{contextName || t("overview.noContext")}</span>
            <span className="size-1 rounded-full bg-[var(--text-subtle)]" />
            <span>{clusterName || t("overview.noCluster")}</span>
            <span className="size-1 rounded-full bg-[var(--text-subtle)]" />
            <span>{namespace}</span>
          </div>

          <button
            type="button"
            disabled={loading || !contextName}
            onClick={onToggle}
            className={`mt-6 inline-flex min-w-36 items-center justify-center gap-2 rounded-xl px-5 py-2.5 text-[13px] font-semibold shadow-lg transition disabled:cursor-not-allowed disabled:opacity-40 ${
              busy || ready
                ? "border border-[color:var(--border)] bg-[var(--surface-2)] text-[var(--text-primary)] hover:bg-[var(--surface-hover)]"
                : "bg-brand-300 text-brand-950 shadow-brand-500/10 hover:bg-brand-100"
            }`}
          >
            {busy ? (
              <LoaderCircle size={16} className="animate-spin" />
            ) : ready ? (
              <WifiOff size={16} />
            ) : (
              <Power size={16} />
            )}
            {busy ? t("overview.cancel") : ready ? t("overview.disconnect") : t("overview.connect")}
          </button>

          {error && (
            <div className="mt-5 flex w-full max-w-2xl items-start gap-3 rounded-xl border border-rose-400/15 bg-rose-400/[0.07] px-4 py-3 text-left text-[12px] text-rose-200">
              <CircleAlert size={16} className="mt-0.5 shrink-0 text-rose-300" />
              <span className="min-w-0 break-words">{error}</span>
            </div>
          )}

          {(busy || ready) && <ConnectionSteps phase={session.phase} />}
        </div>

        <div className="grid grid-cols-3 border-t border-[color:var(--border)] bg-[var(--surface-inset)]">
          <Metric
            icon={Route}
            label={t("overview.podNetwork")}
            value={discovery?.podCIDRs[0] ?? "—"}
            detail={discovery ? t("overview.pods", { count: discovery.pods }) : t("overview.waitingDiscovery")}
          />
          <Metric
            icon={Boxes}
            label="ClusterIP Service"
            value={discovery ? String(discovery.serviceIPs.length) : "—"}
            detail={discovery ? t("overview.routesSynced") : t("overview.waitingDiscovery")}
          />
          <Metric
            icon={Globe2}
            label={t("overview.clusterDns")}
            value={discovery?.dnsServer || "—"}
            detail="cluster.local"
            last
          />
        </div>
      </section>

      <div className="mt-4 grid grid-cols-2 gap-4">
        <SmallPanel
          icon={Server}
          title="Gateway"
          value={ready ? t("core.running") : t("core.managed")}
          detail={t("overview.gatewayDetail")}
          tone={ready ? "success" : "neutral"}
        />
        <SmallPanel
          icon={Orbit}
          title="Mihomo TUN"
          value={ready ? t("core.running") : t("core.onDemand")}
          detail={t("overview.tunDetail")}
          tone={ready ? "warning" : "neutral"}
        />
      </div>
    </div>
  );
}

function StatusBadge({ phase }: { phase: SessionState["phase"] }) {
  const { t } = useI18n();
  const ready = phase === "connected";
  const working =
    phase === "checking" ||
    phase === "installing-gateway" ||
    phase === "discovering-network" ||
    phase === "starting-tunnel";
  const error = phase === "error";
  return (
    <div
      className={`flex h-8 items-center gap-2 rounded-full border px-3 text-[11px] font-medium ${
        ready
          ? "border-emerald-400/15 bg-emerald-400/[0.07] text-emerald-300"
          : error
            ? "border-rose-400/15 bg-rose-400/[0.07] text-rose-300"
            : working
              ? "border-amber-400/15 bg-amber-400/[0.07] text-amber-300"
              : "border-[color:var(--border)] bg-[var(--surface-1)] text-[var(--text-muted)]"
      }`}
    >
      <span
        className={`size-1.5 rounded-full ${
          ready
            ? "bg-emerald-400 shadow-[0_0_8px_rgba(52,211,153,0.8)]"
            : error
              ? "bg-rose-400"
              : working
                ? "animate-pulse bg-amber-300"
                : "bg-[var(--text-subtle)]"
        }`}
      />
      {t(phaseKeys[phase])}
    </div>
  );
}

function ConnectionOrb({ phase }: { phase: SessionState["phase"] }) {
  const working =
    phase === "checking" ||
    phase === "installing-gateway" ||
    phase === "discovering-network" ||
    phase === "starting-tunnel";
  const ready = phase === "connected";
  const failed = phase === "error";
  return (
    <div className="relative">
      {(working || ready) && (
        <div
          className={`absolute -inset-3 rounded-full blur-xl ${
            ready ? "bg-emerald-400/15" : "bg-amber-300/10"
          }`}
        />
      )}
      <div
        className={`relative grid size-[76px] place-items-center rounded-2xl border shadow-2xl ${
          ready
            ? "border-emerald-300/20 bg-emerald-400/10 text-emerald-300"
            : failed
              ? "border-rose-300/20 bg-rose-400/10 text-rose-300"
              : working
                ? "border-amber-300/20 bg-amber-300/10 text-amber-200"
                : "border-[color:var(--border)] bg-[var(--surface-2)] text-[var(--text-muted)]"
        }`}
      >
        {working ? (
          <LoaderCircle size={30} strokeWidth={1.6} className="animate-spin" />
        ) : ready ? (
          <Wifi size={30} strokeWidth={1.6} />
        ) : failed ? (
          <CircleAlert size={30} strokeWidth={1.6} />
        ) : (
          <Power size={30} strokeWidth={1.5} />
        )}
      </div>
    </div>
  );
}

function ConnectionSteps({ phase }: { phase: SessionState["phase"] }) {
  const { t } = useI18n();
  const steps = [
    { phases: ["checking"], label: t("step.access"), detail: t("step.accessDetail") },
    { phases: ["installing-gateway"], label: t("step.gateway"), detail: t("step.gatewayDetail") },
    { phases: ["discovering-network"], label: t("step.discovery"), detail: t("step.discoveryDetail") },
    { phases: ["starting-tunnel", "connected"], label: t("step.tun"), detail: t("step.tunDetail") },
  ];
  const activeIndex = Math.max(
    0,
    steps.findIndex((step) => step.phases.includes(phase)),
  );
  return (
    <div className="mt-8 grid w-full max-w-[760px] grid-cols-4">
      {steps.map((step, index) => {
        const done = index < activeIndex || phase === "connected";
        const active = index === activeIndex && !done;
        return (
          <div key={step.label} className="relative px-2">
            {index > 0 && (
              <div
                className={`absolute right-1/2 top-3 h-px w-full ${
                  done ? "bg-brand-300/45" : "bg-[var(--surface-hover)]"
                }`}
              />
            )}
            <div className="relative mx-auto grid size-6 place-items-center rounded-full bg-[var(--surface-solid)]">
              <div
                className={`grid size-5 place-items-center rounded-full border ${
                  done
                    ? "border-brand-300/40 bg-brand-300/15 text-brand-300"
                    : active
                      ? "border-amber-300/40 bg-amber-300/10 text-amber-200"
                      : "border-[color:var(--border)] text-[var(--text-subtle)]"
                }`}
              >
                {done ? (
                  <Check size={11} strokeWidth={2.5} />
                ) : active ? (
                  <CircleDot size={11} />
                ) : (
                  <span className="size-1 rounded-full bg-current" />
                )}
              </div>
            </div>
            <div className={`mt-2 text-[10px] font-medium ${done || active ? "text-[var(--text-primary)]" : "text-[var(--text-subtle)]"}`}>
              {step.label}
            </div>
            <div className="mt-0.5 hidden text-[9px] text-[var(--text-subtle)] xl:block">{step.detail}</div>
          </div>
        );
      })}
    </div>
  );
}

function SelectField({
  label,
  value,
  disabled,
  options,
  onChange,
  className,
}: {
  label: string;
  value: string;
  disabled: boolean;
  options: Array<{ value: string; label: string }>;
  onChange(value: string): void;
  className?: string;
}) {
  return (
    <label className={className}>
      <span className="mb-1.5 block text-[10px] font-semibold tracking-[0.1em] text-[var(--text-subtle)] uppercase">
        {label}
      </span>
      <div className="relative">
        <select
          value={value}
          disabled={disabled}
          onChange={(event) => onChange(event.target.value)}
          className="h-10 w-full appearance-none rounded-xl border border-[color:var(--border)] bg-[var(--surface-2)] px-3 pr-9 text-[12px] font-medium text-[var(--text-primary)] outline-none transition hover:border-[color:var(--border-strong)] focus:border-brand-300/35 focus:ring-2 focus:ring-brand-300/10 disabled:cursor-not-allowed disabled:opacity-55"
        >
          {options.map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
        </select>
        <ChevronDown
          size={14}
          className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-[var(--text-subtle)]"
        />
      </div>
    </label>
  );
}

function Metric({
  icon: Icon,
  label,
  value,
  detail,
  last = false,
}: {
  icon: LucideIcon;
  label: string;
  value: string;
  detail: string;
  last?: boolean;
}) {
  return (
    <div className={`flex min-w-0 items-center gap-3 px-5 py-4 ${last ? "" : "border-r border-[color:var(--border)]"}`}>
      <div className="grid size-9 shrink-0 place-items-center rounded-xl bg-[var(--surface-2)] text-[var(--text-muted)]">
        <Icon size={16} strokeWidth={1.7} />
      </div>
      <div className="min-w-0 text-left">
        <div className="text-[10px] font-medium text-[var(--text-subtle)]">{label}</div>
        <div className="mt-0.5 truncate font-mono text-[13px] font-medium text-[var(--text-primary)]">{value}</div>
        <div className="mt-0.5 truncate text-[9px] text-[var(--text-subtle)]">{detail}</div>
      </div>
    </div>
  );
}

function SmallPanel({
  icon: Icon,
  title,
  value,
  detail,
  tone,
}: {
  icon: LucideIcon;
  title: string;
  value: string;
  detail: string;
  tone: "success" | "warning" | "neutral";
}) {
  const dot =
    tone === "success" ? "bg-emerald-400" : tone === "warning" ? "bg-amber-300" : "bg-[var(--text-subtle)]";
  return (
    <div className="flex items-center gap-4 rounded-2xl border border-[color:var(--border)] bg-[var(--surface-1)] p-4">
      <div className="grid size-10 place-items-center rounded-xl border border-[color:var(--border)] bg-[var(--surface-1)] text-[var(--text-muted)]">
        <Icon size={17} strokeWidth={1.7} />
      </div>
      <div className="min-w-0 flex-1">
        <div className="text-[11px] font-medium text-[var(--text-primary)]">{title}</div>
        <div className="mt-1 truncate text-[10px] text-[var(--text-subtle)]">{detail}</div>
      </div>
      <div className="flex items-center gap-2 text-[10px] font-medium text-[var(--text-secondary)]">
        <span className={`size-1.5 rounded-full ${dot}`} />
        {value}
      </div>
    </div>
  );
}

function Connections({ ready, metrics }: { ready: boolean; metrics?: Metrics }) {
  const { t } = useI18n();
  const connections = metrics?.connections ?? [];
  return (
    <PageShell
      title={t("connections.title")}
      description={t("connections.description")}
      action={
        <div className="flex h-9 items-center gap-3 rounded-xl border border-[color:var(--border)] bg-[var(--surface-1)] px-3 font-mono text-[10px] text-[var(--text-muted)]">
          <span>↓ {formatBytes(metrics?.downloadTotal ?? 0)}</span>
          <span>↑ {formatBytes(metrics?.uploadTotal ?? 0)}</span>
          <span>{t("connections.memory", { value: formatBytes(metrics?.memory ?? 0) })}</span>
        </div>
      }
    >
      {!ready ? (
        <EmptyState icon={Activity} title={t("connections.disconnectedTitle")} detail={t("connections.disconnectedDetail")} />
      ) : connections.length === 0 ? (
        <EmptyState icon={Activity} title={t("connections.emptyTitle")} detail={t("connections.emptyDetail")} />
      ) : (
        <div className="overflow-hidden rounded-2xl border border-[color:var(--border)]">
          <table className="w-full text-left">
            <thead className="border-b border-[color:var(--border)] bg-[var(--surface-1)] text-[10px] font-semibold tracking-wide text-[var(--text-subtle)] uppercase">
              <tr>
                <th className="px-4 py-3">{t("connections.application")}</th>
                <th className="px-4 py-3">{t("connections.target")}</th>
                <th className="px-4 py-3">{t("connections.protocol")}</th>
                <th className="px-4 py-3">{t("connections.status")}</th>
                <th className="px-4 py-3 text-right">{t("connections.traffic")}</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-[color:var(--border)] text-[12px]">
              {connections.map((connection) => (
                <ConnectionRow
                  key={connection.id}
                  app={connection.metadata.process || executableName(connection.metadata.processPath) || connection.metadata.type || t("connections.application")}
                  target={connectionTarget(connection.metadata.host, connection.metadata.destinationIP, connection.metadata.destinationPort, t("connections.unknownTarget"))}
                  protocol={connection.metadata.network.toUpperCase()}
                  traffic={formatBytes(connection.upload + connection.download)}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}
    </PageShell>
  );
}

function executableName(path: string) {
  return path.split(/[\\/]/).filter(Boolean).pop() ?? "";
}

function connectionTarget(host: string, ip: string, port: string, unknownTarget: string) {
  const address = host || ip || unknownTarget;
  if (!port) return address;
  return address.includes(":") && !host ? `[${address}]:${port}` : `${address}:${port}`;
}

function formatBytes(value: number) {
  if (!Number.isFinite(value) || value <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
  const scaled = value / 1024 ** index;
  return `${scaled >= 10 || index === 0 ? scaled.toFixed(0) : scaled.toFixed(1)} ${units[index]}`;
}

function ConnectionRow({
  app,
  target,
  protocol,
  traffic,
}: {
  app: string;
  target: string;
  protocol: string;
  traffic: string;
}) {
  const { t } = useI18n();
  return (
    <tr className="transition hover:bg-[var(--surface-1)]">
      <td className="px-4 py-3.5 font-medium text-[var(--text-primary)]">{app}</td>
      <td className="px-4 py-3.5 font-mono text-brand-300/90">{target}</td>
      <td className="px-4 py-3.5 text-[var(--text-muted)]">{protocol}</td>
      <td className="px-4 py-3.5">
        <span className="inline-flex items-center gap-1.5 text-emerald-300">
          <span className="size-1.5 rounded-full bg-emerald-400" />
          {t("connections.active")}
        </span>
      </td>
      <td className="px-4 py-3.5 text-right font-mono text-[var(--text-muted)]">{traffic}</td>
    </tr>
  );
}

function NetworkView({
  discovery,
  ready,
}: {
  discovery?: SessionState["discovery"];
  ready: boolean;
}) {
  const { t } = useI18n();
  return (
    <PageShell
      title={t("network.title")}
      description={t("network.description")}
      action={
        <button className="inline-flex h-9 items-center gap-2 rounded-xl border border-[color:var(--border)] bg-[var(--surface-1)] px-3 text-[11px] font-medium text-[var(--text-secondary)] transition hover:bg-[var(--surface-hover)] hover:text-[var(--text-strong)]">
          <RefreshCw size={14} />
          {t("network.rediscover")}
        </button>
      }
    >
      <div className="mb-5 grid grid-cols-3 gap-3">
        <InfoCard icon={Route} label="Pod CIDR" value={discovery?.podCIDRs.length ?? 0} detail={discovery?.podCIDRs[0] ?? t("network.notFound")} />
        <InfoCard icon={Boxes} label={t("network.serviceRoutes")} value={discovery?.serviceIPs.length ?? 0} detail={t("network.exactRoutes")} />
        <InfoCard icon={Globe2} label="DNS Server" value={discovery?.dnsServer ? 1 : 0} detail={discovery?.dnsServer ?? t("network.notFound")} />
      </div>
      {!discovery ? (
        <EmptyState icon={Network} title={t("network.waitingTitle")} detail={t("network.waitingDetail")} />
      ) : (
        <div className="overflow-hidden rounded-2xl border border-[color:var(--border)]">
          <table className="w-full text-left">
            <thead className="border-b border-[color:var(--border)] bg-[var(--surface-1)] text-[10px] font-semibold tracking-wide text-[var(--text-subtle)] uppercase">
              <tr><th className="px-4 py-3">{t("network.type")}</th><th className="px-4 py-3">{t("network.target")}</th><th className="px-4 py-3">{t("network.source")}</th><th className="px-4 py-3">{t("network.status")}</th></tr>
            </thead>
            <tbody className="divide-y divide-[color:var(--border)] text-[12px]">
              {discovery.podCIDRs.map((item) => (
                <NetworkRow key={item} type="Pod CIDR" target={item} source="Node spec.podCIDR" ready={ready} />
              ))}
              <NetworkRow type="Service" target={t("network.clusterIPs", { count: discovery.serviceIPs.length })} source="Service API" ready={ready} />
              {discovery.dnsServer && <NetworkRow type="CoreDNS" target={discovery.dnsServer} source="kube-system/kube-dns" ready={ready} />}
            </tbody>
          </table>
        </div>
      )}
    </PageShell>
  );
}

function InfoCard({ icon: Icon, label, value, detail }: { icon: LucideIcon; label: string; value: number; detail: string }) {
  return (
    <div className="rounded-2xl border border-[color:var(--border)] bg-[var(--surface-1)] p-4">
      <div className="flex items-center justify-between">
        <span className="text-[10px] font-medium text-[var(--text-subtle)]">{label}</span>
        <Icon size={15} className="text-[var(--text-subtle)]" />
      </div>
      <div className="mt-3 text-2xl font-semibold tracking-tight text-[var(--text-strong)]">{value}</div>
      <div className="mt-1 truncate font-mono text-[10px] text-[var(--text-subtle)]">{detail}</div>
    </div>
  );
}

function NetworkRow({ type, target, source, ready }: { type: string; target: string; source: string; ready: boolean }) {
  const { t } = useI18n();
  return (
    <tr className="transition hover:bg-[var(--surface-1)]">
      <td className="px-4 py-3.5 font-medium text-[var(--text-primary)]">{type}</td>
      <td className="px-4 py-3.5 font-mono text-brand-300/90">{target}</td>
      <td className="px-4 py-3.5 text-[var(--text-muted)]">{source}</td>
      <td className="px-4 py-3.5">
        <span className={`inline-flex items-center gap-1.5 ${ready ? "text-emerald-300" : "text-[var(--text-subtle)]"}`}>
          {ready ? <CheckCircle2 size={13} /> : <CircleDot size={13} />}
          {ready ? t("network.applied") : t("network.discovered")}
        </span>
      </td>
    </tr>
  );
}

function Logs({ session, error }: { session: SessionState; error: string }) {
  const { locale, t } = useI18n();
  const time = new Date(session.updatedAt).toLocaleTimeString(locale, { hour12: false });
  return (
    <PageShell
      title={t("logs.title")}
      description={t("logs.description")}
      action={
        <button className="inline-flex h-9 items-center gap-2 rounded-xl border border-[color:var(--border)] bg-[var(--surface-1)] px-3 text-[11px] font-medium text-[var(--text-secondary)] transition hover:bg-[var(--surface-hover)] hover:text-[var(--text-strong)]">
          <Copy size={14} />
          {t("logs.copy")}
        </button>
      }
    >
      <div className="overflow-hidden rounded-2xl border border-[color:var(--border)] bg-[var(--console-bg)] font-mono text-[11px]">
        <LogLine time={time} level={error ? "ERROR" : "INFO"} text={error || t(phaseKeys[session.phase])} />
        {session.discovery && (
          <>
            <LogLine time={time} level="INFO" text={t("logs.podCIDRsFound", { count: session.discovery.podCIDRs.length })} />
            <LogLine time={time} level="INFO" text={t("logs.servicesFound", { count: session.discovery.serviceIPs.length })} />
            <LogLine time={time} level="INFO" text={`CoreDNS ${session.discovery.dnsServer || t("network.notFound")}`} />
          </>
        )}
      </div>
    </PageShell>
  );
}

function Settings({
  update,
  checking,
  onCheck,
  onOpen,
}: {
  update: UpdateInfo;
  checking: boolean;
  onCheck(): void;
  onOpen(): void;
}) {
  const { language, locale, setLanguage, t } = useI18n();
  const checkedAt = update.checkedAt
    ? new Date(update.checkedAt).toLocaleString(locale, { hour12: false })
    : t("settings.checkOnStartup");
  return (
    <PageShell
      title={t("settings.title")}
      description={t("settings.description")}
    >
      <div className="mb-5 flex items-center justify-between gap-6 rounded-2xl border border-[color:var(--border)] bg-[var(--surface-1)] p-5">
        <div>
          <h3 className="text-[13px] font-semibold text-[var(--text-strong)]">{t("settings.language")}</h3>
          <p className="mt-1 text-[11px] text-[var(--text-subtle)]">{t("settings.languageDescription")}</p>
        </div>
        <SelectField
          label={t("settings.language")}
          value={language}
          disabled={false}
          onChange={(value) => setLanguage(value as Language)}
          options={[
            { value: "en", label: t("settings.english") },
            { value: "zh-CN", label: t("settings.chinese") },
          ]}
          className="w-44 shrink-0"
        />
      </div>

      <div className="mb-3 flex items-end justify-between gap-4">
        <div>
          <h3 className="text-[13px] font-semibold text-[var(--text-strong)]">{t("settings.updateTitle")}</h3>
          <p className="mt-1 text-[11px] text-[var(--text-subtle)]">{t("settings.updateDescription")}</p>
        </div>
        <button
          type="button"
          disabled={checking}
          onClick={onCheck}
          className="inline-flex h-9 items-center gap-2 rounded-xl border border-[color:var(--border)] bg-[var(--surface-1)] px-3 text-[11px] font-medium text-[var(--text-secondary)] transition hover:bg-[var(--surface-hover)] hover:text-[var(--text-strong)] disabled:cursor-wait disabled:opacity-50"
        >
          <RefreshCw size={14} className={checking ? "animate-spin" : ""} />
          {checking ? t("settings.checking") : t("settings.checkUpdates")}
        </button>
      </div>

      <div className="overflow-hidden rounded-2xl border border-[color:var(--border)] bg-[var(--surface-1)]">
        <div className="flex items-start justify-between gap-6 p-6">
          <div className="min-w-0">
            <div className="flex items-center gap-3">
              <div className="grid size-11 place-items-center rounded-2xl border border-brand-300/15 bg-brand-400/10 text-brand-300">
                <Download size={20} />
              </div>
              <div>
                <h3 className="text-[14px] font-semibold text-[var(--text-strong)]">Kube Clash Desktop</h3>
                <p className="mt-1 font-mono text-[11px] text-[var(--text-muted)]">
                  {t("settings.currentVersion", { version: update.currentVersion || "dev" })}
                </p>
              </div>
            </div>

            <div className="mt-5 text-[12px]">
              {update.available ? (
                <div className="flex items-center gap-2 text-emerald-300">
                  <CheckCircle2 size={15} />
                  {t("settings.newVersion", { version: update.latestVersion ?? "" })}
                </div>
              ) : update.latestVersion ? (
                <div className="flex items-center gap-2 text-[var(--text-secondary)]">
                  <Check size={15} className="text-brand-300" />
                  {update.currentVersion === "dev"
                    ? t("settings.latestStable", { version: update.latestVersion })
                    : t("settings.upToDate")}
                </div>
              ) : (
                <div className="text-[var(--text-muted)]">{t("settings.noRelease")}</div>
              )}
            </div>

            {update.error && (
              <div className="mt-3 max-w-2xl rounded-xl border border-amber-400/15 bg-amber-400/[0.06] px-3 py-2 text-[11px] text-amber-200">
                {update.error}
              </div>
            )}
            <div className="mt-4 text-[10px] text-[var(--text-subtle)]">{t("settings.lastChecked", { value: checkedAt })}</div>
          </div>

          {(update.available || (update.currentVersion === "dev" && update.latestVersion)) && (
            <button
              type="button"
              onClick={onOpen}
              className="inline-flex shrink-0 items-center gap-2 rounded-xl bg-brand-300 px-4 py-2.5 text-[12px] font-semibold text-brand-950 transition hover:bg-brand-100"
            >
              {update.available ? t("settings.download") : t("settings.releasePage")}
              <ExternalLink size={14} />
            </button>
          )}
        </div>
        <div className="border-t border-[color:var(--border)] bg-[var(--surface-inset)] px-6 py-4 text-[11px] leading-5 text-[var(--text-subtle)]">
          {t("settings.updatePrivacy")}{" "}
          {t("settings.updateVerify")}
        </div>
      </div>
    </PageShell>
  );
}

function LogLine({ time, level, text }: { time: string; level: "INFO" | "ERROR"; text: string }) {
  return (
    <div className="grid grid-cols-[82px_62px_1fr] border-b border-[color:var(--border)] px-4 py-3 last:border-0">
      <span className="text-[var(--text-subtle)]">{time}</span>
      <span className={level === "ERROR" ? "text-rose-300" : "text-brand-300/70"}>{level}</span>
      <span className="break-words text-[var(--text-secondary)]">{text}</span>
    </div>
  );
}

function PageShell({
  title,
  description,
  action,
  children,
}: {
  title: string;
  description: string;
  action?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <div className="mx-auto max-w-[1040px]">
      <div className="mb-6 flex items-center justify-between gap-4">
        <div>
          <h2 className="text-[17px] font-semibold tracking-tight text-[var(--text-strong)]">{title}</h2>
          <p className="mt-1 text-[11px] text-[var(--text-subtle)]">{description}</p>
        </div>
        {action}
      </div>
      {children}
    </div>
  );
}

function EmptyState({ icon: Icon, title, detail }: { icon: LucideIcon; title: string; detail: string }) {
  return (
    <div className="grid min-h-[360px] place-items-center rounded-2xl border border-dashed border-[color:var(--border)] bg-[var(--surface-1)] text-center">
      <div>
        <div className="mx-auto grid size-12 place-items-center rounded-2xl border border-[color:var(--border)] bg-[var(--surface-1)] text-[var(--text-subtle)]">
          <Icon size={20} strokeWidth={1.6} />
        </div>
        <h3 className="mt-4 text-[13px] font-medium text-[var(--text-primary)]">{title}</h3>
        <p className="mt-1.5 text-[11px] text-[var(--text-subtle)]">{detail}</p>
      </div>
    </div>
  );
}

export default App;
