import { Bar, BarChart, Cell, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts"
import { chartTooltip } from "@/components/charts/tooltipTheme"

export interface HistogramBin {
  label: string
  count: number
  color?: string
}

interface HistogramProps {
  bins: HistogramBin[]
  height?: number
  valueLabel?: string
}

/** Frequency distribution of a continuous metric across the fleet (e.g.
 * "how many guests sit in each CPU-utilization bracket"). */
export function Histogram({ bins, height = 160, valueLabel = "resources" }: HistogramProps) {
  if (bins.length === 0 || bins.every((b) => b.count === 0)) {
    return <p className="pt-6 text-center text-sm text-[var(--text-muted)]">No samples to distribute yet.</p>
  }

  return (
    <ResponsiveContainer width="100%" height={height}>
      <BarChart data={bins} margin={{ top: 4, right: 4, left: -22, bottom: 0 }} barCategoryGap="0%">
        <XAxis
          dataKey="label"
          tick={{ fontSize: 10, fill: "var(--text-muted)" }}
          axisLine={{ stroke: "var(--border)" }}
          tickLine={false}
          interval={0}
        />
        <YAxis tick={{ fontSize: 11, fill: "var(--text-muted)" }} axisLine={false} tickLine={false} allowDecimals={false} />
        <Tooltip
          cursor={false}
          formatter={(value) => [`${value} ${valueLabel}`, "Count"]}
          {...chartTooltip}
        />
        {/* No gaps between bars — a histogram's area encodes the distribution.
            No entrance animation — the data re-polls every 15s. Hover
            feedback is a 1px outline on the hovered bin — no band. */}
        <Bar dataKey="count" isAnimationActive={false} activeBar={{ stroke: "var(--text-faint)", strokeWidth: 1 }}>
          {bins.map((b, i) => (
            <Cell key={i} fill={b.color ?? "var(--chart-3)"} />
          ))}
        </Bar>
      </BarChart>
    </ResponsiveContainer>
  )
}
