import {
  Boxes,
  CheckCircle2,
  CircleDot,
  Globe2,
  Network,
  RefreshCw,
  Route,
  type LucideIcon,
} from "lucide-react";
import { EmptyState } from "@/components/shared/empty-state";
import { PageShell } from "@/components/shared/page-shell";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useI18n } from "@/i18n";
import type { SessionState } from "@/types";

export function NetworkView({
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
        <Button type="button" variant="outline" size="sm">
          <RefreshCw data-icon="inline-start" />
          {t("network.rediscover")}
        </Button>
      }
    >
      <div className="mb-5 grid grid-cols-3 gap-3">
        <InfoCard
          icon={Boxes}
          label="Pods"
          value={discovery?.pods ?? 0}
          detail={discovery?.podCIDRs[0] ?? t("network.notFound")}
        />
        <InfoCard
          icon={Route}
          label="Services"
          value={discovery?.services ?? 0}
          detail={t("network.clusterIPs", {
            count: discovery?.serviceIPs.length ?? 0,
          })}
        />
        <InfoCard
          icon={Globe2}
          label="Deployments"
          value={discovery?.deployments ?? 0}
          detail={t("overview.liveInventory")}
        />
      </div>
      {!discovery ? (
        <EmptyState
          icon={Network}
          title={t("network.waitingTitle")}
          detail={t("network.waitingDetail")}
        />
      ) : (
        <div className="overflow-hidden rounded-lg border bg-card">
          <Table>
            <TableHeader>
              <TableRow className="bg-muted/40 hover:bg-muted/40">
                <TableHead className="text-[10px] tracking-wide uppercase">
                  {t("network.type")}
                </TableHead>
                <TableHead className="text-[10px] tracking-wide uppercase">
                  {t("network.target")}
                </TableHead>
                <TableHead className="text-[10px] tracking-wide uppercase">
                  {t("network.source")}
                </TableHead>
                <TableHead className="text-[10px] tracking-wide uppercase">
                  {t("network.status")}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {discovery.podCIDRs.map((item) => (
                <NetworkRow
                  key={item}
                  type="Pod CIDR"
                  target={item}
                  source="Node spec.podCIDR"
                  ready={ready}
                />
              ))}
              <NetworkRow
                type="Pods"
                target={String(discovery.pods ?? 0)}
                source="Informer core/v1.Pod"
                ready={ready}
              />
              <NetworkRow
                type="Services"
                target={String(discovery.services ?? 0)}
                source="Informer core/v1.Service"
                ready={ready}
              />
              <NetworkRow
                type="Deployments"
                target={String(discovery.deployments ?? 0)}
                source="Informer apps/v1.Deployment"
                ready={ready}
              />
              {discovery.dnsServer && (
                <NetworkRow
                  type="CoreDNS"
                  target={discovery.dnsServer}
                  source="kube-system/kube-dns"
                  ready={ready}
                />
              )}
            </TableBody>
          </Table>
        </div>
      )}
    </PageShell>
  );
}

function InfoCard({
  icon: Icon,
  label,
  value,
  detail,
}: {
  icon: LucideIcon;
  label: string;
  value: number;
  detail: string;
}) {
  return (
    <Card className="gap-0 py-0 shadow-none">
      <CardContent className="p-4">
        <div className="flex items-center justify-between">
          <span className="text-[10px] font-medium text-muted-foreground">{label}</span>
          <Icon size={15} className="text-muted-foreground" />
        </div>
        <div className="mt-3 text-2xl font-semibold tracking-tight">{value}</div>
        <div className="mt-1 truncate font-mono text-[10px] text-muted-foreground">{detail}</div>
      </CardContent>
    </Card>
  );
}

function NetworkRow({
  type,
  target,
  source,
  ready,
}: {
  type: string;
  target: string;
  source: string;
  ready: boolean;
}) {
  const { t } = useI18n();
  return (
    <TableRow>
      <TableCell className="font-medium">{type}</TableCell>
      <TableCell className="font-mono text-primary">{target}</TableCell>
      <TableCell className="text-muted-foreground">{source}</TableCell>
      <TableCell>
        <span
          className={`inline-flex items-center gap-1.5 ${
            ready ? "text-success" : "text-muted-foreground"
          }`}
        >
          {ready ? <CheckCircle2 size={13} /> : <CircleDot size={13} />}
          {ready ? t("network.applied") : t("network.discovered")}
        </span>
      </TableCell>
    </TableRow>
  );
}
