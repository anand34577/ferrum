import { useQueries, useQuery, type UseQueryResult } from "@tanstack/react-query"
import { useCallback, useMemo } from "react"
import { api, type ConnectionInventory, type Task } from "@/lib/api"

export interface FleetTask extends Task {
  connId: string
  connName: string
}

// Aggregates /cluster/tasks across every configured connection so widgets and
// the Task Center page share one polling strategy. An optional connection id
// scopes the stream to one Proxmox host/cluster.
//
// This deliberately does NOT fan out over nodes: PVE aggregates the task log
// cluster-wide, so one request per *connection* returns what one request per
// *node* used to, at 1/N the upstream cost on every 10s tick.
export function useClusterTasks(scopeConnId?: string) {
  const { data: inventory } = useQuery({
    queryKey: ["inventory"],
    queryFn: ({ signal }) => api.get<ConnectionInventory[]>("/inventory/", { signal }),
  })

  // Keyed on the id/name pairs, not on `inventory` itself: the inventory
  // payload changes identity on every poll (live CPU/memory numbers), but the
  // set of connections almost never does.
  const targetKey = JSON.stringify(
    (inventory ?? [])
      .filter((conn) => !scopeConnId || scopeConnId === "all" || conn.connectionId === scopeConnId)
      .map((conn) => [conn.connectionId, conn.name]),
  )
  const targets = useMemo(
    () => (JSON.parse(targetKey) as [string, string][]).map(([connId, connName]) => ({ connId, connName })),
    [targetKey],
  )

  // React Query only memoizes `combine` when the function itself is stable,
  // hence useCallback. The merged, sorted list then keeps its identity until
  // a connection's task data actually changes — built inline, every consumer
  // got a brand-new array on every render, which re-ran each page's
  // filter/column memos and re-sorted the whole log for unrelated state
  // changes (opening a dialog, ticking a filter).
  const combine = useCallback(
    (results: UseQueryResult<Task[]>[]) => {
      const tasks: FleetTask[] = results.flatMap((q, i) =>
        (q.data ?? []).map((task) => ({ ...task, connId: targets[i].connId, connName: targets[i].connName })),
      )
      tasks.sort((a, b) => b.starttime - a.starttime)
      return {
        tasks,
        isLoading: results.some((q) => q.isLoading),
        isError: results.length > 0 && results.every((q) => q.isError),
        refetch: () => results.forEach((q) => void q.refetch()),
      }
    },
    [targets],
  )

  return useQueries({
    queries: targets.map((t) => ({
      queryKey: ["cluster-tasks", t.connId],
      queryFn: ({ signal }: { signal: AbortSignal }) =>
        api.get<Task[]>(`/connections/${t.connId}/cluster/tasks`, { signal }),
      refetchInterval: 10_000,
    })),
    combine,
  })
}
