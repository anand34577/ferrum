import { DonutChart, DonutLegend } from "@/components/charts/DonutChart"
import type { WidgetSettings } from "@/lib/dashboardTypes"
import { useScopedInventory } from "@/lib/fleet"
import { WidgetError } from "@/components/dashboard/WidgetChrome"

export function GuestStatusWidget({ settings }: { settings: WidgetSettings }) {
  const { resources, isError } = useScopedInventory(settings)
  if (isError) return <WidgetError />

  const guests = resources.filter((r) => r.type === "qemu" || r.type === "lxc")
  const running = guests.filter((g) => g.status === "running").length
  const stopped = guests.filter((g) => g.status === "stopped").length
  const other = guests.length - running - stopped

  const slices = [
    { name: "Running", value: running, color: "var(--status-ok)" },
    { name: "Stopped", value: stopped, color: "var(--status-error)" },
    ...(other > 0 ? [{ name: "Other", value: other, color: "var(--status-warn)" }] : []),
  ]

  if (guests.length === 0) {
    return <p className="text-sm text-[var(--text-muted)]">No guests found.</p>
  }

  return (
    <div className="flex h-full flex-col items-center justify-center gap-2">
      <DonutChart data={slices} centerValue={String(guests.length)} centerLabel="guests" height={140} />
      <DonutLegend data={slices} />
    </div>
  )
}
