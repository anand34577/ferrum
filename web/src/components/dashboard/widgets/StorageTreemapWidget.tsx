import { TreemapChart } from "@/components/charts/TreemapChart"
import type { WidgetSettings } from "@/lib/dashboardTypes"
import { useScopedInventory } from "@/lib/fleet"
import { formatBytes } from "@/lib/utils"
import { WidgetError } from "@/components/dashboard/WidgetChrome"

// Storage-usage treemap — the disk-space-analyzer view: tile area encodes
// used bytes, color intensity encodes how full each storage is.
export function StorageTreemapWidget({ settings }: { settings: WidgetSettings }) {
  const { resources, isError } = useScopedInventory(settings)
  if (isError) return <WidgetError />

  const tiles = resources
    .filter((r) => r.type === "storage" && (r.disk ?? 0) > 0)
    .map((r) => {
      const used = r.disk ?? 0
      const total = r.maxdisk ?? 0
      const pct = total > 0 ? (used / total) * 100 : 0
      return {
        name: r.storage ?? r.name ?? r.id,
        size: used,
        ratio: pct / 100,
        sub: total > 0 ? `${pct.toFixed(0)}% of ${formatBytes(total)}` : undefined,
      }
    })
    .sort((a, b) => b.size - a.size)

  if (tiles.length === 0) {
    return <p className="text-sm text-[var(--text-muted)]">No storage reporting usage yet.</p>
  }

  return (
    <div>
      <TreemapChart data={tiles} valueFormatter={formatBytes} height={190} />
      <p className="mt-1 text-center text-[10px] text-[var(--text-faint)]">
        Area = used space · darker = closer to full
      </p>
    </div>
  )
}
