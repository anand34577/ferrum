import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import type { ColumnDef } from "@tanstack/react-table"
import { DatabaseBackup, Play, ScrollText } from "lucide-react"
import { type RefObject, useEffect, useMemo, useRef, useState } from "react"
import { toast } from "sonner"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { DataTable } from "@/components/ui/data-table"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { EmptyState } from "@/components/ui/empty-state"
import { ErrorState } from "@/components/ui/error-state"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Meter } from "@/components/ui/meter"
import { PageHeader } from "@/components/ui/page-header"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { StatusDot } from "@/components/ui/status-dot"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Timestamp } from "@/components/ui/timestamp"
import { Hint } from "@/components/ui/tooltip"
import {
  api,
  ApiError,
  type Connection,
  type PBSBackupGroup,
  type PBSCounts,
  type PBSDatastore,
  type PBSGCStatus,
  type PBSPruneResult,
  type PBSSyncJob,
  type PBSTaskStatus,
  type PBSVerifyJob,
} from "@/lib/api"
import { useAuth } from "@/lib/auth"
import { formatBytes } from "@/lib/utils"

function countTotals(counts?: PBSCounts) {
  const parts = [counts?.ct, counts?.host, counts?.vm, counts?.other]
  return {
    groups: parts.reduce((n, p) => n + (p?.groups ?? 0), 0),
    snapshots: parts.reduce((n, p) => n + (p?.snapshots ?? 0), 0),
  }
}

export function PBSPage() {
  const { user } = useAuth()
  const isAdmin = !!user?.isAdmin
  const queryClient = useQueryClient()

  const connectionsQuery = useQuery({
    queryKey: ["connections"],
    queryFn: () => api.get<Connection[]>("/connections/"),
  })
  // PBS remotes are connections whose stored type is "pbs" — the server
  // rejects the /pbs/ tree for anything else, so PVE-only fleets land in
  // the dedicated empty state below instead of a wall of 502s.
  const pbsConnections = useMemo(() => (connectionsQuery.data ?? []).filter((c) => c.type === "pbs"), [connectionsQuery.data])
  const [connId, setConnId] = useState("")
  const activeConnId = connId || pbsConnections[0]?.id || ""
  const activeName = pbsConnections.find((c) => c.id === activeConnId)?.name ?? activeConnId

  const storesQuery = useQuery({
    queryKey: ["pbs-datastores", activeConnId],
    queryFn: () => api.get<PBSDatastore[]>(`/connections/${activeConnId}/pbs/datastores`),
    enabled: !!activeConnId,
    retry: false, // a PBS remote can be plain offline — surface that fast instead of backoff-retrying
    refetchInterval: 30_000,
  })

  // "pbs" prefix covers every page-level key (pbs-datastores, pbs-groups,
  // pbs-task, ...) — the header refresh re-fetches whatever the current
  // tabs have mounted without enumerating them here.
  function refresh() {
    void queryClient.invalidateQueries({ queryKey: ["connections"] })
    void queryClient.invalidateQueries({ queryKey: ["pbs"] })
  }

  return (
    <div className="space-y-4">
      <PageHeader
        title="PBS Backups"
        description="Datastore browsing and maintenance across Proxmox Backup Server remotes."
        icon={DatabaseBackup}
        onRefresh={refresh}
        refreshing={connectionsQuery.isRefetching || storesQuery.isRefetching}
      />

      {connectionsQuery.isError ? (
        <ErrorState title="Couldn't load connections" onRetry={() => void connectionsQuery.refetch()} />
      ) : connectionsQuery.isLoading ? (
        <Skeleton className="h-72" aria-busy />
      ) : pbsConnections.length === 0 ? (
        <EmptyState
          icon={DatabaseBackup}
          title="No PBS connections configured yet"
          description="Add a Proxmox Backup Server connection first — datastores are browsed per remote."
        />
      ) : (
        <>
          <div className="flex flex-wrap items-center gap-2">
            <Label className="text-xs text-[var(--text-muted)]">PBS remote</Label>
            <Select value={activeConnId} onValueChange={setConnId}>
              <SelectTrigger className="w-full sm:w-64">
                <SelectValue placeholder="Select a remote" />
              </SelectTrigger>
              <SelectContent>
                {pbsConnections.map((c) => (
                  <SelectItem key={c.id} value={c.id}>{c.name}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {storesQuery.isError ? (
            <ErrorState
              title={`Couldn't load datastores from ${activeName}`}
              message="The PBS remote may be offline or unreachable — check the connection and try again."
              onRetry={() => void storesQuery.refetch()}
            />
          ) : storesQuery.isLoading ? (
            <div className="space-y-3" aria-busy>
              <Skeleton className="h-56" />
              <Skeleton className="h-56" />
            </div>
          ) : (storesQuery.data ?? []).length === 0 ? (
            <EmptyState
              icon={DatabaseBackup}
              title="No datastores on this remote"
              description="Create a datastore on the PBS host and it will appear here."
            />
          ) : (
            <div className="space-y-3">
              {(storesQuery.data ?? []).map((ds) => (
                <DatastoreCard key={`${activeConnId}/${ds.store}`} connId={activeConnId} ds={ds} isAdmin={isAdmin} />
              ))}
            </div>
          )}
        </>
      )}
    </div>
  )
}

function DatastoreCard({ connId, ds, isAdmin }: { connId: string; ds: PBSDatastore; isAdmin: boolean }) {
  const store = ds.store
  const total = ds.total ?? 0
  const used = ds.used ?? 0
  const totals = countTotals(ds.counts)

  // Lifted out of MaintenanceTab: Radix TabsContent unmounts inactive panels
  // by default, so state local to that component would lose a running GC
  // job's UPID whenever the user switched away from Maintenance and back.
  // DatastoreCard stays mounted for as long as the tabs it hosts do.
  const [gcUpid, setGcUpid] = useState<string | null>(null)
  const [logUpid, setLogUpid] = useState<string | null>(null)
  // Also lifted: the last UPID whose finish was announced, so remounting the
  // Maintenance tab doesn't re-toast (and re-invalidate) a finished GC.
  const gcSettledRef = useRef<string | null>(null)

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex flex-wrap items-center gap-2">
          {store}
          {ds.maintenance && <Badge variant="warn">Maintenance: {ds.maintenance}</Badge>}
          {ds.error && <Badge variant="error">Usage error</Badge>}
        </CardTitle>
        {ds.comment && <p className="mt-1 text-xs text-[var(--text-muted)]">{ds.comment}</p>}
      </CardHeader>
      <CardContent className="space-y-3">
        {/* Capacity lives on the per-store status endpoint upstream; when it
            failed for just this store the listing still arrives with the
            failure attached, so degrade this card instead of the page. */}
        {ds.error ? (
          <p className="text-xs text-[var(--status-error)]">Usage unavailable: {ds.error}</p>
        ) : total > 0 ? (
          <div className="flex flex-wrap items-center gap-3">
            <Meter value={total > 0 ? (used / total) * 100 : 0} className="min-w-24 max-w-md flex-1" showLabel label={`${store} usage`} />
            <span className="whitespace-nowrap text-xs text-[var(--text-muted)] tabular">
              {formatBytes(used)} of {formatBytes(total)}
            </span>
          </div>
        ) : (
          <p className="text-xs text-[var(--text-muted)]">No usage data reported.</p>
        )}
        {totals.groups + totals.snapshots > 0 && (
          <p className="text-xs text-[var(--text-muted)] tabular">
            {totals.groups} backup {totals.groups === 1 ? "group" : "groups"} · {totals.snapshots} snapshots
          </p>
        )}

        <Tabs defaultValue="content">
          <TabsList>
            <TabsTrigger value="content">Content</TabsTrigger>
            <TabsTrigger value="maintenance">Maintenance</TabsTrigger>
            <TabsTrigger value="jobs">Jobs</TabsTrigger>
          </TabsList>
          <TabsContent value="content">
            <ContentTab connId={connId} store={store} isAdmin={isAdmin} />
          </TabsContent>
          <TabsContent value="maintenance">
            <MaintenanceTab
              connId={connId}
              store={store}
              gcStatus={ds["gc-status"]}
              isAdmin={isAdmin}
              gcUpid={gcUpid}
              setGcUpid={setGcUpid}
              settledRef={gcSettledRef}
              logUpid={logUpid}
              setLogUpid={setLogUpid}
            />
          </TabsContent>
          <TabsContent value="jobs">
            <JobsTab connId={connId} store={store} isAdmin={isAdmin} />
          </TabsContent>
        </Tabs>
      </CardContent>
    </Card>
  )
}

// Backup groups in one datastore. Prune lives here rather than on the
// Maintenance tab because the API's prune endpoint requires a backupType +
// backupId — there is no whole-store prune to call.
function ContentTab({ connId, store, isAdmin }: { connId: string; store: string; isAdmin: boolean }) {
  const queryClient = useQueryClient()
  const confirm = useConfirm()

  const groupsQuery = useQuery({
    queryKey: ["pbs-groups", connId, store],
    queryFn: () => api.get<PBSBackupGroup[]>(`/connections/${connId}/pbs/datastores/${encodeURIComponent(store)}/groups`),
    retry: false,
  })

  // Retention windows are chosen at prune time — an absent keep-* option
  // means "unlimited" for that bucket upstream, so at least one must be set
  // or the run would keep everything.
  const [pruneTarget, setPruneTarget] = useState<PBSBackupGroup | null>(null)
  const [keep, setKeep] = useState({ last: "", daily: "", weekly: "" })
  const keepNums = { last: Number(keep.last) || 0, daily: Number(keep.daily) || 0, weekly: Number(keep.weekly) || 0 }
  const hasRetention = keepNums.last > 0 || keepNums.daily > 0 || keepNums.weekly > 0

  const prune = useMutation({
    mutationFn: (target: PBSBackupGroup) =>
      api.post<PBSPruneResult[]>(`/connections/${connId}/pbs/datastores/${encodeURIComponent(store)}/prune`, {
        backupType: target["backup-type"],
        backupId: target["backup-id"],
        keepLast: keepNums.last || undefined,
        keepDaily: keepNums.daily || undefined,
        keepWeekly: keepNums.weekly || undefined,
      }),
    onSuccess: (res, target) => {
      const removed = res.filter((r) => !r.keep).length
      toast.success(`Pruned ${target["backup-type"]}/${target["backup-id"]} — ${removed} removed, ${res.length - removed} kept`)
      queryClient.invalidateQueries({ queryKey: ["pbs-groups", connId, store] })
      queryClient.invalidateQueries({ queryKey: ["pbs-datastores", connId] })
      setPruneTarget(null)
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to prune group"),
  })

  async function runPrune(target: PBSBackupGroup) {
    if (!pruneTarget) return
    const windows = [
      keepNums.last > 0 && `keep last ${keepNums.last}`,
      keepNums.daily > 0 && `keep ${keepNums.daily} daily`,
      keepNums.weekly > 0 && `keep ${keepNums.weekly} weekly`,
    ]
      .filter(Boolean)
      .join(", ")
    const ok = await confirm({
      title: `Prune ${target["backup-type"]}/${target["backup-id"]} on ${store}?`,
      description: `Snapshots outside the chosen retention (${windows}) are deleted permanently from ${store}. This cannot be undone.`,
      confirmLabel: "Prune group",
    })
    if (ok) prune.mutate(target)
  }

  const columns = useMemo<ColumnDef<PBSBackupGroup>[]>(() => {
    const cols: ColumnDef<PBSBackupGroup>[] = [
      {
        id: "group",
        header: "Group",
        cell: (c) => (
          <span className="flex items-center gap-2 font-mono text-xs">
            <Badge variant="outline">{c.row.original["backup-type"]}</Badge>
            {c.row.original["backup-id"]}
          </span>
        ),
      },
      {
        accessorKey: "backup-count",
        header: "Snapshots",
        cell: (c) => <span className="tabular">{c.getValue<number>() ?? "—"}</span>,
      },
      {
        accessorKey: "last-backup",
        header: "Last backup",
        meta: { hideBelowMd: true },
        cell: (c) => {
          const t = c.getValue<number | undefined>()
          return t ? <Timestamp iso={new Date(t * 1000).toISOString()} /> : <span className="text-[var(--text-muted)]">—</span>
        },
      },
      {
        accessorKey: "comment",
        header: "Comment",
        meta: { hideBelowMd: true },
        cell: (c) => (
          <span className="block max-w-48 truncate text-[var(--text-muted)]" title={c.getValue<string>()}>
            {c.getValue<string>() || "—"}
          </span>
        ),
      },
    ]
    if (isAdmin)
      cols.push({
        id: "actions",
        header: "",
        size: 96,
        cell: (c) => (
          <Hint label="Prune this group's snapshots">
            <Button size="sm" variant="ghost" onClick={() => { setKeep({ last: "", daily: "", weekly: "" }); setPruneTarget(c.row.original) }}>
              Prune…
            </Button>
          </Hint>
        ),
      })
    return cols
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isAdmin])

  function closePrune(open: boolean) {
    if (!open) setPruneTarget(null)
  }

  return (
    <div className="space-y-3">
      {groupsQuery.isError ? (
        <ErrorState title={`Couldn't load groups for ${store}`} onRetry={() => void groupsQuery.refetch()} />
      ) : (
        <DataTable
          columns={columns}
          data={groupsQuery.data ?? []}
          loading={groupsQuery.isLoading}
          searchPlaceholder="Search groups by type, id, comment…"
          emptyMessage="No backup groups in this datastore yet."
        />
      )}

      <Dialog open={!!pruneTarget} onOpenChange={closePrune}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>Prune {pruneTarget ? `${pruneTarget["backup-type"]}/${pruneTarget["backup-id"]}` : ""}</DialogTitle>
            <DialogDescription>
              Choose the retention to apply on {store}. A window left blank keeps everything in that bucket.
            </DialogDescription>
          </DialogHeader>
          <div className="grid grid-cols-3 gap-3">
            <div className="space-y-1.5">
              <Label htmlFor="prune-keep-last">Keep last</Label>
              <Input id="prune-keep-last" type="number" min={0} value={keep.last} onChange={(e) => setKeep({ ...keep, last: e.target.value })} placeholder="e.g. 4" />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="prune-keep-daily">Keep daily</Label>
              <Input id="prune-keep-daily" type="number" min={0} value={keep.daily} onChange={(e) => setKeep({ ...keep, daily: e.target.value })} placeholder="e.g. 7" />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="prune-keep-weekly">Keep weekly</Label>
              <Input id="prune-keep-weekly" type="number" min={0} value={keep.weekly} onChange={(e) => setKeep({ ...keep, weekly: e.target.value })} placeholder="e.g. 4" />
            </div>
          </div>
          <DialogFooter className="mt-3">
            <Button variant="secondary" onClick={() => setPruneTarget(null)}>
              Cancel
            </Button>
            <Button variant="destructive" loading={prune.isPending} disabled={!hasRetention} onClick={() => pruneTarget && void runPrune(pruneTarget)}>
              Prune group
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

function GCStat({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border border-[var(--border)] bg-[var(--bg-muted)]/40 px-3 py-2">
      <p className="font-display text-sm font-semibold tabular">{value}</p>
      <p className="text-[10px] text-[var(--text-muted)]">{label}</p>
    </div>
  )
}

function MaintenanceTab({
  connId,
  store,
  gcStatus,
  isAdmin,
  gcUpid,
  setGcUpid,
  settledRef,
  logUpid,
  setLogUpid,
}: {
  connId: string
  store: string
  gcStatus?: PBSGCStatus
  isAdmin: boolean
  // The UPID returned by the GC start call — while set, its task status is
  // polled every 3s, but only for as long as the task reports "running".
  // Lifted into DatastoreCard so it survives this tab unmounting/remounting.
  gcUpid: string | null
  setGcUpid: (upid: string | null) => void
  settledRef: RefObject<string | null>
  logUpid: string | null
  setLogUpid: (upid: string | null) => void
}) {
  const queryClient = useQueryClient()

  const taskQuery = useQuery({
    queryKey: ["pbs-task", connId, gcUpid],
    queryFn: () => api.get<PBSTaskStatus>(`/connections/${connId}/pbs/tasks/${encodeURIComponent(gcUpid!)}/status`),
    enabled: !!gcUpid,
    retry: false,
    refetchInterval: (query) => (query.state.data?.status === "running" ? 3_000 : false),
  })

  // The task status carries no end-time, so "finished" is exactly the first
  // stopped poll — toast it once per UPID, then refresh the listing so the
  // new removed/pending figures actually show up.
  useEffect(() => {
    const t = taskQuery.data
    if (!t || t.status !== "stopped" || settledRef.current === t.upid) return
    settledRef.current = t.upid
    if (t.exitstatus && t.exitstatus !== "OK") toast.error(`Garbage collection on ${store} failed: ${t.exitstatus}`)
    else toast.success(`Garbage collection on ${store} finished`)
    queryClient.invalidateQueries({ queryKey: ["pbs-datastores", connId] })
  }, [taskQuery.data, connId, store, queryClient, settledRef])

  const startGc = useMutation({
    mutationFn: () => api.post<{ upid: string }>(`/connections/${connId}/pbs/datastores/${encodeURIComponent(store)}/gc`),
    onSuccess: (res) => {
      toast.success("Garbage collection started")
      setGcUpid(res.upid)
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to start garbage collection"),
  })

  const logQuery = useQuery({
    queryKey: ["pbs-task-log", connId, logUpid],
    queryFn: () => api.get<string[]>(`/connections/${connId}/pbs/tasks/${encodeURIComponent(logUpid!)}/log`),
    enabled: !!logUpid,
    // Follow the log while the GC task is still running.
    refetchInterval: !!logUpid && taskQuery.data?.status === "running" ? 3_000 : false,
  })

  const task = taskQuery.data
  const taskFailed = !!task && task.status === "stopped" && !!task.exitstatus && task.exitstatus !== "OK"

  return (
    <div className="space-y-4">
      {!isAdmin && <p className="text-sm text-[var(--text-muted)]">Ask an admin to run maintenance actions.</p>}

      <div className="flex flex-wrap items-center gap-3">
        {isAdmin && (
          <Button size="sm" loading={startGc.isPending} onClick={() => startGc.mutate()}>
            Start Garbage Collection
          </Button>
        )}
        {task && task.status === "running" && (
          <span className="flex items-center gap-2 text-sm text-[var(--text-muted)]">
            <StatusDot status="brand" pulse /> Garbage collection running…
          </span>
        )}
        {task && task.status === "stopped" && (
          <span className="flex items-center gap-2 text-sm">
            <Badge variant={taskFailed ? "error" : "ok"}>{taskFailed ? "Failed" : "Finished"}</Badge>
            {task.exitstatus && <span className="font-mono text-xs text-[var(--text-muted)]">{task.exitstatus}</span>}
          </span>
        )}
        {taskQuery.isError && (
          <span className="flex items-center gap-2 text-sm text-[var(--status-error)]">
            Couldn't poll the task status. <Button size="sm" variant="ghost" onClick={() => void taskQuery.refetch()}>Retry</Button>
          </span>
        )}
        {gcUpid && (
          <Hint label="Open the PBS task log">
            <Button size="icon-sm" variant="ghost" aria-label="View task log" onClick={() => setLogUpid(gcUpid)}>
              <ScrollText className="h-3.5 w-3.5" />
            </Button>
          </Hint>
        )}
      </div>

      <p className="text-xs text-[var(--text-muted)]">
        Snapshot verification runs through verify jobs — run one from the Jobs tab.
      </p>

      {/* Last GC outcome comes with the datastore listing itself (reads stay
          open to everyone), so it shows even without the action button. */}
      {gcStatus && (gcStatus["removed-bytes"] !== undefined || gcStatus["pending-bytes"] !== undefined || gcStatus["disk-bytes"] !== undefined) && (
        <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
          {gcStatus["removed-bytes"] !== undefined && <GCStat label="Removed by last GC" value={formatBytes(gcStatus["removed-bytes"])} />}
          {gcStatus["pending-bytes"] !== undefined && <GCStat label="Reclaimable (pending)" value={formatBytes(gcStatus["pending-bytes"])} />}
          {gcStatus["disk-bytes"] !== undefined && <GCStat label="Chunks on disk" value={formatBytes(gcStatus["disk-bytes"])} />}
        </div>
      )}

      <Dialog open={!!logUpid} onOpenChange={(open) => !open && setLogUpid(null)}>
        <DialogContent className="max-w-3xl">
          <DialogHeader>
            <DialogTitle>Garbage collection log</DialogTitle>
            <DialogDescription>{store}{task?.status === "running" && <span className="ml-2 text-[var(--status-warn)]">· running, log follows live</span>}</DialogDescription>
          </DialogHeader>
          {logQuery.isLoading ? (
            <div className="flex justify-center py-8" aria-busy>
              <Skeleton className="h-24 w-full" />
            </div>
          ) : logQuery.isError ? (
            <ErrorState title="Couldn't load the task log" onRetry={() => void logQuery.refetch()} />
          ) : (
            <pre className="max-h-96 overflow-y-auto rounded-md bg-[var(--bg-muted)] p-3 font-mono text-xs whitespace-pre-wrap">
              {(logQuery.data ?? []).join("\n") || "No log output."}
            </pre>
          )}
        </DialogContent>
      </Dialog>
    </div>
  )
}

function JobsTab({ connId, store, isAdmin }: { connId: string; store: string; isAdmin: boolean }) {
  const confirm = useConfirm()

  const syncQuery = useQuery({
    queryKey: ["pbs-sync-jobs", connId],
    queryFn: () => api.get<PBSSyncJob[]>(`/connections/${connId}/pbs/sync-jobs/`),
    retry: false,
  })
  const verifyQuery = useQuery({
    queryKey: ["pbs-verify-jobs", connId],
    queryFn: () => api.get<PBSVerifyJob[]>(`/connections/${connId}/pbs/verify-jobs/`),
    retry: false,
  })
  // Both list endpoints are connection-scoped — filter down to the datastore
  // whose tab this is so the per-store view stays honest.
  const syncJobs = (syncQuery.data ?? []).filter((j) => j.store === store)
  const verifyJobs = (verifyQuery.data ?? []).filter((j) => j.store === store)

  const runJob = useMutation({
    mutationFn: ({ kind, jobId }: { kind: "sync" | "verify"; jobId: string }) =>
      kind === "sync"
        ? api.post<{ upid: string }>(`/connections/${connId}/pbs/sync-jobs/${encodeURIComponent(jobId)}/run`)
        : api.post<{ upid: string }>(`/connections/${connId}/pbs/verify-jobs/${encodeURIComponent(jobId)}/run`),
    onSuccess: (_res, { kind, jobId }) => toast.success(`${kind === "sync" ? "Sync" : "Verify"} job ${jobId} started`),
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to run job"),
  })

  async function runNow(kind: "sync" | "verify", jobId: string, detail: string) {
    const ok = await confirm({
      title: `Run ${kind} job ${jobId} now?`,
      description: `${detail} The job normally follows its schedule; running it now pulls the next sync or verify forward.`,
      confirmLabel: "Run job",
      destructive: false, // a run adds data, it doesn't remove any
    })
    if (ok) runJob.mutate({ kind, jobId })
  }

  const syncColumns = useMemo<ColumnDef<PBSSyncJob>[]>(() => {
    const cols: ColumnDef<PBSSyncJob>[] = [
      {
        accessorKey: "id",
        header: "Job",
        cell: (c) => (
          <div className="min-w-0">
            <p className="font-medium">{c.getValue<string>()}</p>
            {c.row.original.comment && <p className="truncate text-[11px] text-[var(--text-muted)]">{c.row.original.comment}</p>}
          </div>
        ),
      },
      {
        id: "source",
        header: "Source",
        meta: { hideBelowMd: true },
        cell: (c) => {
          const j = c.row.original
          return (
            <span className="block max-w-52 truncate font-mono text-xs text-[var(--text-muted)]" title={`${j.remote ?? ""}${j["remote-ns"] ? `:${j["remote-ns"]}` : ""}/${j["remote-store"]}`}>
              {j.remote ? `${j.remote}/${j["remote-store"]}` : j["remote-store"]}
              {j["remote-ns"] ? ` (${j["remote-ns"]})` : ""}
            </span>
          )
        },
      },
      {
        accessorKey: "schedule",
        header: "Schedule",
        cell: (c) => <span className="font-mono text-xs tabular">{c.getValue<string>() || "—"}</span>,
      },
    ]
    if (isAdmin)
      cols.push({
        id: "actions",
        header: "",
        size: 96,
        cell: (c) => {
          const pending = runJob.isPending && runJob.variables?.kind === "sync" && runJob.variables?.jobId === c.row.original.id
          return (
            <Button
              size="sm"
              variant="ghost"
              loading={pending}
              disabled={runJob.isPending}
              onClick={() => void runNow("sync", c.row.original.id, `Pulls backups from ${c.row.original.remote ?? "the remote"}/${c.row.original["remote-store"]} into ${store}.`)}
            >
              {!pending && <Play className="h-3 w-3" />} Run now
            </Button>
          )
        },
      })
    return cols
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isAdmin, runJob.isPending, runJob.variables, store])

  const verifyColumns = useMemo<ColumnDef<PBSVerifyJob>[]>(() => {
    const cols: ColumnDef<PBSVerifyJob>[] = [
      {
        accessorKey: "id",
        header: "Job",
        cell: (c) => (
          <div className="min-w-0">
            <p className="font-medium">{c.getValue<string>()}</p>
            {c.row.original.comment && <p className="truncate text-[11px] text-[var(--text-muted)]">{c.row.original.comment}</p>}
          </div>
        ),
      },
      {
        accessorKey: "schedule",
        header: "Schedule",
        cell: (c) => <span className="font-mono text-xs tabular">{c.getValue<string>() || "—"}</span>,
      },
    ]
    if (isAdmin)
      cols.push({
        id: "actions",
        header: "",
        size: 96,
        cell: (c) => {
          const pending = runJob.isPending && runJob.variables?.kind === "verify" && runJob.variables?.jobId === c.row.original.id
          return (
            <Button
              size="sm"
              variant="ghost"
              loading={pending}
              disabled={runJob.isPending}
              onClick={() => void runNow("verify", c.row.original.id, `Verifies snapshot integrity in ${store}${c.row.original["ignore-verified"] ? ", skipping already-verified snapshots" : ""}.`)}
            >
              {!pending && <Play className="h-3 w-3" />} Run now
            </Button>
          )
        },
      })
    return cols
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isAdmin, runJob.isPending, runJob.variables, store])

  return (
    <div className="space-y-5">
      <div className="space-y-2">
        <h3 className="text-sm font-semibold">Sync jobs</h3>
        {syncQuery.isError ? (
          <ErrorState title="Couldn't load sync jobs" onRetry={() => void syncQuery.refetch()} />
        ) : (
          <DataTable
            columns={syncColumns}
            data={syncJobs}
            loading={syncQuery.isLoading}
            searchable={false}
            emptyMessage="No sync jobs target this datastore."
          />
        )}
      </div>
      <div className="space-y-2">
        <h3 className="text-sm font-semibold">Verify jobs</h3>
        {verifyQuery.isError ? (
          <ErrorState title="Couldn't load verify jobs" onRetry={() => void verifyQuery.refetch()} />
        ) : (
          <DataTable
            columns={verifyColumns}
            data={verifyJobs}
            loading={verifyQuery.isLoading}
            searchable={false}
            emptyMessage="No verify jobs target this datastore."
          />
        )}
      </div>
    </div>
  )
}
