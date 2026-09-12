import { CartesianGrid, Cell, ResponsiveContainer, ReferenceLine, Scatter, ScatterChart, Tooltip, XAxis, YAxis, ZAxis } from "recharts"
import type { WidgetSettings } from "@/lib/dashboardTypes"
import { useScopedInventory } from "@/lib/fleet"
import { WidgetError } from "@/components/dashboard/WidgetChrome"

/** Node density bubble chart: X = CPU %, Y = memory %, bubble = guest count.
 * The top-right corner is where hosts are drowning — it exposes overloaded
 * and under-used nodes in one glance. */
export function NodeScatterWidget({ settings }: { settings: WidgetSettings }) {
  const { resources, connections, isError } = useScopedInventory(settings)
  if (isError) return <WidgetError />

  const connName = new Map(connections.map((c) => [c.connectionId, c.name]))
  const guestsByNode = new Map<string, number>()
  for (const g of resources) {
    if (g.type === "qemu" || g.type === "lxc") guestsByNode.set(g.node, (guestsByNode.get(g.node) ?? 0) + 1)
  }

  const nodes = resources
    .filter((r) => r.type === "node" && r.status === "online")
    .map((n) => {
      const owner = connections.find((c) => c.resources?.some((x) => x.type === "node" && x.node === n.node))
      return {
        name: n.node,
        conn: owner ? connName.get(owner.connectionId) ?? owner.name : "",
        cpu: Math.round(((n.cpu ?? 0) * 100) * 10) / 10,
        mem: Math.round((((n.mem ?? 0) / (n.maxmem || 1)) * 100) * 10) / 10,
        guests: guestsByNode.get(n.node) ?? 0,
      }
    })

  if (nodes.length === 0) {
    return <p className="text-sm text-[var(--text-muted)]">No online nodes yet.</p>
  }

  return (
    <div className="flex h-full flex-col">
      <ResponsiveContainer width="100%" height="100%" minHeight={140}>
        <ScatterChart margin={{ top: 8, right: 12, left: -18, bottom: 4 }}>
          <CartesianGrid strokeDasharray="3 3" stroke="var(--border)" />
          <XAxis
            type="number"
            dataKey="cpu"
            name="CPU"
            unit="%"
            domain={[0, 100]}
            tick={{ fontSize: 10, fill: "var(--text-muted)" }}
            axisLine={{ stroke: "var(--border)" }}
            tickLine={false}
          />
          <YAxis
            type="number"
            dataKey="mem"
            name="Memory"
            unit="%"
            domain={[0, 100]}
            tick={{ fontSize: 10, fill: "var(--text-muted)" }}
            axisLine={false}
            tickLine={false}
          />
          <ZAxis type="number" dataKey="guests" range={[40, 320]} name="Guests" />
          <ReferenceLine x={75} stroke="var(--status-warn)" strokeDasharray="4 4" ifOverflow="extendDomain" />
          <ReferenceLine y={75} stroke="var(--status-warn)" strokeDasharray="4 4" ifOverflow="extendDomain" />
          <Tooltip
            cursor={{ strokeDasharray: "3 3", stroke: "var(--border-strong)" }}
            content={({ payload }) => {
              const p = payload?.[0]?.payload as { name: string; conn: string; cpu: number; mem: number; guests: number } | undefined
              if (!p) return null
              return (
                <div className="rounded-md border border-[var(--border)] bg-[var(--bg-elevated)] px-2.5 py-1.5 text-xs shadow-md">
                  <p className="font-semibold">{p.name} <span className="font-normal text-[var(--text-muted)]">· {p.conn}</span></p>
                  <p className="text-[var(--text-muted)] tabular">CPU {p.cpu}% · Memory {p.mem}% · {p.guests} guests</p>
                </div>
              )
            }}
          />
          <Scatter data={nodes} isAnimationActive={false}>
            {nodes.map((n) => (
              <Cell
                key={n.name}
                fill={n.cpu >= 75 || n.mem >= 75 ? "var(--status-warn)" : "var(--chart-3)"}
                fillOpacity={n.cpu >= 90 || n.mem >= 90 ? 0.9 : 0.55}
                stroke="var(--chart-3)"
              />
            ))}
          </Scatter>
        </ScatterChart>
      </ResponsiveContainer>
      <p className="pt-1 text-center text-[10px] text-[var(--text-faint)]">Bubble size = guest count · dashed lines mark the 75% load line</p>
    </div>
  )
}
