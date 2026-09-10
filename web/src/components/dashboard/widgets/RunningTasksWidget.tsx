import { Badge } from "@/components/ui/badge"
import type { WidgetSettings } from "@/lib/dashboardTypes"
import { scopedConnection } from "@/lib/fleet"
import { useClusterTasks } from "@/lib/useClusterTasks"
import { WidgetError } from "@/components/dashboard/WidgetChrome"

function statusVariant(status: string): "ok" | "warn" | "error" | "default" {
  if (status === "OK") return "ok"
  if (status === "running" || !status) return "warn"
  return "error"
}

// A failed task's "status" from PVE isn't a short word like "OK" — it's the
// raw failure text (e.g. "command '/usr/bin/termproxy ...' failed: exit code
// 1"), which blew up the badge to the row's full width instead of reading as
// a compact status chip. Collapse it to "Failed" and keep the full text as a
// hover title.
function statusLabel(status: string): string {
  if (status === "OK" || status === "running" || !status) return status || "running"
  return "Failed"
}

export function RunningTasksWidget({ settings }: { settings: WidgetSettings }) {
  const limit = Number(settings.limit) || 8
  const { tasks, isError } = useClusterTasks(scopedConnection(settings))
  const rows = tasks.slice(0, limit)

  if (isError) return <WidgetError />

  if (rows.length === 0) {
    return <p className="text-sm text-[var(--text-muted)]">No recent tasks.</p>
  }

  return (
    <div className="space-y-1.5">
      {rows.map((t) => (
        <div key={t.upid} className="flex items-center gap-2 text-sm">
          <span className="w-24 shrink-0 truncate text-xs text-[var(--text-muted)]">{t.connName}</span>
          <span className="min-w-0 flex-1 truncate font-mono text-xs">{t.type}</span>
          <Badge variant={statusVariant(t.status)} className="max-w-24 shrink-0 truncate" title={t.status || undefined}>
            {statusLabel(t.status)}
          </Badge>
        </div>
      ))}
    </div>
  )
}
