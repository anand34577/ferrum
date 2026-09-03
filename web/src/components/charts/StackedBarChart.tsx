import { Bar, BarChart, Rectangle, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts"
import type { BarShapeProps } from "recharts"
import { chartTooltip } from "@/components/charts/tooltipTheme"

export interface StackedSeries {
  key: string
  label: string
  color: string
}

/** One row per category — `name` plus one numeric field per stack segment,
 * e.g. { name: "pve1", qemu: 4, lxc: 2 }. */
export type StackedRow = { name: string; [key: string]: string | number }

/** Recharts applies one static radius per series, but a stack's silhouette
 * is per-row: a segment with only zero-valued segments before/after it is
 * the visible end of the bar and needs the rounded cap, or single-segment
 * rows end square. */
function segmentRadius(
  data: StackedRow[],
  series: StackedSeries[],
  seriesIndex: number,
  rowIndex: number,
): [number, number, number, number] {
  const row = data[rowIndex]
  if (!row) return [0, 0, 0, 0]
  const isStart = series.slice(0, seriesIndex).every((s) => !row[s.key])
  const isEnd = series.slice(seriesIndex + 1).every((s) => !row[s.key])
  return [isEnd ? 4 : 0, isEnd ? 4 : 0, isStart ? 4 : 0, isStart ? 4 : 0]
}

interface StackedBarChartProps {
  data: StackedRow[]
  series: StackedSeries[]
  height?: number
  valueFormatter?: (v: number) => string
  /** Hide the value axis when bars are labeled or self-evident. */
  showXAxis?: boolean
  showLegend?: boolean
}

/** Horizontal stacked bars — composition across categories (e.g. guest mix
 * per node). Category names stay readable on the Y axis however long they are. */
export function StackedBarChart({ data, series, height = 180, valueFormatter, showXAxis = false, showLegend = true }: StackedBarChartProps) {
  if (data.length === 0) return null

  return (
    <div>
      <ResponsiveContainer width="100%" height={height}>
        <BarChart data={data} layout="vertical" margin={{ top: 0, right: 12, left: 0, bottom: 0 }} barCategoryGap="25%">
          <XAxis type="number" hide={!showXAxis} tick={{ fontSize: 11, fill: "var(--text-muted)" }} axisLine={false} tickLine={false} allowDecimals={false} />
          <YAxis
            type="category"
            dataKey="name"
            width={96}
            tick={{ fontSize: 11, fill: "var(--text-muted)" }}
            axisLine={false}
            tickLine={false}
          />
          <Tooltip
            cursor={false}
            formatter={(value) => (valueFormatter ? valueFormatter(Number(value)) : String(value))}
            labelFormatter={(label) => String(label)}
            {...chartTooltip}
          />
          {series.map((s, i) => {
            // Custom shape + activeBar only so the rounded caps follow the
            // stack silhouette per row (see segmentRadius); activeBar adds
            // the 1px hover outline and must repeat the radius logic because
            // it replaces `shape` for the hovered row.
            const renderSegment = (p: BarShapeProps, outlined: boolean) =>
              p.width && p.height ? (
                <Rectangle
                  x={p.x}
                  y={p.y}
                  width={p.width}
                  height={p.height}
                  fill={s.color}
                  radius={segmentRadius(data, series, i, p.index)}
                  stroke={outlined ? "var(--text-faint)" : "none"}
                  strokeWidth={1}
                />
              ) : null
            return (
              <Bar
                key={s.key}
                dataKey={s.key}
                name={s.label}
                stackId="stack"
                fill={s.color}
                barSize={14}
                // No entrance animation — the data re-polls every 15s and
                // re-running it each time reads as a glitch.
                isAnimationActive={false}
                shape={(p: BarShapeProps) => renderSegment(p, false)}
                activeBar={(p: BarShapeProps) => renderSegment(p, true)}
              />
            )
          })}
        </BarChart>
      </ResponsiveContainer>
      {showLegend && (
        <div className="mt-1 flex flex-wrap justify-center gap-x-4 gap-y-1 text-xs">
          {series.map((s) => (
            <span key={s.key} className="flex items-center gap-1.5 text-[var(--text-muted)]">
              <span className="h-2 w-2 rounded-full" style={{ background: s.color }} />
              {s.label}
            </span>
          ))}
        </div>
      )}
    </div>
  )
}
