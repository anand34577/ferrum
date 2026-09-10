import { Cell, Pie, PieChart, ResponsiveContainer, Tooltip } from "recharts"
import { chartTooltip } from "@/components/charts/tooltipTheme"

export interface DonutSlice {
  name: string
  value: number
  color: string
}

interface DonutChartProps {
  data: DonutSlice[]
  height?: number
  centerLabel?: string
  centerValue?: string
  /** Formats a slice's raw value for the tooltip — e.g. bytes, percent, or plain counts (the default). */
  formatValue?: (v: number) => string
}

export function DonutChart({ data, height = 180, centerLabel, centerValue, formatValue = (v) => String(v) }: DonutChartProps) {
  const total = data.reduce((sum, d) => sum + d.value, 0)
  // Text alternative for screen readers — the visual center label only carries
  // the total, so the full breakdown goes in the accessible name instead.
  const summary =
    (centerValue ? `${centerLabel ? `${centerLabel}: ` : ""}${centerValue}. ` : "") +
    data.map((d) => `${d.name} ${formatValue(d.value)}${total > 0 ? ` (${((d.value / total) * 100).toFixed(0)}%)` : ""}`).join(", ")

  return (
    // w-full is load-bearing: inside a flex-col items-center parent the div
    // would collapse to min-content and ResponsiveContainer would render a
    // squeezed donut with the center label wrapped vertically (seen on the
    // Storage page). Callers size it via a fixed-width wrapper if needed.
    <div className="relative w-full" style={{ height }} role="img" aria-label={summary}>
      <ResponsiveContainer width="100%" height="100%">
        <PieChart>
          <Pie
            data={data}
            dataKey="value"
            nameKey="name"
            innerRadius="62%"
            outerRadius="90%"
            paddingAngle={total > 0 ? 2 : 0}
            // No entry animation: the data behind these donuts is a 15s
            // react-query poll, so every refresh would re-run it — the ring
            // spends that time as a partial arc with the "remaining" part
            // invisible. A donut that's always complete reads calmer.
            isAnimationActive={false}
          >
            {data.map((d) => (
              <Cell key={d.name} fill={d.color} stroke="none" />
            ))}
          </Pie>
          <Tooltip
            formatter={(value) => formatValue(Number(value))}
            // Pinned below the ring instead of following the cursor: a
            // donut this small has its hover point right next to (or over)
            // the center label, and the default cursor-following position
            // rendered the tooltip box on top of it.
            position={{ y: height }}
            allowEscapeViewBox={{ x: true, y: true }}
            {...chartTooltip}
          />
        </PieChart>
      </ResponsiveContainer>
      {centerValue && (
        <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center">
          <span className="font-display text-lg font-semibold">{centerValue}</span>
          {centerLabel && <span className="text-[10px] text-[var(--text-muted)]">{centerLabel}</span>}
        </div>
      )}
    </div>
  )
}

export function DonutLegend({ data, formatValue = (v) => String(v) }: { data: DonutSlice[]; formatValue?: (v: number) => string }) {
  const total = data.reduce((sum, d) => sum + d.value, 0)
  return (
    <div className="flex flex-wrap justify-center gap-3 text-xs">
      {data.map((d) => (
        <div key={d.name} className="flex items-center gap-1.5">
          <span className="h-2 w-2 rounded-full" style={{ background: d.color }} />
          <span className="text-[var(--text-muted)]">{d.name}</span>
          <span className="font-medium">{formatValue(d.value)}</span>
          {total > 0 && <span className="text-[var(--text-muted)]">({((d.value / total) * 100).toFixed(0)}%)</span>}
        </div>
      ))}
    </div>
  )
}
