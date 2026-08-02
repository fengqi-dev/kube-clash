import { Activity, CheckCircle2, Loader2, XCircle } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";

export type SessionTestStatus = "running" | "success" | "error";

export interface SessionTestFlow {
  title: string;
  description: string;
  routes: Array<{
    label?: string;
    nodes: string[];
  }>;
}

export function SessionTestPanel({
  flow,
  status,
  error,
  failure,
  onClose,
}: {
  flow: SessionTestFlow | null;
  status: SessionTestStatus;
  error?: string;
  failure?: { route: number; segment: number };
  onClose(): void;
}) {
  const { t } = useI18n();
  if (!flow) return null;

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && status !== "running") onClose();
      }}
    >
      <DialogContent
        className="min-w-0 sm:max-w-2xl"
        showCloseButton={status !== "running"}
      >
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Activity className="size-4 text-primary" />
            {t("network.sessionTestTitle")}
          </DialogTitle>
          <DialogDescription>
            {flow.title} · {flow.description}
          </DialogDescription>
        </DialogHeader>

        <div className="min-w-0 space-y-3 rounded-lg border bg-muted/20 p-4">
          {flow.routes.map((route, routeIndex) => (
            <div
              key={`${route.label ?? "route"}-${routeIndex}`}
              className="min-w-0 space-y-2"
            >
              {route.label ? (
                <div className="text-[10px] font-medium tracking-wide text-muted-foreground uppercase">
                  {route.label}
                </div>
              ) : null}
              <div className="max-w-full overflow-x-auto pb-1">
                <div className="flex min-w-max items-center py-2">
                  {route.nodes.map((node, nodeIndex) => (
                    <div
                      key={`${node}-${nodeIndex}`}
                      className="contents"
                    >
                      <div
                        className={cn(
                          "max-w-44 shrink-0 rounded-md border bg-background px-3 py-2",
                          "break-words font-mono text-[11px] shadow-xs transition-colors",
                          status === "running" && "border-primary/40",
                          status === "success" && "border-success/45",
                        )}
                      >
                        {node}
                      </div>
                      {nodeIndex < route.nodes.length - 1 ? (
                        <div className="relative mx-2 h-8 w-14 shrink-0 overflow-hidden">
                          <span
                            className={cn(
                              "absolute top-1/2 right-0 left-0 h-px -translate-y-1/2",
                              segmentColor(status, failure, routeIndex, nodeIndex, true),
                            )}
                          />
                          <span
                            className={cn(
                              "absolute top-1/2 left-0 size-2 -translate-y-1/2 rounded-full",
                              status === "running"
                                ? "animate-session-flow bg-primary shadow-[0_0_8px_var(--primary)]"
                                : segmentColor(
                                    status,
                                    failure,
                                    routeIndex,
                                    nodeIndex,
                                    false,
                                  ),
                            )}
                            style={{ animationDelay: `${(routeIndex + nodeIndex) * 140}ms` }}
                          />
                        </div>
                      ) : null}
                    </div>
                  ))}
                </div>
              </div>
            </div>
          ))}
        </div>

        <div
          className={cn(
            "flex items-start gap-2 rounded-md border px-3 py-2.5 text-[12px]",
            status === "running" && "border-primary/25 bg-primary/5",
            status === "success" && "border-success/30 bg-success/8",
            status === "error" && "border-destructive/30 bg-destructive/8",
          )}
        >
          {status === "running" ? (
            <Loader2 className="mt-0.5 size-4 shrink-0 animate-spin text-primary" />
          ) : status === "success" ? (
            <CheckCircle2 className="mt-0.5 size-4 shrink-0 text-success" />
          ) : (
            <XCircle className="mt-0.5 size-4 shrink-0 text-destructive" />
          )}
          <div className="min-w-0">
            <div className="font-medium">
              {status === "running"
                ? t("network.testingSession")
                : status === "success"
                  ? t("network.sessionTestPassed")
                  : t("network.sessionTestFailed")}
            </div>
            {error ? (
              <div className="mt-0.5 break-words text-muted-foreground">{error}</div>
            ) : null}
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function segmentColor(
  status: SessionTestStatus,
  failure: { route: number; segment: number } | undefined,
  route: number,
  segment: number,
  track: boolean,
) {
  if (status === "running") return track ? "bg-primary/25" : "bg-primary";
  if (status === "success") {
    return track ? "bg-success/45" : "left-[calc(100%-0.5rem)] bg-success";
  }
  if (!failure) return track ? "bg-border" : "bg-muted-foreground";
  if (route < failure.route || (route === failure.route && segment < failure.segment)) {
    return track ? "bg-success/45" : "left-[calc(100%-0.5rem)] bg-success";
  }
  if (route === failure.route && segment === failure.segment) {
    return track
      ? "bg-destructive/55"
      : "left-1/2 -translate-x-1/2 bg-destructive";
  }
  return track ? "bg-border" : "hidden";
}
