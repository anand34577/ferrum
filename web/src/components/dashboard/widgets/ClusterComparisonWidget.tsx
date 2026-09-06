import { ChevronRight, Loader2 } from "lucide-react"
import { Link } from "react-router-dom"
import { Meter } from "@/components/ui/meter"
import { StatusDot } from "@/components/ui/status-dot"
import type { WidgetSettings } from "@/lib/dashboardTypes"
import { summarizeFleet, useFleetOverview } from "@/lib/fleet"
import { formatBytes } from "@/lib/utils"
import { WidgetError } from "@/components/dashboard/WidgetChrome"

/** Side-by-side comparison of every Proxmox cluster/server: scale (nodes,
 * guests) next to load (CPU / memory / storage) — the multi-cluster
 * differentiator view, sortable by the widget setting. */
export function ClusterComparisonWidget({ settings }: { settings: WidgetSettings }) {
  const sort = settings.sort ?? "cpu"
  const { data: overview, isLoading, isError } = useFleetOverview()
  if (isLoading) return <Loader2 className="h-4 w-4 animate-spin text-[var(--text-muted)]" />
  if (isError) return <WidgetError />

  const rows = summarizeFleet(overview).connections
  if (rows.length === 0) return <p className="text-sm text-[var(--text-muted)]">No connections configured yet.</p>

  const value = (r: (typeof rows)[number]): string | number => {
    switch (sort) {
      case "name": return r.name.toLowerCase()
      case "memory": return r.memory.pct
      case "storage": return r.storage.pct
      case "vms": return r.vms.total + r.lxcs.total
      default: return r.cpu.pct
    }
  }
  rows.sort((a, b) => {
    const va = value(a)
    const vb = value(b)
    if (typeof va === "string" || typeof vb === "string") return sort === "name" ? String(va).localeCompare(String(vb)) : 0
    return vb - va
  })

  return (
    <div className="h-full overflow-auto">
      <table className="w-full text-xs">
        <thead className="sticky top-0 bg-[var(--bg-surface)]">
          <tr className="border-b border-[var(--border)] font-mono text-[10px] font-semibold uppercase tracking-wider text-[var(--text-muted)]">
            <th className="py-1.5 pr-2 text-left">Connection</th>
            <th className="px-2 py-1.5 text-right">Nodes</th>
            <th className="px-2 py-1.5 text-right">Guests</th>
            <th className="w-24 px-2 py-1.5 text-right">CPU</th>
            <th className="w-24 px-2 py-1.5 text-right">Memory</th>
            <th className="w-24 pl-2 py-1.5 text-right">Storage</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((c) => (
            <tr key={c.connectionId} className="border-b border-[var(--border)] last:border-0">
              <td className="max-w-44 py-1.5 pr-2">
                <div className="flex items-center gap-1.5">
                  <StatusDot status={c.online ? "ok" : "error"} />
                  <div className="min-w-0">
                    <p className="flex items-center gap-1 truncate font-medium">
                      {c.name}
                      <Link to="/inventory" className="text-[var(--text-faint)] hover:text-[var(--text)]" aria-label={`Open inventory for ${c.name}`}>
                        <ChevronRight className="h-3 w-3" />
                      </Link>
                    </p>
                    <p className="truncate font-mono text-[9px] text-[var(--text-faint)]">
                      {c.online ? (c.cluster ? `${c.cluster.name}${c.cluster.quorate ? "" : " · no quorum"}` : "standalone") : c.error ?? "offline"}
                    </p>
                  </div>
                </div>
              </td>
              <td className="px-2 py-1.5 text-right tabular">{c.online ? `${c.nodes.online}/${c.nodes.total}` : "—"}</td>
              <td className="px-2 py-1.5 text-right tabular">{c.online ? c.vms.total + c.lxcs.total : "—"}</td>
              <td className="px-2 py-1.5">{c.online ? <Meter value={c.cpu.pct} showLabel label="CPU" /> : <span className="text-[var(--text-faint)]">—</span>}</td>
              <td className="px-2 py-1.5">{c.online ? <Meter value={c.memory.pct} showLabel label="Memory" /> : <span className="text-[var(--text-faint)]">—</span>}</td>
              <td className="py-1.5 pl-2">{c.online ? <Meter value={c.storage.pct} showLabel label="Storage" /> : <span className="text-[var(--text-faint)]">—</span>}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {rows.some((c) => c.online) && (
        <p className="pt-1.5 text-center text-[10px] text-[var(--text-faint)]">
          Fleet: {formatBytes(summarizeFleet(overview).memUsed)} memory · {formatBytes(summarizeFleet(overview).stoUsed)} storage in use
        </p>
      )}
    </div>
  )
}
