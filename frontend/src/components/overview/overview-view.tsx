import {
  Boxes,
  Layers3,
  LoaderCircle,
  Orbit,
  Power,
  Server,
  ShieldCheck,
  WifiOff,
  type LucideIcon,
} from "lucide-react";
import { ConnectionOrb } from "@/components/overview/connection-orb";
import { ConnectionSteps } from "@/components/overview/connection-steps";
import { TrafficStats } from "@/components/overview/traffic-stats";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useI18n } from "@/i18n";
import { phaseKeys } from "@/lib/phase";
import type { BootstrapData, SessionState } from "@/types";

export function OverviewView({
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
}: {
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
}) {
  const { t } = useI18n();
  const disabled = busy || ready;

  return (
    <div className="mx-auto max-w-[1040px]">
      <section className="flex flex-wrap items-end justify-between gap-4">
        <div className="flex min-w-0 flex-1 flex-wrap gap-3">
          <LabeledSelect
            label="Kubernetes Context"
            value={contextName}
            disabled={disabled || contexts.length === 0}
            onChange={onContextChange}
            options={contexts.map((item) => ({ value: item.name, label: item.name }))}
            className="min-w-[260px] flex-[1.2]"
          />
          <LabeledSelect
            label="DNS Namespace"
            value={namespace}
            disabled={disabled || namespaces.length === 0}
            onChange={onNamespaceChange}
            options={namespaces.map((item) => ({ value: item, label: item }))}
            className="min-w-[200px] flex-1"
          />
        </div>
        <div className="mb-0.5 hidden items-center gap-2 text-[11px] text-muted-foreground xl:flex">
          <ShieldCheck size={14} className="text-success" />
          {t("overview.localCredentials")}
        </div>
      </section>

      <Card className="relative mt-5 gap-0 overflow-hidden border-border py-0 shadow-none">
        <CardContent className="relative flex flex-col items-center px-8 pt-10 pb-9 text-center">
          <ConnectionOrb phase={session.phase} />
          <h2 className="mt-5 text-xl font-semibold tracking-tight">
            {loading ? t("overview.loadingKubeconfig") : t(phaseKeys[session.phase])}
          </h2>
          <div className="mt-2 flex flex-wrap items-center justify-center gap-2 text-xs text-muted-foreground">
            <span>{contextName || t("overview.noContext")}</span>
            <span className="size-1 rounded-full bg-muted-foreground/50" />
            <span>{clusterName || t("overview.noCluster")}</span>
            <span className="size-1 rounded-full bg-muted-foreground/50" />
            <span>{namespace}</span>
          </div>

          <Button
            type="button"
            size="lg"
            disabled={loading || !contextName}
            onClick={onToggle}
            variant={busy || ready ? "outline" : "default"}
            className="mt-6 min-w-36"
          >
            {busy ? (
              <LoaderCircle className="animate-spin" data-icon="inline-start" />
            ) : ready ? (
              <WifiOff data-icon="inline-start" />
            ) : (
              <Power data-icon="inline-start" />
            )}
            {busy ? t("overview.cancel") : ready ? t("overview.disconnect") : t("overview.connect")}
          </Button>

          {error && (
            <Alert variant="destructive" className="mt-5 max-w-2xl text-left">
              <AlertDescription className="break-words">{error}</AlertDescription>
            </Alert>
          )}

          {(busy || ready) && <ConnectionSteps phase={session.phase} />}
        </CardContent>

        <div className="grid grid-cols-3 border-t bg-muted/50">
          <Metric
            icon={Boxes}
            label="Pods"
            value={discovery ? String(discovery.pods ?? 0) : "—"}
            detail={
              discovery
                ? t("overview.liveInventory")
                : t("overview.waitingDiscovery")
            }
          />
          <Metric
            icon={Server}
            label="Services"
            value={discovery ? String(discovery.services ?? 0) : "—"}
            detail={
              discovery
                ? t("overview.routesSynced")
                : t("overview.waitingDiscovery")
            }
          />
          <Metric
            icon={Layers3}
            label="Deployments"
            value={discovery ? String(discovery.deployments ?? 0) : "—"}
            detail={
              discovery
                ? t("overview.liveInventory")
                : t("overview.waitingDiscovery")
            }
            last
          />
        </div>
      </Card>

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
          title="sing-box TUN"
          value={ready ? t("core.running") : t("core.onDemand")}
          detail={t("overview.tunDetail")}
          tone={ready ? "success" : "neutral"}
        />
      </div>

      <TrafficStats
        ready={ready}
        metrics={session.metrics}
        updatedAt={session.updatedAt}
      />
    </div>
  );
}

function LabeledSelect({
  label,
  value,
  disabled,
  onChange,
  options,
  className,
}: {
  label: string;
  value: string;
  disabled: boolean;
  onChange(value: string): void;
  options: Array<{ value: string; label: string }>;
  className?: string;
}) {
  return (
    <label className={className}>
      <span className="mb-1.5 block text-[10px] font-semibold tracking-[0.1em] text-muted-foreground uppercase">
        {label}
      </span>
      <Select value={value || undefined} disabled={disabled} onValueChange={onChange}>
        <SelectTrigger className="h-10 w-full">
          <SelectValue placeholder={label} />
        </SelectTrigger>
        <SelectContent>
          {options.map((option) => (
            <SelectItem key={option.value} value={option.value}>
              {option.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
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
    <div className={`flex min-w-0 items-center gap-3 px-5 py-4 ${last ? "" : "border-r"}`}>
      <div className="grid size-9 shrink-0 place-items-center rounded-md border bg-background text-muted-foreground">
        <Icon size={16} strokeWidth={1.7} />
      </div>
      <div className="min-w-0 text-left">
        <div className="text-[10px] font-medium text-muted-foreground">{label}</div>
        <div className="mt-0.5 truncate font-mono text-[13px] font-medium">{value}</div>
        <div className="mt-0.5 truncate text-[9px] text-muted-foreground">{detail}</div>
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
  tone: "success" | "neutral";
}) {
  return (
    <Card className="gap-0 py-0 shadow-none">
      <CardContent className="flex items-center gap-4 p-4">
        <div className="grid size-10 place-items-center rounded-md border bg-muted/40 text-muted-foreground">
          <Icon size={17} strokeWidth={1.7} />
        </div>
        <div className="min-w-0 flex-1">
          <div className="text-[11px] font-medium">{title}</div>
          <div className="mt-1 truncate text-[10px] text-muted-foreground">{detail}</div>
        </div>
        <div className="flex items-center gap-2 text-[10px] font-medium text-muted-foreground">
          <span
            className={`size-1.5 rounded-full ${
              tone === "success" ? "bg-success" : "bg-muted-foreground"
            }`}
          />
          {value}
        </div>
      </CardContent>
    </Card>
  );
}
