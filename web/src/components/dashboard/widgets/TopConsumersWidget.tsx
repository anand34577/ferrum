import { RankedBarChart } from "@/components/charts/RankedBarChart"
import type { WidgetSettings } from "@/lib/dashboardTypes"
import { useScopedInventory } from "@/lib/fleet"
import { WidgetError } from "@/components/dashboard/WidgetChrome"
import { WidgetViewAllLink } from "@/components/dashboard/WidgetViewAllLink"

export function TopConsumersWidget({ settings }: { settings: WidgetSettings }) {
  const metric = settings.metric === "mem" ? "mem" : "cpu"
  const { resources, isError } = useScopedInventory(settings)
  if (isError) return <WidgetError />

  const running = resources
    .filter((r) => (r.type === "qemu" || r.type === "lxc") && r.status === "running")
    .map((g) => ({
      name: g.name ?? `#${g.vmid}`,
      value: metric === "cpu" ? (g.cpu ?? 0) * 100 : g.maxmem ? ((g.mem ?? 0) / g.maxmem) * 100 : 0,
    }))
    .map((g) => ({ ...g, value: Math.min(100, g.value) }))
    .sort((a, b) => b.value - a.value)
  const guests = running.slice(0, 8)

  if (guests.length === 0) {
    return <p className="text-sm text-[var(--text-muted)]">No running guests.</p>
  }

  return (
    <div className="flex h-full flex-col">
      <RankedBarChart
        data={guests}
        domain={[0, 100]}
        nameWidth={100}
        rowHeight={28}
        colorFor={(g) => (g.value >= 90 ? "var(--status-error)" : g.value >= 75 ? "var(--status-warn)" : undefined)}
        labelFormatter={(v) => `${v.toFixed(0)}%`}
        tooltipLabel={metric === "cpu" ? "CPU" : "Memory"}
      />
      <WidgetViewAllLink to="/inventory" shown={guests.length} total={running.length} />
    </div>
  )
}
