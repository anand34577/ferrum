import { useQuery } from "@tanstack/react-query"
import { useMemo } from "react"
import { api, type Connection, type ConnectionInventory, type FleetOverviewConn } from "@/lib/api"

/**
 * Fleet data layer — the multi-connection plumbing every overview surface
 * shares. Ferrum manages N Proxmox clusters/servers, so every widget and the
 * Overview page scope their data to either "all" (fleet-wide, the default)
 * or one specific connection via the widget's `connection` setting.
 */

export function scopedConnection(settings: Record<string, string> | undefined): string {
  return settings?.connection || "all"
}

/** Fleet rollups per connection — light, one request. */
export function useFleetOverview() {
  return useQuery({
    queryKey: ["overview"],
    queryFn: ({ signal }) => api.get<FleetOverviewConn[]>("/overview", { signal }),
    refetchInterval: 15_000,
  })
}

/** Full per-guest inventory, scoped to the widget's connection setting. */
export function useScopedInventory(settings?: Record<string, string>) {
  const connId = scopedConnection(settings)
  const { data, isLoading } = useQuery({
    queryKey: ["inventory"],
    queryFn: ({ signal }) => api.get<ConnectionInventory[]>("/inventory/", { signal }),
    refetchInterval: 15_000,
  })

  const connections = useMemo(
    () => (connId === "all" ? data ?? [] : (data ?? []).filter((c) => c.connectionId === connId)),
    [data, connId],
  )
  const resources = useMemo(() => connections.flatMap((c) => c.resources ?? []), [connections])
  const scopeName = connId === "all" ? "All connections" : (data ?? []).find((c) => c.connectionId === connId)?.name ?? connId

  return { connections, resources, scopeName, connId, isLoading }
}

/** Connection list for widget scope dropdowns and topology metadata. */
export function useConnections() {
  return useQuery({
    queryKey: ["connections"],
    queryFn: ({ signal }) => api.get<Connection[]>("/connections/", { signal }),
    staleTime: 60_000,
  })
}

/** Fleet-wide totals over the per-connection rollups. */
export function summarizeFleet(overview: FleetOverviewConn[] | undefined) {
  const conns = overview ?? []
  const online = conns.filter((c) => c.online)
  const sum = (f: (c: FleetOverviewConn) => number) => online.reduce((s, c) => s + f(c), 0)

  const cpuCores = sum((c) => c.cpu.cores)
  const cpuUsed = sum((c) => c.cpu.usedCores)
  const memTotal = sum((c) => c.memory.total)
  const memUsed = sum((c) => c.memory.used)
  const stoTotal = sum((c) => c.storage.total)
  const stoUsed = sum((c) => c.storage.used)

  return {
    connections: conns,
    online,
    serversTotal: conns.length,
    serversOnline: online.length,
    clusters: online.filter((c) => c.cluster).length,
    nodesTotal: sum((c) => c.nodes.total),
    nodesOnline: sum((c) => c.nodes.online),
    vms: sum((c) => c.vms.total),
    vmsRunning: sum((c) => c.vms.running),
    lxcs: sum((c) => c.lxcs.total),
    lxcsRunning: sum((c) => c.lxcs.running),
    running: sum((c) => c.vms.running + c.lxcs.running),
    stopped: sum((c) => c.vms.stopped + c.lxcs.stopped),
    templates: sum((c) => c.templates),
    haGuests: sum((c) => c.haGuests),
    cores: cpuCores,
    cpuPct: cpuCores > 0 ? (cpuUsed / cpuCores) * 100 : 0,
    memTotal,
    memUsed,
    memPct: memTotal > 0 ? (memUsed / memTotal) * 100 : 0,
    stoTotal,
    stoUsed,
    stoPct: stoTotal > 0 ? (stoUsed / stoTotal) * 100 : 0,
    alertsCritical: conns.reduce((s, c) => s + c.alerts.critical, 0),
    alertsWarning: conns.reduce((s, c) => s + c.alerts.warning, 0),
    offline: conns.filter((c) => !c.online),
  }
}

export type FleetTotals = ReturnType<typeof summarizeFleet>

/** Severity bucket for a utilization percentage — shared by KPI tones,
 * heatmap cells, and bar colors. */
export function utilizationTone(pct: number): "ok" | "warn" | "error" {
  if (pct >= 90) return "error"
  if (pct >= 75) return "warn"
  return "ok"
}

/** Sum of allocated guest resources vs physical capacity — the overcommit
 * math behind the capacity-planning widget. */
