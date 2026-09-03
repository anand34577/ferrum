import { Bar, BarChart, Rectangle, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts"
import type { BarShapeProps } from "recharts"
import type { WidgetSettings } from "@/lib/dashboardTypes"
import { scopedConnection } from "@/lib/fleet"
import { useClusterTasks } from "@/lib/useClusterTasks"
import { chartTooltip } from "@/components/charts/tooltipTheme"

export function BackupActivityWidget({ settings }: { settings: WidgetSettings }) {
  const { tasks } = useClusterTasks(scopedConnection(settings))
  const backups = tasks.filter((t) => t.type === "vzdump")

  if (backups.length === 0) {
    return <p className="text-sm text-[var(--text-muted)]">No recent backup activity.</p>
  }

  const byConn = new Map<string, { name: string; ok: number; failed: number }>()
  for (const t of backups) {
    const entry = byConn.get(t.connName) ?? { name: t.connName, ok: 0, failed: 0 }
    if (t.status === "OK") entry.ok += 1
    else if (t.endtime) entry.failed += 1 // finished but not "OK" = failed; still-running tasks count as neither
    byConn.set(t.connName, entry)
  }
  const rows = Array.from(byConn.values())

  return (
    <div className="flex h-full flex-col gap-2">
      <ResponsiveContainer width="100%" height={Math.max(100, rows.length * 30)}>
        <BarChart data={rows} layout="vertical" margin={{ top: 0, right: 8, left: 0, bottom: 0 }} barCategoryGap={10}>
          <XAxis type="number" hide />
          <YAxis type="category" dataKey="name" width={80} tick={{ fontSize: 11, fill: "var(--text-muted)" }} axisLine={false} tickLine={false} />
          <Tooltip cursor={false} {...chartTooltip} />
          {/* Rounded caps follow the stack silhouette per row — a connection
              with only successes (or only failures) is a full pill instead of
              ending square on the side with no neighbor. activeBar repeats
              the radius logic (it replaces `shape` for the hovered row) and
              adds the 1px hover outline. */}
          <Bar
            dataKey="ok"
            stackId="a"
            fill="var(--status-ok)"
            name="Succeeded"
            barSize={14}
            shape={(p: BarShapeProps) =>
              p.width && p.height ? (
                <Rectangle
                  x={p.x}
                  y={p.y}
                  width={p.width}
                  height={p.height}
                  fill="var(--status-ok)"
                  radius={p.payload?.failed ? [4, 0, 0, 4] : [4, 4, 4, 4]}
                />
              ) : null
            }
            activeBar={(p: BarShapeProps) =>
              p.width && p.height ? (
                <Rectangle
                  x={p.x}
                  y={p.y}
                  width={p.width}
                  height={p.height}
                  fill="var(--status-ok)"
                  radius={p.payload?.failed ? [4, 0, 0, 4] : [4, 4, 4, 4]}
                  stroke="var(--text-faint)"
                  strokeWidth={1}
                />
              ) : null
            }
          />
          <Bar
            dataKey="failed"
            stackId="a"
            fill="var(--status-error)"
            name="Failed"
            barSize={14}
            shape={(p: BarShapeProps) =>
              p.width && p.height ? (
                <Rectangle
                  x={p.x}
                  y={p.y}
                  width={p.width}
                  height={p.height}
                  fill="var(--status-error)"
                  radius={p.payload?.ok ? [0, 4, 4, 0] : [4, 4, 4, 4]}
                />
              ) : null
            }
            activeBar={(p: BarShapeProps) =>
              p.width && p.height ? (
                <Rectangle
                  x={p.x}
                  y={p.y}
                  width={p.width}
                  height={p.height}
                  fill="var(--status-error)"
                  radius={p.payload?.ok ? [0, 4, 4, 0] : [4, 4, 4, 4]}
                  stroke="var(--text-faint)"
                  strokeWidth={1}
                />
              ) : null
            }
          />
        </BarChart>
      </ResponsiveContainer>
      <p className="text-center text-xs text-[var(--text-muted)]">
        {backups.filter((t) => t.status === "OK").length} succeeded · {backups.filter((t) => t.endtime && t.status !== "OK").length} failed (recent tasks)
      </p>
    </div>
  )
}
