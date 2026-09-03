import type { LucideIcon } from "lucide-react"
import { Sparkline } from "@/components/charts/Sparkline"
import { cn } from "@/lib/utils"

interface KpiCardProps {
  label: string
  /** The headline number, already formatted (e.g. "23%", "1.4 GB/s"). */
  value: string
  /** Secondary context line, e.g. "of 64 GB" or "4 cores". */
  sub?: string
  /** Recent history for the inline sparkline (oldest → newest). */
  spark?: (number | undefined)[]
  sparkColor?: string
  sparkVariant?: "area" | "line"
  icon?: LucideIcon
  /** Colors the value when a threshold is crossed. */
  tone?: "default" | "warn" | "error" | "ok"
  /** 0..100 fills a thin progress rail under the value (optional). */
  progress?: number
  /** Rail thresholds assume utilization (full = bad). Set for coverage-style
   * metrics like "servers online", where a full rail means healthy. */
  progressInvert?: boolean
}

const toneClass: Record<NonNullable<KpiCardProps["tone"]>, string> = {
  default: "text-[var(--text)]",
  ok: "text-[var(--status-ok)]",
  warn: "text-[var(--status-warn)]",
  error: "text-[var(--status-error)]",
}

const toneCardBorder: Record<NonNullable<KpiCardProps["tone"]>, string> = {
  default: "hover:border-[var(--border-strong)]",
  ok: "hover:border-[var(--status-ok)]/40",
  warn: "border-[color-mix(in_oklab,var(--status-warn)_35%,var(--border))] hover:border-[var(--status-warn)]/60",
  error: "border-[color-mix(in_oklab,var(--status-error)_35%,var(--border))] hover:border-[var(--status-error)]/60",
}

const toneIconBg: Record<NonNullable<KpiCardProps["tone"]>, string> = {
  default: "bg-[var(--bg-muted)] text-[var(--text-muted)] border-[var(--border)]",
  ok: "bg-[color-mix(in_oklab,var(--status-ok)_12%,transparent)] text-[var(--status-ok)] border-[color-mix(in_oklab,var(--status-ok)_25%,transparent)]",
  warn: "bg-[color-mix(in_oklab,var(--status-warn)_12%,transparent)] text-[var(--status-warn)] border-[color-mix(in_oklab,var(--status-warn)_25%,transparent)]",
  error: "bg-[color-mix(in_oklab,var(--status-error)_12%,transparent)] text-[var(--status-error)] border-[color-mix(in_oklab,var(--status-error)_25%,transparent)]",
}

/** Big-number KPI card with optional sparkline and progress rail — the
 * "immediate status without deep analysis" tier of the chart taxonomy. */
export function KpiCard({
  label,
  value,
  sub,
  spark,
  sparkColor,
  sparkVariant,
  icon: Icon,
  tone = "default",
  progress,
  progressInvert,
}: KpiCardProps) {
  return (
    <div
      className={cn(
        "corner-frame group relative flex min-w-0 flex-col justify-between rounded-lg border border-[var(--border)] bg-[var(--bg-surface)] px-4.5 py-4 transition-colors duration-200",
        toneCardBorder[tone],
      )}
    >
      {/* Engraved panel-label register, not a casual metric caption. */}
      <div className="flex items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2">
          {Icon && (
            <div
              className={cn(
                "flex h-6 w-6 shrink-0 items-center justify-center rounded-sm border text-xs transition-colors",
                toneIconBg[tone],
              )}
            >
              <Icon className="h-3.5 w-3.5" />
            </div>
          )}
          <span className="panel-label truncate text-[10px] text-[var(--text-muted)]">{label}</span>
        </div>
      </div>
      {/* The digit-bank readout: monospace, tabular, no display-face flourish. */}
      <p className={cn("mt-3 truncate font-mono text-2xl font-bold leading-none tracking-tight tabular", toneClass[tone])}>
        {value}
        {sub && <span className="ml-2 font-sans text-xs font-normal normal-case tracking-normal text-[var(--text-muted)]">{sub}</span>}
      </p>
      {progress !== undefined && (
        <div
          className="mt-3.5 h-1.5 overflow-hidden rounded-sm bg-[var(--track)] p-px"
          role="progressbar"
          aria-valuenow={Math.round(progress)}
          aria-valuemin={0}
          aria-valuemax={100}
          aria-label={label}
        >
          <div
            className={cn(
              "h-full rounded-sm transition-all duration-500 ease-out",
              progressInvert
                ? progress >= 99
                  ? "bg-brand-500"
                  : progress >= 75
                    ? "bg-[var(--status-warn)] shadow-[var(--caution-glow-warn)]"
                    : "bg-[var(--status-error)] shadow-[var(--caution-glow-error)]"
                : progress > 90
                  ? "bg-[var(--status-error)] shadow-[var(--caution-glow-error)]"
                  : progress > 75
                    ? "bg-[var(--status-warn)] shadow-[var(--caution-glow-warn)]"
                    : "bg-brand-500",
            )}
            style={{ width: `${Math.min(100, Math.max(0, progress))}%` }}
          />
        </div>
      )}
      {spark && spark.length > 1 && (
        <div className="mt-2.5">
          <Sparkline data={spark} color={sparkColor ?? "var(--chart-1)"} variant={sparkVariant} height={26} />
        </div>
      )}
    </div>
  )
}
