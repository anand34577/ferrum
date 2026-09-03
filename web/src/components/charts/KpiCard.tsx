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

/** Big-number KPI card with optional sparkline and progress rail — the
 * "immediate status without deep analysis" tier of the chart taxonomy. */
export function KpiCard({ label, value, sub, spark, sparkColor, sparkVariant, icon: Icon, tone = "default", progress, progressInvert }: KpiCardProps) {
  return (
    <div className="flex min-w-0 flex-col justify-between rounded-lg border border-[var(--border)] bg-[var(--bg-surface)] px-3.5 py-3 shadow-xs">
      <div className="flex items-center gap-1.5">
        {Icon && <Icon className="h-3.5 w-3.5 shrink-0 text-[var(--text-muted)]" />}
        <span className="truncate text-xs text-[var(--text-muted)]">{label}</span>
      </div>
      <p className={cn("mt-1.5 truncate font-display text-xl font-semibold leading-tight tabular", toneClass[tone])}>
        {value}
        {sub && <span className="ml-1.5 text-xs font-normal text-[var(--text-muted)]">{sub}</span>}
      </p>
      {progress !== undefined && (
        <div className="mt-1.5 h-1 overflow-hidden rounded-full bg-[var(--track)]" role="progressbar" aria-valuenow={Math.round(progress)} aria-valuemin={0} aria-valuemax={100} aria-label={label}>
          <div
            className={cn(
              "h-full rounded-full",
              progressInvert
                ? progress >= 99 ? "bg-brand-500" : progress >= 75 ? "bg-[var(--status-warn)]" : "bg-[var(--status-error)]"
                : progress > 90 ? "bg-[var(--status-error)]" : progress > 75 ? "bg-[var(--status-warn)]" : "bg-brand-500",
            )}
            style={{ width: `${Math.min(100, Math.max(0, progress))}%` }}
          />
        </div>
      )}
      {spark && spark.length > 1 && <Sparkline data={spark} color={sparkColor ?? "var(--chart-1)"} variant={sparkVariant} height={26} />}
    </div>
  )
}
