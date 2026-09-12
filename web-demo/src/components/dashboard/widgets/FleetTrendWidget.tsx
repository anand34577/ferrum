import { useQueries } from "@tanstack/react-query"
import { Loader2 } from "lucide-react"
import { useMemo } from "react"
import { ResourceAreaChart } from "@/components/charts/ResourceAreaChart"
import type { WidgetSettings } from "@/lib/dashboardTypes"
import { useScopedInventory } from "@/lib/fleet"
import { api, type RRDPoint } from "@/lib/api"
import { FORMATTERS, NODE_SERIES, rowNum, type ChartRow } from "@/lib/metrics"
import { formatBytes } from "@/lib/utils"
import { WidgetError } from "@/components/dashboard/WidgetChrome"

// Ferrum doesn't run its own time-series store — this widget gets a real
// fleet-wide trend by pulling each node's native PVE RRD history and
// summing/averaging across every node in the scoped connections, bucketed by
// timestamp. It's O(nodes) requests, which is fine at the fleet sizes this
// app targets (tens of nodes, not thousands).
export function FleetTrendWidget({ settings }: { settings: WidgetSettings }) {
  const timeframe = settings.timeframe ?? "hour"
  const { connections, isLoading: inventoryLoading, isError: inventoryError } = useScopedInventory(settings)

  const nodePairs: Array<{ connId: string; node: string }> = useMemo(
    () =>
      connections.flatMap((conn) =>
        (conn.resources ?? [])
          .filter((r) => r.type === "node")
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

  const loading = nodePairs.length > 0 && rrdQueries.some((q) => q.isLoading)

  if (inventoryLoading) return <Loader2 className="h-4 w-4 animate-spin text-[var(--text-muted)]" />
  if (inventoryError) return <WidgetError />
  if (nodePairs.length === 0) return <p className="text-sm text-[var(--text-muted)]">No nodes reporting yet.</p>
  if (loading) return <Loader2 className="h-4 w-4 animate-spin text-[var(--text-muted)]" />
  if (rrdQueries.every((q) => q.isError)) return <WidgetError />

  // Bucket every node's points by minute so nodes with slightly different
  // sample timestamps still line up into one series. CPU averages across
  // nodes; memory and network sum (fleet totals).
  const buckets = new Map<number, { cpuSum: number; cpuCount: number; memSum: number; maxmemSum: number; netinSum: number; netoutSum: number }>()
  for (const q of rrdQueries) {
    for (const point of q.data ?? []) {
      if (point.time === undefined) continue
      const bucket = Math.round(point.time / 60) * 60
      const entry = buckets.get(bucket) ?? { cpuSum: 0, cpuCount: 0, memSum: 0, maxmemSum: 0, netinSum: 0, netoutSum: 0 }
      if (point.cpu !== undefined) {
        entry.cpuSum += point.cpu
        entry.cpuCount += 1
      }
      entry.memSum += point.mem ?? 0
      entry.maxmemSum += point.maxmem ?? 0
      entry.netinSum += point.netin ?? 0
      entry.netoutSum += point.netout ?? 0
      buckets.set(bucket, entry)
    }
  }

  const data: ChartRow[] = Array.from(buckets.entries())
    .sort(([a], [b]) => a - b)
    .map(([time, b]) => ({
      time,
      cpu: b.cpuCount > 0 ? (b.cpuSum / b.cpuCount) * 100 : undefined,
      mem: b.memSum,
      netin: b.netinSum,
      netout: b.netoutSum,
    }))

  if (data.length === 0) {
    return <p className="text-sm text-[var(--text-muted)]">No historical data available yet.</p>
  }

  const latest = data[data.length - 1]
  const latestCpu = rowNum(latest, "cpu")
  const latestMem = rowNum(latest, "mem")
  const latestNetin = rowNum(latest, "netin")
  const latestNetout = rowNum(latest, "netout")

  return (
    <div className="flex h-full min-h-0 flex-col gap-2">
      <div className="flex flex-wrap gap-x-6 gap-y-1 text-xs text-[var(--text-muted)]">
        <span>
          Fleet CPU (avg across {nodePairs.length} nodes):{" "}
          <span className="font-medium text-[var(--text)]">{latestCpu !== undefined ? `${latestCpu.toFixed(1)}%` : "-"}</span>
        </span>
        <span>
          Fleet memory in use: <span className="font-medium text-[var(--text)]">{formatBytes(latestMem ?? 0)}</span>
        </span>
        <span>
          Fleet network: <span className="font-medium text-[var(--text)]">{formatBytes(latestNetin ?? 0)}/s in · {formatBytes(latestNetout ?? 0)}/s out</span>
        </span>
      </div>
      <div className="grid min-h-0 flex-1 gap-4 lg:grid-cols-3">
        <div className="flex min-w-0 flex-col">
          <p className="mb-1 text-[10px] font-medium text-[var(--text-muted)]">CPU (average)</p>
          <ResourceAreaChart data={data} series={NODE_SERIES.cpu()} yDomain={[0, 100]} yTickFormatter={FORMATTERS.pct} syncId="fleet-trend" height="100%" />
        </div>
        <div className="flex min-w-0 flex-col">
          <p className="mb-1 text-[10px] font-medium text-[var(--text-muted)]">Memory (total in use)</p>
          <ResourceAreaChart data={data} series={[{ key: "mem", label: "Memory used", color: "var(--chart-2)" }]} valueKind="bytes" syncId="fleet-trend" height="100%" />
        </div>
        <div className="flex min-w-0 flex-col">
          <p className="mb-1 text-[10px] font-medium text-[var(--text-muted)]">Network throughput (total)</p>
          <ResourceAreaChart data={data} series={NODE_SERIES.network()} valueKind="rate" syncId="fleet-trend" height="100%" showLegend />
        </div>
      </div>
    </div>
  )
}
