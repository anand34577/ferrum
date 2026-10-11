import { Bar, BarChart, Cell, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts"
import type { WidgetSettings } from "@/lib/dashboardTypes"
import { useScopedInventory } from "@/lib/fleet"
import { chartTooltip } from "@/components/charts/tooltipTheme"
import { chartBarRadius } from "@/lib/chartRadius"
import { useTheme } from "@/lib/theme"
import { WidgetError } from "@/components/dashboard/WidgetChrome"
import { WidgetViewAllLink } from "@/components/dashboard/WidgetViewAllLink"

const PALETTE = ["var(--chart-1)", "var(--chart-2)", "var(--chart-3)", "var(--chart-4)", "var(--chart-5)"]

/** Guest count per resource pool across every connection — the grouping
 * multi-server operators use to carve a fleet up by tenant, team, or
 * environment instead of by which physical host a guest happens to sit on.
 * Purely derived from inventory already fetched by every other widget. */
export function PoolUsageWidget({ settings }: { settings: WidgetSettings }) {
  const { resources, isError } = useScopedInventory(settings)
  const { look } = useTheme()
  const r = chartBarRadius(look)
  if (isError) return <WidgetError />

  const guests = resources.filter((res) => res.type === "qemu" || res.type === "lxc")
  const counts = new Map<string, number>()
  let unassigned = 0
  for (const g of guests) {
    if (!g.pool) {
      unassigned += 1
      continue
    }
    counts.set(g.pool, (counts.get(g.pool) ?? 0) + 1)
  }

  if (guests.length === 0) {
    return <p className="text-sm text-[var(--text-muted)]">No guests found.</p>
  }
  if (counts.size === 0) {
    return <p className="text-sm text-[var(--text-muted)]">No guests are assigned to a pool yet.</p>
  }

  const allPools = Array.from(counts.entries())
    .sort(([, a], [, b]) => b - a)
    .map(([name, count]) => ({ name, count }))
  const rows = allPools.slice(0, 8)

  return (
    <div className="flex h-full flex-col justify-center">
      <ResponsiveContainer width="100%" height={Math.max(110, rows.length * 26)}>
        <BarChart data={rows} layout="vertical" margin={{ top: 0, right: 20, left: 0, bottom: 0 }} barCategoryGap="25%">
          <XAxis type="number" hide allowDecimals={false} />
          <YAxis type="category" dataKey="name" width={90} tick={{ fontSize: 11, fill: "var(--text-muted)" }} axisLine={false} tickLine={false} />
          <Tooltip cursor={false} formatter={(value) => [`${value} guest${Number(value) === 1 ? "" : "s"}`, ""]} {...chartTooltip} />
          <Bar dataKey="count" radius={[0, r, r, 0]} barSize={16} isAnimationActive={false} activeBar={{ stroke: "var(--text-faint)", strokeWidth: 1 }}>
            {rows.map((row, i) => (
              <Cell key={row.name} fill={PALETTE[i % PALETTE.length]} />
            ))}
          </Bar>
        </BarChart>
      </ResponsiveContainer>
      <p className="mt-1 text-center text-xs text-[var(--text-muted)]">
        {counts.size} pool{counts.size === 1 ? "" : "s"} across {guests.length} guests{unassigned > 0 ? ` · ${unassigned} unassigned` : ""}
      </p>
      <WidgetViewAllLink to="/inventory" shown={rows.length} total={allPools.length} />
    </div>
  )
}
