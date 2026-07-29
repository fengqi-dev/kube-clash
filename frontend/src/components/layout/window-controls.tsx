import { Maximize2, Minus, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/i18n";
import { Quit, WindowMinimise, WindowToggleMaximise } from "../../../wailsjs/runtime/runtime";

export function WindowControls() {
  const { t } = useI18n();
  return (
    <div className="window-no-drag ml-1 flex items-center overflow-hidden rounded-lg border bg-card">
      <Button
        type="button"
        variant="ghost"
        size="icon"
        aria-label={t("window.minimise")}
        title={t("window.minimise")}
        onClick={() => WindowMinimise()}
        className="rounded-none"
      >
        <Minus strokeWidth={1.8} />
      </Button>
      <Button
        type="button"
        variant="ghost"
        size="icon"
        aria-label={t("window.maximise")}
        title={t("window.maximise")}
        onClick={() => WindowToggleMaximise()}
        className="rounded-none border-x"
      >
        <Maximize2 size={13} strokeWidth={1.8} />
      </Button>
      <Button
        type="button"
        variant="ghost"
        size="icon"
        aria-label={t("window.close")}
        title={t("window.close")}
        onClick={() => Quit()}
        className="rounded-none hover:bg-destructive hover:text-white"
      >
        <X strokeWidth={1.8} />
      </Button>
    </div>
  );
}
