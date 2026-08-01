import { Copy } from "lucide-react";
import { PageShell } from "@/components/shared/page-shell";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { useI18n } from "@/i18n";
import { phaseKeys } from "@/lib/phase";
import { cn } from "@/lib/utils";
import type { SessionState } from "@/types";

type DisplayLogLevel = "INFO" | "WARN" | "ERROR";

function displayLogLevel(level: string): DisplayLogLevel {
  if (level === "ERROR") return "ERROR";
  if (level === "WARN") return "WARN";
  return "INFO";
}

export function LogsView({
  session,
  error,
}: {
  session: SessionState;
  error: string;
}) {
  const { locale, t } = useI18n();

  const lines = (session.events ?? []).map((event) => ({
    time: new Date(event.time).toLocaleTimeString(locale, { hour12: false }),
    level: displayLogLevel(event.level),
    text: event.message,
  }));

  if (lines.length === 0) {
    const time = new Date(session.updatedAt).toLocaleTimeString(locale, { hour12: false });
    lines.push({
      time,
      level: error ? "ERROR" : "INFO",
      text: error || t(phaseKeys[session.phase]),
    });
    if (session.discovery) {
      lines.push(
        {
          time,
          level: "INFO",
          text: t("logs.podCIDRsFound", { count: session.discovery.podCIDRs.length }),
        },
        {
          time,
          level: "INFO",
          text: t("logs.servicesFound", { count: session.discovery.serviceIPs.length }),
        },
        {
          time,
          level: "INFO",
          text: `CoreDNS ${session.discovery.dnsServer || t("network.notFound")}`,
        },
      );
    }
  }

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
            <LogLine key={`${line.time}-${line.text}-${index}`} {...line} />
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
  level: DisplayLogLevel;
  text: string;
}) {
  return (
    <div className="grid grid-cols-[82px_62px_1fr] border-b px-4 py-3 last:border-0">
      <span className="text-muted-foreground">{time}</span>
      <span
        className={cn(
          level === "ERROR"
            ? "text-destructive"
            : level === "WARN"
              ? "text-amber-600 dark:text-amber-400"
              : "text-primary/80",
        )}
      >
        {level}
      </span>
      <span className="break-words text-muted-foreground">{text}</span>
    </div>
  );
}
