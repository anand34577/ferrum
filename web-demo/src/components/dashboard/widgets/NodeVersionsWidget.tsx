import { useQueries } from "@tanstack/react-query"
import { Loader2 } from "lucide-react"
import { useMemo } from "react"
import { StatusDot } from "@/components/ui/status-dot"
import { api, type NodeStatus } from "@/lib/api"
import type { WidgetSettings } from "@/lib/dashboardTypes"
import { useScopedInventory } from "@/lib/fleet"
import { formatUptime } from "@/lib/utils"
import { WidgetError } from "@/components/dashboard/WidgetChrome"

/** PVE/kernel version per node, flagging drift from the fleet's most common
 * version — the thing you check before assuming every host behaves the same.
 * O(nodes) requests, same tradeoff FleetTrendWidget makes for RRD history:
 * fine at the node counts this app targets. */
export function NodeVersionsWidget({ settings }: { settings: WidgetSettings }) {
  const { connections, isError: inventoryError } = useScopedInventory(settings)

  const nodePairs = useMemo(
    () =>
      connections.flatMap((conn) =>
        (conn.resources ?? []).filter((r) => r.type === "node").map((r) => ({ connId: conn.connectionId, node: r.node })),
      ),
    [connections],
  )

  const statusQueries = useQueries({
    queries: nodePairs.map(({ connId, node }) => ({
      queryKey: ["node-status", connId, node],
      queryFn: () => api.get<NodeStatus>(`/connections/${connId}/nodes/${node}/status`),
      staleTime: 60_000,
      retry: false,
    })),
  })

  if (inventoryError) return <WidgetError />
  if (nodePairs.length === 0) return <p className="text-sm text-[var(--text-muted)]">No nodes reporting yet.</p>
  if (statusQueries.some((q) => q.isLoading)) return <Loader2 className="h-4 w-4 animate-spin text-[var(--text-muted)]" />
  if (statusQueries.every((q) => q.isError)) return <WidgetError />

  const rows = nodePairs
    .map(({ node }, i) => ({ node, status: statusQueries[i].data }))
    .filter((r): r is { node: string; status: NodeStatus } => !!r.status)

  const counts = new Map<string, number>()
  for (const r of rows) counts.set(r.status.pveversion, (counts.get(r.status.pveversion) ?? 0) + 1)
  const majority = Array.from(counts.entries()).sort(([, a], [, b]) => b - a)[0]?.[0]

  return (
    <div className="h-full space-y-1 overflow-auto">
      {rows.map((r) => {
        const drift = majority !== undefined && r.status.pveversion !== majority
        return (
          <div key={r.node} className="flex items-center justify-between gap-2 rounded-sm px-1.5 py-1 text-xs hover:bg-[var(--bg-muted)]">
            <span className="flex min-w-0 items-center gap-1.5">
              <StatusDot status={drift ? "warn" : "ok"} />
              <span className="truncate font-medium text-[var(--text)]">{r.node}</span>
            </span>
            <span className="flex shrink-0 items-center gap-2 text-[10px] text-[var(--text-muted)]">
              <span className={drift ? "text-[var(--status-warn)]" : ""}>{r.status.pveversion}</span>
              <span>up {formatUptime(r.status.uptime)}</span>
            </span>
          </div>
        )
      })}
      {majority !== undefined && counts.size > 1 && (
        <p className="pt-1 text-center text-[10px] text-[var(--status-warn)]">Version drift across {counts.size} PVE builds</p>
      )}
    </div>
  )
}
