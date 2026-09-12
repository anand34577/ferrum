import { StatusDot } from "@/components/ui/status-dot"
import { useFleetOverview, useScopedInventory } from "@/lib/fleet"
import type { WidgetSettings } from "@/lib/dashboardTypes"
import { formatRelativeTime } from "@/lib/utils"
import { WidgetError } from "@/components/dashboard/WidgetChrome"

export function ConnectionStatusWidget({ settings }: { settings: WidgetSettings }) {
  const { connections, isError } = useScopedInventory(settings)
  // /overview rows carry the poll latency + last-checked timestamp the raw
  // inventory rows don't have; both queries share a cache key so this costs
  // nothing once the overview page or another widget has fetched it.
  const { data: overview } = useFleetOverview()
  const overviewById = new Map((overview ?? []).map((o) => [o.connectionId, o]))

  if (isError) return <WidgetError />

  if (!connections.length) {
    return <p className="text-sm text-[var(--text-muted)]">No connections configured yet.</p>
  }

  return (
    <div className="space-y-2">
      {connections.map((c) => {
        const ov = overviewById.get(c.connectionId)
        return (
          <div key={c.connectionId} className="flex items-center justify-between gap-2 rounded-md bg-[var(--bg-muted)] px-3 py-2">
            <span className="flex min-w-0 items-center gap-2 text-sm font-medium">
              <StatusDot status={c.online ? "ok" : "error"} />
              <span className="truncate">{c.name}</span>
            </span>
            {c.online ? (
              <span className="flex shrink-0 items-center gap-2.5 text-xs text-[var(--text-muted)] tabular">
                {ov?.latencyMs !== undefined && (
                  <span title="Round-trip time of the last fleet poll">{ov.latencyMs} ms</span>
                )}
                <span title={ov?.checkedAt}>checked {ov ? formatRelativeTime(ov.checkedAt) : "—"}</span>
              </span>
            ) : (
              <span className="shrink-0 truncate text-xs text-[var(--status-error)]" title={c.error}>
                {c.error ?? "unreachable"}
              </span>
            )}
          </div>
        )
      })}
    </div>
  )
}
