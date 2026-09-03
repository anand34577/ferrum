import { useQueries, useQuery } from "@tanstack/react-query"
import { api, type ConnectionInventory, type Task } from "@/lib/api"

export interface FleetTask extends Task {
  connId: string
  connName: string
}

// Aggregates /nodes/{node}/tasks across every configured connection so
// widgets and the Task Center page share one polling strategy. An optional
// connection id scopes the stream to one Proxmox host/cluster.
export function useClusterTasks(scopeConnId?: string) {
  const { data: inventory } = useQuery({
    queryKey: ["inventory"],
    queryFn: ({ signal }) => api.get<ConnectionInventory[]>("/inventory/", { signal }),
  })

  const nodeTargets =
    inventory
      ?.filter((conn) => !scopeConnId || scopeConnId === "all" || conn.connectionId === scopeConnId)
      .flatMap((conn) =>
        (conn.resources ?? [])
          .filter((r) => r.type === "node")
          .map((n) => ({ connId: conn.connectionId, connName: conn.name, node: n.node })),
      ) ?? []

  const taskQueries = useQueries({
    queries: nodeTargets.map((t) => ({
      queryKey: ["tasks", t.connId, t.node],
      queryFn: ({ signal }) => api.get<Task[]>(`/connections/${t.connId}/nodes/${t.node}/tasks`, { signal }),
      refetchInterval: 10_000,
    })),
  })

  const tasks: FleetTask[] = nodeTargets.flatMap((t, i) =>
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
