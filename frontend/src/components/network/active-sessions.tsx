import { useEffect, useRef, useState } from "react";
import { Activity, Copy, Loader2, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { backend } from "@/backend";
import {
  ActionTypeBadge,
  exchangeIcon,
  mirrorIcon,
  portForwardIcon,
  previewIcon,
} from "@/components/network/action-icons";
import {
  SessionTestPanel,
  type SessionTestFlow,
  type SessionTestStatus,
} from "@/components/network/session-test-panel";
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
  ConnectivityTestResult,
  InterceptInfo,
  PortForwardInfo,
  PreviewInfo,
} from "@/types";

export function ActiveSessions({
  contextName,
  ready,
  refreshKey,
  inventoryRevision = 0,
  scope = "network",
}: {
  contextName: string;
  ready: boolean;
  refreshKey: number;
  /** Informer-driven; reload after reconcile may stop stale bindings. */
  inventoryRevision?: number;
  /** Workload: pod PF only. Network: service PF + Exchange + Mirror + Preview. */
  scope?: "network" | "podPortForward";
}) {
  const { t } = useI18n();
  const [forwards, setForwards] = useState<PortForwardInfo[]>([]);
  const [exchanges, setExchanges] = useState<InterceptInfo[]>([]);
  const [mirrors, setMirrors] = useState<InterceptInfo[]>([]);
  const [previews, setPreviews] = useState<PreviewInfo[]>([]);
  const [busy, setBusy] = useState(false);
  const [testingSessionID, setTestingSessionID] = useState("");
  const [testFlow, setTestFlow] = useState<SessionTestFlow | null>(null);
  const [testStatus, setTestStatus] = useState<SessionTestStatus>("running");
  const [testError, setTestError] = useState("");
  const [testFailure, setTestFailure] = useState<
    { route: number; segment: number } | undefined
  >();
  const reloadGeneration = useRef(0);
  const podOnly = scope === "podPortForward";

  async function reload() {
    const generation = ++reloadGeneration.current;
    const [forwardItems, exchangeItems, mirrorItems, previewItems] = await Promise.all([
      backend.listPortForwards(),
      !podOnly && ready ? backend.listIntercepts() : Promise.resolve([]),
      !podOnly && ready ? backend.listMirrors() : Promise.resolve([]),
      !podOnly && ready ? backend.listPreviews() : Promise.resolve([]),
    ]);
    if (generation !== reloadGeneration.current) return;
    setForwards(
      forwardItems.filter(
        (item) =>
          item.context === contextName &&
          (podOnly ? item.kind === "pod" : item.kind === "service"),
      ),
    );
    setExchanges(exchangeItems);
    setMirrors(mirrorItems);
    setPreviews(previewItems);
  }

  useEffect(() => {
    setForwards([]);
    setExchanges([]);
    setMirrors([]);
    setPreviews([]);
  }, [contextName, podOnly]);

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
      reloadGeneration.current += 1;
    };
  }, [contextName, podOnly, ready, refreshKey, inventoryRevision, t]);

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

  async function stopMirror(id: string) {
    setBusy(true);
    try {
      await backend.stopIntercept(id);
      await reload();
      toast.success(t("mirror.stopped"));
    } catch (error) {
      toast.error(t("mirror.stopFailed"), {
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

  async function testForward(item: PortForwardInfo) {
    const startedAt = Date.now();
    setTestingSessionID(item.id);
    setTestFlow({
      title: `${item.kind}/${item.namespace}/${item.name}`,
      description: `${(item.protocol || "tcp").toUpperCase()} ${item.address} → :${item.remotePort}`,
      routes: [
        {
          nodes: [
            t("network.flowLocalClient"),
            item.address,
            t("network.flowDataPlane"),
            `${item.kind}/${item.namespace}/${item.name}:${item.remotePort}`,
          ],
        },
      ],
    });
    setTestStatus("running");
    setTestError("");
    setTestFailure(undefined);
    try {
      const result = await backend.testPortForward(item.id);
      await waitForTestAnimation(startedAt);
      applyTestResult(result, {
        "local-listener": { route: 0, segment: 0 },
      });
    } catch (error) {
      await waitForTestAnimation(startedAt);
      setTestStatus("error");
      setTestError(error instanceof Error ? error.message : String(error));
    } finally {
      setTestingSessionID("");
    }
  }

  async function testIntercept(
    item: InterceptInfo | PreviewInfo,
    kind: "exchange" | "mirror" | "preview",
  ) {
    const startedAt = Date.now();
    setTestingSessionID(item.id);
    const localTargets = item.locals
      .map(
        (port) =>
          `${(port.protocol || "tcp").toUpperCase()} ${port.localHost}:${port.localPort}`,
      )
      .join(" · ");
    const service = `${item.namespace}/${item.service}`;
    const routes =
      kind === "mirror"
        ? [
            {
              label: t("network.flowPrimary"),
              nodes: [
                t("network.flowClusterClient"),
                service,
                t("network.flowOriginalPods"),
              ],
            },
            {
              label: t("network.flowShadow"),
              nodes: [
                service,
                t("network.flowGatewayMirror"),
                localTargets,
              ],
            },
          ]
        : [
            {
              nodes: [
                t("network.flowClusterClient"),
                service,
                kind === "exchange"
                  ? t("network.flowGatewayExchange")
                  : t("network.flowGatewayPreview"),
                localTargets,
              ],
            },
          ];
    setTestFlow({
      title: `${t(
        kind === "exchange"
          ? "network.tabExchange"
          : kind === "mirror"
            ? "network.tabMirror"
            : "network.tabPreview",
      )} · ${service}`,
      description: localTargets,
      routes,
    });
    setTestStatus("running");
    setTestError("");
    setTestFailure(undefined);
    try {
      const result = await backend.testIntercept(item.id);
      await waitForTestAnimation(startedAt);
      applyTestResult(
        result,
        kind === "mirror"
          ? {
              "gateway-control": { route: 1, segment: 0 },
              "local-target": { route: 1, segment: 1 },
            }
          : {
              "gateway-control": { route: 0, segment: 1 },
              "local-target": { route: 0, segment: 2 },
            },
      );
    } catch (error) {
      await waitForTestAnimation(startedAt);
      setTestStatus("error");
      setTestError(error instanceof Error ? error.message : String(error));
    } finally {
      setTestingSessionID("");
    }
  }

  const empty =
    forwards.length === 0 &&
    exchanges.length === 0 &&
    mirrors.length === 0 &&
    previews.length === 0;

  function applyTestResult(
    result: ConnectivityTestResult,
    layers: Partial<
      Record<
        NonNullable<ConnectivityTestResult["failedLayer"]>,
        { route: number; segment: number }
      >
    >,
  ) {
    if (result.passed) {
      setTestStatus("success");
      return;
    }
    setTestStatus("error");
    setTestError(result.error || t("network.sessionTestFailed"));
    setTestFailure(result.failedLayer ? layers[result.failedLayer] : undefined);
  }

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
                  {(item.protocol || "tcp").toUpperCase()} {item.address} → :{item.remotePort}
                </TableCell>
                <TableCell>
                  <div className="flex gap-1">
                    <Button
                      type="button"
                      size="sm"
                      variant="ghost"
                      disabled={busy}
                      aria-label={`${t("portfwd.copyAddress")}: ${item.address}`}
                      onClick={() => void copyAddress(item.address)}
                    >
                      <Copy size={14} />
                    </Button>
                    <Button
                      type="button"
                      size="icon-sm"
                      variant="ghost"
                      disabled={busy || testingSessionID !== ""}
                      aria-label={`${t("network.testSession")}: ${item.kind}/${item.namespace}/${item.name}`}
                      onClick={() => void testForward(item)}
                    >
                      {testingSessionID === item.id ? (
                        <Loader2 size={14} className="animate-spin" />
                      ) : (
                        <Activity size={14} />
                      )}
                    </Button>
                    <Button
                      type="button"
                      size="sm"
                      variant="ghost"
                      disabled={busy}
                      aria-label={`${t("portfwd.stop")}: ${item.kind}/${item.namespace}/${item.name}`}
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
                  <div className="flex gap-1">
                    <Button
                      type="button"
                      size="icon-sm"
                      variant="ghost"
                      disabled={busy || testingSessionID !== ""}
                      aria-label={`${t("network.testSession")}: ${item.namespace}/${item.service}`}
                      onClick={() => void testIntercept(item, "exchange")}
                    >
                      {testingSessionID === item.id ? (
                        <Loader2 size={14} className="animate-spin" />
                      ) : (
                        <Activity size={14} />
                      )}
                    </Button>
                    <Button
                      type="button"
                      size="sm"
                      variant="ghost"
                      disabled={busy}
                      aria-label={`${t("portfwd.stop")}: ${item.namespace}/${item.service}`}
                      onClick={() => void stopExchange(item.id)}
                    >
                      {busy ? <Loader2 size={14} className="animate-spin" /> : <Trash2 size={14} />}
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            ))}
            {mirrors.map((item) => (
              <TableRow key={`mr-${item.id}`}>
                <TableCell>
                  <ActionTypeBadge
                    label={t("network.tabMirror")}
                    icon={mirrorIcon}
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
                  <div className="flex gap-1">
                    <Button
                      type="button"
                      size="icon-sm"
                      variant="ghost"
                      disabled={busy || testingSessionID !== ""}
                      aria-label={`${t("network.testSession")}: ${item.namespace}/${item.service}`}
                      onClick={() => void testIntercept(item, "mirror")}
                    >
                      {testingSessionID === item.id ? (
                        <Loader2 size={14} className="animate-spin" />
                      ) : (
                        <Activity size={14} />
                      )}
                    </Button>
                    <Button
                      type="button"
                      size="sm"
                      variant="ghost"
                      disabled={busy}
                      aria-label={`${t("portfwd.stop")}: ${item.namespace}/${item.service}`}
                      onClick={() => void stopMirror(item.id)}
                    >
                      {busy ? <Loader2 size={14} className="animate-spin" /> : <Trash2 size={14} />}
                    </Button>
                  </div>
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
                  <div className="flex gap-1">
                    <Button
                      type="button"
                      size="icon-sm"
                      variant="ghost"
                      disabled={busy || testingSessionID !== ""}
                      aria-label={`${t("network.testSession")}: ${item.namespace}/${item.service}`}
                      onClick={() => void testIntercept(item, "preview")}
                    >
                      {testingSessionID === item.id ? (
                        <Loader2 size={14} className="animate-spin" />
                      ) : (
                        <Activity size={14} />
                      )}
                    </Button>
                    <Button
                      type="button"
                      size="sm"
                      variant="ghost"
                      disabled={busy}
                      aria-label={`${t("portfwd.stop")}: ${item.namespace}/${item.service}`}
                      onClick={() => void stopPreview(item.id)}
                    >
                      {busy ? <Loader2 size={14} className="animate-spin" /> : <Trash2 size={14} />}
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
      <SessionTestPanel
        flow={testFlow}
        status={testStatus}
        error={testError}
        failure={testFailure}
        onClose={() => setTestFlow(null)}
      />
    </section>
  );
}

async function waitForTestAnimation(startedAt: number) {
  const remaining = 900 - (Date.now() - startedAt);
  if (remaining > 0) {
    await new Promise((resolve) => window.setTimeout(resolve, remaining));
  }
}
