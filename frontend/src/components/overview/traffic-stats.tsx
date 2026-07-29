import {
  ArrowDown,
  ArrowUp,
  CloudDownload,
  CloudUpload,
  Link2,
  Microchip,
  type LucideIcon,
} from "lucide-react";
import { Card, CardContent } from "@/components/ui/card";
import { useTrafficHistory, type TrafficPoint } from "@/hooks/use-traffic-history";
import { useI18n } from "@/i18n";
import { formatBytes, formatSpeed } from "@/lib/format";
import type { Metrics } from "@/types";

const uploadColor = "#f59e0b";
const downloadColor = "#3b82f6";

export function TrafficStats({
  ready,
  metrics,
  updatedAt,
}: {
  ready: boolean;
  metrics?: Metrics;
  updatedAt?: string;
}) {
  const { t, language } = useI18n();
  const traffic = useTrafficHistory(ready, metrics, updatedAt);

  return (
    <section>
      <div className="mb-2.5 flex items-end justify-between gap-3">
        <h3 className="text-[13px] font-semibold tracking-tight">{t("traffic.title")}</h3>
        <div className="flex items-center gap-3 text-[11px] text-muted-foreground">
          <LegendDot color={uploadColor} label={t("traffic.upload")} />
          <LegendDot color={downloadColor} label={t("traffic.download")} />
          <span className="hidden rounded-md border px-1.5 py-0.5 text-[10px] sm:inline">
            {t("traffic.window")}
          </span>
        </div>
      </div>

      <Card className="gap-0 overflow-hidden py-0 shadow-none">
        <CardContent className="p-4 pb-3">
          <TrafficChart
            history={traffic.history}
            windowMs={traffic.windowMs}
            language={language}
          />
        </CardContent>

        <div className="grid grid-cols-2 gap-px border-t bg-border/60 sm:grid-cols-3 lg:grid-cols-6">
          <StatCell
            icon={ArrowUp}
            label={t("traffic.uploadSpeed")}
            value={formatSpeed(traffic.uploadSpeed)}
          />
          <StatCell
            icon={ArrowDown}
            label={t("traffic.downloadSpeed")}
            value={formatSpeed(traffic.downloadSpeed)}
          />
          <StatCell
            icon={Link2}
            label={t("traffic.activeConnections")}
            value={String(traffic.connections)}
          />
          <StatCell
            icon={CloudUpload}
            label={t("traffic.uploadTotal")}
            value={formatBytes(traffic.uploadTotal)}
          />
          <StatCell
            icon={CloudDownload}
            label={t("traffic.downloadTotal")}
            value={formatBytes(traffic.downloadTotal)}
          />
          <StatCell
            icon={Microchip}
            label={t("traffic.memory")}
            value={formatBytes(traffic.memory)}
          />
        </div>
      </Card>
    </section>
  );
}

function LegendDot({ color, label }: { color: string; label: string }) {
  return (
    <span className="inline-flex items-center gap-1.5">
      <span className="size-1.5 rounded-full" style={{ background: color }} />
      {label}
    </span>
  );
}

function StatCell({
  icon: Icon,
  label,
  value,
}: {
  icon: LucideIcon;
  label: string;
  value: string;
}) {
  return (
    <div className="flex min-w-0 items-center gap-2.5 bg-card px-3.5 py-3">
      <Icon size={14} strokeWidth={1.8} className="shrink-0 text-muted-foreground" />
      <div className="min-w-0">
        <div className="truncate text-[10px] text-muted-foreground">{label}</div>
        <div className="mt-0.5 truncate font-mono text-[13px] font-semibold tabular-nums tracking-tight">
          {value}
        </div>
      </div>
    </div>
  );
}

function TrafficChart({
  history,
  windowMs,
  language,
}: {
  history: TrafficPoint[];
  windowMs: number;
  language: string;
}) {
  const width = 720;
  const height = 160;
  const pad = { top: 12, right: 12, bottom: 28, left: 52 };
  const innerW = width - pad.left - pad.right;
  const innerH = height - pad.top - pad.bottom;
  const now = history.length > 0 ? history[history.length - 1].at : Date.now();
  const start = now - windowMs;
  const maxSpeed = Math.max(
    1,
    ...history.map((point) => Math.max(point.upload, point.download)),
  );
  const yMax = niceCeil(maxSpeed);

  const toX = (at: number) => pad.left + ((at - start) / windowMs) * innerW;
  const toY = (value: number) => pad.top + innerH - (value / yMax) * innerH;

  const uploadPath = polyline(history, toX, toY, "upload");
  const downloadPath = polyline(history, toX, toY, "download");

  const yTicks = [0, 0.5, 1].map((ratio) => ({
    value: yMax * ratio,
    y: toY(yMax * ratio),
  }));
  const xTicks = [0, 0.5, 1].map((ratio) => {
    const at = start + windowMs * ratio;
    return { at, x: toX(at) };
  });

  return (
    <div className="relative w-full overflow-hidden rounded-md bg-muted/20">
      <svg
        viewBox={`0 0 ${width} ${height}`}
        className="h-[160px] w-full"
        role="img"
        aria-label="traffic chart"
      >
        {yTicks.map((tick) => (
          <g key={tick.value}>
            <line
              x1={pad.left}
              x2={width - pad.right}
              y1={tick.y}
              y2={tick.y}
              stroke="currentColor"
              className="text-border"
              strokeWidth={1}
            />
            <text
              x={pad.left - 8}
              y={tick.y + 3}
              textAnchor="end"
              className="fill-muted-foreground"
              fontSize={10}
              fontFamily="ui-monospace, monospace"
            >
              {formatAxisSpeed(tick.value)}
            </text>
          </g>
        ))}
        {xTicks.map((tick) => (
          <text
            key={tick.at}
            x={tick.x}
            y={height - 8}
            textAnchor="middle"
            className="fill-muted-foreground"
            fontSize={10}
            fontFamily="ui-monospace, monospace"
          >
            {formatAxisTime(tick.at, language)}
          </text>
        ))}
        {downloadPath ? (
          <path
            d={downloadPath}
            fill="none"
            stroke={downloadColor}
            strokeWidth={2}
            strokeLinejoin="round"
            strokeLinecap="round"
          />
        ) : null}
        {uploadPath ? (
          <path
            d={uploadPath}
            fill="none"
            stroke={uploadColor}
            strokeWidth={2}
            strokeLinejoin="round"
            strokeLinecap="round"
          />
        ) : null}
      </svg>
      {history.length === 0 ? (
        <div className="pointer-events-none absolute inset-0 grid place-items-center text-xs text-muted-foreground">
          —
        </div>
      ) : null}
    </div>
  );
}

function polyline(
  history: TrafficPoint[],
  toX: (at: number) => number,
  toY: (value: number) => number,
  key: "upload" | "download",
) {
  if (history.length === 0) return "";
  return history
    .map((point, index) => {
      const command = index === 0 ? "M" : "L";
      return `${command}${toX(point.at).toFixed(1)} ${toY(point[key]).toFixed(1)}`;
    })
    .join(" ");
}

function niceCeil(value: number) {
  if (value <= 0) return 1024;
  const exponent = Math.floor(Math.log10(value));
  const fraction = value / 10 ** exponent;
  const nice = fraction <= 1 ? 1 : fraction <= 2 ? 2 : fraction <= 5 ? 5 : 10;
  return nice * 10 ** exponent;
}

function formatAxisSpeed(value: number) {
  if (value <= 0) return "0";
  return formatBytes(value).replace(" ", "");
}

function formatAxisTime(at: number, language: string) {
  return new Intl.DateTimeFormat(language === "zh-CN" ? "zh-CN" : "en-US", {
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(at);
}
