import { CircleAlert, LoaderCircle, Wifi, WifiOff } from "lucide-react";
import { isBusyPhase } from "@/lib/phase";
import { cn } from "@/lib/utils";
import type { SessionState } from "@/types";

export function ConnectionOrb({
  phase,
  disabled,
  ariaLabel,
  onClick,
}: {
  phase: SessionState["phase"];
  disabled?: boolean;
  ariaLabel: string;
  onClick(): void;
}) {
  const working = isBusyPhase(phase);
  const ready = phase === "connected";
  const error = phase === "error";

  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onClick}
      aria-label={ariaLabel}
      className={cn(
        "relative grid size-[72px] place-items-center rounded-full border transition-colors outline-none",
        "focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50",
        "disabled:pointer-events-none disabled:opacity-50",
        "active:translate-y-px",
        ready && "border-success/30 bg-success/10 text-success hover:bg-success/15",
        error && "border-destructive/30 bg-destructive/10 text-destructive hover:bg-destructive/15",
        working && "border-primary/35 bg-primary/10 text-primary hover:bg-primary/15",
        !ready &&
          !error &&
          !working &&
          "border-primary/80 bg-primary text-primary-foreground hover:bg-primary/90",
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
    </button>
  );
}
