import { useQuery } from "@tanstack/react-query"
import { Meter } from "@/components/ui/meter"
import type { WidgetSettings } from "@/lib/dashboardTypes"
import { api, type ConnectionInventory } from "@/lib/api"
import { formatBytes } from "@/lib/utils"
import { WidgetError } from "@/components/dashboard/WidgetChrome"

/** Stacked memory usage bars per node (used vs total, with swap pressure
 * context) — the memory-at-a-glance list. */
export function MemoryByNodeWidget({ settings }: { settings: WidgetSettings }) {
  const connId = settings.connection ?? "all"
  const { data, isError } = useQuery({
    queryKey: ["inventory"],
    queryFn: () => api.get<ConnectionInventory[]>("/inventory/"),
    refetchInterval: 15_000,
  })

  if (isError) return <WidgetError />

  const nodes = (data ?? [])
    .filter((c) => connId === "all" || c.connectionId === connId)
    .flatMap((c) => c.resources ?? [])
    .filter((r) => r.type === "node" && (r.maxmem ?? 0) > 0)
    .map((n) => ({
      name: n.node,
      used: n.mem ?? 0,
      total: n.maxmem ?? 0,
      pct: ((n.mem ?? 0) / (n.maxmem || 1)) * 100,
    }))
    .sort((a, b) => b.pct - a.pct)

  if (nodes.length === 0) {
    return <p className="text-sm text-[var(--text-muted)]">No nodes reporting yet.</p>
  }

  return (
    <div className="flex h-full flex-col justify-center gap-1.5 overflow-auto">
      {nodes.map((n) => (
        <div key={n.name} className="flex items-center gap-2 text-xs">
          <span className="w-24 shrink-0 truncate text-[var(--text-muted)]" title={n.name}>{n.name}</span>
          <Meter value={n.pct} size="md" showLabel label={`${n.name} memory`} className="flex-1" />
          <span className="hidden w-32 shrink-0 text-right text-[10px] text-[var(--text-faint)] tabular sm:block">
            {formatBytes(n.used)} / {formatBytes(n.total)}
          </span>
        </div>
      ))}
      <p className="pt-1 text-center text-[10px] text-[var(--text-faint)]">Worst first · bars show used memory of total</p>
    </div>
  )
}
