import { useQueries, useQuery } from "@tanstack/react-query"
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

  const targets =
    inventory
      ?.filter((conn) => !scopeConnId || scopeConnId === "all" || conn.connectionId === scopeConnId)
      .map((conn) => ({ connId: conn.connectionId, connName: conn.name })) ?? []

  const taskQueries = useQueries({
    queries: targets.map((t) => ({
      queryKey: ["cluster-tasks", t.connId],
      queryFn: ({ signal }: { signal: AbortSignal }) =>
        api.get<Task[]>(`/connections/${t.connId}/cluster/tasks`, { signal }),
      refetchInterval: 10_000,
    })),
  })

  const tasks: FleetTask[] = targets.flatMap((t, i) =>
    (taskQueries[i].data ?? []).map((task) => ({ ...task, connId: t.connId, connName: t.connName })),
  )
  tasks.sort((a, b) => b.starttime - a.starttime)

  return {
    tasks,
    isLoading: taskQueries.some((q) => q.isLoading),
    isError: taskQueries.length > 0 && taskQueries.every((q) => q.isError),
    refetch: () => taskQueries.forEach((q) => q.refetch()),
  }
}
