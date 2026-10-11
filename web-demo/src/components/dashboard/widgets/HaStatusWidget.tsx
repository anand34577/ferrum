import { StatusDot } from "@/components/ui/status-dot"
import type { WidgetSettings } from "@/lib/dashboardTypes"
import { useFleetOverview } from "@/lib/fleet"
import { WidgetError } from "@/components/dashboard/WidgetChrome"

/** Cluster quorum and HA coverage at a glance — the thing you actually check
 * after a network blip: is every cluster still quorate, and how many guests
 * are HA-protected if a node goes down. Reuses the fleet overview poll every
 * other summary widget already shares, so this costs zero extra requests. */
export function HaStatusWidget({ settings: _settings }: { settings: WidgetSettings }) {
  const { data, isError } = useFleetOverview()
  if (isError) return <WidgetError />

  const conns = data ?? []
  const clustered = conns.filter((c) => c.cluster)
  const notQuorate = clustered.filter((c) => c.cluster && !c.cluster.quorate)
  const haTotal = conns.reduce((s, c) => s + c.haGuests, 0)

  if (conns.length === 0) {
    return <p className="text-sm text-[var(--text-muted)]">No connections configured.</p>
  }

  return (
    <div className="flex h-full flex-col gap-2">
      <div className="flex flex-wrap gap-x-6 gap-y-1 text-xs text-[var(--text-muted)]">
        <span>
          Clusters: <span className="font-medium text-[var(--text)]">{clustered.length}</span>
        </span>
        <span>
          HA-protected guests: <span className="font-medium text-[var(--text)]">{haTotal}</span>
        </span>
        {notQuorate.length > 0 && (
          <span className="font-medium text-[var(--status-error)]">{notQuorate.length} cluster{notQuorate.length === 1 ? "" : "s"} lost quorum</span>
        )}
      </div>
      <div className="min-h-0 flex-1 space-y-1 overflow-auto">
        {conns.map((c) => {
          const status = !c.online ? "error" : c.cluster && !c.cluster.quorate ? "error" : "ok"
          const label = !c.online
            ? "offline"
            : c.cluster
              ? c.cluster.quorate
                ? `quorate · ${c.cluster.nodes} node${c.cluster.nodes === 1 ? "" : "s"}`
                : "no quorum"
              : "standalone"
          return (
            <div key={c.connectionId} className="flex items-center justify-between gap-2 rounded-sm px-1.5 py-1 text-xs hover:bg-[var(--bg-muted)]">
              <span className="flex min-w-0 items-center gap-1.5">
                <StatusDot status={status} />
                <span className="truncate">{c.cluster?.name ?? c.name}</span>
              </span>
              <span className="flex shrink-0 items-center gap-2 text-[var(--text-muted)]">
                {c.haGuests > 0 && <span>{c.haGuests} HA</span>}
                <span className={status === "error" ? "text-[var(--status-error)]" : ""}>{label}</span>
              </span>
            </div>
          )
        })}
      </div>
    </div>
  )
}
