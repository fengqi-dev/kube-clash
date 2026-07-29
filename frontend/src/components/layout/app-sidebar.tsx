import {
  ArrowRightLeft,
  Cable,
  Eye,
  Gauge,
  Network,
  Orbit,
  ScrollText,
  Settings2,
  ShieldCheck,
  Waypoints,
  type LucideIcon,
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import {
  isNetworkView,
  type AppView,
  type NetworkSubView,
} from "@/hooks/use-session";
import { useI18n, type TranslationKey } from "@/i18n";
import { cn } from "@/lib/utils";

const navigation: Array<{ id: Exclude<AppView, "settings" | NetworkSubView>; icon: LucideIcon }> = [
  { id: "overview", icon: Gauge },
  { id: "connections", icon: Waypoints },
  { id: "network", icon: Network },
  { id: "logs", icon: ScrollText },
];

const networkChildren: Array<{ id: NetworkSubView; icon: LucideIcon; label: TranslationKey }> = [
  { id: "network-portfwd", icon: Cable, label: "network.tabPortForward" },
  { id: "network-exchange", icon: ArrowRightLeft, label: "network.tabExchange" },
  { id: "network-preview", icon: Eye, label: "network.tabPreview" },
];

const navKeys: Record<AppView, TranslationKey> = {
  overview: "nav.overview",
  connections: "nav.connections",
  network: "nav.network",
  "network-portfwd": "network.tabPortForward",
  "network-exchange": "network.tabExchange",
  "network-preview": "network.tabPreview",
  logs: "nav.logs",
  settings: "nav.settings",
};

export function AppSidebar({
  view,
  ready,
  coreVersion,
  updateAvailable,
  onNavigate,
}: {
  view: AppView;
  ready: boolean;
  coreVersion?: string;
  updateAvailable: boolean;
  onNavigate(view: AppView): void;
}) {
  const { t } = useI18n();
  const networkActive = isNetworkView(view);

  return (
    <aside className="relative z-10 flex w-[220px] shrink-0 flex-col border-r border-sidebar-border bg-sidebar px-3 py-3 text-sidebar-foreground">
      <div className="window-drag flex h-11 items-center gap-2.5 px-2">
        <div className="grid size-8 place-items-center rounded-md bg-primary text-primary-foreground">
          <Orbit size={18} strokeWidth={1.9} />
        </div>
        <div className="min-w-0">
          <div className="truncate text-sm font-semibold tracking-tight">KubeLoop</div>
          <div className="truncate text-[11px] text-muted-foreground">Network client</div>
        </div>
      </div>

      <nav className="mt-5 space-y-0.5">
        {navigation.map(({ id, icon: Icon }) => {
          if (id === "network") {
            const parentActive = view === "network";
            return (
              <div key={id} className="space-y-0.5">
                <Button
                  type="button"
                  variant="ghost"
                  onClick={() => onNavigate("network")}
                  className={cn(
                    "h-9 w-full justify-start gap-2.5 rounded-md px-2.5 text-[13px] font-medium",
                    parentActive
                      ? "bg-sidebar-accent text-sidebar-accent-foreground hover:bg-sidebar-accent"
                      : networkActive
                        ? "text-foreground hover:bg-accent/70"
                        : "text-muted-foreground hover:bg-accent/70 hover:text-foreground",
                  )}
                >
                  <Icon
                    size={16}
                    strokeWidth={1.9}
                    className={networkActive ? "text-foreground" : "text-muted-foreground"}
                  />
                  {t(navKeys.network)}
                </Button>
                <div className="ml-3 space-y-0.5 border-l border-sidebar-border pl-2">
                  {networkChildren.map(({ id: childId, icon: ChildIcon, label }) => {
                    const active = view === childId;
                    return (
                      <Button
                        key={childId}
                        type="button"
                        variant="ghost"
                        onClick={() => onNavigate(childId)}
                        className={cn(
                          "h-8 w-full justify-start gap-2 rounded-md px-2 text-[12px] font-medium",
                          active
                            ? "bg-sidebar-accent text-sidebar-accent-foreground hover:bg-sidebar-accent"
                            : "text-muted-foreground hover:bg-accent/70 hover:text-foreground",
                        )}
                      >
                        <ChildIcon
                          size={14}
                          strokeWidth={1.9}
                          className={active ? "text-foreground" : "text-muted-foreground"}
                        />
                        {t(label)}
                      </Button>
                    );
                  })}
                </div>
              </div>
            );
          }

          const active = view === id;
          return (
            <Button
              key={id}
              type="button"
              variant="ghost"
              onClick={() => onNavigate(id)}
              className={cn(
                "h-9 w-full justify-start gap-2.5 rounded-md px-2.5 text-[13px] font-medium",
                active
                  ? "bg-sidebar-accent text-sidebar-accent-foreground hover:bg-sidebar-accent"
                  : "text-muted-foreground hover:bg-accent/70 hover:text-foreground",
              )}
            >
              <Icon
                size={16}
                strokeWidth={1.9}
                className={active ? "text-foreground" : "text-muted-foreground"}
              />
              {t(navKeys[id])}
              {id === "connections" && ready && (
                <span className="ml-auto size-1.5 rounded-full bg-success" />
              )}
            </Button>
          );
        })}
      </nav>

      <div className="mt-auto space-y-2 px-1 pb-1">
        <div className="rounded-md border border-sidebar-border bg-muted/40 px-3 py-2.5">
          <div className="flex items-center gap-2 text-[11px] font-medium text-muted-foreground">
            <ShieldCheck size={13} className="text-primary" />
            sing-box Core
          </div>
          <div className="mt-1.5 flex items-center justify-between gap-2">
            <span className="font-mono text-[11px] text-muted-foreground">
              {coreVersion ?? "v1.19.28"}
            </span>
            <Badge
              variant="secondary"
              className={cn(
                "rounded-md px-1.5 py-0 text-[10px] font-medium",
                ready && "bg-success/15 text-success",
              )}
            >
              {ready ? t("core.running") : t("core.onDemand")}
            </Badge>
          </div>
        </div>
        <Separator className="bg-sidebar-border" />
        <Button
          type="button"
          variant="ghost"
          onClick={() => onNavigate("settings")}
          className={cn(
            "h-9 w-full justify-start gap-2.5 rounded-md px-2.5 text-[13px] font-medium",
            view === "settings"
              ? "bg-sidebar-accent text-sidebar-accent-foreground hover:bg-sidebar-accent"
              : "text-muted-foreground hover:bg-muted hover:text-foreground",
          )}
        >
          <Settings2
            size={16}
            strokeWidth={1.9}
            className={view === "settings" ? "text-primary" : undefined}
          />
          {t("nav.settings")}
          {updateAvailable && <span className="ml-auto size-1.5 rounded-full bg-primary" />}
        </Button>
      </div>
    </aside>
  );
}

export { navKeys };
