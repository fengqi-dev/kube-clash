import { useEffect, useMemo, useState } from "react";
import { Circle, Network } from "lucide-react";
import { toast } from "sonner";
import { backend } from "@/backend";
import {
  ActionIconButton,
  exchangeIcon,
  portForwardIcon,
  previewIcon,
} from "@/components/network/action-icons";
import { ActiveSessions } from "@/components/network/active-sessions";
import { ExchangeDialog } from "@/components/network/exchange-dialog";
import { PortForwardDialog } from "@/components/network/portfwd-dialog";
import { PreviewCreateDialog } from "@/components/network/preview-create-dialog";
import {
  ALL_NAMESPACES,
  ResourceToolbar,
} from "@/components/network/resource-toolbar";
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
import { cn } from "@/lib/utils";
import type { ServiceInfo, SessionState } from "@/types";

type ServiceBinding = "exchange" | "preview" | "portForward" | "idle";

export function NetworkView({
  contextName,
  namespaces,
  namespaceScoped = false,
  ready,
  session,
}: {
  contextName: string;
  namespaces: string[];
  namespaceScoped?: boolean;
  ready: boolean;
  session: SessionState;
}) {
  const { t } = useI18n();
  const [namespace, setNamespace] = useState(
    namespaceScoped && namespaces[0] ? namespaces[0] : ALL_NAMESPACES,
  );
  const [query, setQuery] = useState("");
  const [services, setServices] = useState<ServiceInfo[]>([]);
  const [loading, setLoading] = useState(false);
  const [refreshKey, setRefreshKey] = useState(0);
  const [pfOpen, setPfOpen] = useState(false);
  const [exOpen, setExOpen] = useState(false);
  const [previewOpen, setPreviewOpen] = useState(false);
  const [selected, setSelected] = useState<ServiceInfo | null>(null);
  const [bindings, setBindings] = useState<Record<string, ServiceBinding>>({});

  const connected = session.phase === "connected";
  const liveServices = session.services;
  const canExchange = session.capabilities?.serviceWrite !== false;
  const canPreview = session.capabilities?.serviceCreate !== false;

  useEffect(() => {
    if (!namespaceScoped) return;
    if (namespaces.length === 0) return;
    if (namespace === ALL_NAMESPACES || !namespaces.includes(namespace)) {
      setNamespace(namespaces[0]);
    }
  }, [namespace, namespaceScoped, namespaces]);

  async function reload() {
    if (!contextName) {
      setServices([]);
      return;
    }
    if (connected && liveServices) {
      setServices(liveServices);
      return;
    }
    setLoading(true);
    try {
      setServices(await backend.listServices(contextName, namespace));
    } catch (error) {
      toast.error(t("network.loadFailed"), {
        description: error instanceof Error ? error.message : String(error),
      });
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    if (connected && liveServices) {
      setServices(liveServices);
      return;
    }
    void reload();
  }, [connected, contextName, liveServices, namespace]);

  useEffect(() => {
    let active = true;
    Promise.all([
      backend.listPortForwards(),
      ready ? backend.listIntercepts() : Promise.resolve([]),
      ready ? backend.listPreviews() : Promise.resolve([]),
    ])
      .then(([forwards, exchanges, previews]) => {
        if (!active) return;
        const next: Record<string, ServiceBinding> = {};
        for (const item of forwards) {
          if (item.kind !== "service") continue;
          next[`${item.namespace}/${item.name}`] = "portForward";
        }
        for (const item of exchanges) {
          next[`${item.namespace}/${item.service}`] = "exchange";
        }
        for (const item of previews) {
          next[`${item.namespace}/${item.service}`] = "preview";
        }
        setBindings(next);
      })
      .catch(() => undefined);
    return () => {
      active = false;
    };
  }, [ready, refreshKey, session.updatedAt]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return services.filter((item) => {
      if (namespace !== ALL_NAMESPACES && item.namespace !== namespace) {
        return false;
      }
      if (!q) return true;
      return (
        item.name.toLowerCase().includes(q) ||
        item.namespace.toLowerCase().includes(q) ||
        item.clusterIP.toLowerCase().includes(q)
      );
    });
  }, [namespace, query, services]);

  function bumpActive() {
    setRefreshKey((value) => value + 1);
  }

  return (
    <PageShell title={t("network.title")} description={t("network.description")}>
      <ResourceToolbar
        namespaces={namespaces}
        namespace={namespace}
        onNamespaceChange={setNamespace}
        query={query}
        onQueryChange={setQuery}
        searchPlaceholder={t("network.searchServices")}
        count={filtered.length}
        loading={loading}
        disabled={!contextName}
        onRefresh={() => void reload()}
        allowAllNamespaces={!namespaceScoped}
        actions={
          <ActionIconButton
            label={
              canPreview ? t("network.createPreview") : t("network.previewDenied")
            }
            icon={previewIcon}
            disabled={!ready || !canPreview}
            onClick={() => setPreviewOpen(true)}
          />
        }
      />

      {!contextName ? (
        <EmptyState
          icon={Network}
          title={t("network.waitingTitle")}
          detail={t("network.selectContext")}
        />
      ) : (
        <div className="overflow-hidden rounded-lg border bg-card">
          {!loading && filtered.length === 0 ? (
            <div className="px-4 py-12 text-center text-[12px] text-muted-foreground">
              {t("network.emptyServices")}
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="h-9 text-[11px] font-medium text-muted-foreground">
                    {t("network.colName")}
                  </TableHead>
                  <TableHead className="h-9 text-[11px] font-medium text-muted-foreground">
                    {t("network.colNamespace")}
                  </TableHead>
                  <TableHead className="h-9 text-[11px] font-medium text-muted-foreground">
                    {t("network.colClusterIP")}
                  </TableHead>
                  <TableHead className="h-9 text-[11px] font-medium text-muted-foreground">
                    {t("network.colPorts")}
                  </TableHead>
                  <TableHead className="h-9 text-[11px] font-medium text-muted-foreground">
                    {t("network.colStatus")}
                  </TableHead>
                  <TableHead className="h-9 text-[11px] font-medium text-muted-foreground">
                    {t("network.actions")}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {filtered.map((item) => (
                  <TableRow key={`${item.namespace}/${item.name}`}>
                    <TableCell className="font-medium">{item.name}</TableCell>
                    <TableCell className="text-primary">{item.namespace}</TableCell>
                    <TableCell className="font-mono text-[12px]">{item.clusterIP}</TableCell>
                    <TableCell className="font-mono text-[12px] text-muted-foreground">
                      <div className="flex flex-col gap-0.5">
                        {item.ports.map((port) => (
                          <span key={`${port.protocol}-${port.port}-${port.name || ""}`}>
                            {port.protocol}/{port.port}
                          </span>
                        ))}
                      </div>
                    </TableCell>
                    <TableCell>
                      <ServiceStatus
                        binding={bindings[`${item.namespace}/${item.name}`] ?? "idle"}
                      />
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-1">
                        <ActionIconButton
                          label={t("network.tabPortForward")}
                          icon={portForwardIcon}
                          disabled={!ready}
                          onClick={() => {
                            setSelected(item);
                            setPfOpen(true);
                          }}
                        />
                        <ActionIconButton
                          label={
                            canExchange
                              ? t("network.tabExchange")
                              : t("network.exchangeDenied")
                          }
                          icon={exchangeIcon}
                          disabled={!ready || !canExchange}
                          onClick={() => {
                            setSelected(item);
                            setExOpen(true);
                          }}
                        />
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </div>
      )}

      <ActiveSessions
        ready={ready}
        refreshKey={refreshKey}
        sessionUpdatedAt={session.updatedAt}
      />

      <PortForwardDialog
        open={pfOpen}
        onOpenChange={setPfOpen}
        contextName={contextName}
        kind="service"
        target={selected}
        onStarted={bumpActive}
      />
      <ExchangeDialog
        open={exOpen}
        onOpenChange={setExOpen}
        service={selected}
        onStarted={bumpActive}
      />
      <PreviewCreateDialog
        open={previewOpen}
        onOpenChange={setPreviewOpen}
        namespaces={namespaces}
        defaultNamespace={namespace}
        onStarted={bumpActive}
      />
    </PageShell>
  );
}

function ServiceStatus({ binding }: { binding: ServiceBinding }) {
  const { t } = useI18n();
  const label =
    binding === "exchange"
      ? t("network.tabExchange")
      : binding === "preview"
        ? t("network.tabPreview")
        : binding === "portForward"
          ? t("network.tabPortForward")
          : t("network.idle");
  const ok = binding !== "idle";
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 text-[12px]",
        ok ? "text-success" : "text-muted-foreground",
      )}
    >
      <Circle
        size={8}
        className={ok ? "fill-success text-success" : "fill-muted-foreground/50 text-muted-foreground/50"}
      />
      {label}
    </span>
  );
}
