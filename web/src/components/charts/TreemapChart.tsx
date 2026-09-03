import { Treemap as RechartsTreemap, ResponsiveContainer, Tooltip } from "recharts"
import { chartTooltip } from "@/components/charts/tooltipTheme"

export interface TreemapDatum {
  name: string
  /** Primary size measure (e.g. bytes used). */
  size: number
  /** Optional 0..100 fill ratio deciding the tile's color intensity. */
  ratio?: number
  /** Optional secondary line under the name (e.g. "42% full"). */
  sub?: string
  [key: string]: unknown
}

interface TreemapChartProps {
  data: TreemapDatum[]
  height?: number
  valueFormatter: (v: number) => string
}

const ratioColor = (ratio: number): string =>
  `color-mix(in oklab, var(--chart-2) ${Math.round(25 + ratio * 70)}%, var(--bg-muted))`

interface TileProps {
  x?: number
  y?: number
  width?: number
  height?: number
  name?: string
  size?: number
  ratio?: number
  sub?: string
}

function Tile(props: TileProps) {
  const { x = 0, y = 0, width, height, name, size, ratio = 0, sub } = props
  if (width === undefined || height === undefined || width < 4 || height < 4) return null
  const compact = width < 64 || height < 34
  return (
    <g>
      <rect
        x={x}
        y={y}
        width={width}
        height={height}
        rx={4}
        style={{
          fill: ratioColor(Math.min(1, Math.max(0, ratio))),
          stroke: "var(--bg-surface)",
          strokeWidth: 2,
        }}
      />
      {!compact && (
        <text
          x={x + 8}
          y={y + 18}
          fill="var(--text)"
          fontSize={12}
          fontWeight={600}
          style={{ pointerEvents: "none" }}
        >
          {name}
        </text>
      )}
      {!compact && sub && width > 90 && height > 50 && (
        <text x={x + 8} y={y + 32} fill="var(--text-muted)" fontSize={10} style={{ pointerEvents: "none" }}>
          {sub}
        </text>
      )}
      {/* screen-reader / tooltip fallback for tiny tiles */}
      {compact && <title>{`${name}: ${size}`}</title>}
    </g>
  )
}

/** Proportional-area tiles — the disk-space-analyzer chart. Area encodes
 * usage; color intensity encodes how full each storage is. */
export function TreemapChart({ data, height = 200, valueFormatter }: TreemapChartProps) {
  const filtered = data.filter((d) => d.size > 0)
  if (filtered.length === 0) {
    return <p className="pt-6 text-center text-sm text-[var(--text-muted)]">No storage usage to show.</p>
  }
  return (
    <ResponsiveContainer width="100%" height={height}>
      <RechartsTreemap
        data={filtered}
        dataKey="size"
        nameKey="name"
        aspectRatio={4 / 3}
        content={<Tile />}
        // No entrance animation — the inventory re-polls every 15s.
        isAnimationActive={false}
      >
        <Tooltip
          {...chartTooltip}
          formatter={(value, name, entry) => {
            const item = entry?.payload as TreemapDatum | undefined
            return [valueFormatter(Number(value)), item?.sub ? `${name} (${item.sub})` : String(name)]
          }}
        />
      </RechartsTreemap>
    </ResponsiveContainer>
  )
}
