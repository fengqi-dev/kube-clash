import { Settings2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { WindowControls } from "@/components/layout/window-controls";
import { navKeys } from "@/components/layout/app-sidebar";
import type { AppView } from "@/hooks/use-session";
import { useI18n, type TranslationKey } from "@/i18n";

const headerKeys: Record<AppView, TranslationKey> = {
  overview: "header.overview",
  clusters: "header.clusters",
  connections: "header.connections",
  workload: "header.workload",
  network: "header.network",
  "host-aliases": "header.hostAliases",
  logs: "header.logs",
  mcp: "header.mcp",
  settings: "header.settings",
};

export function AppHeader({
  view,
  onOpenSettings,
}: {
  view: AppView;
  onOpenSettings(): void;
}) {
  const { t } = useI18n();

  return (
    <header className="window-drag flex h-14 shrink-0 items-center justify-between border-b border-border bg-background px-6">
      <div>
        <h1 className="text-sm font-semibold tracking-tight">{t(navKeys[view])}</h1>
        <p className="mt-0.5 text-[12px] text-muted-foreground">{t(headerKeys[view])}</p>
      </div>
      <div className="window-no-drag flex items-center gap-2">
        <Button
          type="button"
          variant="outline"
          size="icon"
          aria-label={t("nav.settings")}
          onClick={onOpenSettings}
        >
          <Settings2 size={16} />
        </Button>
        <WindowControls />
      </div>
    </header>
  );
}
