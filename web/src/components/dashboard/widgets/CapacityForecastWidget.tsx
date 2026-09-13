import { useQuery } from "@tanstack/react-query"
import { useMemo } from "react"
import { TrendingUp } from "lucide-react"
import { api, type CapacityWarning } from "@/lib/api"
import type { WidgetSettings } from "@/lib/dashboardTypes"
import { scopedConnection } from "@/lib/fleet"
import { cn } from "@/lib/utils"
import { WidgetError } from "@/components/dashboard/WidgetChrome"
import { Skeleton } from "@/components/ui/skeleton"

const metricLabel: Record<CapacityWarning["metric"], string> = {
  disk: "Disk",
  mem: "Memory",
  cpu: "CPU",
}

const confidenceTone: Record<CapacityWarning["confidence"], string> = {
  high: "text-[var(--text-muted)]",
  medium: "text-[var(--text-faint)]",
  low: "text-[var(--text-faint)]",
}

/** Nodes trending toward 90% capacity within the configured horizon, per
 * GET /forecast/capacity-warnings — a linear projection over each node's
 * historical RRD data, not a live threshold check. */
export function CapacityForecastWidget({ settings }: { settings: WidgetSettings }) {
  const connId = scopedConnection(settings)
  const horizonDays = Number(settings.horizonDays ?? "30") || 30

  const { data, isError, isLoading } = useQuery({
    queryKey: ["forecast", "capacity-warnings", horizonDays],
    queryFn: ({ signal }) => api.get<CapacityWarning[]>(`/forecast/capacity-warnings?horizonDays=${horizonDays}`, { signal }),
    refetchInterval: 60_000,
  })

  const rows = useMemo(
    () => (connId === "all" ? data ?? [] : (data ?? []).filter((w) => w.connectionId === connId)),
    [data, connId],
  )

  if (isError) return <WidgetError />

  if (isLoading) {
    return <Skeleton className="h-24" />
  }

  if (rows.length === 0) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-1 text-center">
        <p className="font-display text-2xl font-semibold text-[var(--status-ok)]">On track</p>
        <p className="text-xs text-[var(--text-muted)]">No node is trending toward capacity within {horizonDays} days.</p>
      </div>
    )
  }

  return (
    <div className="flex h-full flex-col gap-1.5 overflow-auto">
      {rows.map((w) => (
        <div key={`${w.connectionId}-${w.node}-${w.metric}`} className="flex items-center justify-between gap-2 rounded-md bg-[var(--bg-muted)] px-3 py-2">
          <span className="flex min-w-0 items-center gap-2">
            <TrendingUp className={cn("h-3.5 w-3.5 shrink-0", w.daysToWarning !== undefined && w.daysToWarning <= 7 ? "text-[var(--status-error)]" : "text-[var(--status-warn)]")} />
            <span className="min-w-0">
              <span className="block truncate text-sm font-medium">{w.node}</span>
              <span className="block truncate text-[10px] text-[var(--text-faint)]">{w.connectionName} · {metricLabel[w.metric]}</span>
            </span>
          </span>
          <span className="shrink-0 text-right text-xs tabular">
            <span className="block font-medium">{w.daysToWarning !== undefined ? `${Math.round(w.daysToWarning)}d to 90%` : "—"}</span>
            <span className={cn("block text-[10px]", confidenceTone[w.confidence])}>{w.currentPct.toFixed(0)}% now · {w.confidence} confidence</span>
          </span>
        </div>
      ))}
    </div>
  )
}
