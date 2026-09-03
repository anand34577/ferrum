import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts"
import { formatRRDTick, formatRRDTooltip } from "@/lib/utils"

export interface ChartSeries {
  key: string
  label: string
  color: string
  /** Formats the value in the tooltip (units: %, bytes, B/s, …). */
  formatter?: (v: number) => string
  /** Render the `<key>Band` [avg, max] tuples (cf=MAX samples) as a
   * translucent envelope around this series. */
  band?: boolean
}

interface ResourceAreaChartProps {
  data: Record<string, unknown>[]
  series: ChartSeries[]
  /** Fixed pixel height, or "100%" to fill the parent's height (the parent
   * must have a definite height — e.g. a widget body). */
  height?: number | "100%"
  /** Formats Y axis ticks — without it byte-valued axes render as 1.6e+10. */
  yTickFormatter?: (v: number) => string
  /** Pin the Y range when the unit implies one, e.g. [0, 100] for percents. */
  yDomain?: [number | "auto" | "dataMin", number | "auto" | "dataMax"]
  showLegend?: boolean
  /** Set the same syncId on several charts to align their hover crosshairs. */
  syncId?: string
  /** Force integer Y ticks for count-valued series (load average, guests). */
  allowDecimals?: boolean
}

function TooltipContent({
  active,
  payload,
  label,
  series,
}: {
  active?: boolean
  payload?: Array<{ dataKey?: string | number; value?: unknown }>
  label?: unknown
  series: ChartSeries[]
}) {
  if (!active || !payload || payload.length === 0) return null
  // Band payloads share the label with their main series — keep the first.
  const seen = new Set<string>()
  const items: { label: string; color: string; text: string }[] = []
  for (const p of payload) {
    const s = series.find((x) => x.key === p.dataKey)
    if (!s || seen.has(s.label) || typeof p.value !== "number") continue
    seen.add(s.label)
    items.push({ label: s.label, color: s.color, text: s.formatter ? s.formatter(p.value) : String(Math.round(p.value * 100) / 100) })
  }
  if (items.length === 0) return null
  return (
    <div className="rounded-md border border-[var(--border)] bg-[var(--bg-elevated)] px-2.5 py-1.5 text-xs shadow-md">
      <p className="mb-1 text-[10px] text-[var(--text-muted)]">{typeof label === "number" ? formatRRDTooltip(label) : String(label ?? "")}</p>
      {items.map((it) => (
        <p key={it.label} className="flex items-center gap-1.5">
          <span className="h-2 w-2 shrink-0 rounded-full" style={{ background: it.color }} />
          <span className="text-[var(--text-muted)]">{it.label}</span>
          <span className="ml-auto pl-3 font-medium tabular">{it.text}</span>
        </p>
      ))}
    </div>
  )
}

export function ResourceAreaChart({
  data,
  series,
  height = 200,
  yTickFormatter,
  yDomain,
  showLegend = false,
  syncId,
  allowDecimals = true,
}: ResourceAreaChartProps) {
  // X tick granularity follows the visible span: minutes for an hour view,
  // day-hours for a week, dates for a year.
  const times = data.map((d) => d.time as number).filter((t) => typeof t === "number" && t > 0)
  const span = times.length > 1 ? times[times.length - 1] - times[0] : 0
  const tickFormatter = (t: number) => formatRRDTick(t, span)

  return (
    <div className={height === "100%" ? "flex h-full min-h-0 flex-col" : undefined}>
      <ResponsiveContainer
        width="100%"
        height={height === "100%" ? "100%" : height}
        className={height === "100%" ? "min-h-0" : undefined}
      >
        {/* Margins reserve real room for the axis tick text and hover dots —
            a negative left margin (the old value) made recharts clip the Y
            labels, and a near-zero top clipped the first tick/dot. The YAxis
            width below owns the left gutter instead. */}
        <AreaChart data={data} margin={{ top: 8, right: 14, left: 0, bottom: 4 }} syncId={syncId}>
          <defs>
            {series.map((s) => (
              <linearGradient key={s.key} id={`grad-${s.key}`} x1="0" y1="0" x2="0" y2="1">
                <stop offset="5%" stopColor={s.color} stopOpacity={0.3} />
                <stop offset="95%" stopColor={s.color} stopOpacity={0} />
              </linearGradient>
            ))}
          </defs>
          <CartesianGrid strokeDasharray="3 3" stroke="var(--border)" vertical={false} />
          <XAxis
            dataKey="time"
            tickFormatter={tickFormatter}
            tick={{ fontSize: 11, fill: "var(--text-muted)" }}
            axisLine={{ stroke: "var(--border)" }}
            tickLine={false}
            minTickGap={40}
          />
          <YAxis
            tick={{ fontSize: 11, fill: "var(--text-muted)" }}
            axisLine={false}
            tickLine={false}
            width={56}
            tickFormatter={yTickFormatter}
            domain={yDomain}
            allowDecimals={allowDecimals}
          />
          <Tooltip content={<TooltipContent series={series} />} cursor={{ stroke: "var(--border-strong)", strokeDasharray: "3 3" }} />
          {/* Peak envelopes first so the average line and its gradient sit on top. */}
          {series
            .filter((s) => s.band)
            .map((s) => (
              <Area
                key={`${s.key}-band`}
                dataKey={`${s.key}Band`}
                name={s.label}
                stroke="none"
                fill={s.color}
                fillOpacity={0.12}
                connectNulls
                isAnimationActive={false}
              />
            ))}
          {series.map((s) => (
            <Area
              key={s.key}
              type="monotone"
              dataKey={s.key}
              name={s.label}
              stroke={s.color}
              fill={`url(#grad-${s.key})`}
              strokeWidth={1.75}
              connectNulls
              dot={false}
              activeDot={{ r: 3 }}
              // No entrance animation — RRD series re-poll and re-running it
              // each time reads as a glitch (same convention as the bars).
              isAnimationActive={false}
            />
          ))}
        </AreaChart>
      </ResponsiveContainer>
      {showLegend && series.length > 1 && (
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
