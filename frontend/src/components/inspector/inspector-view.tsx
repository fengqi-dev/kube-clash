import { useEffect, useMemo, useState } from "react";
import { Download, ScanSearch, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { backend } from "@/backend";
import { PageShell } from "@/components/shared/page-shell";
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
import type {
  InspectorEvent,
  InspectorCAState,
  InspectorTarget,
  SessionState,
} from "@/types";

interface Flow {
  id: string;
  method?: string;
  authority?: string;
  path?: string;
  statusCode?: number;
  grpcStatus?: string;
  requestMessages?: number;
  responseMessages?: number;
  durationMs?: number;
  error?: string;
  source?: string;
  bytes: number;
  events: InspectorEvent[];
}

const maxFlows = 500;
const maxFlowBytes = 50 * 1024 * 1024;

function requiresInspectorCA(protocol: InspectorTarget["protocol"]) {
  return protocol !== "http";
}

export function InspectorView({
  ready,
  session,
}: {
  ready: boolean;
  session: SessionState;
}) {
  const { t } = useI18n();
  const [host, setHost] = useState("");
  const [port, setPort] = useState("80");
  const [serviceTarget, setServiceTarget] = useState("");
  const [protocol, setProtocol] = useState<InspectorTarget["protocol"]>("http");
  const [captureBody, setCaptureBody] = useState(false);
  const [descriptorSet, setDescriptorSet] = useState("");
  const [targets, setTargets] = useState<InspectorTarget[]>([]);
  const [active, setActive] = useState(false);
  const [busy, setBusy] = useState(false);
  const [flows, setFlows] = useState<Record<string, Flow>>({});
  const [selected, setSelected] = useState("");
  const [filter, setFilter] = useState("");
  const [caState, setCAState] = useState<InspectorCAState>({
    present: false,
    trusted: false,
  });
  const supported = ready && session.gatewayCapabilities?.inspector === true;
  const serviceTargets = useMemo(
    () =>
      (session.services ?? []).flatMap((service) =>
        service.ports
          .filter((item) => item.protocol.toUpperCase() === "TCP")
          .map((item) => ({
            key: `${service.namespace}/${service.name}:${item.port}`,
            label: `${service.namespace}/${service.name}:${item.port}`,
            service,
            port: item.port,
          })),
      ),
    [session.services],
  );

  useEffect(
    () =>
      backend.onInspectorEvent((event) => {
        setFlows((current) => {
          const previous = current[event.flowId] ?? {
            id: event.flowId,
            bytes: 0,
            events: [],
          };
          const payload = event.payload ?? {};
          const next: Flow = {
            ...previous,
            method:
              event.type === 1 && typeof payload.method === "string"
                ? payload.method
                : previous.method,
            authority:
              event.type === 1 && typeof payload.authority === "string"
                ? payload.authority
                : previous.authority,
            path:
              event.type === 1 && typeof payload.path === "string"
                ? payload.path
                : previous.path,
            statusCode:
              typeof payload.statusCode === "number"
                ? payload.statusCode
                : previous.statusCode,
            grpcStatus:
              typeof payload.grpcStatus === "string"
                ? payload.grpcStatus
                : previous.grpcStatus,
            requestMessages:
              event.type === 6 && payload.direction === "request"
                ? (previous.requestMessages ?? 0) + 1
                : previous.requestMessages,
            responseMessages:
              event.type === 6 && payload.direction === "response"
                ? (previous.responseMessages ?? 0) + 1
                : previous.responseMessages,
            durationMs:
              typeof payload.durationMs === "number"
                ? payload.durationMs
                : previous.durationMs,
            error:
              event.type === 5 && typeof payload.error === "string"
                ? payload.error
                : previous.error,
            source:
              event.type === 1 && typeof payload.source === "string"
                ? payload.source
                : previous.source,
            bytes: previous.bytes + JSON.stringify(event).length * 2,
            events: [...previous.events.slice(-255), event],
          };
          const entries = Object.entries({ ...current, [event.flowId]: next }).slice(
            -maxFlows,
          );
          let totalBytes = entries.reduce((sum, [, flow]) => sum + flow.bytes, 0);
          while (entries.length > 1 && totalBytes > maxFlowBytes) {
            totalBytes -= entries.shift()?.[1].bytes ?? 0;
          }
          return Object.fromEntries(entries);
        });
      }),
    [],
  );

  useEffect(() => {
    let mounted = true;
    backend
      .getInspectorState()
      .then((state) => {
        if (!mounted) return;
        setActive(state.active);
        setTargets(state.targets ?? []);
      })
      .catch(() => undefined);
    backend
      .inspectorCAStatus()
      .then((state) => {
        if (mounted) setCAState(state);
      })
      .catch(() => undefined);
    return () => {
      mounted = false;
    };
  }, []);

  useEffect(() => {
    if (!ready) {
      setActive(false);
      setSelected("");
    }
  }, [ready]);

  const rows = useMemo(() => {
    const query = filter.trim().toLowerCase();
    return Object.values(flows)
      .filter((flow) =>
        query
          ? [
              flow.method,
              flow.authority,
              flow.path,
              flow.source,
              flow.grpcStatus,
              flow.statusCode,
              flow.error,
            ]
              .filter((value) => value !== undefined)
              .some((value) => String(value).toLowerCase().includes(query))
          : true,
      )
      .reverse();
  }, [filter, flows]);
  const selectedFlow = selected ? flows[selected] : undefined;

  async function applyTargets(next: InspectorTarget[]) {
    if (active) {
      const needsTLSRestart =
        next.some((target) => requiresInspectorCA(target.protocol)) &&
        !targets.some((target) => requiresInspectorCA(target.protocol));
      if (needsTLSRestart) {
        if (!caState.trusted) throw new Error(t("inspector.caRequired"));
        await backend.stopInspector();
        try {
          await backend.startInspector({ maxBodySize: 64 * 1024, targets: next });
        } catch (error) {
          setActive(false);
          throw error;
        }
      } else {
        await backend.updateInspectorTargets(next);
      }
    }
    setTargets(next);
  }

  async function addTarget() {
    const normalizedHost = host.trim().toLowerCase();
    const numericPort = Number(port);
    if (
      !normalizedHost ||
      !Number.isInteger(numericPort) ||
      numericPort < 1 ||
      numericPort > 65535
    ) {
      toast.error(t("inspector.invalidTarget"));
      return;
    }
    if (targets.some((item) => item.host === normalizedHost && item.port === numericPort)) {
      toast.error(t("inspector.duplicateTarget"));
      return;
    }
    const next = [
      ...targets,
      {
        id: `${normalizedHost}-${numericPort}`,
        host: normalizedHost,
        port: numericPort,
        protocol,
        captureBody,
        descriptorSet: protocol === "grpc" && descriptorSet ? descriptorSet : undefined,
        ...(serviceTarget
          ? (() => {
              const selectedService = serviceTargets.find(
                (item) => item.key === serviceTarget,
              );
              return selectedService
                ? {
                    namespace: selectedService.service.namespace,
                    service: selectedService.service.name,
                    serviceUID: selectedService.service.uid,
                    addresses: [selectedService.service.clusterIP],
                  }
                : {};
            })()
          : {}),
      },
    ];
    setBusy(true);
    try {
      await applyTargets(next);
      setHost("");
      setServiceTarget("");
      setDescriptorSet("");
    } catch (error) {
      toast.error(t("inspector.updateFailed"), { description: String(error) });
    } finally {
      setBusy(false);
    }
  }

  async function removeTarget(index: number) {
    setBusy(true);
    try {
      await applyTargets(targets.filter((_, itemIndex) => itemIndex !== index));
    } catch (error) {
      toast.error(t("inspector.updateFailed"), { description: String(error) });
    } finally {
      setBusy(false);
    }
  }

  async function toggle() {
    setBusy(true);
    try {
      if (active) {
        await backend.stopInspector();
        setActive(false);
      } else {
        if (
          targets.some((target) => requiresInspectorCA(target.protocol)) &&
          !caState.trusted
        ) {
          throw new Error(t("inspector.caRequired"));
        }
        await backend.startInspector({ maxBodySize: 64 * 1024, targets });
        setActive(true);
      }
    } catch (error) {
      toast.error(t("inspector.actionFailed"), { description: String(error) });
    } finally {
      setBusy(false);
    }
  }

  async function installCA() {
    setBusy(true);
    try {
      const state = await backend.installInspectorCA();
      setCAState(state);
      toast.success(t("inspector.caInstalled"));
    } catch (error) {
      toast.error(t("inspector.caInstallFailed"), { description: String(error) });
    } finally {
      setBusy(false);
    }
  }

  async function removeCA() {
    setBusy(true);
    try {
      if (active && targets.some((target) => requiresInspectorCA(target.protocol))) {
        await backend.stopInspector();
        setActive(false);
      }
      await backend.removeInspectorCA();
      setCAState({ present: false, trusted: false });
      toast.success(t("inspector.caRemoved"));
    } catch (error) {
      toast.error(t("inspector.caRemoveFailed"), { description: String(error) });
    } finally {
      setBusy(false);
    }
  }

  function clearFlows() {
    setFlows({});
    setSelected("");
  }

  function exportFlows() {
    const value = selectedFlow ? [selectedFlow] : rows;
    const blob = new Blob([JSON.stringify(value, null, 2)], {
      type: "application/json",
    });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = `kube-loop-inspector-${new Date().toISOString()}.json`;
    link.click();
    URL.revokeObjectURL(url);
  }

  return (
    <PageShell
      title={t("inspector.title")}
      description={t("inspector.description")}
      action={
        <Button
          type="button"
          size="sm"
          disabled={!supported || busy}
          onClick={() => void toggle()}
        >
          {active ? t("inspector.stop") : t("inspector.start")}
        </Button>
      }
    >
      {!supported ? (
        <div className="rounded-lg border bg-card p-5 text-sm text-muted-foreground">
          {ready ? t("inspector.unavailable") : t("inspector.disconnected")}
        </div>
      ) : null}

      <section className="flex items-center justify-between gap-4 rounded-lg border bg-card p-4">
        <div className="min-w-0">
          <h2 className="text-sm font-medium">{t("inspector.caTitle")}</h2>
          <p className="text-xs text-muted-foreground">
            {caState.trusted
              ? t("inspector.caTrusted")
              : caState.present
                ? t("inspector.caNotTrusted")
                : t("inspector.caMissing")}
          </p>
          {caState.fingerprint ? (
            <p className="mt-1 truncate font-mono text-[10px] text-muted-foreground">
              SHA-256 {caState.fingerprint}
            </p>
          ) : null}
          {caState.trustError ? (
            <p className="mt-1 text-xs text-destructive">{caState.trustError}</p>
          ) : null}
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {!caState.trusted ? (
            <Button type="button" variant="outline" disabled={busy} onClick={() => void installCA()}>
              {t("inspector.caInstall")}
            </Button>
          ) : null}
          {caState.present ? (
            <Button type="button" variant="outline" disabled={busy} onClick={() => void removeCA()}>
              {t("inspector.caRemove")}
            </Button>
          ) : null}
        </div>
      </section>

      <section className="space-y-3 rounded-lg border bg-card p-4">
        <div>
          <h2 className="text-sm font-medium">{t("inspector.targets")}</h2>
          <p className="text-xs text-muted-foreground">{t("inspector.targetsHint")}</p>
        </div>
        <div className="flex items-center gap-2">
          <select
            className="h-9 max-w-56 rounded-md border border-input bg-background px-2 text-sm"
            value={serviceTarget}
            onChange={(event) => {
              const value = event.target.value;
              setServiceTarget(value);
              const selectedService = serviceTargets.find(
                (item) => item.key === value,
              );
              if (selectedService) {
                setHost(
                  `${selectedService.service.name}.${selectedService.service.namespace}.svc`,
                );
                setPort(String(selectedService.port));
              }
            }}
            disabled={busy}
          >
            <option value="">Service / manual host</option>
            {serviceTargets.map((item) => (
              <option key={item.key} value={item.key}>
                {item.label}
              </option>
            ))}
          </select>
          <select
            className="h-9 rounded-md border border-input bg-background px-2 text-sm"
            value={protocol}
            onChange={(event) => {
              const value = event.target.value as InspectorTarget["protocol"];
              setProtocol(value);
              if (port === "80" || port === "443") {
                setPort(value === "http" ? "80" : "443");
              }
            }}
            disabled={busy}
          >
            <option value="http">HTTP</option>
            <option value="https">HTTPS</option>
            <option value="http2">HTTP/2</option>
            <option value="grpc">gRPC</option>
          </select>
          <input
            className="h-9 min-w-0 flex-1 rounded-md border border-input bg-background px-3 text-sm outline-none focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/40"
            value={host}
            onChange={(event) => {
              setHost(event.target.value);
              setServiceTarget("");
            }}
            placeholder={t("inspector.host")}
            disabled={busy}
          />
          <input
            className="h-9 w-28 rounded-md border border-input bg-background px-3 text-sm outline-none focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/40"
            type="number"
            min={1}
            max={65535}
            value={port}
            onChange={(event) => setPort(event.target.value)}
            placeholder={t("inspector.port")}
            disabled={busy}
          />
          <label className="flex shrink-0 items-center gap-2 text-xs text-muted-foreground">
            <input
              type="checkbox"
              checked={captureBody}
              onChange={(event) => setCaptureBody(event.target.checked)}
            />
            {t("inspector.captureBody")}
          </label>
          {protocol === "grpc" ? (
            <label className="shrink-0 text-xs text-muted-foreground">
              <span className="sr-only">FileDescriptorSet</span>
              <input
                className="max-w-44 text-xs"
                type="file"
                accept=".protoset,.pb,application/octet-stream"
                disabled={busy}
                onChange={(event) => {
                  const file = event.target.files?.[0];
                  if (!file) {
                    setDescriptorSet("");
                    return;
                  }
                  if (file.size > 32 * 1024) {
                    toast.error(t("inspector.invalidTarget"), {
                      description: "FileDescriptorSet must not exceed 32 KiB",
                    });
                    event.target.value = "";
                    setDescriptorSet("");
                    return;
                  }
                  void file.arrayBuffer().then((value) => {
                    setDescriptorSet(
                      btoa(String.fromCharCode(...new Uint8Array(value))),
                    );
                  });
                }}
              />
            </label>
          ) : null}
          <Button type="button" variant="outline" disabled={busy} onClick={() => void addTarget()}>
            {t("inspector.add")}
          </Button>
        </div>
        {targets.length === 0 ? (
          <p className="text-xs text-muted-foreground">{t("inspector.noTargets")}</p>
        ) : (
          <div className="flex flex-wrap gap-2">
            {targets.map((target, index) => (
              <div
                key={`${target.host}:${target.port}`}
                className="flex items-center gap-2 rounded-md border bg-muted/30 px-2.5 py-1.5 font-mono text-xs"
              >
                <span>{target.protocol.toUpperCase()} {target.host}:{target.port}</span>
                {target.captureBody ? (
                  <span className="text-muted-foreground">{t("inspector.bodyOn")}</span>
                ) : null}
                <button
                  type="button"
                  aria-label={t("inspector.remove")}
                  disabled={busy}
                  onClick={() => void removeTarget(index)}
                >
                  <Trash2 className="size-3.5 text-muted-foreground" />
                </button>
              </div>
            ))}
          </div>
        )}
      </section>

      <div className="grid min-h-0 grid-cols-[minmax(0,1fr)_minmax(280px,0.42fr)] gap-4">
        <div className="overflow-hidden rounded-lg border bg-card">
          <div className="flex items-center gap-2 border-b p-2">
            <input
              className="h-8 min-w-0 flex-1 rounded-md border border-input bg-background px-3 text-xs outline-none focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/40"
              value={filter}
              onChange={(event) => setFilter(event.target.value)}
              placeholder="Filter method, target, path, status, or source"
            />
            <Button type="button" size="sm" variant="outline" onClick={exportFlows}>
              <Download className="size-3.5" />
              Export
            </Button>
            <Button type="button" size="sm" variant="outline" onClick={clearFlows}>
              <Trash2 className="size-3.5" />
              Clear
            </Button>
          </div>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t("inspector.method")}</TableHead>
                <TableHead>{t("inspector.target")}</TableHead>
                <TableHead>{t("inspector.path")}</TableHead>
                <TableHead>{t("inspector.status")}</TableHead>
                <TableHead className="text-right">{t("inspector.duration")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={5} className="h-32 text-center text-muted-foreground">
                    <ScanSearch className="mx-auto mb-2 size-5" />
                    {t("inspector.empty")}
                  </TableCell>
                </TableRow>
              ) : (
                rows.map((flow) => (
                  <TableRow
                    key={flow.id}
                    className="cursor-pointer"
                    data-state={selected === flow.id ? "selected" : undefined}
                    onClick={() => setSelected(flow.id)}
                  >
                    <TableCell className="font-mono text-xs">{flow.method ?? "—"}</TableCell>
                    <TableCell className="max-w-48 truncate font-mono text-xs">
                      {flow.authority ?? "—"}
                    </TableCell>
                    <TableCell className="max-w-56 truncate font-mono text-xs">
                      {flow.path ?? "—"}
                    </TableCell>
                    <TableCell>
                      {flow.error
                        ? t("inspector.tlsBypass")
                        : flow.grpcStatus !== undefined
                          ? `gRPC ${flow.grpcStatus}`
                          : (flow.statusCode ?? "…")}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {flow.durationMs === undefined ? "…" : `${flow.durationMs} ms`}
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </div>
        <pre className="max-h-[430px] overflow-auto rounded-lg border bg-card p-3 text-[11px] leading-5 text-muted-foreground">
          {selectedFlow
            ? JSON.stringify(selectedFlow.events, null, 2)
            : t("inspector.selectFlow")}
        </pre>
      </div>
    </PageShell>
  );
}
