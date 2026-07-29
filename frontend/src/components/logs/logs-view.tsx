import { Copy } from "lucide-react";
import { PageShell } from "@/components/shared/page-shell";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { useI18n } from "@/i18n";
import { phaseKeys } from "@/lib/phase";
import { cn } from "@/lib/utils";
import type { SessionState } from "@/types";

export function LogsView({
  session,
  error,
}: {
  session: SessionState;
  error: string;
}) {
  const { locale, t } = useI18n();
  const time = new Date(session.updatedAt).toLocaleTimeString(locale, { hour12: false });
  const lines = [
    { time, level: error ? ("ERROR" as const) : ("INFO" as const), text: error || t(phaseKeys[session.phase]) },
    ...(session.discovery
      ? [
          {
            time,
            level: "INFO" as const,
            text: t("logs.podCIDRsFound", { count: session.discovery.podCIDRs.length }),
          },
          {
            time,
            level: "INFO" as const,
            text: t("logs.servicesFound", { count: session.discovery.serviceIPs.length }),
          },
          {
            time,
            level: "INFO" as const,
            text: `CoreDNS ${session.discovery.dnsServer || t("network.notFound")}`,
          },
        ]
      : []),
  ];

  async function copyDiagnostics() {
    const payload = lines.map((line) => `${line.time} ${line.level} ${line.text}`).join("\n");
    try {
      await navigator.clipboard.writeText(payload);
    } catch {
      // Clipboard may be unavailable in some desktop webviews.
    }
  }

  return (
    <PageShell
      title={t("logs.title")}
      description={t("logs.description")}
      action={
        <Button type="button" variant="outline" size="sm" onClick={() => void copyDiagnostics()}>
          <Copy data-icon="inline-start" />
          {t("logs.copy")}
        </Button>
      }
    >
      <div className="overflow-hidden rounded-lg border bg-[var(--console)] font-mono text-[11px]">
        <ScrollArea className="h-[420px]">
          {lines.map((line, index) => (
            <LogLine key={`${line.text}-${index}`} {...line} />
          ))}
        </ScrollArea>
      </div>
    </PageShell>
  );
}

function LogLine({
  time,
  level,
  text,
}: {
  time: string;
  level: "INFO" | "ERROR";
  text: string;
}) {
  return (
    <div className="grid grid-cols-[82px_62px_1fr] border-b px-4 py-3 last:border-0">
      <span className="text-muted-foreground">{time}</span>
      <span className={cn(level === "ERROR" ? "text-destructive" : "text-primary/80")}>{level}</span>
      <span className="break-words text-muted-foreground">{text}</span>
    </div>
  );
}
