import { useMutation, useQueries, useQuery, useQueryClient } from "@tanstack/react-query"
import { HardDrive, Play, Plus, RefreshCw, Trash2 } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { EmptyState } from "@/components/ui/empty-state"
import { ErrorState } from "@/components/ui/error-state"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { MultiSelect } from "@/components/ui/multi-select"
import { PageHeader } from "@/components/ui/page-header"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { StatusDot } from "@/components/ui/status-dot"
import { Hint } from "@/components/ui/tooltip"
import { api, ApiError, type BackupJob, type ClusterResource, type ConnectionInventory, type ReplicationJob, type ReplicationStatus } from "@/lib/api"

export function BackupsPage() {
  const queryClient = useQueryClient()
  const confirm = useConfirm()
  const { data: inventory, isLoading, isError, refetch } = useQuery({
    queryKey: ["inventory"],
    queryFn: () => api.get<ConnectionInventory[]>("/inventory/"),
  })
  const connections = inventory ?? []

  const jobQueries = useQueries({
    queries: connections.map((c) => ({
      queryKey: ["backup-jobs", c.connectionId],
      queryFn: () => api.get<BackupJob[]>(`/connections/${c.connectionId}/cluster/backup-jobs`),
      retry: false,
    })),
  })

  const [form, setForm] = useState({ connId: "", node: "", storage: "", vmid: [] as string[] })
  const runNow = useMutation({
    mutationFn: () =>
      api.post(`/connections/${form.connId}/cluster/backup-jobs/run`, {
        node: form.node,
        storage: form.storage,
        vmids: form.vmid.length ? form.vmid : undefined,
      }),
    onSuccess: () => {
      toast.success("Backup started — watch it in the Task Center")
      queryClient.invalidateQueries({ queryKey: ["backup-jobs"] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to start backup"),
  })

  const [scheduleConnId, setScheduleConnId] = useState("")
  const emptyScheduleForm = {
    schedule: "sat 02:00", storage: "", vmids: [] as string[], mode: "snapshot", compress: "zstd", prune: "",
    notificationMode: "", mailTo: "", mailNotification: "", bwlimit: "", pigz: "",
  }
  const [scheduleForm, setScheduleForm] = useState(emptyScheduleForm)
  const [showAdvanced, setShowAdvanced] = useState(false)
  const createJob = useMutation({
    mutationFn: () =>
      api.post(`/connections/${scheduleConnId}/cluster/backup-jobs`, {
        schedule: scheduleForm.schedule,
        storage: scheduleForm.storage,
        vmids: scheduleForm.vmids.length ? scheduleForm.vmids.join(",") : undefined,
        mode: scheduleForm.mode,
        compress: scheduleForm.compress,
        enabled: true,
        prune: scheduleForm.prune ? Number(scheduleForm.prune) : undefined,
        notificationMode: scheduleForm.notificationMode || undefined,
        mailTo: scheduleForm.notificationMode === "legacy-sendmail" ? scheduleForm.mailTo || undefined : undefined,
        mailNotification: scheduleForm.notificationMode === "legacy-sendmail" ? scheduleForm.mailNotification || undefined : undefined,
        bandwidthLimitKBps: scheduleForm.bwlimit ? Number(scheduleForm.bwlimit) : undefined,
        pigz: scheduleForm.pigz !== "" ? Number(scheduleForm.pigz) : undefined,
      }),
    onSuccess: () => {
      toast.success("Backup job scheduled")
      queryClient.invalidateQueries({ queryKey: ["backup-jobs"] })
      setScheduleForm(emptyScheduleForm)
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to schedule backup job"),
  })

  const deleteJob = useMutation({
    mutationFn: ({ connId, jobId }: { connId: string; jobId: string }) =>
      api.delete(`/connections/${connId}/cluster/backup-jobs/${encodeURIComponent(jobId)}`),
    onSuccess: () => {
      toast.success("Backup job removed")
      queryClient.invalidateQueries({ queryKey: ["backup-jobs"] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to remove backup job"),
  })

  async function removeJob(connId: string, connName: string, jobId: string) {
    const ok = await confirm({
      title: `Remove job ${jobId}?`,
      description: `Ferrum stops scheduling this vzdump job on ${connName}. Existing backups on the storage are not deleted.`,
      confirmLabel: "Remove job",
    })
    if (ok) deleteJob.mutate({ connId, jobId })
  }

  // Dropdown data from the inventory we already hold — no more guessing
  // node/storage names (PVE rejects unknown ones with a cryptic error).
  const runNodes = form.connId
    ? Array.from(new Set((connections.find((c) => c.connectionId === form.connId)?.resources ?? []).filter((r) => r.type === "node").map((r) => r.node)))
    : []
  const runStorages = form.connId
    ? Array.from(new Set((connections.find((c) => c.connectionId === form.connId)?.resources ?? []).filter((r) => r.type === "storage" && r.storage).map((r) => r.storage!)))
    : []
  const schedStorages = scheduleConnId
    ? Array.from(new Set((connections.find((c) => c.connectionId === scheduleConnId)?.resources ?? []).filter((r) => r.type === "storage" && r.storage).map((r) => r.storage!)))
    : []
  // Guest pick-lists sourced from the inventory already in cache — a typo'd
  // VMID can't reach the Proxmox API, matching the pattern GuestDetailDialog
  // already uses for disk/node/storage fields.
  function guestOptions(connId: string) {
    return (connections.find((c) => c.connectionId === connId)?.resources ?? [])
      .filter((r) => r.type === "qemu" || r.type === "lxc")
      .map((r) => ({ value: String(r.vmid), label: `${r.name ?? `#${r.vmid}`} (#${r.vmid})` }))
  }
  const runGuestOptions = form.connId ? guestOptions(form.connId) : []
  const schedGuestOptions = scheduleConnId ? guestOptions(scheduleConnId) : []

  return (
    <div className="space-y-4">
      <PageHeader
        title="Backups"
        description="Scheduled vzdump jobs across every connection."
        icon={HardDrive}
      />

      {isError ? (
        <ErrorState title="Couldn't load connections" onRetry={refetch} />
      ) : isLoading ? (
        <div className="space-y-3" aria-busy>
          <Skeleton className="h-56" />
          <Skeleton className="h-44" />
        </div>
      ) : connections.length === 0 ? (
        <EmptyState
          icon={HardDrive}
          title="No connections configured yet"
          description="Add a Proxmox connection first — backup jobs are scheduled per cluster."
        />
      ) : (
        <>
          <Card>
            <CardHeader>
              <CardTitle>Schedule a backup job</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
                <div className="space-y-1.5">
                  <Label>Connection</Label>
                  <Select
                    value={scheduleConnId}
                    onValueChange={(v) => {
                      setScheduleConnId(v)
                      setScheduleForm({ ...scheduleForm, vmids: [] })
                    }}
                  >
                    <SelectTrigger>
                      <SelectValue placeholder="Select..." />
                    </SelectTrigger>
                    <SelectContent>
                      {connections.map((c) => (
                        <SelectItem key={c.connectionId} value={c.connectionId}>{c.name}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-1.5">
                  <Label>Schedule</Label>
                  <Input
                    value={scheduleForm.schedule}
                    onChange={(e) => setScheduleForm({ ...scheduleForm, schedule: e.target.value })}
                    placeholder="sat 02:00"
                  />
                </div>
                <div className="space-y-1.5">
                  <Label>Storage</Label>
                  <Select value={scheduleForm.storage} onValueChange={(v) => setScheduleForm({ ...scheduleForm, storage: v })}>
                    <SelectTrigger>
                      <SelectValue placeholder={schedStorages.length ? "Select storage..." : "Pick a connection first"} />
                    </SelectTrigger>
                    <SelectContent>
                      {schedStorages.map((s) => (
                        <SelectItem key={s} value={s}>{s}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-1.5">
                  <Label>Guests (blank = all)</Label>
                  <MultiSelect
                    options={schedGuestOptions}
                    selected={scheduleForm.vmids}
                    onChange={(v) => setScheduleForm({ ...scheduleForm, vmids: v })}
                    allLabel={scheduleConnId ? "All guests" : "Pick a connection first"}
                    label="Guests"
                    className="w-full"
                  />
                </div>
                <div className="space-y-1.5">
                  <Label>Mode</Label>
                  <Select value={scheduleForm.mode} onValueChange={(v) => setScheduleForm({ ...scheduleForm, mode: v })}>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="snapshot">Snapshot</SelectItem>
                      <SelectItem value="suspend">Suspend</SelectItem>
                      <SelectItem value="stop">Stop</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-1.5">
                  <Label>Keep last N (optional)</Label>
                  <Input
                    type="number"
                    min={1}
                    value={scheduleForm.prune}
                    onChange={(e) => setScheduleForm({ ...scheduleForm, prune: e.target.value })}
                    placeholder="7"
                  />
                </div>
              </div>

              <Button variant="ghost" size="sm" className="mt-2" onClick={() => setShowAdvanced(!showAdvanced)}>
                {showAdvanced ? "Hide" : "Show"} advanced options
              </Button>
              {showAdvanced && (
                <div className="mt-2 grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
                  <div className="space-y-1.5">
                    <Label>Notification mode</Label>
                    <Select
                      value={scheduleForm.notificationMode || "default"}
                      onValueChange={(v) => setScheduleForm({ ...scheduleForm, notificationMode: v === "default" ? "" : v })}
                    >
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="default">Cluster default</SelectItem>
                        <SelectItem value="notification-system">Notification system</SelectItem>
                        <SelectItem value="legacy-sendmail">Legacy sendmail</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                  {scheduleForm.notificationMode === "legacy-sendmail" && (
                    <>
                      <div className="space-y-1.5">
                        <Label>Mail to</Label>
                        <Input
                          value={scheduleForm.mailTo}
                          onChange={(e) => setScheduleForm({ ...scheduleForm, mailTo: e.target.value })}
                          placeholder="admin@example.com"
                        />
                      </div>
                      <div className="space-y-1.5">
                        <Label>Mail on</Label>
                        <Select
                          value={scheduleForm.mailNotification || "failure"}
                          onValueChange={(v) => setScheduleForm({ ...scheduleForm, mailNotification: v })}
                        >
                          <SelectTrigger>
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectItem value="failure">Failure only</SelectItem>
                            <SelectItem value="always">Always</SelectItem>
                          </SelectContent>
                        </Select>
                      </div>
                    </>
                  )}
                  <div className="space-y-1.5">
                    <Label>Bandwidth limit (KB/s, optional)</Label>
                    <Input
                      type="number"
                      min={0}
                      value={scheduleForm.bwlimit}
                      onChange={(e) => setScheduleForm({ ...scheduleForm, bwlimit: e.target.value })}
                      placeholder="unlimited"
                    />
                  </div>
                  <div className="space-y-1.5">
                    <Label>Pigz threads (optional)</Label>
                    <Input
                      type="number"
                      min={0}
                      value={scheduleForm.pigz}
                      onChange={(e) => setScheduleForm({ ...scheduleForm, pigz: e.target.value })}
                      placeholder="off"
                    />
                  </div>
                </div>
              )}

              <Button
                className="mt-3"
                size="sm"
                loading={createJob.isPending}
                disabled={!scheduleConnId || !scheduleForm.schedule || !scheduleForm.storage}
                onClick={() => createJob.mutate()}
              >
                {!createJob.isPending && <Plus className="h-3.5 w-3.5" />} Schedule job
              </Button>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Run a backup now</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
                <div className="space-y-1.5">
                  <Label>Connection</Label>
                  <Select value={form.connId} onValueChange={(v) => setForm({ ...form, connId: v, node: "", storage: "", vmid: [] })}>
                    <SelectTrigger>
                      <SelectValue placeholder="Select..." />
                    </SelectTrigger>
                    <SelectContent>
                      {connections.map((c) => (
                        <SelectItem key={c.connectionId} value={c.connectionId}>{c.name}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-1.5">
                  <Label>Node</Label>
                  <Select value={form.node} onValueChange={(v) => setForm({ ...form, node: v })}>
                    <SelectTrigger>
                      <SelectValue placeholder={runNodes.length ? "Select node..." : "Pick a connection first"} />
                    </SelectTrigger>
                    <SelectContent>
                      {runNodes.map((n) => (
                        <SelectItem key={n} value={n}>{n}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-1.5">
                  <Label>Storage</Label>
                  <Select value={form.storage} onValueChange={(v) => setForm({ ...form, storage: v })}>
                    <SelectTrigger>
                      <SelectValue placeholder={runStorages.length ? "Select storage..." : "Pick a connection first"} />
                    </SelectTrigger>
                    <SelectContent>
                      {runStorages.map((s) => (
                        <SelectItem key={s} value={s}>{s}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-1.5">
                  <Label>Guests (blank = all)</Label>
                  <MultiSelect
                    options={runGuestOptions}
                    selected={form.vmid}
                    onChange={(v) => setForm({ ...form, vmid: v })}
                    allLabel={form.connId ? "All guests" : "Pick a connection first"}
                    label="Guests"
                    className="w-full"
                  />
                </div>
              </div>
              <Button
                className="mt-3"
                size="sm"
                loading={runNow.isPending}
                disabled={!form.connId || !form.node || !form.storage}
                onClick={() => runNow.mutate()}
              >
                {!runNow.isPending && <Play className="h-3.5 w-3.5" />} Run backup
              </Button>
            </CardContent>
          </Card>

          {connections.map((c, i) => {
            const q = jobQueries[i]
            return (
              <Card key={c.connectionId}>
                <CardHeader>
                  <CardTitle>{c.name}</CardTitle>
                </CardHeader>
                <CardContent className="space-y-2">
                  {q.isLoading && <Skeleton className="h-14" />}
                  {q.isError && (
                    <p className="text-sm text-[var(--text-muted)]">Could not load backup jobs for this connection.</p>
                  )}
                  {q.data?.length === 0 && (
                    <p className="text-sm text-[var(--text-muted)]">No scheduled backup jobs — schedule one above.</p>
                  )}
                  {q.data?.map((job) => (
                    <div key={job.id} className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-[var(--border)] px-3 py-2 text-sm">
                      <div className="min-w-0">
                        <p className="truncate font-medium">{job.id}</p>
                        <p className="break-words text-xs text-[var(--text-muted)]">
                          {job.schedule ?? "manual"} · storage: {job.storage ?? "-"} · guests: {job.vmid ?? "all"}
                        </p>
                      </div>
                      <div className="flex shrink-0 items-center gap-2">
                        <Badge variant={job.enabled === 0 ? "default" : "ok"}>{job.enabled === 0 ? "Disabled" : "Enabled"}</Badge>
                        <Hint label="Remove job">
                          <Button
                            size="icon"
                            variant="ghost"
                            className="hover:bg-[color-mix(in_oklab,var(--status-error)_12%,transparent)] hover:text-[var(--status-error)]"
                            aria-label={`Remove job ${job.id}`}
                            onClick={() => removeJob(c.connectionId, c.name, job.id)}
                          >
                            <Trash2 className="h-3.5 w-3.5" />
                          </Button>
                        </Hint>
                      </div>
                    </div>
                  ))}
                </CardContent>
              </Card>
            )
          })}
        </>
      )}

      {connections.length > 0 && (
        <>
          <div className="pt-2">
            <h2 className="font-display text-sm font-semibold">Replication</h2>
            <p className="text-sm text-[var(--text-muted)]">Storage replication (pvesr) jobs — periodic guest-disk sync to another node.</p>
          </div>
          {connections.map((c) => (
            <ReplicationCard key={c.connectionId} connId={c.connectionId} name={c.name} nodes={(c.resources ?? []).filter((r) => r.type === "node")} />
          ))}
        </>
      )}
    </div>
  )
}

function ReplicationCard({ connId, name, nodes }: { connId: string; name: string; nodes: ClusterResource[] }) {
  const queryClient = useQueryClient()
  const jobsQuery = useQuery({
    queryKey: ["replication-jobs", connId],
    queryFn: () => api.get<ReplicationJob[]>(`/connections/${connId}/cluster/replication-jobs`),
    retry: false,
  })

  const statusQueries = useQueries({
    queries: nodes.map((n) => ({
      queryKey: ["replication-status", connId, n.node],
      queryFn: () => api.get<ReplicationStatus[]>(`/connections/${connId}/nodes/${n.node}/replication`),
      retry: false,
    })),
  })
  const statusById = new Map<string, ReplicationStatus>()
  statusQueries.forEach((q) => (q.data ?? []).forEach((s) => statusById.set(s.id, s)))

  const runNow = useMutation({
    mutationFn: (job: ReplicationJob) => {
      const node = job.source || nodes[0]?.node
      return api.post(`/connections/${connId}/nodes/${node}/replication/${encodeURIComponent(job.id)}/run`)
    },
    onSuccess: () => {
      toast.success("Replication started")
      queryClient.invalidateQueries({ queryKey: ["replication-status", connId] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to start replication"),
  })

  if (jobsQuery.isError) return null // replication isn't configured on many clusters — no need for an error card everywhere
  if (jobsQuery.data?.length === 0) return null

  return (
    <Card>
      <CardHeader>
        <CardTitle>{name}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-2">
        {jobsQuery.data?.map((job) => {
          const status = statusById.get(job.id)
          return (
            <div key={job.id} className="flex flex-wrap items-center justify-between gap-2 rounded-md bg-[var(--bg-muted)] px-3 py-2 text-sm">
              <div className="min-w-0">
                <p className="flex items-center gap-2 font-medium">
                  {status?.error && <StatusDot status="error" />}
                  Guest #{job.guest} <span className="font-mono text-xs text-[var(--text-muted)]">→ {job.target}</span>
                </p>
                <p className="break-words text-xs text-[var(--text-muted)] tabular">
                  {job.schedule ?? "manual"}
                  {status?.last_sync ? ` · last sync ${new Date(status.last_sync * 1000).toLocaleString()}` : ""}
                  {status?.next_sync ? ` · next ${new Date(status.next_sync * 1000).toLocaleString()}` : ""}
                </p>
                {status?.error && <p className="break-words text-xs text-[var(--status-error)]">{status.error}</p>}
              </div>
              <div className="flex shrink-0 items-center gap-2">
                {status?.fail_count ? <Badge variant="error">{status.fail_count} failures</Badge> : null}
                <Badge variant={job.disable === 1 ? "default" : "ok"}>{job.disable === 1 ? "Disabled" : "Enabled"}</Badge>
                <Hint label="Run now">
                  <Button size="icon" variant="ghost" aria-label="Run replication now" disabled={runNow.isPending} onClick={() => runNow.mutate(job)}>
                    <RefreshCw className="h-3.5 w-3.5" />
                  </Button>
                </Hint>
              </div>
            </div>
          )
        })}
      </CardContent>
    </Card>
  )
}
