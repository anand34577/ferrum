import { useQuery } from "@tanstack/react-query"
import { Bar, BarChart, Cell, LabelList, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts"
import type { WidgetSettings } from "@/lib/dashboardTypes"
import { api, type ConnectionInventory } from "@/lib/api"
import { cn } from "@/lib/utils"
import { chartTooltip } from "@/components/charts/tooltipTheme"

/** CPU utilization per node — the classic "which host is hot" bar chart,
 * colored by severity so overloaded nodes jump out. */
export function CpuByNodeWidget({ settings }: { settings: WidgetSettings }) {
  const connId = settings.connection ?? "all"
  const { data } = useQuery({
    queryKey: ["inventory"],
    queryFn: () => api.get<ConnectionInventory[]>("/inventory/"),
    refetchInterval: 15_000,
  })

  const nodes = (data ?? [])
    .filter((c) => connId === "all" || c.connectionId === connId)
    .flatMap((c) => c.resources ?? [])
    .filter((r) => r.type === "node")
    .map((n) => ({
      name: n.node,
      pct: Math.min(100, (n.cpu ?? 0) * 100),
      conn: (data ?? []).find((c) => c.resources?.some((r) => r.type === "node" && r.node === n.node))?.name ?? "",
    }))
    .sort((a, b) => b.pct - a.pct)

  if (nodes.length === 0) {
    return <p className="text-sm text-[var(--text-muted)]">No nodes reporting yet.</p>
  }

  return (
    <div className="flex h-full flex-col">
      <ResponsiveContainer width="100%" height="100%" minHeight={110}>
        <BarChart data={nodes} margin={{ top: 14, right: 4, left: -26, bottom: 0 }}>
          <XAxis
            dataKey="name"
            tick={{ fontSize: 10, fill: "var(--text-muted)" }}
            axisLine={{ stroke: "var(--border)" }}
            tickLine={false}
            interval={0}
            angle={nodes.length > 6 ? -30 : 0}
            textAnchor={nodes.length > 6 ? "end" : "middle"}
            height={nodes.length > 6 ? 44 : 22}
          />
          <YAxis domain={[0, 100]} tick={{ fontSize: 10, fill: "var(--text-muted)" }} axisLine={false} tickLine={false} tickFormatter={(v) => `${v}%`} />
          <Tooltip
            cursor={false}
            formatter={(value) => [`${Number(value).toFixed(1)}%`, "CPU"]}
            labelFormatter={(label, payload) => {
              const conn = (payload?.[0]?.payload as { conn?: string } | undefined)?.conn
              return conn ? `${label} — ${conn}` : String(label)
            }}
            {...chartTooltip}
          />
          {/* No background track: on a column chart recharts stretches it
              across the whole category band, which reads as giant gray
              blocks whenever a connection has few nodes. The Y axis already
              gives the scale. Hover feedback is a 1px outline on the
              hovered column — no band. */}
          <Bar
            dataKey="pct"
            radius={[4, 4, 0, 0]}
            isAnimationActive={false}
            activeBar={{ stroke: "var(--text-faint)", strokeWidth: 1 }}
          >
            {nodes.map((n) => (
              <Cell key={n.name} fill={n.pct >= 90 ? "var(--status-error)" : n.pct >= 75 ? "var(--status-warn)" : "var(--color-brand-500)"} />
            ))}
            <LabelList dataKey="pct" position="top" formatter={(v) => `${Number(v).toFixed(0)}`} style={{ fill: "var(--text-muted)", fontSize: 10 }} />
          </Bar>
        </BarChart>
      </ResponsiveContainer>
      <p className={cn("pt-1 text-center text-[10px] text-[var(--text-faint)]")}>
        {nodes.length} node{nodes.length === 1 ? "" : "s"} · worst first{connId === "all" ? " · all connections" : ""}
      </p>
    </div>
  )
}
