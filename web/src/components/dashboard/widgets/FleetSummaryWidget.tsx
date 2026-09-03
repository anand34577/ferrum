import { Cpu, HardDrive, MemoryStick, Server } from "lucide-react"
import type { WidgetSettings } from "@/lib/dashboardTypes"
import { useScopedInventory } from "@/lib/fleet"
import { cn, formatBytes } from "@/lib/utils"

export function FleetSummaryWidget({ settings }: { settings: WidgetSettings }) {
  const { resources, isLoading } = useScopedInventory(settings)

  const nodes = resources.filter((r) => r.type === "node")
  const guests = resources.filter((r) => r.type === "qemu" || r.type === "lxc")
  const running = guests.filter((g) => g.status === "running")
  const totalMem = nodes.reduce((sum, n) => sum + (n.maxmem ?? 0), 0)
  const usedMem = nodes.reduce((sum, n) => sum + (n.mem ?? 0), 0)
  const totalDisk = nodes.reduce((sum, n) => sum + (n.maxdisk ?? 0), 0)
  const usedDisk = nodes.reduce((sum, n) => sum + (n.disk ?? 0), 0)

  const cards = [
    {
      label: "Nodes online",
      value: `${nodes.filter((n) => n.status === "online").length}`,
      total: `${nodes.length}`,
      progress: nodes.length ? (nodes.filter((n) => n.status === "online").length / nodes.length) * 100 : 0,
      icon: Server,
    },
    {
      label: "Guests running",
      value: `${running.length}`,
      total: `${guests.length}`,
      progress: guests.length ? (running.length / guests.length) * 100 : 0,
      icon: Cpu,
    },
    {
      label: "Memory used",
      value: formatBytes(usedMem),
      total: formatBytes(totalMem),
      progress: totalMem ? (usedMem / totalMem) * 100 : 0,
      icon: MemoryStick,
    },
    {
      label: "Storage used",
      value: formatBytes(usedDisk),
      total: formatBytes(totalDisk),
      progress: totalDisk ? (usedDisk / totalDisk) * 100 : 0,
      icon: HardDrive,
    },
  ]

  return (
    <div className="grid h-full grid-cols-2 gap-3 sm:grid-cols-4">
      {cards.map((c) => (
        <div key={c.label} className="flex flex-col justify-center rounded-md bg-[var(--bg-muted)] px-3 py-2">
          <div className="flex items-center justify-between">
            <div className="min-w-0">
              <p className="text-[10px] text-[var(--text-muted)]">{c.label}</p>
              <p className="font-display text-lg font-semibold">
                {isLoading ? "-" : c.value}
                <span className="ml-1 text-xs font-normal text-[var(--text-muted)]">/ {c.total}</span>
              </p>
            </div>
            <c.icon className="h-6 w-6 shrink-0 text-brand-500 opacity-70" />
          </div>
          <div className="mt-1.5 h-1 overflow-hidden rounded-full bg-[var(--track)]">
            <div
              className={cn("h-full rounded-full", c.progress > 90 ? "bg-[var(--status-error)]" : c.progress > 75 ? "bg-[var(--status-warn)]" : "bg-brand-500")}
              style={{ width: `${Math.min(100, c.progress)}%` }}
            />
          </div>
        </div>
      ))}
    </div>
  )
}
