import { useQuery } from "@tanstack/react-query"
import { DonutChart, DonutLegend } from "@/components/charts/DonutChart"
import { StatusDot } from "@/components/ui/status-dot"
import { api, type AlertInstance } from "@/lib/api"
import type { WidgetSettings } from "@/lib/dashboardTypes"
import { scopedConnection } from "@/lib/fleet"
import { useMemo } from "react"
import { WidgetError } from "@/components/dashboard/WidgetChrome"

export function AlertActivityWidget({ settings }: { settings: WidgetSettings }) {
  const connId = scopedConnection(settings)
  const summaryQuery = useQuery({
    queryKey: ["alerts-summary"],
    queryFn: () => api.get<{ warning: number; critical: number }>("/alerts/summary"),
    refetchInterval: 30_000,
  })
  const activeQuery = useQuery({
    queryKey: ["alerts", "active"],
    queryFn: () => api.get<AlertInstance[]>("/alerts/?status=active"),
    refetchInterval: 30_000,
  })

  const active = useMemo(
    () => (connId === "all" ? activeQuery.data ?? [] : (activeQuery.data ?? []).filter((a) => a.connectionId === connId)),
    [activeQuery.data, connId],
  )
  // The summary endpoint is fleet-wide; when scoped, derive counts from the
  // filtered active list so the donut matches the visible rows.
  const warning = connId === "all" ? summaryQuery.data?.warning ?? 0 : active.filter((a) => a.severity === "warning").length
  const critical = connId === "all" ? summaryQuery.data?.critical ?? 0 : active.filter((a) => a.severity === "critical").length
  const total = warning + critical

  if (summaryQuery.isError && activeQuery.isError) return <WidgetError />

  if (total === 0) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-1 text-center">
        <p className="font-display text-2xl font-semibold text-[var(--status-ok)]">All clear</p>
        <p className="text-xs text-[var(--text-muted)]">No active alerts across your fleet.</p>
      </div>
    )
  }

  const slices = [
    ...(critical > 0 ? [{ name: "Critical", value: critical, color: "var(--status-error)" }] : []),
    ...(warning > 0 ? [{ name: "Warning", value: warning, color: "var(--status-warn)" }] : []),
  ]

  const latest = active.slice(0, 3)

  return (
    <div className="flex h-full flex-col items-center justify-center gap-2">
      <DonutChart data={slices} centerValue={String(total)} centerLabel="active" height={110} />
      <DonutLegend data={slices} />
      {latest.length > 0 && (
        <div className="mb-1 w-full space-y-1 border-t border-[var(--border)] pt-2">
          {latest.map((a) => (
            <p key={a.id} className="flex items-center gap-1.5 truncate text-xs text-[var(--text-muted)]">
              <StatusDot status={a.severity === "critical" ? "error" : "warn"} />
              <span className="truncate">{a.resourceName}</span>
            </p>
          ))}
        </div>
      )}
    </div>
  )
}
