import { Bar, BarChart, Cell, LabelList, Rectangle, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts"
import type { BarShapeProps, LabelProps } from "recharts"
import { chartTooltip } from "@/components/charts/tooltipTheme"
import { chartBarRadius } from "@/lib/chartRadius"
import { useTheme } from "@/lib/theme"

export interface RankedBarRow {
  name: string
  value: number
}

interface RankedBarChartProps<T extends RankedBarRow> {
  data: T[]
  /** Fixed axis domain (e.g. [0, 100] for a percent) — omit to let recharts
   * size the axis to the data (e.g. uptime, which has no natural ceiling). */
  domain?: [number, number]
  /** Per-row fill override (e.g. severity coloring: >=90 red, >=75 amber) —
   * return undefined for a row to keep the default flat brand fill. */
  colorFor?: (row: T) => string | undefined
  labelFormatter: (value: number) => string
  tooltipLabel: string | ((row: T) => string)
  /** Row height in px — total chart height is rows × this, with a floor. */
  rowHeight?: number
  nameWidth?: number
}

/** The one horizontal "ranked list as bars" chart — used for anything that's
 * fundamentally a sorted top-N (top consumers, node comparison, uptime
 * leaderboard): a fixed-domain percent bar shows a background track for the
 * unfilled remainder; an open-domain bar (nothing to compare against, like
 * uptime) omits it so an unrelated ceiling doesn't get implied. */
export function RankedBarChart<T extends RankedBarRow>({
  data,
  domain,
  colorFor,
  labelFormatter,
  tooltipLabel,
  rowHeight = 26,
  nameWidth = 90,
}: RankedBarChartProps<T>) {
  const { look } = useTheme()
  const r = chartBarRadius(look)

  // Recharts' built-in LabelList text wraps onto a second line once the
  // space between the bar's end and the chart edge gets tight — exactly the
  // case for the longest bar (it's the whole point of a ranked chart that
  // one row runs closest to full width). A plain <text> here is never
  // width-constrained, so it always stays on one line; the generous right
  // margin below is what keeps it from being clipped instead.
  const valueLabel = (props: LabelProps) => {
    const { x, y, width, height, value } = props
    if (x == null || y == null || width == null || height == null || value == null) return null
    return (
      <text
        x={Number(x) + Number(width) + 6}
        y={Number(y) + Number(height) / 2}
        dy={4}
        fontSize={11}
        fill="var(--text-muted)"
        textAnchor="start"
      >
        {labelFormatter(Number(value))}
      </text>
    )
  }

  return (
    <ResponsiveContainer width="100%" height={Math.max(100, data.length * rowHeight)}>
      <BarChart data={data} layout="vertical" margin={{ top: 0, right: 56, left: 0, bottom: 0 }}>
        <XAxis type="number" domain={domain} hide />
        <YAxis type="category" dataKey="name" width={nameWidth} tick={{ fontSize: 11, fill: "var(--text-muted)" }} axisLine={false} tickLine={false} />
        <Tooltip
          cursor={false}
          formatter={(value, _name, item) => [
            labelFormatter(Number(value)),
            typeof tooltipLabel === "function" ? tooltipLabel(item.payload as T) : tooltipLabel,
          ]}
          {...chartTooltip}
        />
        {/* background = full-length track so the unfilled remainder stays
            visible when there's a real ceiling to compare against (a percent).
            Animation off — this data re-polls every 15s and re-running the
            entry animation each time reads as a glitch, not motion. Hover
            feedback is a 1px outline on the row, not a fill change. */}
        <Bar
          dataKey="value"
          radius={[r, r, r, r]}
          barSize={12}
          isAnimationActive={false}
          background={
            domain
              ? (p: BarShapeProps) => (
                  <Rectangle
                    x={p.x}
                    y={p.y}
                    width={p.width}
                    height={p.height}
                    fill="var(--track)"
                    radius={r}
                    stroke={p.isActive ? "var(--text-faint)" : "none"}
                    strokeWidth={1}
                  />
                )
              : undefined
          }
          activeBar={{ stroke: "var(--text-faint)", strokeWidth: 1 }}
        >
          {data.map((row, i) => (
            <Cell key={row.name + i} fill={colorFor?.(row) ?? "var(--color-brand-500)"} />
          ))}
          <LabelList dataKey="value" content={valueLabel} />
        </Bar>
      </BarChart>
    </ResponsiveContainer>
  )
}
