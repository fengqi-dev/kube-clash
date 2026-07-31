import type { ReactNode } from "react";
import { toast } from "sonner";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";

function isCopyable(value?: string | null): value is string {
  const text = value?.trim() ?? "";
  return Boolean(text) && text !== "None" && text !== "none";
}

export function CopyableText({
  value,
  label,
  className,
  empty = "—",
  titleKey = "network.copyIP",
  successKey = "network.ipCopied",
  failKey = "network.ipCopyFailed",
}: {
  value?: string | null;
  label?: ReactNode;
  className?: string;
  empty?: string;
  titleKey?: "network.copyIP" | "network.copyAddress";
  successKey?: "network.ipCopied" | "network.addressCopied";
  failKey?: "network.ipCopyFailed" | "network.addressCopyFailed";
}) {
  const { t } = useI18n();
  if (!isCopyable(value)) {
    return <span className={className}>{label ?? empty}</span>;
  }

  const text = value;
  async function copy() {
    try {
      await navigator.clipboard.writeText(text);
      toast.success(t(successKey), { description: text });
    } catch {
      toast.error(t(failKey));
    }
  }

  return (
    <button
      type="button"
      title={t(titleKey)}
      onClick={() => void copy()}
      className={cn(
        "cursor-pointer rounded px-0.5 -mx-0.5 text-left transition-colors hover:bg-muted hover:text-foreground",
        className,
      )}
    >
      {label ?? text}
    </button>
  );
}
