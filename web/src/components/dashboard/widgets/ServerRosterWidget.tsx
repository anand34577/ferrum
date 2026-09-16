import { Badge } from "@/components/ui/badge"
import { StatusDot } from "@/components/ui/status-dot"
import type { WidgetSettings } from "@/lib/dashboardTypes"
import { useConnections, useFleetOverview } from "@/lib/fleet"
import { WidgetError } from "@/components/dashboard/WidgetChrome"

/** The one-look fleet roster: every managed Proxmox server or PBS instance,
 * its type, and its headline health metric — the thing a multi-server
 * operator actually wants above the fold instead of clicking into each
 * connection one at a time. Zero extra requests: both queries are already
 * shared by every other dashboard widget. */
export function ServerRosterWidget({ settings: _settings }: { settings: WidgetSettings }) {
  const { data: connections, isError: connError } = useConnections()
  const { data: overview, isError: overviewError } = useFleetOverview()
  if (connError || overviewError) return <WidgetError />

  const conns = connections ?? []
  if (conns.length === 0) {
    return <p className="text-sm text-[var(--text-muted)]">No connections configured yet.</p>
  }

  const overviewById = new Map((overview ?? []).map((o) => [o.connectionId, o]))

  return (
    <div className="h-full space-y-1 overflow-auto">
      {conns.map((c) => {
        const ov = overviewById.get(c.id)
        const online = ov?.online ?? false
        return (
          <div key={c.id} className="flex items-center justify-between gap-2 rounded-sm px-1.5 py-1.5 text-xs hover:bg-[var(--bg-muted)]">
            <span className="flex min-w-0 items-center gap-2">
              <StatusDot status={online ? "ok" : "error"} />
              <span className="truncate font-medium text-[var(--text)]">{c.name}</span>
              <Badge variant="outline" className="shrink-0 text-[10px]">{c.type === "pbs" ? "PBS" : "PVE"}</Badge>
            </span>
            <span className="flex shrink-0 items-center gap-2.5 text-[var(--text-muted)]">
              {!online ? (
                <span className="text-[var(--status-error)]">{ov?.error ?? "unreachable"}</span>
              ) : c.type === "pbs" ? (
                <span>{ov?.latencyMs !== undefined ? `${ov.latencyMs} ms` : "online"}</span>
              ) : (
                <>
                  <span>{ov?.nodes.online ?? 0}/{ov?.nodes.total ?? 0} nodes</span>
                  <span>{(ov?.vms.running ?? 0) + (ov?.lxcs.running ?? 0)} running</span>
                  <span className={((ov?.cpu.pct ?? 0) >= 90) ? "text-[var(--status-error)]" : ((ov?.cpu.pct ?? 0) >= 75) ? "text-[var(--status-warn)]" : ""}>
                    {(ov?.cpu.pct ?? 0).toFixed(0)}% cpu
                  </span>
                </>
              )}
            </span>
          </div>
        )
      })}
    </div>
  )
}
