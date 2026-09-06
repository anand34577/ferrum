import { cn } from "@/lib/utils"
import { utilizationTone } from "@/lib/fleet"

// Glow colors are derived from the actual live token (accent or status),
// not a hardcoded rgba() snapshot of one look's default — a fixed rgba here
// used to glow orange (Oxide's default accent) or stock Tailwind amber/red
// even under a different accent choice or a look with its own status ramp.
const fillTone: Record<"ok" | "warn" | "error", string> = {
  ok: "bg-brand-500 shadow-[0_0_6px_color-mix(in_oklab,var(--color-brand-500)_35%,transparent)]",
  warn: "bg-[var(--status-warn)] shadow-[0_0_8px_color-mix(in_oklab,var(--status-warn)_35%,transparent)]",
  error: "bg-[var(--status-error)] shadow-[0_0_8px_color-mix(in_oklab,var(--status-error)_40%,transparent)]",
}

const textTone: Record<"ok" | "warn" | "error", string> = {
  ok: "text-[var(--status-ok)]",
  warn: "text-[var(--status-warn)]",
  error: "text-[var(--status-error)]",
}

const sizeClass = {
  xs: "h-1",
  sm: "h-1.5",
  md: "h-2",
} as const

/** Canonical usage-percentage bar — CPU/memory/disk/wearout gauges, etc.
 * Single source of truth for thresholds (`utilizationTone`), color, and
 * accessibility so every meter in the app looks and behaves the same. */
export function Meter({
  value,
  size = "sm",
  invert = false,
  showLabel = false,
  label,
  className,
  trackClassName,
}: {
  /** 0..100. */
  value: number
  size?: keyof typeof sizeClass
  /** Set for coverage-style metrics where a full bar is healthy (e.g. "%
   * online") instead of the default utilization semantics (full = bad). */
  invert?: boolean
  /** Render the rounded percentage next to the bar. */
  showLabel?: boolean
  /** Accessible name; defaults to a generic "Usage" if omitted. */
  label?: string
  className?: string
  trackClassName?: string
}) {
  const pct = Math.min(100, Math.max(0, value))
  const tone = invert ? (pct >= 99 ? "ok" : pct >= 75 ? "warn" : "error") : utilizationTone(pct)

  return (
    <div className={cn("flex min-w-0 items-center gap-2", className)}>
      <div
        className={cn("w-full min-w-8 overflow-hidden rounded-none bg-[var(--track)]", sizeClass[size], trackClassName)}
        role="progressbar"
        aria-valuenow={Math.round(pct)}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-label={label ?? "Usage"}
      >
        <div
          className={cn("h-full rounded-none transition-all duration-300 ease-out", fillTone[tone])}
          style={{ width: `${pct}%` }}
        />
      </div>
      {showLabel && (
        <span className={cn("w-11 shrink-0 text-right text-xs font-semibold tabular", textTone[tone])}>
          {pct.toFixed(0)}%
        </span>
      )}
    </div>
  )
}

/** A single stacked horizontal bar with a legend — used for the guest
 * running/stopped mix, storage-by-type rollup, and similar composition
 * breakdowns. */
export function SplitMeter({
  segments,
  total,
  formatValue,
  size = "sm",
}: {
  segments: { label: string; value: number; color: string }[]
  total: number
  formatValue?: (v: number) => string
  size?: keyof typeof sizeClass
}) {
  const fmt = formatValue ?? ((v: number) => String(v))
  return (
    <div>
      <div
        className={cn("flex w-full overflow-hidden rounded-none bg-[var(--track)]", sizeClass[size])}
        role="img"
        aria-label={segments.map((s) => `${s.label} ${fmt(s.value)}`).join(", ")}
      >
        {total > 0 &&
          segments
            .filter((s) => s.value > 0)
            .map((s) => <div key={s.label} className="h-full" style={{ width: `${(s.value / total) * 100}%`, background: s.color }} />)}
      </div>
      <div className="mt-1.5 flex flex-wrap gap-x-3 gap-y-0.5 text-[10px] text-[var(--text-muted)]">
        {segments
          .filter((s) => s.value > 0)
          .map((s) => (
            <span key={s.label} className="flex items-center gap-1">
              <span className="h-1.5 w-1.5 rounded-full" style={{ background: s.color }} aria-hidden />
              {s.label} <span className="tabular">{fmt(s.value)}</span>
              {total > 0 && <span className="text-[var(--text-faint)] tabular">{Math.round((s.value / total) * 100)}%</span>}
            </span>
          ))}
        {total === 0 && <span>Nothing to show yet.</span>}
      </div>
    </div>
  )
}
