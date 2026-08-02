import { useEffect, useState, type ReactNode } from "react";
import {
  ArrowRightLeft,
  Cable,
  Copy,
  CopyPlus,
  Eye,
  Loader2,
  RotateCcw,
  Server,
  ShieldCheck,
  type LucideIcon,
} from "lucide-react";
import { toast } from "sonner";
import { backend } from "@/backend";
import { ConnectionOrb } from "@/components/overview/connection-orb";
import { ConnectionSteps } from "@/components/overview/connection-steps";
import { TrafficStats } from "@/components/overview/traffic-stats";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { useI18n } from "@/i18n";
import type { AppView } from "@/hooks/use-session";
import { phaseKeys } from "@/lib/phase";
import { cn } from "@/lib/utils";
import type { Discovery, SessionState } from "@/types";

export function OverviewView({
  contextName,
  clusterName,
  session,
  loading,
  error,
  busy,
  ready,
  onToggle,
  onManageClusters,
  onNavigate,
}: {
  contextName: string;
  clusterName: string;
  session: SessionState;
  loading: boolean;
  error: string;
  busy: boolean;
  ready: boolean;
  onToggle(): void;
  onManageClusters(): void;
  onNavigate(view: AppView): void;
}) {
  const { t } = useI18n();
  const [podPortForwards, setPodPortForwards] = useState(0);
  const [networkPortForwards, setNetworkPortForwards] = useState(0);
  const [exchanges, setExchanges] = useState(0);
  const [mirrors, setMirrors] = useState(0);
  const [previews, setPreviews] = useState(0);
  const [metricsTick, setMetricsTick] = useState(0);
  const [resetOpen, setResetOpen] = useState(false);
  const [resetting, setResetting] = useState(false);

  useEffect(() => {
    let active = true;
    const refresh = () => {
      Promise.all([
        backend.listPortForwards(),
        backend.listIntercepts(),
        backend.listMirrors(),
        backend.listPreviews(),
        backend.sessionIntentCounts(),
      ])
        .then(([forwards, intercepts, mirrorItems, previewItems, intents]) => {
          if (!active) return;
          // Prefer live counts; fall back to persisted intents when the cluster is down.
          setPodPortForwards(
            Math.max(
              forwards.filter((item) => item.kind === "pod").length,
              intents.podPortForwards ?? 0,
            ),
          );
          setNetworkPortForwards(
            Math.max(
              forwards.filter((item) => item.kind === "service").length,
              intents.networkPortForwards ?? 0,
            ),
          );
          setExchanges(Math.max(intercepts.length, intents.exchanges ?? 0));
          setMirrors(Math.max(mirrorItems.length, intents.mirrors ?? 0));
          setPreviews(previewItems.length);
        })
        .catch(() => undefined);
    };
    refresh();
    return () => {
      active = false;
    };
  }, [session.inventoryRevision, ready, metricsTick]);

  async function confirmReset() {
    setResetting(true);
    try {
      await backend.resetSessions();
      setPodPortForwards(0);
      setNetworkPortForwards(0);
      setExchanges(0);
      setMirrors(0);
      setMetricsTick((value) => value + 1);
      setResetOpen(false);
      toast.success(t("overview.resetSessionsDone"));
    } catch (error) {
      toast.error(t("overview.resetSessionsFailed"), {
        description: error instanceof Error ? error.message : String(error),
      });
    } finally {
      setResetting(false);
    }
  }

  const issues = session.capabilities?.issues ?? [];
  const networkIssues = session.network?.issues ?? [];
  const gatewayManifest = session.gatewayManifest ?? "";

  return (
    <div className="mx-auto max-w-[880px] space-y-5">
      <Card className="gap-0 overflow-hidden border-border py-0 shadow-none">
        <div className="flex flex-wrap items-center gap-3 border-b px-5 py-3">
          <div className="min-w-0 flex-1">
            <div className="truncate text-sm font-medium">
              {contextName || t("overview.noContext")}
            </div>
            <div className="mt-0.5 truncate text-[11px] text-muted-foreground">
              {session.kubernetesVersion
                ? t("overview.kubernetesVersion", { version: session.kubernetesVersion })
                : clusterName && clusterName !== contextName
                  ? clusterName
                  : t("overview.noCluster")}
            </div>
          </div>
          <Button type="button" variant="outline" size="sm" onClick={onManageClusters}>
            <Server size={14} data-icon="inline-start" />
            {t("overview.manageClusters")}
          </Button>
          <div className="hidden items-center gap-1.5 text-[11px] text-muted-foreground sm:flex">
            <ShieldCheck size={13} className="text-success" />
            {t("overview.localCredentials")}
          </div>
        </div>

        <CardContent className="px-5 py-5">
          <div className="grid min-h-[16.5rem] items-stretch gap-5 sm:grid-cols-[minmax(0,1.15fr)_auto_minmax(0,0.85fr)]">
            <DiscoverySummary
              contextName={contextName}
              discovery={session.discovery}
              ready={ready}
            />

            <div className="flex flex-col items-center justify-center self-center px-2 text-center">
              <ConnectionOrb
                phase={session.phase}
                busy={busy}
                disabled={loading || !contextName}
                ariaLabel={
                  busy
                    ? t("overview.cancel")
                    : ready
                      ? t("overview.disconnect")
                      : t("overview.connect")
                }
                onClick={onToggle}
              />
              <h2 className="mt-2.5 text-[14px] font-semibold tracking-tight">
                {loading ? t("overview.loadingKubeconfig") : t(phaseKeys[session.phase])}
              </h2>
              <p className="mt-0.5 max-w-[9rem] truncate text-[11px] text-muted-foreground">
                {contextName
                  ? [contextName, clusterName || t("overview.noCluster")].join(" · ")
                  : t("overview.selectClusterFirst")}
              </p>
            </div>

            <SessionMetrics
              podPortForwards={podPortForwards}
              networkPortForwards={networkPortForwards}
              exchanges={exchanges}
              mirrors={mirrors}
              previews={previews}
              onNavigate={onNavigate}
              onReset={() => setResetOpen(true)}
              resetting={resetting}
            />
          </div>

          <Dialog open={resetOpen} onOpenChange={(open) => !resetting && setResetOpen(open)}>
            <DialogContent showCloseButton={!resetting}>
              <DialogHeader>
                <DialogTitle>{t("overview.resetSessionsTitle")}</DialogTitle>
                <DialogDescription>{t("overview.resetSessionsDesc")}</DialogDescription>
              </DialogHeader>
              <DialogFooter>
                <Button
                  type="button"
                  variant="outline"
                  disabled={resetting}
                  onClick={() => setResetOpen(false)}
                >
                  {t("actions.cancel")}
                </Button>
                <Button
                  type="button"
                  variant="destructive"
                  disabled={resetting}
                  onClick={() => void confirmReset()}
                >
                  {resetting ? (
                    <Loader2 data-icon="inline-start" className="animate-spin" />
                  ) : (
                    <RotateCcw data-icon="inline-start" />
                  )}
                  {t("overview.resetSessionsConfirm")}
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>

          {error ? (
            <Alert variant="destructive" className="mt-4 w-full text-left">
              <AlertDescription className="break-words">{error}</AlertDescription>
            </Alert>
          ) : null}
          {session.dnsWarning ? (
            <Alert className="mt-4 w-full text-left">
              <AlertDescription className="break-words">{session.dnsWarning}</AlertDescription>
            </Alert>
          ) : null}
          {networkIssues.length > 0 ? (
            <Alert className="mt-4 w-full text-left">
              <AlertDescription>
                <ul className="list-disc space-y-1 pl-4 text-[12px]">
                  {networkIssues.map((item) => (
                    <li key={`${item.code}:${item.target}:${item.conflict}:${item.interface}`}>
                      {item.message}
                    </li>
                  ))}
                </ul>
              </AlertDescription>
            </Alert>
          ) : null}

          {issues.length > 0 || gatewayManifest ? (
            <Alert className="mt-4 w-full text-left">
              <AlertDescription className="space-y-2">
                {issues.length > 0 ? (
                  <ul className="list-disc space-y-1 pl-4 text-[12px]">
                    {issues.map((item) => (
                      <li key={item}>{item}</li>
                    ))}
                  </ul>
                ) : (
                  <p className="text-[12px]">{t("overview.rbacGatewayMissing")}</p>
                )}
                {gatewayManifest ? (
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={() => {
                      void navigator.clipboard.writeText(gatewayManifest).then(
                        () => toast.success(t("overview.rbacCopied")),
                        () => toast.error(t("overview.rbacCopyFailed")),
                      );
                    }}
                  >
                    <Copy size={14} data-icon="inline-start" />
                    {t("overview.rbacCopyYaml")}
                  </Button>
                ) : null}
              </AlertDescription>
            </Alert>
          ) : null}

          {busy ? <ConnectionSteps phase={session.phase} /> : null}
        </CardContent>
      </Card>

      <TrafficStats
        ready={ready}
        metrics={session.metrics}
      />
    </div>
  );
}

function SidePanel({
  title,
  children,
  footer,
}: {
  title: string;
  children: ReactNode;
  footer?: ReactNode;
}) {
  return (
    <div className="flex h-full min-w-0 flex-col rounded-lg border bg-muted/25 px-3.5 py-3">
      <div className="mb-2 text-[11px] font-medium tracking-wide text-muted-foreground uppercase">
        {title}
      </div>
      <div className="flex min-h-0 min-w-0 flex-1 flex-col">{children}</div>
      {footer ? <div className="mt-2.5">{footer}</div> : null}
    </div>
  );
}

function DiscoverySummary({
  contextName,
  discovery,
  ready,
}: {
  contextName: string;
  discovery?: Discovery;
  ready: boolean;
}) {
  const { t } = useI18n();
  const [podCIDRs, setPodCIDRs] = useState("");
  const [serviceCIDRs, setServiceCIDRs] = useState("");
  const [dnsServer, setDnsServer] = useState("");
  const [clusterDomains, setClusterDomains] = useState("cluster.local");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!contextName) {
      setPodCIDRs("");
      setServiceCIDRs("");
      setDnsServer("");
      setClusterDomains("cluster.local");
      return;
    }
    let active = true;
    backend
      .getManualNetwork(contextName)
      .then((manual) => {
        if (!active) return;
        setPodCIDRs(
          (manual.podCIDRs?.length ? manual.podCIDRs : discovery?.podCIDRs)?.join(", ") ?? "",
        );
        setServiceCIDRs(
          (manual.serviceCIDRs?.length
            ? manual.serviceCIDRs
            : discovery?.serviceCIDRs
          )?.join(", ") ?? "",
        );
        setDnsServer(manual.dnsServer || discovery?.dnsServer || "");
        setClusterDomains(
          (manual.clusterDomains?.length
            ? manual.clusterDomains
            : discovery?.clusterDomains?.length
              ? discovery.clusterDomains
              : ["cluster.local"]
          ).join(", "),
        );
      })
      .catch(() => {
        if (!active) return;
        setPodCIDRs(discovery?.podCIDRs?.join(", ") ?? "");
        setServiceCIDRs(discovery?.serviceCIDRs?.join(", ") ?? "");
        setDnsServer(discovery?.dnsServer || "");
        setClusterDomains((discovery?.clusterDomains ?? ["cluster.local"]).join(", "));
      });
    return () => {
      active = false;
    };
  }, [
    contextName,
    discovery?.dnsServer,
    discovery?.podCIDRs,
    discovery?.serviceCIDRs,
    discovery?.clusterDomains,
  ]);

  async function save() {
    if (!contextName) return;
    setSaving(true);
    try {
      await backend.setManualNetwork(contextName, {
        podCIDRs: splitCIDRs(podCIDRs),
        serviceCIDRs: splitCIDRs(serviceCIDRs),
        dnsServer: dnsServer.trim(),
        clusterDomains: splitCIDRs(clusterDomains),
      });
      toast.success(ready ? t("overview.networkSavedReconnect") : t("overview.networkSaved"));
    } catch (error) {
      toast.error(t("overview.networkSaveFailed"), {
        description: error instanceof Error ? error.message : String(error),
      });
    } finally {
      setSaving(false);
    }
  }

  return (
    <SidePanel
      title={t("overview.networkPanel")}
      footer={
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="w-full"
          disabled={!contextName || saving}
          onClick={() => void save()}
        >
          {t("overview.networkSave")}
        </Button>
      }
    >
      <dl className="space-y-2">
        <NetworkField
          label={t("overview.clusterDomain")}
          value={clusterDomains}
          placeholder="cluster.local, corp.local"
          onChange={setClusterDomains}
          disabled={!contextName}
        />
        <NetworkField
          label={t("overview.podNetwork")}
          value={podCIDRs}
          placeholder="10.244.0.0/16"
          onChange={setPodCIDRs}
          disabled={!contextName}
        />
        <NetworkField
          label={t("overview.serviceNetwork")}
          value={serviceCIDRs}
          placeholder="10.96.0.0/12"
          onChange={setServiceCIDRs}
          disabled={!contextName}
        />
        <NetworkField
          label={t("overview.clusterDns")}
          value={dnsServer}
          placeholder="10.96.0.10"
          onChange={setDnsServer}
          disabled={!contextName}
        />
      </dl>
      <p className="mt-2 min-h-[2.5rem] text-[10px] leading-snug text-muted-foreground">
        {t("overview.networkHint")}
      </p>
    </SidePanel>
  );
}

function NetworkField({
  label,
  value,
  placeholder,
  onChange,
  disabled,
}: {
  label: string;
  value: string;
  placeholder: string;
  onChange(value: string): void;
  disabled?: boolean;
}) {
  return (
    <div className="grid grid-cols-[5.5rem_minmax(0,1fr)] items-center gap-x-2 text-[12px]">
      <dt className="text-muted-foreground">{label}</dt>
      <dd>
        <input
          className="h-7 w-full min-w-0 rounded-md border border-input bg-background px-2 font-mono text-[11px] outline-none transition-colors focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/40 disabled:opacity-50"
          value={value}
          placeholder={placeholder}
          disabled={disabled}
          onChange={(event) => onChange(event.target.value)}
        />
      </dd>
    </div>
  );
}

function splitCIDRs(raw: string): string[] {
  return raw
    .split(/[\n,;]+/)
    .map((item) => item.trim())
    .filter(Boolean);
}

function SessionMetrics({
  podPortForwards,
  networkPortForwards,
  exchanges,
  mirrors,
  previews,
  onNavigate,
  onReset,
  resetting,
}: {
  podPortForwards: number;
  networkPortForwards: number;
  exchanges: number;
  mirrors: number;
  previews: number;
  onNavigate(view: AppView): void;
  onReset(): void;
  resetting: boolean;
}) {
  const { t } = useI18n();
  const resettable = podPortForwards + networkPortForwards + exchanges + mirrors;
  const groups: {
    title: string;
    rows: { icon: LucideIcon; label: string; value: number; view: AppView }[];
  }[] = [
    {
      title: t("overview.podGroup"),
      rows: [
        {
          icon: Cable,
          label: t("network.tabPortForward"),
          value: podPortForwards,
          view: "workload",
        },
      ],
    },
    {
      title: t("overview.networkGroup"),
      rows: [
        {
          icon: Cable,
          label: t("network.tabPortForward"),
          value: networkPortForwards,
          view: "network",
        },
        {
          icon: ArrowRightLeft,
          label: t("network.tabExchange"),
          value: exchanges,
          view: "network",
        },
        {
          icon: CopyPlus,
          label: t("network.tabMirror"),
          value: mirrors,
          view: "network",
        },
        {
          icon: Eye,
          label: t("network.tabPreview"),
          value: previews,
          view: "network",
        },
      ],
    },
  ];

  return (
    <SidePanel
      title={t("overview.sessionsPanel")}
      footer={
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="w-full"
          disabled={resettable === 0 || resetting}
          onClick={onReset}
        >
          {resetting ? (
            <Loader2 data-icon="inline-start" className="animate-spin" />
          ) : (
            <RotateCcw data-icon="inline-start" />
          )}
          {t("overview.resetSessions")}
        </Button>
      }
    >
      <div className="flex min-h-0 flex-1 flex-col justify-between">
        {groups.map((group) => (
          <div key={group.title} className="min-w-0">
            <div className="mb-1.5 text-[11px] font-medium text-foreground/85">{group.title}</div>
            <div className="space-y-1.5 pl-0.5">
              {group.rows.map((row) => (
                <button
                  key={`${group.title}-${row.label}`}
                  type="button"
                  onClick={() => onNavigate(row.view)}
                  className={cn(
                    "flex w-full items-center gap-2 rounded-md px-1 py-0.5 text-left",
                    "transition-colors hover:bg-accent/60 focus-visible:outline-none",
                    "focus-visible:ring-2 focus-visible:ring-ring/50",
                  )}
                >
                  <div className="grid size-6 shrink-0 place-items-center rounded-md border bg-background text-muted-foreground">
                    <row.icon size={13} strokeWidth={1.7} />
                  </div>
                  <span className="text-[11px] text-muted-foreground">{row.label}</span>
                  <span className="ml-auto font-mono text-[13px] font-semibold tabular-nums">
                    {row.value}
                  </span>
                </button>
              ))}
            </div>
          </div>
        ))}
      </div>
    </SidePanel>
  );
}
