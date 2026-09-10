import { Meter } from "@/components/ui/meter"
import type { WidgetSettings } from "@/lib/dashboardTypes"
import { useScopedInventory } from "@/lib/fleet"
import { formatBytes } from "@/lib/utils"
import { WidgetError } from "@/components/dashboard/WidgetChrome"
import { WidgetViewAllLink } from "@/components/dashboard/WidgetViewAllLink"

export function StorageUsageWidget({ settings }: { settings: WidgetSettings }) {
  const { resources, isError } = useScopedInventory(settings)
  if (isError) return <WidgetError />

  const eligible = resources
    .filter((r) => r.type === "storage" && (r.maxdisk ?? 0) > 0)
    .sort((a, b) => (b.disk ?? 0) / (b.maxdisk || 1) - (a.disk ?? 0) / (a.maxdisk || 1))
  const pools = eligible.slice(0, 8)

  if (pools.length === 0) {
    return <p className="text-sm text-[var(--text-muted)]">No storage pools found.</p>
  }

  return (
    <div className="flex h-full flex-col">
      <div className="space-y-1.5">
        {pools.map((p) => {
          const pct = Math.min(100, ((p.disk ?? 0) / (p.maxdisk || 1)) * 100)
          return (
            <div key={p.id} className="flex items-center gap-2 text-sm">
              <span className="w-28 shrink-0 truncate" title={p.storage ?? p.name}>{p.storage ?? p.name}</span>
              <Meter value={pct} label={`${p.storage ?? p.name} usage`} className="min-w-16 flex-1" />
              {/* Fixed width (not just shrink-0) — every row's byte string is a
                  different length, and without a fixed column the Meter above
                  (flex-1) claims whatever's left, so bars end up visibly
                  different lengths despite representing the same 0-100% scale. */}
              <span className="w-32 shrink-0 whitespace-nowrap text-right text-xs text-[var(--text-muted)] tabular">
                {formatBytes(p.disk ?? 0)} / {formatBytes(p.maxdisk ?? 0)}
              </span>
            </div>
          )
        })}
      </div>
      <WidgetViewAllLink to="/storage" shown={pools.length} total={eligible.length} />
    </div>
  )
}
