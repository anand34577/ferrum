import { Boxes, Container, Cpu, HardDrive, MemoryStick, Network, ShieldCheck, TriangleAlert } from "lucide-react"
import { summarizeFleet } from "@/lib/fleet"
import { useFleetOverview } from "@/lib/fleet"
import { cn, formatBytes, formatPercentFine } from "@/lib/utils"

/** The at-a-glance KPI matrix for the whole estate: servers, nodes, VMs,
 * containers, then the three resource families and alerts. Fleet-wide —
 * one tile per question an admin asks first. */
export function FleetOverviewWidget() {
  const { data: overview, isLoading } = useFleetOverview()
  if (isLoading) return <p className="text-sm text-[var(--text-muted)]">Loading fleet…</p>
  const t = summarizeFleet(overview)

  const primary = [
    { label: "Proxmox servers", value: `${t.serversOnline}`, sub: `of ${t.serversTotal}`, icon: Network, tone: t.offline.length > 0 ? "warn" : "ok" },
    { label: "Nodes online", value: `${t.nodesOnline}`, sub: `of ${t.nodesTotal}`, icon: ShieldCheck, tone: t.nodesOnline < t.nodesTotal ? "warn" : "ok" },
    { label: "Virtual machines", value: `${t.vms}`, sub: `${t.vmsRunning} running`, icon: Boxes, tone: "default" },
    { label: "Containers", value: `${t.lxcs}`, sub: `${t.lxcsRunning} running`, icon: Container, tone: "default" },
  ] as const

  const meters = [
    { label: "CPU", pct: t.cpuPct, value: formatPercentFine(t.cpuPct), sub: `${t.cores} cores`, icon: Cpu },
    { label: "Memory", pct: t.memPct, value: formatPercentFine(t.memPct), sub: `${formatBytes(t.memUsed)} / ${formatBytes(t.memTotal)}`, icon: MemoryStick },
    { label: "Storage", pct: t.stoPct, value: formatPercentFine(t.stoPct), sub: `${formatBytes(t.stoUsed)} / ${formatBytes(t.stoTotal)}`, icon: HardDrive },
  ]

  const alertTotal = t.alertsCritical + t.alertsWarning

  return (
    <div className="grid h-full grid-cols-2 content-start gap-2.5 lg:grid-cols-4">
      {primary.map((c) => (
        <div key={c.label} className="rounded-md border border-[var(--border)] bg-[var(--bg-muted)]/60 px-3 py-2">
          <div className="flex items-center justify-between gap-2">
            <p className="truncate text-[10px] text-[var(--text-muted)]">{c.label}</p>
            <c.icon className="h-3.5 w-3.5 shrink-0 text-brand-500 opacity-80" />
          </div>
          <p className="font-display text-xl font-semibold leading-tight tabular">
            {c.value}
            <span className="ml-1.5 text-xs font-normal text-[var(--text-muted)]">{c.sub}</span>
          </p>
        </div>
      ))}
      {meters.map((m) => {
        const tone = m.pct >= 90 ? "error" : m.pct >= 75 ? "warn" : "ok"
        return (
          <div key={m.label} className="rounded-md border border-[var(--border)] bg-[var(--bg-muted)]/60 px-3 py-2">
            <div className="flex items-center justify-between gap-2">
              <p className="truncate text-[10px] text-[var(--text-muted)]">{m.label}</p>
              <m.icon className="h-3.5 w-3.5 shrink-0 text-brand-500 opacity-80" />
            </div>
            <p className={cn("font-display text-xl font-semibold leading-tight tabular", tone === "error" ? "text-[var(--status-error)]" : tone === "warn" ? "text-[var(--status-warn)]" : "text-[var(--status-ok)]")}>{m.value}</p>
            <div className="mt-1 h-1 overflow-hidden rounded-sm bg-[var(--track)]">
              <div
                className={cn("h-full rounded-sm", tone === "error" ? "bg-[var(--status-error)]" : tone === "warn" ? "bg-[var(--status-warn)]" : "bg-brand-500")}
                style={{ width: `${Math.min(100, m.pct)}%` }}
              />
            </div>
            <p className="mt-0.5 truncate text-[10px] text-[var(--text-faint)] tabular">{m.sub}</p>
          </div>
        )
      })}
      <div className="rounded-md border border-[var(--border)] bg-[var(--bg-muted)]/60 px-3 py-2">
        <div className="flex items-center justify-between gap-2">
          <p className="truncate text-[10px] text-[var(--text-muted)]">Alerts</p>
          <TriangleAlert className={cn("h-3.5 w-3.5 shrink-0", alertTotal > 0 ? "text-[var(--status-warn)]" : "text-[var(--status-ok)]")} />
        </div>
        <p className={cn("font-display text-xl font-semibold leading-tight tabular", t.alertsCritical > 0 ? "text-[var(--status-error)]" : alertTotal > 0 ? "text-[var(--status-warn)]" : "text-[var(--status-ok)]")}>{alertTotal}</p>
        <p className="mt-0.5 text-[10px] text-[var(--text-faint)]">{t.alertsCritical} critical · {t.alertsWarning} warning</p>
      </div>
      <div className="rounded-md border border-[var(--border)] bg-[var(--bg-muted)]/60 px-3 py-2">
        <div className="flex items-center justify-between gap-2">
          <p className="truncate text-[10px] text-[var(--text-muted)]">Workloads</p>
          <Boxes className="h-3.5 w-3.5 shrink-0 text-brand-500 opacity-80" />
        </div>
        <p className="font-display text-xl font-semibold leading-tight tabular">{t.running}</p>
        <p className="mt-0.5 text-[10px] text-[var(--text-faint)]">{t.stopped} stopped · {t.templates} templates</p>
      </div>
    </div>
  )
}
