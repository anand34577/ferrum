import { useQueries } from "@tanstack/react-query"
import { StatusDot } from "@/components/ui/status-dot"
import { Timestamp } from "@/components/ui/timestamp"
import { api, type ClusterLogEntry } from "@/lib/api"
import type { WidgetSettings } from "@/lib/dashboardTypes"
import { scopedConnection, useConnections } from "@/lib/fleet"
import { WidgetError } from "@/components/dashboard/WidgetChrome"

interface FleetLogEntry extends ClusterLogEntry {
  connName: string
}

/** Raw PVE cluster log — config changes, service restarts, permission edits —
 * the "what happened on this cluster" feed that's distinct from task activity
 * or alerts. One request per connection, same O(connections) cost pattern as
 * useClusterTasks. */
export function ClusterActivityWidget({ settings }: { settings: WidgetSettings }) {
  const connId = scopedConnection(settings)
  const { data: connections, isError: connError } = useConnections()

  const targets = (connections ?? []).filter((c) => connId === "all" || c.id === connId)

  const logQueries = useQueries({
    queries: targets.map((c) => ({
      queryKey: ["cluster-log", c.id],
      queryFn: ({ signal }: { signal: AbortSignal }) => api.get<ClusterLogEntry[]>(`/connections/${c.id}/cluster/log`, { signal }),
      refetchInterval: 30_000,
      retry: false,
    })),
  })

  if (connError) return <WidgetError />
  if (targets.length > 0 && logQueries.every((q) => q.isError)) return <WidgetError />

  const entries: FleetLogEntry[] = targets
    .flatMap((c, i) => (logQueries[i].data ?? []).map((e) => ({ ...e, connName: c.name })))
    .sort((a, b) => b.time - a.time)
    .slice(0, 12)

  if (entries.length === 0) {
    return <p className="text-sm text-[var(--text-muted)]">No recent cluster events.</p>
  }

  return (
    <div className="h-full space-y-1 overflow-auto">
      {entries.map((e, i) => (
        <div key={`${e.node}-${e.pid}-${e.time}-${i}`} className="flex items-start gap-1.5 rounded-sm px-1 py-1 text-xs hover:bg-[var(--bg-muted)]">
          <StatusDot status={e.pri <= 3 ? "error" : e.pri <= 4 ? "warn" : "muted"} className="mt-1" />
          <div className="min-w-0 flex-1">
            <p className="truncate text-[var(--text)]">{e.msg}</p>
            <p className="text-[10px] text-[var(--text-muted)]">
              {e.connName} · {e.node} · <Timestamp iso={new Date(e.time * 1000).toISOString()} />
            </p>
          </div>
        </div>
      ))}
    </div>
  )
}
