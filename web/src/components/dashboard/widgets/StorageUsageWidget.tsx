import type { WidgetSettings } from "@/lib/dashboardTypes"
import { useScopedInventory } from "@/lib/fleet"
import { formatBytes } from "@/lib/utils"

export function StorageUsageWidget({ settings }: { settings: WidgetSettings }) {
  const { resources } = useScopedInventory(settings)

  const pools = resources
    .filter((r) => r.type === "storage" && (r.maxdisk ?? 0) > 0)
    .sort((a, b) => (b.disk ?? 0) / (b.maxdisk || 1) - (a.disk ?? 0) / (a.maxdisk || 1))
    .slice(0, 8)

  if (pools.length === 0) {
    return <p className="text-sm text-[var(--text-muted)]">No storage pools found.</p>
  }

  return (
    <div className="space-y-1.5">
      {pools.map((p) => {
        const pct = Math.min(100, ((p.disk ?? 0) / (p.maxdisk || 1)) * 100)
        return (
          <div key={p.id} className="flex items-center gap-2 text-sm">
            <span className="w-28 truncate">{p.storage ?? p.name}</span>
            <div className="h-1.5 flex-1 overflow-hidden rounded-sm bg-[var(--track)]">
              <div
                className={pct > 85 ? "h-full bg-[var(--status-error)]" : "h-full bg-brand-500"}
                style={{ width: `${pct}%` }}
              />
            </div>
            <span className="w-24 text-right text-xs text-[var(--text-muted)]">
              {formatBytes(p.disk ?? 0)} / {formatBytes(p.maxdisk ?? 0)}
            </span>
          </div>
        )
      })}
    </div>
  )
}
