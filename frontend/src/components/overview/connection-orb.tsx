import { CircleAlert, LoaderCircle, Wifi, WifiOff } from "lucide-react";
import { isBusyPhase } from "@/lib/phase";
import { cn } from "@/lib/utils";
import type { SessionState } from "@/types";

export function ConnectionOrb({ phase }: { phase: SessionState["phase"] }) {
  const working = isBusyPhase(phase);
  const ready = phase === "connected";
  const error = phase === "error";

  return (
    <div className="relative">
      <div
        className={cn(
          "relative grid size-[72px] place-items-center rounded-lg border",
          ready && "border-success/30 bg-success/10 text-success",
          error && "border-destructive/30 bg-destructive/10 text-destructive",
          working && "border-primary/35 bg-primary/10 text-primary",
          !ready && !error && !working && "border-border bg-muted text-muted-foreground",
        )}
      >
        {working ? (
          <LoaderCircle size={30} strokeWidth={1.6} className="animate-spin" />
        ) : ready ? (
          <Wifi size={30} strokeWidth={1.6} />
        ) : error ? (
          <CircleAlert size={30} strokeWidth={1.6} />
        ) : (
          <WifiOff size={30} strokeWidth={1.6} />
        )}
      </div>
    </div>
  );
}
