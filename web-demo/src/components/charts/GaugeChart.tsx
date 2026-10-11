import { useId } from "react"
import { cn } from "@/lib/utils"

interface GaugeChartProps {
  /** 0-100 percent; the remaining arc stays visible as a muted track. */
  value: number
  label: string
  size?: number
}

function gaugeColor(value: number): string {
  if (value >= 90) return "var(--status-error)"
  if (value >= 75) return "var(--status-warn)"
  return "var(--color-brand-500, #bd5a2c)"
}

/**
 * Circular percent gauge drawn as plain SVG (stroke-dasharray over a full
 * track circle). Rewritten from a recharts RadialBarChart because that
 * rendered the "unfilled" remainder invisible — the track now always shows,
 * the arc length is exactly value/100 of the circle, and the whole thing is
 * crisp at any size in both themes.
 */
export function GaugeChart({ value, label, size = 110 }: GaugeChartProps) {
  const clamped = Math.max(0, Math.min(100, value))
  const color = gaugeColor(clamped)
  // useId, not just the label — two gauges sharing a label (e.g. two guests
  // both showing "CPU") would otherwise emit duplicate <linearGradient id>s,
  // and the browser renders both using whichever <defs> came first.
  const autoId = useId()
  const gradId = `gauge-${label.replace(/[^a-z0-9]/gi, "")}${autoId.replace(/[^a-z0-9]/gi, "")}`

  const stroke = 7
  const r = (size - stroke) / 2
  const c = 2 * Math.PI * r
  const arc = (clamped / 100) * c

  return (
    <div className="flex flex-col items-center" style={{ width: size }}>
      <div className="relative" style={{ width: size, height: size }}>
        <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} role="img" aria-label={`${label}: ${Math.round(clamped)}%`}>
          <defs>
            {/* Sweeps from a lighter tint into the true status color, following
                the arc's own direction — a flat stroke read fine but static;
                this reads as one continuous fill rather than a painted-on ring. */}
            <linearGradient id={gradId} x1="0%" y1="0%" x2="100%" y2="100%">
              <stop offset="0%" stopColor={color} stopOpacity={0.55} />
              <stop offset="100%" stopColor={color} />
            </linearGradient>
          </defs>
          {/* Track — the unfilled remainder, always visible. --track, not
              --bg-muted: the muted token is near-invisible on dark surfaces,
              which read as a transparent/missing ring. */}
          <circle cx={size / 2} cy={size / 2} r={r} fill="none" stroke="var(--track)" strokeWidth={stroke} />
          {/* Value arc — starts at 12 o'clock, sweeps clockwise. */}
          <circle
            cx={size / 2}
            cy={size / 2}
            r={r}
            fill="none"
            stroke={`url(#${gradId})`}
            strokeWidth={stroke}
            strokeLinecap="butt"
            strokeDasharray={`${arc} ${c - arc}`}
            transform={`rotate(-90 ${size / 2} ${size / 2})`}
            style={{ transition: "stroke-dasharray 500ms ease" }}
          />
        </svg>
        <div className="pointer-events-none absolute inset-0 flex items-center justify-center">
          <span className={cn("font-display text-lg font-semibold", clamped >= 90 && "text-[var(--status-error)]")}>{Math.round(clamped)}%</span>
        </div>
      </div>
      <span className="mt-1 text-xs text-[var(--text-muted)]">{label}</span>
    </div>
  )
}
