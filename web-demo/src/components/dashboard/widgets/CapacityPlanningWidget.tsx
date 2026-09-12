import type { WidgetSettings } from "@/lib/dashboardTypes"
import { useScopedInventory, utilizationTone } from "@/lib/fleet"
import { cn, formatBytes } from "@/lib/utils"
import { WidgetError } from "@/components/dashboard/WidgetChrome"

interface Row {
  label: string
  physical: number
  allocated: number
  used: number
  unit: (v: number) => string
}

/** Physical vs allocated vs actual usage for CPU and memory — exposes
 * overcommit (allocated > physical) and stranded capacity at a glance. */
export function CapacityPlanningWidget({ settings }: { settings: WidgetSettings }) {
  const { resources, isError } = useScopedInventory(settings)
  if (isError) return <WidgetError />

  let cores = 0
  let memTotal = 0
  let memUsed = 0
  let allocCores = 0
  let allocMem = 0
  for (const r of resources) {
    if (r.type === "node" && r.status === "online") {
      cores += r.maxcpu ?? 0
      memTotal += r.maxmem ?? 0
      memUsed += r.mem ?? 0
    } else if ((r.type === "qemu" || r.type === "lxc") && r.status === "running") {
      allocCores += r.maxcpu ?? 0
      allocMem += r.maxmem ?? 0
    }
  }

  if (cores === 0 && memTotal === 0) {
    return <p className="text-sm text-[var(--text-muted)]">No online nodes yet.</p>
  }

  const rows: Row[] = [
    {
      label: "CPU (cores)",
      physical: cores,
      allocated: allocCores,
      used: 0, // filled from overview-free math below
      unit: (v) => `${Math.round(v)} cores`,
    },
    {
      label: "Memory",
      physical: memTotal,
      allocated: allocMem,
      used: memUsed,
      unit: formatBytes,
    },
  ]
  // CPU "used" from node cpu fractions × cores.
  let cpuUsedCores = 0
  for (const r of resources) {
    if (r.type === "node" && r.status === "online") cpuUsedCores += (r.cpu ?? 0) * (r.maxcpu ?? 0)
  }
  rows[0].used = cpuUsedCores

  return (
    <div className="flex h-full flex-col justify-center gap-4 overflow-auto">
      {rows.map((row) => {
        const overcommit = row.physical > 0 ? row.allocated / row.physical : 0
        const usedPct = row.physical > 0 ? (row.used / row.physical) * 100 : 0
        const allocPct = row.physical > 0 ? (row.allocated / row.physical) * 100 : 0
        return (
          <div key={row.label} className="space-y-1.5">
            <div className="flex items-baseline justify-between gap-2">
              <p className="text-xs font-medium">{row.label}</p>
              <p className={cn("text-[10px] tabular", overcommit > 1 ? "font-medium text-[var(--status-warn)]" : "text-[var(--text-faint)]")}>
                allocated {row.unit(row.allocated)} / {row.unit(row.physical)} physical
                {overcommit > 1 ? ` · ${overcommit.toFixed(2)}× overcommit` : ""}
              </p>
            </div>
            {/* Physical capacity rail with allocated + used overlays */}
            <div
              className="relative h-3.5 overflow-hidden rounded-none bg-[var(--track)]"
              role="progressbar"
              aria-valuenow={Math.round(usedPct)}
              aria-valuemin={0}
              aria-valuemax={100}
              aria-label={`${row.label} actual use`}
            >
              {/* No title/tooltip here — the header line above already spells
                  out both numbers ("allocated X / Y physical"), and the
                  legend below explains the two colors, so a hover tooltip
                  repeating the same figures would be pure redundancy. */}
              <div className="absolute inset-y-0 left-0 bg-brand-200/70 dark:bg-brand-800/60" style={{ width: `${Math.min(100, allocPct)}%` }} />
              <div
                className={cn(
                  "absolute inset-y-0 left-0",
                  utilizationTone(usedPct) === "error" ? "bg-[var(--status-error)]" : utilizationTone(usedPct) === "warn" ? "bg-[var(--status-warn)]" : "bg-brand-500",
                )}
                style={{ width: `${Math.min(100, usedPct)}%` }}
              />
              {allocPct > 100 && (
                <div className="absolute inset-y-0 right-0 w-1 bg-[var(--status-warn)]" title="Allocated exceeds physical capacity" />
              )}
            </div>
            <div className="flex gap-4 text-[10px] text-[var(--text-faint)]">
              <span className="flex items-center gap-1"><span className="inline-block h-2 w-2 rounded-full bg-brand-500" /> actual use {row.unit(row.used)}</span>
              <span className="flex items-center gap-1"><span className="inline-block h-2 w-2 rounded-full bg-brand-300" /> guest allocation</span>
              <span className="flex items-center gap-1"><span className="inline-block h-2 w-2 rounded-full bg-[var(--track)] ring-1 ring-[var(--border)]" /> physical</span>
            </div>
          </div>
        )
      })}
      <p className="mt-auto text-center text-[10px] text-[var(--text-faint)]">Allocation counts running guests at their configured size — ballooning may reclaim memory.</p>
    </div>
  )
}
