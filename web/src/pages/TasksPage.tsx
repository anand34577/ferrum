import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import type { ColumnDef } from "@tanstack/react-table"
import { CheckCircle2, CircleSlash, Loader2, ScrollText, Square, Terminal, XCircle } from "lucide-react"
import { useEffect, useMemo, useState } from "react"
import { Link, useSearchParams } from "react-router-dom"
import { toast } from "sonner"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { DataTable } from "@/components/ui/data-table"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { ErrorState } from "@/components/ui/error-state"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { MultiSelect } from "@/components/ui/multi-select"
import { PageHeader } from "@/components/ui/page-header"
import { Hint } from "@/components/ui/tooltip"
import { api, ApiError } from "@/lib/api"
import { useClusterTasks, type FleetTask } from "@/lib/useClusterTasks"
import { cn, guestUrl } from "@/lib/utils"

function duration(task: FleetTask): string {
  const end = task.endtime || Math.floor(Date.now() / 1000)
  const secs = Math.max(0, end - task.starttime)
  if (secs < 60) return `${secs}s`
  if (secs < 3600) return `${Math.floor(secs / 60)}m ${secs % 60}s`
  return `${Math.floor(secs / 3600)}h ${Math.floor((secs % 3600) / 60)}m`
}

/** Human task label: PVE task types like "vzdump" / "qmigrate" / "aptupdate". */
const TASK_LABELS: Record<string, string> = {
  vzdump: "Backup",
  qmigrate: "VM migration",
  vxmove: "CT migration",
  qmstart: "VM start",
  qmstop: "VM stop",
  qmshutdown: "VM shutdown",
  vzsuspend: "CT suspend",
  vzstart: "CT start",
  aptupdate: "Package update",
  srvstart: "Service start",
  srvstop: "Service stop",
  cephcreate: "Ceph create",
  cephdestroy: "Ceph destroy",
}

function taskLabel(t: FleetTask): string {
  const base = TASK_LABELS[t.type] ?? t.type
  return t.id ? `${base} · ${t.id}` : base
}

/** Task types whose `id` field is a guest VMID — those rows deep-link to the
 * guest (Inventory with the detail dialog opened). */
const GUEST_TASK_TYPES = new Set(["qmstart", "qmstop", "qmshutdown", "qmigrate", "vzsuspend", "vzstart", "vxmove"])

function guestVmid(t: FleetTask): number | null {
  if (!GUEST_TASK_TYPES.has(t.type)) return null
  const n = Number(t.id)
  return Number.isFinite(n) && n > 0 ? n : null
}

export function TasksPage() {
  const { tasks, isLoading, isError, refetch } = useClusterTasks()
  const queryClient = useQueryClient()
  const confirm = useConfirm()
  const [logTask, setLogTask] = useState<FleetTask | null>(null)
  // Filters live in the URL (same convention as Inventory) so a filtered
  // task view survives navigation and can be pasted to a teammate during an
  // incident — they used to evaporate on every route change.
  const [searchParams, setSearchParams] = useSearchParams()
  // Empty array = "all" (the MultiSelect convention).
  const [statusFilter, setStatusFilter] = useState<string[]>(() => searchParams.get("status")?.split(",").filter(Boolean) ?? [])
  const [connFilter, setConnFilter] = useState<string[]>(() => searchParams.get("conn")?.split(",").filter(Boolean) ?? [])
  const [nodeFilter, setNodeFilter] = useState<string[]>(() => searchParams.get("node")?.split(",").filter(Boolean) ?? [])

  useEffect(() => {
    const next = new URLSearchParams(searchParams)
    if (connFilter.length) next.set("conn", connFilter.join(","))
    else next.delete("conn")
    if (nodeFilter.length) next.set("node", nodeFilter.join(","))
    else next.delete("node")
    if (statusFilter.length) next.set("status", statusFilter.join(","))
    else next.delete("status")
    setSearchParams(next, { replace: true })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connFilter, nodeFilter, statusFilter])

  const logQuery = useQuery({
    queryKey: ["task-log", logTask?.connId, logTask?.node, logTask?.upid],
    queryFn: () => api.get<string[]>(`/connections/${logTask!.connId}/nodes/${logTask!.node}/tasks/${encodeURIComponent(logTask!.upid)}/log`),
    enabled: !!logTask,
    // Follow the log while the task is still running.
    refetchInterval: logTask && !logTask.endtime ? 3_000 : false,
  })

  const cancelMutation = useMutation({
    mutationFn: (task: FleetTask) => api.delete(`/connections/${task.connId}/nodes/${task.node}/tasks/${encodeURIComponent(task.upid)}`),
    onSuccess: () => {
      toast.success("Task cancelled")
      refetch()
      queryClient.invalidateQueries({ queryKey: ["inventory"] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to cancel task — it may have already finished"),
  })

  async function cancelTask(task: FleetTask) {
    const ok = await confirm({
      title: `Cancel ${task.type} on ${task.node}?`,
      description: "Proxmox is asked to stop the task. Depending on the task type it may take a moment to actually terminate.",
      confirmLabel: "Cancel task",
    })
    if (ok) cancelMutation.mutate(task)
  }

  const connections = useMemo(() => Array.from(new Set(tasks.map((t) => t.connName))).sort(), [tasks])
  const nodes = useMemo(
    () =>
      Array.from(new Set(tasks.filter((t) => connFilter.length === 0 || connFilter.includes(t.connName)).map((t) => t.node))).sort(),
    [tasks, connFilter],
  )

  const isFailed = (t: FleetTask) => !!t.endtime && t.status !== "OK"
  const counts = useMemo(
    () => ({
      running: tasks.filter((t) => !t.endtime).length,
      ok: tasks.filter((t) => t.endtime && t.status === "OK").length,
      failed: tasks.filter(isFailed).length,
    }),
    [tasks],
  )

  const filtered = useMemo(() => {
    let rows = tasks
    if (connFilter.length > 0) rows = rows.filter((t) => connFilter.includes(t.connName))
    if (nodeFilter.length > 0) rows = rows.filter((t) => nodeFilter.includes(t.node))
    if (statusFilter.length > 0)
      rows = rows.filter((t) =>
        statusFilter.some((s) => (s === "running" ? !t.endtime : s === "ok" ? !!t.endtime && t.status === "OK" : isFailed(t))),
      )
    return rows
  }, [tasks, connFilter, nodeFilter, statusFilter])

  const columns = useMemo<ColumnDef<FleetTask>[]>(
    () => [
      {
        accessorKey: "starttime",
        header: "When",
        cell: (c) => (
          <div className="min-w-0 whitespace-nowrap">
            <p className="text-xs font-medium tabular">{new Date(c.getValue<number>() * 1000).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" })}</p>
            <p className="text-[10px] text-[var(--text-muted)] tabular">{new Date(c.getValue<number>() * 1000).toLocaleDateString([], { month: "short", day: "numeric" })}</p>
          </div>
        ),
      },
      { accessorKey: "connName", header: "Cluster", cell: (c) => <span className="block max-w-36 truncate text-xs font-medium" title={c.getValue<string>()}>{c.getValue<string>()}</span> },
      { accessorKey: "node", header: "Node", cell: (c) => <span className="block max-w-28 truncate font-mono text-xs" title={c.getValue<string>()}>{c.getValue<string>()}</span> },
      {
        accessorKey: "type",
        header: "Task",
        cell: (c) => {
          const vmid = guestVmid(c.row.original)
          const label = <span className="block max-w-40 truncate text-xs" title={c.getValue<string>()}>{taskLabel(c.row.original)}</span>
          // Guest-scoped tasks link straight to the guest — "which VM was
          // this backup/migration for" shouldn't require a manual search.
          return vmid !== null ? (
            <Link to={guestUrl(c.row.original.connId, vmid)} className="underline-offset-2 hover:underline">
              {label}
            </Link>
          ) : (
            label
          )
        },
      },
      { accessorKey: "user", header: "User", meta: { hideBelowMd: true }, cell: (c) => <span className="block max-w-32 truncate font-mono text-xs text-[var(--text-muted)]" title={c.getValue<string>()}>{c.getValue<string>()}</span> },
      {
        id: "duration",
        header: "Duration",
        meta: { hideBelowMd: true },
        cell: (c) => <span className="whitespace-nowrap font-mono text-xs text-[var(--text-muted)] tabular">{duration(c.row.original)}</span>,
      },
      {
        accessorKey: "status",
        header: "Status",
        cell: (c) => {
          const raw = c.getValue<string>() || ""
          const failed = isFailed(c.row.original)
          return (
            // Cap the cell: PVE failure statuses are full error sentences
            // (e.g. a whole termproxy command) — truncate here, full text
            // lives in the log dialog.
            <div className="min-w-0 max-w-64">
              <Badge variant={failed ? "error" : raw === "OK" ? "ok" : "warn"}>{failed ? "failed" : raw || "running"}</Badge>
              {failed && (
                <p className="mt-0.5 truncate font-mono text-[11px] text-[var(--text-muted)]" title={raw}>
                  {raw}
                </p>
              )}
            </div>
          )
        },
      },
      {
        id: "actions",
        header: "",
        size: 72,
        cell: (c) => (
          <div className="flex items-center justify-end gap-1">
            <Hint label="View log">
              <Button size="icon" variant="ghost" aria-label="View log" onClick={() => setLogTask(c.row.original)}>
                <ScrollText className="h-3.5 w-3.5" />
              </Button>
            </Hint>
            {!c.row.original.endtime && (
              <Hint label="Cancel task">
                <Button
                  size="icon"
                  variant="ghost-danger"
                  aria-label="Cancel task"
                  disabled={cancelMutation.isPending}
                  onClick={() => cancelTask(c.row.original)}
                >
                  <Square className="h-3.5 w-3.5" />
                </Button>
              </Hint>
            )}
          </div>
        ),
      },
    ],
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [cancelMutation],
  )

  return (
    <div className="space-y-4">
      <PageHeader
        title="Task Center"
        description="Recent and running Proxmox tasks across every connected cluster and node."
        icon={Terminal}
        onRefresh={() => void refetch()}
        refreshing={isLoading}
      />

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <SummaryChip label="Running" value={counts.running} tone="warn" icon={<CircleSlash className="h-3.5 w-3.5" />} />
        <SummaryChip label="Finished OK" value={counts.ok} tone="ok" icon={<CheckCircle2 className="h-3.5 w-3.5" />} />
        <SummaryChip label="Failed" value={counts.failed} tone="error" icon={<XCircle className="h-3.5 w-3.5" />} />
      </div>

      {isError && (
        <ErrorState title="Couldn't load tasks" message="The task list could not be fetched from your connections." onRetry={refetch} />
      )}

      {!isError && (
      <Card>
        <CardContent className="pt-4">
          <DataTable
            columns={columns}
            data={filtered}
            loading={isLoading}
            searchPlaceholder="Search tasks by type, node, user…"
            emptyMessage={tasks.length === 0 ? "No tasks yet — actions you take on the fleet will show up here." : "No tasks match the current filters."}
            toolbar={
              <div className="flex flex-wrap items-center gap-2">
                <MultiSelect
                  options={connections.map((c) => ({ value: c, label: c }))}
                  selected={connFilter}
                  onChange={(v) => setConnFilter(v)}
                  allLabel="All clusters"
                  label="Filter by cluster"
                  className="w-44"
                />
                <MultiSelect
                  options={nodes.map((n) => ({ value: n, label: n }))}
                  selected={nodeFilter}
                  onChange={setNodeFilter}
                  allLabel="All nodes"
                  label="Filter by node"
                  className="w-40"
                />
                <MultiSelect
                  options={[
                    { value: "running", label: "Running" },
                    { value: "ok", label: "Finished OK" },
                    { value: "failed", label: "Failed" },
                  ]}
                  selected={statusFilter}
                  onChange={setStatusFilter}
                  allLabel="All statuses"
                  label="Filter by status"
                  className="w-40"
                />
                {(connFilter.length > 0 || nodeFilter.length > 0 || statusFilter.length > 0) && (
                  <Button size="sm" variant="ghost" onClick={() => (setConnFilter([]), setNodeFilter([]), setStatusFilter([]))}>
                    Reset filters
                  </Button>
                )}
              </div>
            }
          />
        </CardContent>
      </Card>
      )}

      <Dialog open={!!logTask} onOpenChange={(open) => !open && setLogTask(null)}>
        <DialogContent className="max-w-3xl">
          <DialogHeader>
            <DialogTitle>{logTask && taskLabel(logTask)}</DialogTitle>
            <DialogDescription>
              {logTask?.node} · {logTask?.connName}
              {logTask && !logTask.endtime && <span className="ml-2 text-[var(--status-warn)]">· running, log follows live</span>}
              {logTask && isFailed(logTask) && (
                <p className="mt-1 break-all font-mono text-[11px] text-[var(--status-error)]">{logTask.status}</p>
              )}
            </DialogDescription>
          </DialogHeader>
          {logQuery.isLoading ? (
            <div className="flex justify-center py-8" aria-busy>
              <Loader2 className="h-6 w-6 animate-spin text-brand-500" />
            </div>
          ) : (
            <pre className="max-h-96 overflow-y-auto rounded-md bg-[var(--bg-muted)] p-3 font-mono text-xs whitespace-pre-wrap">
              {(logQuery.data ?? []).join("\n") || (logTask && isFailed(logTask) ? logTask.status : "No log output.")}
            </pre>
          )}
        </DialogContent>
      </Dialog>
    </div>
  )
}

function SummaryChip({ label, value, tone, icon }: { label: string; value: number; tone: "ok" | "warn" | "error"; icon?: React.ReactNode }) {
  return (
    <div className={cn("flex items-center gap-2.5 rounded-lg border border-[var(--border)] bg-[var(--bg-surface)] px-3.5 py-2.5")}>
      <span
        className={cn(
          "flex h-7 w-7 items-center justify-center rounded-sm",
          tone === "ok" && "bg-[color-mix(in_oklab,var(--status-ok)_14%,transparent)] text-[var(--status-ok)]",
          tone === "warn" && "bg-[color-mix(in_oklab,var(--status-warn)_14%,transparent)] text-[var(--status-warn)]",
          tone === "error" && "bg-[color-mix(in_oklab,var(--status-error)_14%,transparent)] text-[var(--status-error)]",
        )}
      >
        {icon ?? <span className="text-xs font-semibold">{value > 99 ? "99+" : value}</span>}
      </span>
      <div className="min-w-0">
        <p className="font-display text-lg font-semibold leading-none tabular">{value}</p>
        <p className="mt-0.5 text-[11px] text-[var(--text-muted)]">{label}</p>
      </div>
    </div>
  )
}
