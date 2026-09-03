import { useQueries } from "@tanstack/react-query"
import { Loader2 } from "lucide-react"
import { useMemo } from "react"
import { Heatmap, type HeatmapRow } from "@/components/charts/Heatmap"
import type { WidgetSettings } from "@/lib/dashboardTypes"
import { useScopedInventory } from "@/lib/fleet"
import { api, type RRDPoint } from "@/lib/api"
import { formatRRDTick } from "@/lib/utils"

const COLS = 24

// Nodes × time color matrix — answers "which host ran hot, and when" at a
// glance, using each node's native PVE RRD history (no extra storage here).
export function UtilizationHeatmapWidget({ settings }: { settings: WidgetSettings }) {
  const timeframe = settings.timeframe ?? "day"
  const metric = settings.metric === "mem" ? "mem" : "cpu"
  const { connections } = useScopedInventory(settings)

  const nodePairs: Array<{ connId: string; node: string }> = useMemo(
    () =>
      connections.flatMap((conn) =>
        (conn.resources ?? [])
          .filter((r) => r.type === "node" && r.status === "online")
          .map((r) => ({ connId: conn.connectionId, node: r.node })),
      ),
    [connections],
  )

  const rrdQueries = useQueries({
    queries: nodePairs.map(({ connId, node }) => ({
      queryKey: ["node-rrd", connId, node, timeframe, "avg"],
      queryFn: () => api.get<RRDPoint[]>(`/connections/${connId}/nodes/${node}/rrddata?timeframe=${timeframe}`),
      retry: false,
      staleTime: 60_000,
    })),
  })

  const { rows, labels } = useMemo(() => {
    const points = rrdQueries.map((q) => q.data ?? [])
    let minT = Infinity
    let maxT = -Infinity
    for (const set of points) {
      for (const p of set) {
        if (!p.time) continue
        if (p.time < minT) minT = p.time
        if (p.time > maxT) maxT = p.time
      }
    }
    if (!nodePairs.length || !Number.isFinite(minT) || maxT <= minT) return { rows: [] as HeatmapRow[], labels: undefined }

    const step = (maxT - minT) / COLS
    const heatRows = points.map((set, nodeIdx) => {
      const sums = new Array<number>(COLS).fill(0)
      const counts = new Array<number>(COLS).fill(0)
      for (const p of set) {
        if (!p.time) continue
        const col = Math.min(COLS - 1, Math.floor((p.time - minT) / step))
        let v: number | undefined
        if (metric === "cpu") v = p.cpu !== undefined ? p.cpu * 100 : undefined
        else v = p.maxmem ? ((p.mem ?? 0) / p.maxmem) * 100 : undefined
        if (v !== undefined) {
          sums[col] += v
          counts[col] += 1
        }
      }
      return { label: nodePairs[nodeIdx].node, values: sums.map((s, i) => (counts[i] > 0 ? s / counts[i] : undefined)) }
    })

    const colLabels = Array.from({ length: COLS }, (_, i) =>
      formatRRDTick(minT + (i + 0.5) * step, maxT - minT),
    )
    return { rows: heatRows, labels: colLabels }
  }, [rrdQueries, nodePairs, metric])

  if (connections.length === 0) return <p className="text-sm text-[var(--text-muted)]">No connections configured yet.</p>
  if (nodePairs.length === 0) return <p className="text-sm text-[var(--text-muted)]">No online nodes yet.</p>
  if (rrdQueries.some((q) => q.isLoading)) return <Loader2 className="h-4 w-4 animate-spin text-[var(--text-muted)]" />
  if (rows.length === 0) return <p className="text-sm text-[var(--text-muted)]">No historical data available yet.</p>

  return (
    <div>
      <Heatmap
        rows={rows}
        columnLabels={labels}
        valueFormatter={metric === "cpu" ? (v) => `${v.toFixed(1)}% CPU` : (v) => `${v.toFixed(1)}% mem`}
        cellHeight={16}
      />
      <p className="mt-1.5 text-center text-[10px] text-[var(--text-faint)]">
        Average {metric === "cpu" ? "CPU" : "memory"} utilization per node · {nodePairs.length} node{nodePairs.length === 1 ? "" : "s"}
      </p>
    </div>
  )
}
