import { CircleAlert, LoaderCircle, Power } from "lucide-react";
import { isBusyPhase } from "@/lib/phase";
import { cn } from "@/lib/utils";
import type { SessionState } from "@/types";

export function ConnectionOrb({
  phase,
  busy = false,
  disabled,
  ariaLabel,
  onClick,
}: {
  phase: SessionState["phase"];
  busy?: boolean;
  disabled?: boolean;
  ariaLabel: string;
  onClick(): void;
}) {
  const working = busy || isBusyPhase(phase);
  const ready = phase === "connected" && !working;
  const error = phase === "error" && !working;

  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onClick}
      aria-label={ariaLabel}
      aria-busy={working}
      className={cn(
        "relative grid size-[72px] place-items-center rounded-full border transition-all outline-none",
        "focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50",
        "disabled:pointer-events-none disabled:opacity-50",
        "active:translate-y-px active:scale-[0.98]",
        // Connected = running (click to stop)
        ready &&
          "border-[#30A46C]/40 bg-[#30A46C] text-white shadow-[0_8px_24px_-8px_rgba(48,164,108,0.55)] hover:bg-[#2b9462]",
        // Error
        error &&
          "border-destructive/40 bg-destructive text-white shadow-[0_8px_24px_-8px_rgba(229,72,77,0.45)] hover:bg-destructive/90",
        // Connecting / busy
        working &&
          "border-[#F5A524]/45 bg-[#F5A524]/15 text-[#C4841D] hover:bg-[#F5A524]/20",
        // Idle = start
        !ready &&
          !error &&
          !working &&
          "border-[#326CE5]/50 bg-[#326CE5] text-white shadow-[0_8px_24px_-8px_rgba(50,108,229,0.55)] hover:bg-[#2b5fd4]",
      )}
    >
      {working ? (
        <LoaderCircle size={30} strokeWidth={1.8} className="animate-spin" />
      ) : error ? (
        <CircleAlert size={30} strokeWidth={1.8} />
      ) : (
        <Power
          size={30}
          strokeWidth={1.8}
          className={cn(ready && "drop-shadow-sm")}
        />
      )}
      {ready ? (
        <span
          aria-hidden
          className="pointer-events-none absolute inset-[-5px] rounded-full border border-[#30A46C]/35"
        />
      ) : null}
    </button>
  );
}
