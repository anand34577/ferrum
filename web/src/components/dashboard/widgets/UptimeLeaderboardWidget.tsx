import { RankedBarChart } from "@/components/charts/RankedBarChart"
import type { WidgetSettings } from "@/lib/dashboardTypes"
import { useScopedInventory } from "@/lib/fleet"
import { formatUptime } from "@/lib/utils"
import { WidgetError } from "@/components/dashboard/WidgetChrome"
import { WidgetViewAllLink } from "@/components/dashboard/WidgetViewAllLink"

export function UptimeLeaderboardWidget({ settings }: { settings: WidgetSettings }) {
  const scope = settings.scope ?? "guest"
  const { resources, isError } = useScopedInventory(settings)
  if (isError) return <WidgetError />

  const wantType = scope === "node" ? (t: string) => t === "node" : (t: string) => t === "qemu" || t === "lxc"
  const eligible = resources
    .filter((r) => wantType(r.type) && r.status === (scope === "node" ? "online" : "running") && (r.uptime ?? 0) > 0)
    .sort((a, b) => (b.uptime ?? 0) - (a.uptime ?? 0))
  const rows = eligible.slice(0, 8).map((r) => ({ name: r.name ?? r.node ?? "?", value: r.uptime ?? 0 }))

  if (rows.length === 0) {
    return <p className="text-sm text-[var(--text-muted)]">No {scope === "node" ? "online nodes" : "running guests"} yet.</p>
  }

  return (
    <div className="flex h-full flex-col">
      <RankedBarChart
        data={rows}
        colorFor={() => "var(--chart-6)"}
        labelFormatter={formatUptime}
        tooltipLabel="Uptime"
      />
      <WidgetViewAllLink to="/inventory" shown={rows.length} total={eligible.length} />
    </div>
  )
}
