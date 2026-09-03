import { ChevronRight } from "lucide-react"
import { Link } from "react-router-dom"
import { StatusDot } from "@/components/ui/status-dot"
import type { WidgetSettings } from "@/lib/dashboardTypes"
import { summarizeFleet, useFleetOverview, utilizationTone } from "@/lib/fleet"
import { cn, formatBytes } from "@/lib/utils"

const toneClass = { ok: "text-[var(--status-ok)]", warn: "text-[var(--status-warn)]", error: "text-[var(--status-error)]" } as const

function Bar({ pct }: { pct: number }) {
  const tone = utilizationTone(pct)
  return (
    <div className="flex items-center justify-end gap-2">
      <div className="h-1.5 w-full min-w-10 overflow-hidden rounded-sm bg-[var(--track)]">
        <div
          className={cn("h-full rounded-sm", tone === "error" ? "bg-[var(--status-error)]" : tone === "warn" ? "bg-[var(--status-warn)]" : "bg-brand-500")}
          style={{ width: `${Math.min(100, pct)}%` }}
        />
      </div>
      <span className={cn("w-9 shrink-0 text-right text-[11px] tabular", toneClass[tone])}>{pct.toFixed(0)}%</span>
    </div>
  )
}

/** Side-by-side comparison of every Proxmox cluster/server: scale (nodes,
 * guests) next to load (CPU / memory / storage) — the multi-cluster
 * differentiator view, sortable by the widget setting. */
export function ClusterComparisonWidget({ settings }: { settings: WidgetSettings }) {
  const sort = settings.sort ?? "cpu"
  const { data: overview, isLoading } = useFleetOverview()
  if (isLoading) return <p className="text-sm text-[var(--text-muted)]">Loading fleet…</p>

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
          <tr className="border-b border-[var(--border)] text-[10px] uppercase tracking-wide text-[var(--text-muted)]">
            <th className="py-1.5 pr-2 text-left font-medium">Connection</th>
            <th className="px-2 py-1.5 text-right font-medium">Nodes</th>
            <th className="px-2 py-1.5 text-right font-medium">Guests</th>
            <th className="w-24 px-2 py-1.5 text-right font-medium">CPU</th>
            <th className="w-24 px-2 py-1.5 text-right font-medium">Memory</th>
            <th className="w-24 pl-2 py-1.5 text-right font-medium">Storage</th>
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
              <td className="px-2 py-1.5">{c.online ? <Bar pct={c.cpu.pct} /> : <span className="text-[var(--text-faint)]">—</span>}</td>
              <td className="px-2 py-1.5">{c.online ? <Bar pct={c.memory.pct} /> : <span className="text-[var(--text-faint)]">—</span>}</td>
              <td className="py-1.5 pl-2">{c.online ? <Bar pct={c.storage.pct} /> : <span className="text-[var(--text-faint)]">—</span>}</td>
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
