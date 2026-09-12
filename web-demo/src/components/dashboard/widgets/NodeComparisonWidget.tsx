import { RankedBarChart } from "@/components/charts/RankedBarChart"
import type { WidgetSettings } from "@/lib/dashboardTypes"
import { useScopedInventory } from "@/lib/fleet"
import { WidgetError } from "@/components/dashboard/WidgetChrome"
import { WidgetViewAllLink } from "@/components/dashboard/WidgetViewAllLink"

const metricLabel: Record<string, string> = { cpu: "CPU", mem: "Memory", disk: "Disk" }

export function NodeComparisonWidget({ settings }: { settings: WidgetSettings }) {
  const metric = settings.metric ?? "cpu"
  const { resources, isError } = useScopedInventory(settings)
  if (isError) return <WidgetError />

  const allNodes = resources
    .filter((r) => r.type === "node")
    .map((n) => {
      let pct = 0
      if (metric === "cpu") pct = (n.cpu ?? 0) * 100
      else if (metric === "mem") pct = n.maxmem ? ((n.mem ?? 0) / n.maxmem) * 100 : 0
      else pct = n.maxdisk ? ((n.disk ?? 0) / n.maxdisk) * 100 : 0
      return { name: n.node ?? "?", value: Math.min(100, pct) }
    })
    .sort((a, b) => b.value - a.value)
  const nodes = allNodes.slice(0, 10)

  if (nodes.length === 0) {
    return <p className="text-sm text-[var(--text-muted)]">No nodes reporting yet.</p>
  }

  return (
    <div className="flex h-full flex-col">
      <RankedBarChart
        data={nodes}
        domain={[0, 100]}
        colorFor={(n) => (n.value >= 90 ? "var(--status-error)" : n.value >= 75 ? "var(--status-warn)" : "var(--chart-3)")}
        labelFormatter={(v) => `${v.toFixed(0)}%`}
        tooltipLabel={metricLabel[metric]}
      />
      <WidgetViewAllLink to="/inventory" shown={nodes.length} total={allNodes.length} />
    </div>
  )
}
