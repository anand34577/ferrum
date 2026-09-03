import { useQuery } from "@tanstack/react-query"
import type { WidgetSettings } from "@/lib/dashboardTypes"
import { api, type ConnectionInventory } from "@/lib/api"
import { cn, formatBytes } from "@/lib/utils"

/** Stacked memory usage bars per node (used vs total, with swap pressure
 * context) — the memory-at-a-glance list. */
export function MemoryByNodeWidget({ settings }: { settings: WidgetSettings }) {
  const connId = settings.connection ?? "all"
  const { data } = useQuery({
    queryKey: ["inventory"],
    queryFn: () => api.get<ConnectionInventory[]>("/inventory/"),
    refetchInterval: 15_000,
  })

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
      {nodes.map((n) => {
        const tone = n.pct >= 90 ? "error" : n.pct >= 75 ? "warn" : "ok"
        return (
          <div key={n.name} className="flex items-center gap-2 text-xs">
            <span className="w-24 shrink-0 truncate text-[var(--text-muted)]" title={n.name}>{n.name}</span>
            <div className="h-2.5 flex-1 overflow-hidden rounded-sm bg-[var(--track)]">
              <div
                className={cn("h-full rounded-sm", tone === "error" ? "bg-[var(--status-error)]" : tone === "warn" ? "bg-[var(--status-warn)]" : "bg-brand-500")}
                style={{ width: `${Math.min(100, n.pct)}%` }}
              />
            </div>
            <span className={cn("w-10 shrink-0 text-right font-medium tabular", tone === "error" ? "text-[var(--status-error)]" : tone === "warn" ? "text-[var(--status-warn)]" : "text-[var(--text)]")}>
              {n.pct.toFixed(0)}%
            </span>
            <span className="hidden w-32 shrink-0 text-right text-[10px] text-[var(--text-faint)] tabular sm:block">
              {formatBytes(n.used)} / {formatBytes(n.total)}
            </span>
          </div>
        )
      })}
      <p className="pt-1 text-center text-[10px] text-[var(--text-faint)]">Worst first · bars show used memory of total</p>
    </div>
  )
}
