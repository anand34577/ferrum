import { cn } from "@/lib/utils"

export interface HeatmapRow {
  label: string
  /** One value per column; undefined = no sample (renders as empty). */
  values: (number | undefined)[]
}

interface HeatmapProps {
  rows: HeatmapRow[]
  /** Label per column; rendered sparsely when there are many. */
  columnLabels?: string[]
  valueFormatter: (v: number) => string
  /** 0..1 normalization bounds — defaults to the data's own max. */
  max?: number
  /** Cell height in px. */
  cellHeight?: number
}

/** Color-intensity matrix — spot when a resource ran hot across nodes and
 * time in one glance. Intensity ramps from the muted background to the
 * brand accent via oklab color mixing, so it tracks light/dark themes. */
export function Heatmap({ rows, columnLabels, valueFormatter, max, cellHeight = 18 }: HeatmapProps) {
  if (rows.length === 0) return null
  const colCount = Math.max(...rows.map((r) => r.values.length))
  const dataMax =
    max ??
    Math.max(
      0,
      ...rows.flatMap((r) => r.values.filter((v): v is number => typeof v === "number")),
    )

  const cellStyle = (v: number | undefined): React.CSSProperties => {
    if (typeof v !== "number" || dataMax <= 0) return { background: "var(--bg-muted)" }
    const ratio = Math.min(1, Math.max(0, v / dataMax))
    return {
      background: `color-mix(in oklab, var(--chart-1) ${Math.round(8 + ratio * 86)}%, var(--bg-muted))`,
    }
  }

  const labelEvery = columnLabels ? Math.max(1, Math.ceil(columnLabels.length / 6)) : 0
  // With many columns, clamp to a fixed readable cell width so labels stay
  // on their own column (the row then scrolls if needed). With few columns,
  // flex-fill instead so the matrix always spans its card — a fixed 560px
  // target overflowed narrow rails and clipped the last column labels.
  const cellWidth = columnLabels && columnLabels.length > 6 ? Math.max(22, Math.min(120, Math.floor(560 / columnLabels.length))) : undefined
  const cellStyleWidth = cellWidth ? { width: cellWidth, minWidth: cellWidth } : { flex: "1 1 0" }

  return (
    <div className="overflow-x-auto">
      <div className="min-w-max space-y-0.5">
        {columnLabels && (
          <div className="flex gap-0.5 pl-[86px] text-[9px] leading-tight text-[var(--text-faint)]">
            {columnLabels.map((l, i) => (
              <div
                key={i}
                className="truncate text-center"
                style={{ ...cellStyleWidth, paddingTop: 1 }}
                title={l}
              >
                {i % labelEvery === 0 ? l : ""}
              </div>
            ))}
          </div>
        )}
        {rows.map((row) => (
          <div key={row.label} className="flex items-center gap-0.5">
            <span className="w-[86px] shrink-0 truncate pr-2 text-right text-xs text-[var(--text-muted)]" title={row.label}>
              {row.label}
            </span>
            {Array.from({ length: colCount }, (_, c) => {
              const v = row.values[c]
              return (
                <div
                  key={c}
                  className={cn(
                    "rounded-sm transition-transform hover:scale-110",
                    typeof v !== "number" && "opacity-40",
                  )}
                  style={{ ...cellStyle(v), ...cellStyleWidth, height: cellHeight }}
                  title={typeof v === "number" ? `${row.label} — ${valueFormatter(v)}` : `${row.label} — no data`}
                />
              )
            })}
          </div>
        ))}
        <div className="flex items-center justify-end gap-1.5 pt-1.5 text-[10px] text-[var(--text-faint)]">
          <span>low</span>
          {[0.15, 0.35, 0.55, 0.75, 0.95].map((r) => (
            <span
              key={r}
              className="inline-block h-2.5 w-4 rounded-sm"
              style={{ background: `color-mix(in oklab, var(--chart-1) ${Math.round(8 + r * 86)}%, var(--bg-muted))` }}
            />
          ))}
          <span>high</span>
        </div>
      </div>
    </div>
  )
}
