import { useQueries } from "@tanstack/react-query"
import { Loader2 } from "lucide-react"
import { useMemo } from "react"
import { StatusDot } from "@/components/ui/status-dot"
import { Timestamp } from "@/components/ui/timestamp"
import { api, type ReplicationStatus } from "@/lib/api"
import type { WidgetSettings } from "@/lib/dashboardTypes"
import { useScopedInventory } from "@/lib/fleet"
import { WidgetError } from "@/components/dashboard/WidgetChrome"
import { WidgetViewAllLink } from "@/components/dashboard/WidgetViewAllLink"

interface FleetReplication extends ReplicationStatus {
  connName: string
  node: string
}

/** Storage replication health across every node in the fleet — the DR
 * safety net for setups that replicate guests between servers instead of
 * (or alongside) shared storage. A job with a rising fail_count on some
 * node nobody's watching is exactly what a multi-server dashboard should
 * surface. O(nodes) requests, the same fanout FleetTrendWidget already uses
 * for RRD history. */
export function ReplicationStatusWidget({ settings }: { settings: WidgetSettings }) {
  const { connections, isError: inventoryError } = useScopedInventory(settings)

  const nodePairs = useMemo(
    () =>
      connections.flatMap((conn) =>
        (conn.resources ?? []).filter((r) => r.type === "node").map((r) => ({ connId: conn.connectionId, connName: conn.name, node: r.node })),
      ),
    [connections],
  )

  const repQueries = useQueries({
    queries: nodePairs.map(({ connId, node }) => ({
      queryKey: ["node-replication", connId, node],
      queryFn: () => api.get<ReplicationStatus[]>(`/connections/${connId}/nodes/${node}/replication`),
      staleTime: 30_000,
      retry: false,
    })),
  })

  if (inventoryError) return <WidgetError />
  if (nodePairs.length === 0) return <p className="text-sm text-[var(--text-muted)]">No nodes reporting yet.</p>
  if (repQueries.some((q) => q.isLoading)) return <Loader2 className="h-4 w-4 animate-spin text-[var(--text-muted)]" />
  if (repQueries.every((q) => q.isError)) return <WidgetError />

  const jobs: FleetReplication[] = nodePairs.flatMap(({ connName, node }, i) => (repQueries[i].data ?? []).map((j) => ({ ...j, connName, node })))

  if (jobs.length === 0) {
    return <p className="text-sm text-[var(--text-muted)]">No replication jobs configured.</p>
  }

  const failing = jobs.filter((j) => j.error || (j.fail_count ?? 0) > 0).sort((a, b) => (b.fail_count ?? 0) - (a.fail_count ?? 0))
  const healthy = jobs.length - failing.length
  const shown = failing.slice(0, 6)

  return (
    <div className="flex h-full flex-col gap-2">
      <div className="flex flex-wrap gap-x-6 gap-y-1 text-xs text-[var(--text-muted)]">
        <span>
          Jobs: <span className="font-medium text-[var(--text)]">{jobs.length}</span>
        </span>
        <span className="text-[var(--status-ok)]">{healthy} healthy</span>
        {failing.length > 0 && <span className="font-medium text-[var(--status-error)]">{failing.length} failing</span>}
      </div>
      {failing.length === 0 ? (
        <div className="flex flex-1 flex-col items-center justify-center gap-1 text-center">
          <p className="font-display text-2xl font-semibold text-[var(--status-ok)]">All in sync</p>
          <p className="text-xs text-[var(--text-muted)]">Every replication job succeeded its last run.</p>
        </div>
      ) : (
        <div className="min-h-0 flex-1 space-y-1 overflow-auto">
          {shown.map((j) => (
            <div key={`${j.connName}-${j.node}-${j.id}`} className="rounded-sm px-1.5 py-1 text-xs hover:bg-[var(--bg-muted)]">
              <div className="flex items-center justify-between gap-2">
                <span className="flex min-w-0 items-center gap-1.5">
                  <StatusDot status="error" />
                  <span className="truncate font-medium text-[var(--text)]">{j.id}</span>
                </span>
                <span className="shrink-0 text-[var(--text-muted)]">{j.connName} · {j.node}</span>
              </div>
              <p className="truncate text-[10px] text-[var(--status-error)]">
                {j.error ?? `${j.fail_count} consecutive failure${j.fail_count === 1 ? "" : "s"}`}
                {j.last_sync ? <> · last ok <Timestamp iso={new Date(j.last_sync * 1000).toISOString()} /></> : null}
              </p>
            </div>
          ))}
          <WidgetViewAllLink to="/tasks" shown={shown.length} total={failing.length} />
        </div>
      )}
    </div>
  )
}
