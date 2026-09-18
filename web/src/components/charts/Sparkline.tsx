import { useId } from "react"
import { Area, AreaChart, Line, LineChart, ResponsiveContainer } from "recharts"

interface SparklineProps {
  /** Sample values; undefined gaps are bridged. */
  data: (number | undefined)[]
  color?: string
  height?: number
  /** Render as a line (no fill) — better for non-monotone counts. */
  variant?: "area" | "line"
}

/** Tiny inline trend chart without axes — gives KPI cards an at-a-glance
 * history without competing with the main charts for space. */
export function Sparkline({ data, color = "var(--chart-1)", height = 30, variant = "area" }: SparklineProps) {
  // useId, not just the color — two sparklines sharing a color would
  // otherwise emit duplicate <linearGradient id>s, and the browser renders
  // both using whichever <defs> came first. Same fix as GaugeChart. Called
  // unconditionally, before the early return below, per the Rules of Hooks.
  const autoId = useId()
  const rows = data.map((v, i) => ({ i, v: typeof v === "number" && Number.isFinite(v) ? v : undefined }))
  if (rows.length < 2) return <div style={{ height }} />
  const Chart = variant === "line" ? LineChart : AreaChart
  const gradId = `spark-${color.replace(/[^a-z0-9]/gi, "")}${autoId.replace(/[^a-z0-9]/gi, "")}`
  return (
    // Purely decorative trend cue — the KpiCard it lives in already states the
    // number and label in text, so screen readers should skip this entirely
    // rather than announce an unlabeled chart.
    <div className="pointer-events-none" style={{ height }} aria-hidden="true">
      <ResponsiveContainer width="100%" height="100%">
        <Chart data={rows} margin={{ top: 2, right: 0, left: 0, bottom: 0 }}>
          {variant === "area" && (
            <defs>
              <linearGradient id={gradId} x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor={color} stopOpacity={0.3} />
                <stop offset="100%" stopColor={color} stopOpacity={0} />
              </linearGradient>
            </defs>
          )}
          {variant === "area" ? (
            <Area
              type="monotone"
              dataKey="v"
              stroke={color}
              strokeWidth={1.5}
              fill={`url(#${gradId})`}
              connectNulls
              dot={false}
              isAnimationActive={false}
            />
          ) : (
            <Line type="monotone" dataKey="v" stroke={color} strokeWidth={1.5} connectNulls dot={false} isAnimationActive={false} />
          )}
        </Chart>
      </ResponsiveContainer>
    </div>
  )
}
