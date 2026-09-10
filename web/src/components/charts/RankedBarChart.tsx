export interface RankedBarRow {
  name: string
  value: number
}

interface RankedBarChartProps<T extends RankedBarRow> {
  data: T[]
  /** Fixed axis domain (e.g. [0, 100] for a percent) — omit to scale each bar
   * relative to the largest value in this list (e.g. uptime, which has no
   * natural ceiling). */
  domain?: [number, number]
  /** Per-row fill override (e.g. severity coloring: >=90 red, >=75 amber) —
   * return undefined for a row to keep the default flat brand fill. */
  colorFor?: (row: T) => string | undefined
  labelFormatter: (value: number) => string
  tooltipLabel: string | ((row: T) => string)
  /** Row height in px. */
  rowHeight?: number
  nameWidth?: number
}

/** The one horizontal "ranked list as bars" chart — used for anything that's
 * fundamentally a sorted top-N (top consumers, node comparison, uptime
 * leaderboard). Plain flex rows, not an SVG chart: every row shares the exact
 * same name column width and the exact same value column width, so every
 * track lines up and starts at the same x regardless of how long an
 * individual label or formatted value happens to be — a fixed-domain percent
 * bar also gets a background track for the unfilled remainder; an open-domain
 * bar (nothing to compare against) omits it so an unrelated ceiling isn't
 * implied. */
export function RankedBarChart<T extends RankedBarRow>({
  data,
  domain,
  colorFor,
  labelFormatter,
  tooltipLabel,
  rowHeight = 26,
  nameWidth = 90,
}: RankedBarChartProps<T>) {
  const [min, max] = domain ?? [0, Math.max(1, ...data.map((d) => d.value))]
  // The value column is sized to fit the longest formatted value in THIS
  // list (in ch, since these are tabular/mono-ish numbers) rather than a
  // guessed fixed width — wide enough for "12d 4h" and "100%" alike, and
  // identical across every row so the bars all end at the same x.
  const valueWidth = Math.max(2, ...data.map((d) => labelFormatter(d.value).length))

  return (
    <div className="flex flex-col justify-center gap-1.5" style={{ minHeight: data.length * rowHeight }}>
      {data.map((row, i) => {
        const pct = Math.min(100, Math.max(0, ((row.value - min) / (max - min || 1)) * 100))
        const tip = typeof tooltipLabel === "function" ? tooltipLabel(row) : tooltipLabel
        return (
          <div
            key={row.name + i}
            className="flex items-center gap-2 text-xs animate-in fade-in duration-300"
            style={{ height: rowHeight }}
            title={`${row.name} — ${tip}: ${labelFormatter(row.value)}`}
          >
            <span className="shrink-0 truncate text-[var(--text-muted)]" style={{ width: nameWidth }}>
              {row.name}
            </span>
            <div className="h-3 min-w-8 flex-1 overflow-hidden rounded-none bg-[var(--track)]">
              <div
                className="h-full rounded-none transition-[width] duration-300 ease-out"
                style={{ width: `${pct}%`, background: colorFor?.(row) ?? "var(--color-brand-500)" }}
              />
            </div>
            <span
              className="shrink-0 whitespace-nowrap text-right tabular text-[var(--text-muted)]"
              style={{ width: `${valueWidth}ch` }}
            >
              {labelFormatter(row.value)}
            </span>
          </div>
        )
      })}
    </div>
  )
}
