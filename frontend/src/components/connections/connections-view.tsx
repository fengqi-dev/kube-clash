import { useEffect, useState } from "react";
import { Activity } from "lucide-react";
import { EmptyState } from "@/components/shared/empty-state";
import { PageShell } from "@/components/shared/page-shell";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useI18n } from "@/i18n";
import {
  executableName,
  formatBytes,
  formatDuration,
  formatSpeed,
} from "@/lib/format";
import type { Connection, Metrics } from "@/types";

const stickyForMs = 30_000;
const maxTableRows = 100;

const columns = [
  { key: "process", align: "left" as const },
  { key: "network", align: "left" as const },
  { key: "inbound", align: "left" as const },
  { key: "destination", align: "left" as const },
  { key: "source", align: "left" as const },
  { key: "download", align: "right" as const },
  { key: "upload", align: "right" as const },
  { key: "downloadSpeed", align: "right" as const },
  { key: "uploadSpeed", align: "right" as const },
  { key: "outbound", align: "left" as const },
  { key: "rule", align: "left" as const },
  { key: "duration", align: "left" as const },
] as const;

export function ConnectionsView({
  ready,
  metrics,
}: {
  ready: boolean;
  metrics?: Metrics;
}) {
  const { t } = useI18n();
  const live = Array.isArray(metrics?.connections) ? metrics.connections : [];
  const liveCount = live.length;
  const [sticky, setSticky] = useState<Connection[]>([]);
  const now = Date.now();

  useEffect(() => {
    if (!ready) {
      setSticky([]);
      return;
    }
    if (liveCount > 0) {
      setSticky(live.slice(0, maxTableRows));
      return;
    }
    if (sticky.length === 0) return;
    const timer = window.setTimeout(() => setSticky([]), stickyForMs);
    return () => window.clearTimeout(timer);
  }, [live, liveCount, ready, sticky.length]);

  const connections = (liveCount > 0 ? live : sticky).slice(0, maxTableRows);

  return (
    <PageShell
      title={t("connections.title")}
      description={t("connections.description")}
      action={
        <div className="flex h-8 items-center gap-3 rounded-md border bg-card px-3 font-mono text-[11px] text-muted-foreground">
          <span>↓ {formatBytes(metrics?.downloadTotal ?? 0)}</span>
          <span>↑ {formatBytes(metrics?.uploadTotal ?? 0)}</span>
        </div>
      }
    >
      {!ready ? (
        <EmptyState
          icon={Activity}
          title={t("connections.disconnectedTitle")}
          detail={t("connections.disconnectedDetail")}
        />
      ) : (
        <div className="overflow-auto rounded-lg border bg-card">
          <Table>
            <TableHeader>
              <TableRow className="bg-muted/50 hover:bg-muted/50">
                {columns.map((column) => (
                  <TableHead
                    key={column.key}
                    className={
                      column.align === "right"
                        ? "text-right text-[10px] tracking-wide uppercase"
                        : "text-[10px] tracking-wide uppercase"
                    }
                  >
                    {t(`connections.${column.key}`)}
                  </TableHead>
                ))}
              </TableRow>
            </TableHeader>
            <TableBody>
              {connections.length === 0 ? (
                <TableRow className="hover:bg-transparent">
                  <TableCell
                    colSpan={columns.length}
                    className="h-40 text-center text-muted-foreground"
                  >
                    <div className="mx-auto max-w-sm space-y-1">
                      <p className="text-sm font-medium text-foreground">
                        {t("connections.emptyTitle")}
                      </p>
                      <p className="text-xs">{t("connections.emptyDetail")}</p>
                    </div>
                  </TableCell>
                </TableRow>
              ) : (
                connections.map((connection) => (
                  <TableRow key={connection.id}>
                    <TableCell className="max-w-[140px] truncate font-medium">
                      {connection.process ||
                        executableName(connection.process) ||
                        "—"}
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {connection.network?.toUpperCase() || "—"}
                    </TableCell>
                    <TableCell className="max-w-[130px] truncate text-[11px] text-muted-foreground">
                      {connection.inbound || "—"}
                    </TableCell>
                    <TableCell className="max-w-[220px] truncate font-mono text-[11px] text-primary">
                      {connection.destination || "—"}
                    </TableCell>
                    <TableCell className="font-mono text-[11px] text-muted-foreground">
                      {connection.source || "—"}
                    </TableCell>
                    <TableCell className="text-right font-mono text-muted-foreground">
                      {formatBytes(connection.download ?? 0)}
                    </TableCell>
                    <TableCell className="text-right font-mono text-muted-foreground">
                      {formatBytes(connection.upload ?? 0)}
                    </TableCell>
                    <TableCell className="text-right font-mono text-muted-foreground">
                      {formatSpeed(connection.downloadSpeed ?? 0)}
                    </TableCell>
                    <TableCell className="text-right font-mono text-muted-foreground">
                      {formatSpeed(connection.uploadSpeed ?? 0)}
                    </TableCell>
                    <TableCell className="max-w-[120px] truncate text-[11px] text-muted-foreground">
                      {connection.outbound || "—"}
                    </TableCell>
                    <TableCell className="max-w-[180px] truncate text-[11px] text-muted-foreground">
                      {connection.rule || "—"}
                    </TableCell>
                    <TableCell className="font-mono text-[11px] text-muted-foreground">
                      {formatDuration(connection.startedAt, now)}
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </div>
      )}
    </PageShell>
  );
}
