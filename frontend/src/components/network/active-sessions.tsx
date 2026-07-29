import { useEffect, useState } from "react";
import { Copy, Loader2, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { backend } from "@/backend";
import {
  ActionTypeBadge,
  exchangeIcon,
  portForwardIcon,
  previewIcon,
} from "@/components/network/action-icons";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useI18n } from "@/i18n";
import type { InterceptInfo, PortForwardInfo, PreviewInfo } from "@/types";

export function ActiveSessions({
  ready,
  refreshKey,
  inventoryRevision = 0,
  scope = "network",
}: {
  ready: boolean;
  refreshKey: number;
  /** Informer-driven; reload after reconcile may stop stale bindings. */
  inventoryRevision?: number;
  /** Workload: pod PF only. Network: service PF + Exchange + Preview. */
  scope?: "network" | "podPortForward";
}) {
  const { t } = useI18n();
  const [forwards, setForwards] = useState<PortForwardInfo[]>([]);
  const [exchanges, setExchanges] = useState<InterceptInfo[]>([]);
  const [previews, setPreviews] = useState<PreviewInfo[]>([]);
  const [busy, setBusy] = useState(false);
  const podOnly = scope === "podPortForward";

  async function reload() {
    const [forwardItems, exchangeItems, previewItems] = await Promise.all([
      backend.listPortForwards(),
      !podOnly && ready ? backend.listIntercepts() : Promise.resolve([]),
      !podOnly && ready ? backend.listPreviews() : Promise.resolve([]),
    ]);
    setForwards(
      forwardItems.filter((item) =>
        podOnly ? item.kind === "pod" : item.kind === "service",
      ),
    );
    setExchanges(exchangeItems);
    setPreviews(previewItems);
  }

  useEffect(() => {
    let active = true;
    reload()
      .catch((error: Error) => {
        if (active) {
          toast.error(t("network.activeLoadFailed"), { description: error.message });
        }
      });
    return () => {
      active = false;
    };
  }, [podOnly, ready, refreshKey, inventoryRevision, t]);

  async function stopForward(id: string) {
    setBusy(true);
    try {
      await backend.stopPortForward(id);
      await reload();
      toast.success(t("portfwd.stopped"));
    } catch (error) {
      toast.error(t("portfwd.stopFailed"), {
        description: error instanceof Error ? error.message : String(error),
      });
    } finally {
      setBusy(false);
    }
  }

  async function stopExchange(id: string) {
    setBusy(true);
    try {
      await backend.stopIntercept(id);
      await reload();
      toast.success(t("intercept.stopped"));
    } catch (error) {
      toast.error(t("intercept.stopFailed"), {
        description: error instanceof Error ? error.message : String(error),
      });
    } finally {
      setBusy(false);
    }
  }

  async function stopPreview(id: string) {
    setBusy(true);
    try {
      await backend.stopPreview(id);
      await reload();
      toast.success(t("preview.stopped"));
    } catch (error) {
      toast.error(t("preview.stopFailed"), {
        description: error instanceof Error ? error.message : String(error),
      });
    } finally {
      setBusy(false);
    }
  }

  async function copyAddress(address: string) {
    try {
      await navigator.clipboard.writeText(address);
      toast.success(t("portfwd.copied"), { description: address });
    } catch {
      toast.error(t("portfwd.copyFailed"));
    }
  }

  const empty =
    forwards.length === 0 && exchanges.length === 0 && previews.length === 0;

  return (
    <section className="mt-5 overflow-hidden rounded-lg border bg-card">
      <div className="border-b px-4 py-3">
        <div className="text-[13px] font-semibold">{t("network.activeTitle")}</div>
        <p className="mt-1 text-[11px] text-muted-foreground">
          {podOnly ? t("workload.activeDescription") : t("network.activeDescription")}
        </p>
      </div>

      {empty ? (
        <div className="px-4 py-10 text-center text-[12px] text-muted-foreground">
          {t("network.activeEmpty")}
        </div>
      ) : (
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead className="h-9 text-[11px] font-medium text-muted-foreground">
                {t("network.type")}
              </TableHead>
              <TableHead className="h-9 text-[11px] font-medium text-muted-foreground">
                {t("network.target")}
              </TableHead>
              <TableHead className="h-9 text-[11px] font-medium text-muted-foreground">
                {t("network.mapping")}
              </TableHead>
              <TableHead className="h-9 text-[11px] font-medium text-muted-foreground">
                {t("network.actions")}
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {forwards.map((item) => (
              <TableRow key={`pf-${item.id}`}>
                <TableCell>
                  <ActionTypeBadge
                    label={t("network.tabPortForward")}
                    icon={portForwardIcon}
                  />
                </TableCell>
                <TableCell className="font-medium">
                  {item.kind}/{item.namespace}/{item.name}
                </TableCell>
                <TableCell className="font-mono text-[12px] text-muted-foreground">
                  {item.address} → :{item.remotePort}
                </TableCell>
                <TableCell>
                  <div className="flex gap-1">
                    <Button
                      type="button"
                      size="sm"
                      variant="ghost"
                      disabled={busy}
                      onClick={() => void copyAddress(item.address)}
                    >
                      <Copy size={14} />
                    </Button>
                    <Button
                      type="button"
                      size="sm"
                      variant="ghost"
                      disabled={busy}
                      onClick={() => void stopForward(item.id)}
                    >
                      {busy ? <Loader2 size={14} className="animate-spin" /> : <Trash2 size={14} />}
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            ))}
            {exchanges.map((item) => (
              <TableRow key={`ex-${item.id}`}>
                <TableCell>
                  <ActionTypeBadge
                    label={t("network.tabExchange")}
                    icon={exchangeIcon}
                  />
                </TableCell>
                <TableCell className="font-medium">
                  {item.namespace}/{item.service}
                </TableCell>
                <TableCell className="font-mono text-[12px] text-muted-foreground">
                  {item.locals
                    .map((port) => `${port.servicePort} → ${port.localHost}:${port.localPort}`)
                    .join(", ")}
                </TableCell>
                <TableCell>
                  <Button
                    type="button"
                    size="sm"
                    variant="ghost"
                    disabled={busy}
                    onClick={() => void stopExchange(item.id)}
                  >
                    {busy ? <Loader2 size={14} className="animate-spin" /> : <Trash2 size={14} />}
                  </Button>
                </TableCell>
              </TableRow>
            ))}
            {previews.map((item) => (
              <TableRow key={`pv-${item.id}`}>
                <TableCell>
                  <ActionTypeBadge
                    label={t("network.tabPreview")}
                    icon={previewIcon}
                  />
                </TableCell>
                <TableCell className="font-medium">
                  {item.namespace}/{item.service}
                  {item.clusterIP ? (
                    <span className="ml-2 font-mono text-[11px] text-muted-foreground">
                      {item.clusterIP}
                    </span>
                  ) : null}
                </TableCell>
                <TableCell className="font-mono text-[12px] text-muted-foreground">
                  {item.locals
                    .map((port) => `${port.servicePort} → ${port.localHost}:${port.localPort}`)
                    .join(", ")}
                </TableCell>
                <TableCell>
                  <Button
                    type="button"
                    size="sm"
                    variant="ghost"
                    disabled={busy}
                    onClick={() => void stopPreview(item.id)}
                  >
                    {busy ? <Loader2 size={14} className="animate-spin" /> : <Trash2 size={14} />}
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </section>
  );
}
