import { useQueries } from "@tanstack/react-query"
import { Loader2 } from "lucide-react"
import { api, type PBSDatastore } from "@/lib/api"
import type { WidgetSettings } from "@/lib/dashboardTypes"
import { scopedConnection, useConnections } from "@/lib/fleet"
import { formatBytes } from "@/lib/utils"
import { WidgetError } from "@/components/dashboard/WidgetChrome"

interface FleetDatastore extends PBSDatastore {
  connName: string
}

/** Backup-storage health across every PBS server the fleet manages — usage
 * and maintenance state, the surface a multi-server setup needs so backup
 * capacity doesn't silently run out on a box nobody's looking at. One
 * request per PBS connection, same O(connections) cost as the cluster-log
 * and replication widgets. */
export function PbsDatastoresWidget({ settings }: { settings: WidgetSettings }) {
  const connId = scopedConnection(settings)
  const { data: connections, isError: connError } = useConnections()

  const targets = (connections ?? []).filter((c) => c.type === "pbs" && (connId === "all" || c.id === connId))

  const storeQueries = useQueries({
    queries: targets.map((c) => ({
      queryKey: ["pbs-datastores", c.id],
      queryFn: ({ signal }: { signal: AbortSignal }) => api.get<PBSDatastore[]>(`/connections/${c.id}/pbs/datastores`, { signal }),
      refetchInterval: 30_000,
      retry: false,
    })),
  })

  if (connError) return <WidgetError />
  if (targets.length === 0) {
    return <p className="text-sm text-[var(--text-muted)]">No PBS servers configured.</p>
  }
  if (storeQueries.some((q) => q.isLoading)) return <Loader2 className="h-4 w-4 animate-spin text-[var(--text-muted)]" />
  if (storeQueries.every((q) => q.isError)) return <WidgetError />

  const stores: FleetDatastore[] = targets.flatMap((c, i) => (storeQueries[i].data ?? []).map((d) => ({ ...d, connName: c.name })))

  if (stores.length === 0) {
    return <p className="text-sm text-[var(--text-muted)]">No datastores reporting yet.</p>
  }

  return (
    <div className="h-full space-y-1.5 overflow-auto">
      {stores.map((d) => {
        const pct = d.total ? ((d.used ?? 0) / d.total) * 100 : 0
        const tone = d.error || d.maintenance ? "error" : pct >= 90 ? "error" : pct >= 75 ? "warn" : "ok"
        return (
          <div key={`${d.connName}-${d.store}`} className="rounded-md bg-[var(--bg-muted)] px-2.5 py-1.5">
            <div className="flex items-center justify-between gap-2 text-xs">
              <span className="min-w-0 truncate font-medium text-[var(--text)]">{d.store}</span>
              <span className="shrink-0 text-[var(--text-muted)]">{d.connName}</span>
            </div>
            {d.error ? (
              <p className="mt-0.5 truncate text-[10px] text-[var(--status-error)]">{d.error}</p>
            ) : (
              <>
                <div className="mt-1 h-1.5 w-full overflow-hidden rounded-full bg-[var(--bg-elevated)]">
                  <div
                    className="h-full rounded-full"
                    style={{
                      width: `${Math.min(100, pct)}%`,
                      background: tone === "error" ? "var(--status-error)" : tone === "warn" ? "var(--status-warn)" : "var(--status-ok)",
                    }}
                  />
                </div>
                <div className="mt-0.5 flex items-center justify-between text-[10px] text-[var(--text-muted)]">
                  <span>{d.total ? `${formatBytes(d.used ?? 0)} / ${formatBytes(d.total)}` : "—"}</span>
                  <span>{d.maintenance ? d.maintenance : `${pct.toFixed(0)}%`}</span>
                </div>
              </>
            )}
          </div>
        )
      })}
    </div>
  )
}
