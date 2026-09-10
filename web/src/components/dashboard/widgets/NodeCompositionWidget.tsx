import { StackedBarChart } from "@/components/charts/StackedBarChart"
import type { WidgetSettings } from "@/lib/dashboardTypes"
import { useScopedInventory } from "@/lib/fleet"
import { WidgetError } from "@/components/dashboard/WidgetChrome"
import { WidgetViewAllLink } from "@/components/dashboard/WidgetViewAllLink"

// Composition across categories: what each node's guest fleet is made of
// (VMs vs containers), stacked so the host mix is visible per node.
export function NodeCompositionWidget({ settings }: { settings: WidgetSettings }) {
  const sortBy = settings.sort === "total" ? "total" : "qemu"
  const { resources, isError } = useScopedInventory(settings)
  if (isError) return <WidgetError />

  const byNode = new Map<string, { name: string; qemu: number; lxc: number; total: number }>()
  for (const r of resources) {
    if (r.type !== "qemu" && r.type !== "lxc") continue
    const key = r.node
    const entry = byNode.get(key) ?? { name: key, qemu: 0, lxc: 0, total: 0 }
    if (r.type === "qemu") entry.qemu += 1
    else entry.lxc += 1
    entry.total += 1
    byNode.set(key, entry)
  }
  const allNodes = Array.from(byNode.values()).sort((a, b) => (sortBy === "total" ? b.total - a.total : b.qemu - a.qemu))
  const rows = allNodes.slice(0, 10)

  if (rows.length === 0) {
    return <p className="text-sm text-[var(--text-muted)]">No guests reporting yet.</p>
  }

  return (
    <div className="flex h-full flex-col justify-center">
      <StackedBarChart
        data={rows}
        series={[
          { key: "qemu", label: "VMs", color: "var(--chart-1)" },
          { key: "lxc", label: "Containers", color: "var(--chart-2)" },
        ]}
        height={Math.max(110, rows.length * 26)}
        showLegend
      />
      <WidgetViewAllLink to="/inventory" shown={rows.length} total={allNodes.length} />
    </div>
  )
}
