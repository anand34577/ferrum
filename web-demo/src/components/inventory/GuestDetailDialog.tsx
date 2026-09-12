import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Camera, Copy, HardDrive, Loader2, Lock, Network, Pencil, Snowflake, SquareTerminal, Sun, Terminal, Trash2, Workflow, X } from "lucide-react"
import { useEffect, useMemo, useState } from "react"
import { toast } from "sonner"
import { GaugeChart } from "@/components/charts/GaugeChart"
import { ResourceAreaChart } from "@/components/charts/ResourceAreaChart"
import { FirewallRulesPanel } from "@/components/firewall/FirewallRulesPanel"
import { FileRestoreBrowser } from "@/components/inventory/FileRestoreBrowser"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Meter } from "@/components/ui/meter"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { StatusDot } from "@/components/ui/status-dot"
import { Switch } from "@/components/ui/switch"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Textarea } from "@/components/ui/textarea"
import { TypeChip } from "@/components/ui/type-chip"
import { guestDotStatus } from "@/lib/utils"
import {
  api,
  ApiError,
  type AgentNetworkInterface,
  type ClusterResource,
  type ConnectionInventory,
  type GuestAgentExecResult,
  type GuestAgentExecStatus,
  type GuestAgentFSInfo,
  type GuestAgentOSInfo,
  type GuestConfig,
  type GuestLiveStatus,
  type RRDPoint,
  type Snapshot,
  type StorageContentItem,
} from "@/lib/api"
import { buildConsoleUrl, buildShellUrl } from "@/lib/console"
import { FORMATTERS, GUEST_SERIES, buildRRDRows, type ChartRow, type SeriesSpec } from "@/lib/metrics"
import { useLiveRates } from "@/lib/useLiveRates"
import { formatBytes, formatRate, formatUptime } from "@/lib/utils"

interface GuestDetailDialogProps {
  connId: string
  guest: ClusterResource | null
  onOpenChange: (open: boolean) => void
}

export function GuestDetailDialog({ connId, guest, onOpenChange }: GuestDetailDialogProps) {
  const open = !!guest
  const queryClient = useQueryClient()
  const confirm = useConfirm()
  const base = guest ? `/connections/${connId}/guests/${guest.type}/${guest.node}/${guest.vmid}` : ""

  const configQuery = useQuery({
    queryKey: ["guest-config", connId, guest?.id],
    queryFn: () => api.get<GuestConfig>(`${base}/config`),
    enabled: open,
  })

  // Shares the "inventory" cache key with every other screen — this dialog
  // never causes an extra request, it just reads what's already fetched.
  // Used to turn the disk/node/storage identifier fields in the Actions tab
  // from free text into pick-lists, so a typo can't reach the Proxmox API.
  const inventoryQuery = useQuery({
    queryKey: ["inventory"],
    queryFn: () => api.get<ConnectionInventory[]>("/inventory/"),
    enabled: open,
  })
  const sameConnResources = useMemo(
    () => inventoryQuery.data?.find((c) => c.connectionId === connId)?.resources ?? [],
    [inventoryQuery.data, connId],
  )
  const otherNodes = useMemo(
    () => sameConnResources.filter((r) => r.type === "node" && r.node !== guest?.node),
    [sameConnResources, guest?.node],
  )
  const storages = useMemo(
    () => Array.from(new Set(sameConnResources.filter((r) => r.type === "storage").map((r) => r.storage ?? r.id))).sort(),
    [sameConnResources],
  )
  const diskKeys = useMemo(() => (configQuery.data?.disks ?? []).map((d) => d.key), [configQuery.data])

  const [editingConfig, setEditingConfig] = useState(false)
  const [configForm, setConfigForm] = useState({ cores: "", memory: "", tags: "", notes: "" })

  useEffect(() => {
    if (configQuery.data) {
      setConfigForm({
        cores: configQuery.data.cores?.toString() ?? "",
        memory: configQuery.data.memory?.toString() ?? "",
        tags: configQuery.data.tags ?? "",
        notes: configQuery.data.notes ?? "",
      })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [configQuery.data])

  const updateConfig = useMutation({
    mutationFn: () =>
      api.put(`${base}/config`, {
        cores: configForm.cores ? Number(configForm.cores) : undefined,
        memory: configForm.memory ? Number(configForm.memory) : undefined,
        tags: configForm.tags,
        notes: configForm.notes,
      }),
    onSuccess: () => {
      toast.success("Configuration updated")
      setEditingConfig(false)
      queryClient.invalidateQueries({ queryKey: ["guest-config", connId, guest?.id] })
      queryClient.invalidateQueries({ queryKey: ["inventory"] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to update configuration"),
  })

  const [resizeDisk, setResizeDisk] = useState("")
  const [resizeAmount, setResizeAmount] = useState("")
  const resizeDiskMutation = useMutation({
    mutationFn: () => api.post(`${base}/resize`, { disk: resizeDisk, size: `+${resizeAmount}G` }),
    onSuccess: () => {
      toast.success(`Grew ${resizeDisk} by ${resizeAmount}G`)
      setResizeAmount("")
      queryClient.invalidateQueries({ queryKey: ["guest-config", connId, guest?.id] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Resize failed"),
  })

  const [moveDisk, setMoveDisk] = useState("")
  const [moveTargetStorage, setMoveTargetStorage] = useState("")
  const [moveDeleteSource, setMoveDeleteSource] = useState(false)
  const moveDiskMutation = useMutation({
    mutationFn: () => api.post(`${base}/move-disk`, { disk: moveDisk, storage: moveTargetStorage, delete: moveDeleteSource }),
    onSuccess: () => {
      toast.success(`Moving ${moveDisk} to ${moveTargetStorage}`)
      setMoveDisk("")
      setMoveTargetStorage("")
      queryClient.invalidateQueries({ queryKey: ["guest-config", connId, guest?.id] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Disk move failed"),
  })

  // --- QEMU guest agent (exec, fsfreeze, shutdown, set-password) ---
  const agentPing = useMutation({
    mutationFn: () => api.post(`${base}/agent/ping`),
    onSuccess: () => toast.success("Guest agent responded"),
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Guest agent unavailable"),
  })

  const [execCommand, setExecCommand] = useState("")
  const [execPid, setExecPid] = useState<number | null>(null)
  const execStatusQuery = useQuery({
    queryKey: ["guest-agent-exec-status", connId, guest?.id, execPid],
    queryFn: () => api.get<GuestAgentExecStatus>(`${base}/agent/exec-status?pid=${execPid}`),
    enabled: execPid !== null,
    refetchInterval: (query) => (query.state.data?.exited ? false : 1000),
  })
  const execMutation = useMutation({
    mutationFn: () => api.post<GuestAgentExecResult>(`${base}/agent/exec`, { command: ["/bin/sh", "-c", execCommand] }),
    onSuccess: (res) => setExecPid(res.pid),
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Exec failed — is the guest agent running?"),
  })

  const fsfreeze = useMutation({
    mutationFn: (action: "freeze" | "thaw") => api.post(`${base}/agent/fsfreeze/${action}`),
    onSuccess: (_, action) => toast.success(action === "freeze" ? "Filesystems frozen" : "Filesystems thawed"),
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Fsfreeze failed"),
  })

  const agentShutdown = useMutation({
    mutationFn: () => api.post(`${base}/agent/shutdown`),
    onSuccess: () => toast.success("Shutdown requested via guest agent"),
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Agent shutdown failed"),
  })

  async function confirmFreeze() {
    const ok = await confirm({
      title: "Freeze filesystems?",
      description: "This suspends I/O inside the guest until thawed — leaving it frozen for long, or an unexpected disconnect, can hang the guest.",
      confirmLabel: "Freeze filesystems",
    })
    if (ok) fsfreeze.mutate("freeze")
  }

  async function confirmAgentShutdown() {
    const ok = await confirm({
      title: "Shut down guest via agent?",
      description: "Requests a graceful shutdown from inside the guest through the guest agent. This cannot be undone.",
      confirmLabel: "Shut down",
    })
    if (ok) agentShutdown.mutate()
  }

  const [agentPwUser, setAgentPwUser] = useState("")
  const [agentPwPass, setAgentPwPass] = useState("")
  const agentSetPassword = useMutation({
    mutationFn: () => api.post(`${base}/agent/set-password`, { username: agentPwUser, password: agentPwPass }),
    onSuccess: () => {
      toast.success(`Password updated for ${agentPwUser}`)
      setAgentPwPass("")
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Set password failed"),
  })

  const snapshotsQuery = useQuery({
    queryKey: ["guest-snapshots", connId, guest?.id],
    queryFn: () => api.get<Snapshot[]>(`${base}/snapshots`),
    enabled: open,
  })

  const backupsQuery = useQuery({
    queryKey: ["guest-backups", connId, guest?.id],
    queryFn: () => api.get<StorageContentItem[]>(`${base}/backups`),
    enabled: open,
  })
  const [browsingBackup, setBrowsingBackup] = useState<string | null>(null)
  const setBackupProtected = useMutation({
    mutationFn: ({ volid, protect }: { volid: string; protect: boolean }) =>
      api.put(`/connections/${connId}/nodes/${guest?.node}/storage/${volid.split(":")[0]}/content/${encodeURIComponent(volid)}/protected`, {
        protected: protect,
      }),
    onSuccess: () => {
      toast.success("Backup archive updated")
      queryClient.invalidateQueries({ queryKey: ["guest-backups", connId, guest?.id] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to update backup archive"),
  })

  // LXC needs no in-guest agent at all — the host already sees a
  // container's network namespace directly, so its IPs come back the same
  // way a QEMU VM's do once the guest agent reports them.
  const agentEnabled = open && guest?.status === "running"
  const agentQuery = useQuery({
    queryKey: ["guest-agent-network", connId, guest?.id],
    queryFn: () => api.get<AgentNetworkInterface[]>(`${base}/agent/network`),
    enabled: agentEnabled,
    retry: false,
  })

  // OS info / hostname / timezone / filesystem usage — QEMU-only (LXC has no
  // guest agent), and only worth asking once we know the agent is actually
  // reachable (agentQuery succeeding on the network call above is the same
  // reachability signal Proxmox's own Summary tab relies on).
  const agentInfoEnabled = agentEnabled && guest?.type === "qemu" && agentQuery.isSuccess
  const osInfoQuery = useQuery({
    queryKey: ["guest-agent-osinfo", connId, guest?.id],
    queryFn: () => api.get<GuestAgentOSInfo>(`${base}/agent/osinfo`),
    enabled: agentInfoEnabled,
    retry: false,
  })
  const fsInfoQuery = useQuery({
    queryKey: ["guest-agent-fsinfo", connId, guest?.id],
    queryFn: () => api.get<GuestAgentFSInfo[]>(`${base}/agent/fsinfo`),
    enabled: agentInfoEnabled,
    retry: false,
  })
  const hostnameQuery = useQuery({
    queryKey: ["guest-agent-hostname", connId, guest?.id],
    queryFn: () => api.get<{ hostname: string }>(`${base}/agent/hostname`),
    enabled: agentInfoEnabled,
    retry: false,
  })
  const timezoneQuery = useQuery({
    queryKey: ["guest-agent-timezone", connId, guest?.id],
    queryFn: () => api.get<{ zone: string }>(`${base}/agent/timezone`),
    enabled: agentInfoEnabled,
    retry: false,
  })

  // Full metrics tab state: window + peak envelope + live status polling.
  const [metricTimeframe, setMetricTimeframe] = useState("hour")
  const [metricPeaks, setMetricPeaks] = useState(true)
  const metricAvgQuery = useQuery({
    queryKey: ["guest-rrd-metrics", connId, guest?.id, metricTimeframe, "avg"],
    queryFn: () => api.get<RRDPoint[]>(`${base}/rrddata?timeframe=${metricTimeframe}`),
    enabled: open,
  })
  const metricMaxQuery = useQuery({
    queryKey: ["guest-rrd-metrics", connId, guest?.id, metricTimeframe, "max"],
    queryFn: () => api.get<RRDPoint[]>(`${base}/rrddata?timeframe=${metricTimeframe}&cf=MAX`),
    enabled: open && metricPeaks,
    staleTime: 60_000,
  })
  const liveStatusQuery = useQuery({
    queryKey: ["guest-live-status", connId, guest?.id],
    queryFn: () => api.get<GuestLiveStatus>(`${base}/status`),
    enabled: open && guest?.status === "running",
    refetchInterval: 5_000,
  })
  const liveStatus = liveStatusQuery.data
  const liveRates = useLiveRates(liveStatus)
  const paused = liveStatus?.qmpstatus === "paused"

  const metricSpecs: SeriesSpec[] = useMemo(
    () => [
      ...GUEST_SERIES.cpu(metricPeaks),
      ...GUEST_SERIES.memory(metricPeaks),
      ...GUEST_SERIES.network(metricPeaks),
      ...GUEST_SERIES.disk(metricPeaks),
    ],
    [metricPeaks],
  )
  const metricRows: ChartRow[] = useMemo(
    () => buildRRDRows(metricAvgQuery.data, metricPeaks ? metricMaxQuery.data : undefined, metricSpecs),
    [metricAvgQuery.data, metricMaxQuery.data, metricSpecs, metricPeaks],
  )

  const openConsole = useMutation({
    mutationFn: async () => {
      const res = await api.post<{ wsPath: string; password?: string }>(`${base}/console`)
      return res
    },
    onSuccess: ({ wsPath, password }) => {
      if (guest) window.open(buildConsoleUrl(connId, guest, wsPath, password), "_blank", "width=1024,height=768")
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to open console"),
  })

  const openShell = useMutation({
    mutationFn: async () => api.post<{ wsPath: string }>(`${base}/shell`),
    onSuccess: ({ wsPath }) => {
      if (guest) window.open(buildShellUrl(connId, guest.name ?? `guest #${guest.vmid}`, wsPath, guest.node, guest), "_blank", "width=900,height=600")
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to open shell"),
  })

  function invalidate() {
    queryClient.invalidateQueries({ queryKey: ["inventory"] })
    queryClient.invalidateQueries({ queryKey: ["guest-snapshots", connId, guest?.id] })
  }

  const [snapName, setSnapName] = useState("")
  const createSnap = useMutation({
    mutationFn: () => api.post(`${base}/snapshots`, { name: snapName }),
    onSuccess: () => {
      toast.success("Snapshot creation started")
      setSnapName("")
      invalidate()
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to create snapshot"),
  })

  const rollbackSnap = useMutation({
    mutationFn: (name: string) => api.post(`${base}/snapshots/${encodeURIComponent(name)}/rollback`),
    onSuccess: () => {
      toast.success("Rollback started")
      invalidate()
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Rollback failed"),
  })

  async function rollbackSnapshot(name: string) {
    const ok = await confirm({
      title: `Roll back to "${name}"?`,
      description: "The guest's disks are reverted to the state captured in this snapshot. Changes made since then are lost.",
      confirmLabel: "Roll back",
    })
    if (ok) rollbackSnap.mutate(name)
  }

  async function removeSnapshot(name: string) {
    const ok = await confirm({
      title: `Delete snapshot "${name}"?`,
      description: "The snapshot point is removed. The guest's current data is not affected.",
      confirmLabel: "Delete snapshot",
    })
    if (ok) deleteSnap.mutate(name)
  }

  const deleteSnap = useMutation({
    mutationFn: (name: string) => api.delete(`${base}/snapshots/${encodeURIComponent(name)}`),
    onSuccess: () => {
      toast.success("Snapshot deleted")
      invalidate()
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to delete snapshot"),
  })

  const [cloneNewId, setCloneNewId] = useState("")
  const cloneGuest = useMutation({
    mutationFn: () => api.post(`${base}/clone`, { newId: Number(cloneNewId), full: true }),
    onSuccess: () => {
      toast.success("Clone started")
      onOpenChange(false)
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Clone failed"),
  })

  const [migrateTarget, setMigrateTarget] = useState("")
  const migrateGuest = useMutation({
    mutationFn: () => api.post(`${base}/migrate`, { targetNode: migrateTarget, online: guest?.status === "running" }),
    onSuccess: () => {
      toast.success("Migration started")
      onOpenChange(false)
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Migration failed"),
  })

  const unlockGuest = useMutation({
    mutationFn: () => api.post(`${base}/unlock`),
    onSuccess: () => {
      toast.success("Guest unlocked")
      invalidate()
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Unlock failed"),
  })

  async function clearLock() {
    const ok = await confirm({
      title: "Clear this guest's lock?",
      description: "Only do this if you're sure no task is actually using the guest — clearing a lock held by a running operation can corrupt its state.",
      confirmLabel: "Clear lock",
    })
    if (ok) unlockGuest.mutate()
  }

  const deleteGuest = useMutation({
    mutationFn: () => api.delete(base),
    onSuccess: () => {
      toast.success("Guest deletion started")
      queryClient.invalidateQueries({ queryKey: ["inventory"] })
      onOpenChange(false)
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Delete failed"),
  })

  async function removeGuest() {
    if (!guest) return
    const ok = await confirm({
      title: `Delete ${guest.name} (#${guest.vmid})?`,
      description:
        guest.status === "running"
          ? "Stop the guest first — running guests cannot be deleted. Deletion removes the guest and all of its disks from the cluster. This cannot be undone."
          : "The guest and all of its disks are removed from the cluster. This cannot be undone.",
      confirmLabel: "Delete guest",
    })
    if (ok) deleteGuest.mutate()
  }

  if (!guest) return null

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-3xl">
        <DialogHeader>
          <DialogTitle className="flex flex-wrap items-center gap-2">
            <TypeChip type={guest.type} />
            {guest.name} <span className="font-mono text-sm font-normal text-[var(--text-muted)]">#{guest.vmid}</span>
          </DialogTitle>
          <DialogDescription className="flex flex-wrap items-center gap-1.5">
            <span>{guest.node}</span>
            <span aria-hidden>·</span>
            <span className="flex items-center gap-1.5">
              {/* A QEMU guest suspended without saving state still reports
                  status "running" — qmpstatus is the only field that says
                  it is actually paused, so prefer it when they disagree. */}
              <StatusDot status={guestDotStatus(paused ? "paused" : guest.status)} />
              {paused ? "paused" : (guest.status ?? "unknown")}
            </span>
            {guest.hastate && (
              <>
                <span aria-hidden>·</span>
                <span>HA: {guest.hastate}</span>
              </>
            )}
          </DialogDescription>
        </DialogHeader>

        {guest.status === "running" && (
          <div className="flex flex-wrap items-center justify-around gap-x-4 gap-y-3 rounded-md border border-[var(--border)] bg-[var(--bg-muted)] px-3 py-3">
            <GaugeChart value={(guest.cpu ?? 0) * 100} label="CPU" size={72} />
            <GaugeChart value={guest.maxmem ? ((guest.mem ?? 0) / guest.maxmem) * 100 : 0} label="Memory" size={72} />
            {(guest.maxdisk ?? 0) > 0 && (
              <GaugeChart value={guest.maxdisk ? ((guest.disk ?? 0) / guest.maxdisk) * 100 : 0} label="Disk" size={72} />
            )}
            <div className="min-w-40 space-y-0.5 text-xs text-[var(--text-muted)]">
              <p className="flex justify-between gap-3">
                <span>Memory</span>
                <span className="tabular">{formatBytes(guest.mem ?? 0)} / {formatBytes(guest.maxmem ?? 0)}</span>
              </p>
              {(guest.maxdisk ?? 0) > 0 && (
                <p className="flex justify-between gap-3">
                  <span>Disk</span>
                  <span className="tabular">{formatBytes(guest.disk ?? 0)} / {formatBytes(guest.maxdisk ?? 0)}</span>
                </p>
              )}
              <p className="flex justify-between gap-3">
                <span>Uptime</span>
                <span className="tabular">{formatUptime(guest.uptime ?? 0)}</span>
              </p>
            </div>
          </div>
        )}

        {/* mt-3 keeps the tab bar clear of the live-status strip above it. */}
        <Tabs defaultValue="config" className="mt-3">
          <TabsList>
            <TabsTrigger value="config">Config</TabsTrigger>
            <TabsTrigger value="hardware">Hardware</TabsTrigger>
            <TabsTrigger value="network">Network</TabsTrigger>
            <TabsTrigger value="firewall">Firewall</TabsTrigger>
            <TabsTrigger value="metrics">Metrics</TabsTrigger>
            <TabsTrigger value="snapshots">Snapshots</TabsTrigger>
            <TabsTrigger value="backups">Backups</TabsTrigger>
            {guest.type === "qemu" && <TabsTrigger value="agent">Agent</TabsTrigger>}
            <TabsTrigger value="actions">Actions</TabsTrigger>
          </TabsList>

          <TabsContent value="config" className="space-y-3">
            {configQuery.isLoading ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : configQuery.isError ? (
              <p className="text-sm text-[var(--status-error)]">Couldn't load this guest's configuration.</p>
            ) : editingConfig ? (
              <div className="space-y-3">
                <div className="grid grid-cols-2 gap-3">
                  <div className="space-y-1.5">
                    <Label>Cores</Label>
                    <Input
                      type="number"
                      min={1}
                      value={configForm.cores}
                      onChange={(e) => setConfigForm((f) => ({ ...f, cores: e.target.value }))}
                    />
                  </div>
                  <div className="space-y-1.5">
                    <Label>Memory (MB)</Label>
                    <Input
                      type="number"
                      min={16}
                      value={configForm.memory}
                      onChange={(e) => setConfigForm((f) => ({ ...f, memory: e.target.value }))}
                    />
                  </div>
                </div>
                <div className="space-y-1.5">
                  <Label>Tags (comma or semicolon separated)</Label>
                  <Input value={configForm.tags} onChange={(e) => setConfigForm((f) => ({ ...f, tags: e.target.value }))} />
                </div>
                <div className="space-y-1.5">
                  <Label>Notes</Label>
                  <Textarea className="font-sans" rows={3} value={configForm.notes} onChange={(e) => setConfigForm((f) => ({ ...f, notes: e.target.value }))} />
                </div>
                <div className="flex gap-2">
                  <Button size="sm" disabled={updateConfig.isPending} onClick={() => updateConfig.mutate()}>
                    {updateConfig.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : null} Save
                  </Button>
                  <Button size="sm" variant="ghost" onClick={() => setEditingConfig(false)}>
                    <X className="h-3.5 w-3.5" /> Cancel
                  </Button>
                </div>
              </div>
            ) : (
              <>
                <dl className="grid grid-cols-2 gap-3 text-sm">
                  <div>
                    <dt className="text-xs text-[var(--text-muted)]">Cores</dt>
                    <dd>{configQuery.data?.cores ?? "-"}</dd>
                  </div>
                  <div>
                    <dt className="text-xs text-[var(--text-muted)]">Memory</dt>
                    <dd>{configQuery.data?.memory ? formatBytes(configQuery.data.memory * 1024 * 1024) : "-"}</dd>
                  </div>
                  <div>
                    <dt className="text-xs text-[var(--text-muted)]">Boot order</dt>
                    <dd className="font-mono text-xs">{configQuery.data?.boot ?? "-"}</dd>
                  </div>
                  <div>
                    <dt className="text-xs text-[var(--text-muted)]">Tags</dt>
                    <dd>{configQuery.data?.tags || "-"}</dd>
                  </div>
                  {configQuery.data?.notes && (
                    <div className="col-span-2">
                      <dt className="text-xs text-[var(--text-muted)]">Notes</dt>
                      <dd className="whitespace-pre-wrap text-xs">{configQuery.data.notes}</dd>
                    </div>
                  )}
                </dl>
                <Button size="sm" variant="secondary" onClick={() => setEditingConfig(true)}>
                  <Pencil className="h-3.5 w-3.5" /> Edit
                </Button>
              </>
            )}
          </TabsContent>

          <TabsContent value="hardware" className="space-y-4">
            <div>
              <p className="mb-1.5 text-xs font-medium text-[var(--text-muted)]">Disks</p>
              <div className="space-y-1">
                {(configQuery.data?.disks ?? []).map((d) => (
                  <div key={d.key} className="flex items-start gap-2 rounded-md border border-[var(--border)] px-3 py-1.5 text-sm">
                    <span className="w-16 shrink-0 font-mono text-xs text-[var(--text-muted)]">{d.key}</span>
                    <span className="break-all font-mono text-xs">{d.value}</span>
                  </div>
                ))}
                {(configQuery.data?.disks ?? []).length === 0 && <p className="text-sm text-[var(--text-muted)]">No disks found.</p>}
              </div>
            </div>
            <div>
              <p className="mb-1.5 text-xs font-medium text-[var(--text-muted)]">Network interfaces (configured)</p>
              <div className="space-y-1">
                {(configQuery.data?.networkDevices ?? []).map((n) => (
                  <div key={n.key} className="flex items-start gap-2 rounded-md border border-[var(--border)] px-3 py-1.5 text-sm">
                    <span className="w-16 shrink-0 font-mono text-xs text-[var(--text-muted)]">{n.key}</span>
                    <span className="break-all font-mono text-xs">{n.value}</span>
                  </div>
                ))}
                {(configQuery.data?.networkDevices ?? []).length === 0 && <p className="text-sm text-[var(--text-muted)]">No network devices found.</p>}
              </div>
            </div>
          </TabsContent>

          <TabsContent value="network" className="space-y-2">
            {guest.status !== "running" ? (
              <p className="text-sm text-[var(--text-muted)]">Start this {guest.type === "lxc" ? "container" : "VM"} to see its live network info.</p>
            ) : agentQuery.isLoading ? (
              <Skeleton className="h-24" />
            ) : agentQuery.isError ? (
              guest.type === "lxc" ? (
                <p className="text-sm text-[var(--text-muted)]">Couldn't read the container's network namespace.</p>
              ) : (
                <p className="text-sm text-[var(--text-muted)]">
                  Guest agent unavailable. Install <code className="rounded-sm bg-[var(--bg-muted)] px-1 py-0.5">qemu-guest-agent</code> inside the VM and enable
                  the QEMU Guest Agent option for it, then reopen this dialog.
                </p>
              )
            ) : (
              <div className="space-y-1.5">
                {(agentQuery.data ?? [])
                  .filter((i) => i.name !== "lo")
                  .map((iface) => (
                    <div key={iface.name} className="rounded-md border border-[var(--border)] px-3 py-2 text-sm">
                      <div className="flex items-center justify-between">
                        <span className="font-medium">{iface.name}</span>
                        {iface["hardware-address"] && (
                          <span className="font-mono text-xs text-[var(--text-muted)]">{iface["hardware-address"]}</span>
                        )}
                      </div>
                      <div className="mt-1 flex flex-wrap gap-1.5">
                        {iface["ip-addresses"].map((ip) => (
                          <Badge key={ip} variant="default">{ip}</Badge>
                        ))}
                        {iface["ip-addresses"].length === 0 && <span className="text-xs text-[var(--text-muted)]">No IP reported</span>}
                      </div>
                    </div>
                  ))}
                {(agentQuery.data ?? []).filter((i) => i.name !== "lo").length === 0 && (
                  <p className="text-sm text-[var(--text-muted)]">No non-loopback interfaces reported yet.</p>
                )}
              </div>
            )}

            {agentInfoEnabled && (osInfoQuery.data || hostnameQuery.data || timezoneQuery.data) && (
              <div className="mt-3 grid grid-cols-2 gap-x-4 gap-y-1 rounded-md border border-[var(--border)] px-3 py-2 text-sm sm:grid-cols-4">
                <div>
                  <p className="text-xs text-[var(--text-muted)]">Hostname</p>
                  <p className="truncate font-medium">{hostnameQuery.data?.hostname || "—"}</p>
                </div>
                <div>
                  <p className="text-xs text-[var(--text-muted)]">Timezone</p>
                  <p className="truncate font-medium">{timezoneQuery.data?.zone || "—"}</p>
                </div>
                <div>
                  <p className="text-xs text-[var(--text-muted)]">OS</p>
                  <p className="truncate font-medium">
                    {(osInfoQuery.data?.["pretty-name"] as string) ?? (osInfoQuery.data?.name as string) ?? "—"}
                  </p>
                </div>
                <div>
                  <p className="text-xs text-[var(--text-muted)]">Kernel</p>
                  <p className="truncate font-medium">
                    {(osInfoQuery.data?.["kernel-release"] as string) ?? (osInfoQuery.data?.version as string) ?? "—"}
                  </p>
                </div>
              </div>
            )}

            {agentInfoEnabled && (fsInfoQuery.data ?? []).length > 0 && (
              <div className="mt-3 space-y-1.5">
                <p className="text-xs font-medium text-[var(--text-muted)]">Filesystems</p>
                {(fsInfoQuery.data ?? []).map((fs, i) => {
                  const total = Number(fs["total-bytes"] ?? 0)
                  const used = Number(fs["used-bytes"] ?? 0)
                  const pctUsed = total > 0 ? (used / total) * 100 : 0
                  return (
                    <div key={`${fs.name ?? i}`} className="rounded-md border border-[var(--border)] px-3 py-2 text-sm">
                      <p className="flex items-baseline justify-between gap-2">
                        <span className="truncate font-medium">{String(fs.mountpoint ?? fs.name ?? "—")}</span>
                        <span className="shrink-0 text-xs text-[var(--text-muted)] tabular">
                          {total > 0 ? `${formatBytes(used)} / ${formatBytes(total)}` : String(fs.type ?? "")}
                        </span>
                      </p>
                      {total > 0 && <Meter value={pctUsed} size="xs" label={`${String(fs.mountpoint ?? fs.name ?? "filesystem")} usage`} className="mt-1" />}
                    </div>
                  )
                })}
              </div>
            )}
          </TabsContent>

          <TabsContent value="firewall">
            <FirewallRulesPanel basePath={base + "/firewall"} queryKey={["guest-fw-rules", connId, guest.id]} />
          </TabsContent>

          <TabsContent value="metrics" className="space-y-3">
            {/* Live counters — status/current cumulative diffs → bytes/sec */}
            {guest.status === "running" && (
              <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
                <LiveChip icon={Network} label="Net in" value={formatRate(liveRates.netin ?? 0)} />
                <LiveChip icon={Network} label="Net out" value={formatRate(liveRates.netout ?? 0)} />
                <LiveChip icon={HardDrive} label="Disk read" value={formatRate(liveRates.diskread ?? 0)} />
                <LiveChip icon={HardDrive} label="Disk write" value={formatRate(liveRates.diskwrite ?? 0)} />
              </div>
            )}
            {guest.status === "running" && liveStatus?.balloon !== undefined && guest.type === "qemu" && (
              <p className="text-xs text-[var(--text-muted)]">
                Balloon target: {formatBytes(liveStatus.balloon)}
                {liveStatus.cpus ? ` · ${liveStatus.cpus} vCPU` : ""}
              </p>
            )}

            <div className="flex items-center justify-end gap-3">
              <label className="flex items-center gap-2 text-xs text-[var(--text-muted)]">
                Peaks
                <Switch checked={metricPeaks} onCheckedChange={setMetricPeaks} aria-label="Show peak envelope" />
              </label>
              <Select value={metricTimeframe} onValueChange={setMetricTimeframe}>
                <SelectTrigger className="w-32">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="hour">Last hour</SelectItem>
                  <SelectItem value="day">Last day</SelectItem>
                  <SelectItem value="week">Last week</SelectItem>
                  <SelectItem value="month">Last month</SelectItem>
                  <SelectItem value="year">Last year</SelectItem>
                </SelectContent>
              </Select>
            </div>

            {metricAvgQuery.isLoading ? (
              <Skeleton className="h-40" />
            ) : metricAvgQuery.isError ? (
              <p className="text-sm text-[var(--status-error)]">Couldn't load metrics for this guest.</p>
            ) : metricRows.length === 0 ? (
              <p className="text-sm text-[var(--text-muted)]">No historical data available yet.</p>
            ) : (
              <div className="grid gap-4">
                <MetricChart title="CPU utilization" rows={metricRows} series={GUEST_SERIES.cpu(metricPeaks)} yDomain={[0, 100]} yTickFormatter={FORMATTERS.pct} />
                <MetricChart title="Memory" rows={metricRows} series={GUEST_SERIES.memory(metricPeaks)} valueKind="bytes" showLegend />
                <div className="grid gap-4 sm:grid-cols-2">
                  <MetricChart title="Network traffic" rows={metricRows} series={GUEST_SERIES.network(metricPeaks)} valueKind="rate" showLegend />
                  <MetricChart title="Disk I/O" rows={metricRows} series={GUEST_SERIES.disk(metricPeaks)} valueKind="rate" showLegend />
                </div>
              </div>
            )}
          </TabsContent>

          <TabsContent value="snapshots" className="space-y-3">
            <div className="flex gap-2">
              <Input placeholder="snapshot name" value={snapName} onChange={(e) => setSnapName(e.target.value)} />
              <Button size="sm" disabled={!snapName || createSnap.isPending} onClick={() => createSnap.mutate()}>
                <Camera className="h-3.5 w-3.5" /> Create
              </Button>
            </div>
            {snapshotsQuery.isError && (
              <p className="text-sm text-[var(--status-error)]">Couldn't load snapshots for this guest.</p>
            )}
            {/* Scoped scroll instead of growing the whole dialog past the tab
                bar — a guest with a long snapshot history stays inside this tab. */}
            <div className={(snapshotsQuery.data?.length ?? 0) > 8 ? "max-h-96 space-y-1.5 overflow-y-auto pr-1" : "space-y-1.5"}>
              {(snapshotsQuery.data ?? []).map((snap) => (
                <div key={snap.name} className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-[var(--border)] px-3 py-2 text-sm">
                  <div className="min-w-0">
                    <p className="truncate font-medium">{snap.name}</p>
                    {snap.description && <p className="break-words text-xs text-[var(--text-muted)]">{snap.description}</p>}
                  </div>
                  <div className="flex shrink-0 gap-1">
                    <Button size="sm" variant="ghost" onClick={() => rollbackSnapshot(snap.name)}>
                      Rollback
                    </Button>
                    <Button
                      size="icon"
                      variant="ghost"
                      aria-label={`Delete snapshot ${snap.name}`}
                      className="hover:bg-[color-mix(in_oklab,var(--status-error)_12%,transparent)] hover:text-[var(--status-error)]"
                      onClick={() => removeSnapshot(snap.name)}
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </Button>
                  </div>
                </div>
              ))}
              {snapshotsQuery.data?.length === 0 && (
                <p className="text-sm text-[var(--text-muted)]">No snapshots yet.</p>
              )}
            </div>
          </TabsContent>

          <TabsContent value="backups" className="space-y-1.5">
            {backupsQuery.isLoading ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : backupsQuery.isError ? (
              <p className="text-sm text-[var(--status-error)]">Couldn't load backup archives for this guest.</p>
            ) : (backupsQuery.data ?? []).length === 0 ? (
              <p className="text-sm text-[var(--text-muted)]">No backup archives found for this guest on its node's storage.</p>
            ) : (
              <div className={(backupsQuery.data?.length ?? 0) > 8 ? "max-h-96 space-y-1.5 overflow-y-auto pr-1" : "space-y-1.5"}>
              {(backupsQuery.data ?? [])
                .sort((a, b) => (b.ctime ?? 0) - (a.ctime ?? 0))
                .map((b) => (
                  <div key={b.volid} className="flex items-center justify-between gap-2 rounded-md border border-[var(--border)] px-3 py-2 text-sm">
                    <div className="min-w-0">
                      <p className="truncate font-mono text-xs">{b.volid}</p>
                      <p className="text-xs text-[var(--text-muted)]">
                        {b.ctime ? new Date(b.ctime * 1000).toLocaleString() : "unknown date"}
                        {b.size ? ` · ${formatBytes(b.size)}` : ""}
                        {b.format ? ` · ${b.format}` : ""}
                      </p>
                    </div>
                    <div className="flex shrink-0 items-center gap-2">
                      {b.protected ? <Badge variant="ok">Protected</Badge> : null}
                      <Button
                        size="sm"
                        variant="ghost"
                        loading={setBackupProtected.isPending}
                        onClick={async () => {
                          if (b.protected) {
                            const ok = await confirm({
                              title: "Remove backup protection?",
                              description: "This archive becomes eligible for prune-backups deletion again.",
                              confirmLabel: "Remove protection",
                            })
                            if (!ok) return
                          }
                          setBackupProtected.mutate({ volid: b.volid, protect: !b.protected })
                        }}
                      >
                        {b.protected ? "Unprotect" : "Protect"}
                      </Button>
                      <Button size="sm" variant="ghost" onClick={() => setBrowsingBackup(b.volid)}>
                        Browse files
                      </Button>
                    </div>
                  </div>
                ))}
              </div>
            )}
            {browsingBackup && (
              <FileRestoreBrowser
                connId={connId}
                node={guest.node}
                storage={browsingBackup.split(":")[0]}
                volume={browsingBackup}
                open={!!browsingBackup}
                onOpenChange={(o) => !o && setBrowsingBackup(null)}
              />
            )}
          </TabsContent>

          {guest.type === "qemu" && (
            <TabsContent value="agent" className="space-y-4">
              {guest.status !== "running" ? (
                <p className="text-sm text-[var(--text-muted)]">Start this VM to use the guest agent.</p>
              ) : (
                <>
                  <div className="flex flex-wrap gap-2">
                    <Button size="sm" variant="secondary" disabled={agentPing.isPending} onClick={() => agentPing.mutate()}>
                      {agentPing.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : null} Ping agent
                    </Button>
                    <Button size="sm" variant="secondary" disabled={fsfreeze.isPending} onClick={() => void confirmFreeze()}>
                      <Snowflake className="h-3.5 w-3.5" /> Freeze filesystems
                    </Button>
                    <Button size="sm" variant="secondary" disabled={fsfreeze.isPending} onClick={() => fsfreeze.mutate("thaw")}>
                      <Sun className="h-3.5 w-3.5" /> Thaw filesystems
                    </Button>
                    <Button size="sm" variant="secondary" disabled={agentShutdown.isPending} onClick={() => void confirmAgentShutdown()}>
                      Guest shutdown
                    </Button>
                  </div>

                  <div className="space-y-1.5">
                    <Label>Run command (via guest agent, in-guest shell)</Label>
                    <div className="flex gap-2">
                      <Input
                        placeholder="e.g. uptime"
                        value={execCommand}
                        onChange={(e) => setExecCommand(e.target.value)}
                        className="font-mono text-xs"
                      />
                      <Button size="sm" disabled={!execCommand || execMutation.isPending} onClick={() => execMutation.mutate()}>
                        <Terminal className="h-3.5 w-3.5" /> Run
                      </Button>
                    </div>
                    {execPid !== null && (
                      <div className="rounded-md border border-[var(--border)] bg-[var(--bg-muted)] p-2">
                        {!execStatusQuery.data?.exited ? (
                          <p className="flex items-center gap-1.5 text-xs text-[var(--text-muted)]">
                            <Loader2 className="h-3 w-3 animate-spin" /> Running (pid {execPid})…
                          </p>
                        ) : (
                          <>
                            <p className="text-xs text-[var(--text-muted)]">Exit code: {execStatusQuery.data.exitcode ?? 0}</p>
                            {execStatusQuery.data["out-data"] && (
                              <pre className="mt-1 max-h-40 overflow-auto whitespace-pre-wrap break-all font-mono text-xs">{execStatusQuery.data["out-data"]}</pre>
                            )}
                            {execStatusQuery.data["err-data"] && (
                              <pre className="mt-1 max-h-40 overflow-auto whitespace-pre-wrap break-all font-mono text-xs text-[var(--status-error)]">
                                {execStatusQuery.data["err-data"]}
                              </pre>
                            )}
                          </>
                        )}
                      </div>
                    )}
                  </div>

                  <div className="space-y-1.5 border-t border-[var(--border)] pt-4">
                    <Label>Set in-guest user password</Label>
                    <div className="flex gap-2">
                      <Input placeholder="username" value={agentPwUser} onChange={(e) => setAgentPwUser(e.target.value)} className="w-32" />
                      <Input
                        type="password"
                        placeholder="new password"
                        value={agentPwPass}
                        onChange={(e) => setAgentPwPass(e.target.value)}
                      />
                      <Button
                        size="sm"
                        disabled={!agentPwUser || !agentPwPass || agentSetPassword.isPending}
                        onClick={() => agentSetPassword.mutate()}
                      >
                        Set
                      </Button>
                    </div>
                  </div>
                </>
              )}
            </TabsContent>
          )}

          <TabsContent value="actions" className="space-y-4">
            <div className="flex gap-2">
              <Button size="sm" variant="secondary" disabled={openConsole.isPending} onClick={() => openConsole.mutate()}>
                {openConsole.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <SquareTerminal className="h-3.5 w-3.5" />}
                Open console
              </Button>
              <Button size="sm" variant="secondary" disabled={openShell.isPending} onClick={() => openShell.mutate()}>
                {openShell.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <SquareTerminal className="h-3.5 w-3.5" />}
                Open shell
              </Button>
            </div>
            <div className="space-y-1.5">
              <Label>Clone to new VMID</Label>
              <div className="flex gap-2">
                <Input placeholder="e.g. 105" value={cloneNewId} onChange={(e) => setCloneNewId(e.target.value)} />
                <Button size="sm" disabled={!cloneNewId || cloneGuest.isPending} onClick={() => cloneGuest.mutate()}>
                  <Copy className="h-3.5 w-3.5" /> Clone
                </Button>
              </div>
            </div>
            <div className="space-y-1.5">
              <Label>Grow disk</Label>
              <div className="flex gap-2">
                <Select value={resizeDisk} onValueChange={setResizeDisk}>
                  <SelectTrigger className="w-32"><SelectValue placeholder="disk…" /></SelectTrigger>
                  <SelectContent>
                    {diskKeys.map((k) => <SelectItem key={k} value={k}>{k}</SelectItem>)}
                  </SelectContent>
                </Select>
                <Input
                  placeholder="add GB, e.g. 10"
                  type="number"
                  min={1}
                  value={resizeAmount}
                  onChange={(e) => setResizeAmount(e.target.value)}
                />
                <Button
                  size="sm"
                  disabled={!resizeDisk || !resizeAmount || resizeDiskMutation.isPending}
                  onClick={() => resizeDiskMutation.mutate()}
                >
                  <HardDrive className="h-3.5 w-3.5" /> Grow
                </Button>
              </div>
              <p className="text-xs text-[var(--text-muted)]">Disks can only be grown, never shrunk, and only while the OS supports online resize.</p>
            </div>
            <div className="space-y-1.5">
              <Label>Move disk to another storage</Label>
              <div className="flex gap-2">
                <Select value={moveDisk} onValueChange={setMoveDisk}>
                  <SelectTrigger className="w-32"><SelectValue placeholder="disk…" /></SelectTrigger>
                  <SelectContent>
                    {diskKeys.map((k) => <SelectItem key={k} value={k}>{k}</SelectItem>)}
                  </SelectContent>
                </Select>
                <Select value={moveTargetStorage} onValueChange={setMoveTargetStorage}>
                  <SelectTrigger><SelectValue placeholder="target storage…" /></SelectTrigger>
                  <SelectContent>
                    {storages.map((s) => <SelectItem key={s} value={s}>{s}</SelectItem>)}
                  </SelectContent>
                </Select>
                <Button
                  size="sm"
                  disabled={!moveDisk || !moveTargetStorage || moveDiskMutation.isPending}
                  onClick={() => moveDiskMutation.mutate()}
                >
                  {moveDiskMutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <HardDrive className="h-3.5 w-3.5" />} Move
                </Button>
              </div>
              <label className="flex items-center gap-2 text-xs text-[var(--text-muted)]">
                <Switch checked={moveDeleteSource} onCheckedChange={setMoveDeleteSource} aria-label="Delete source disk after move" />
                Delete source disk once the move succeeds
              </label>
            </div>
            <div className="space-y-1.5">
              <Label>Migrate to node</Label>
              <div className="flex gap-2">
                <Select value={migrateTarget} onValueChange={setMigrateTarget}>
                  <SelectTrigger><SelectValue placeholder="target node…" /></SelectTrigger>
                  <SelectContent>
                    {otherNodes.map((n) => <SelectItem key={n.node} value={n.node!}>{n.node}</SelectItem>)}
                  </SelectContent>
                </Select>
                <Button size="sm" disabled={!migrateTarget || migrateGuest.isPending} onClick={() => migrateGuest.mutate()}>
                  <Workflow className="h-3.5 w-3.5" /> Migrate
                </Button>
              </div>
            </div>
            <div className="flex gap-2 border-t border-[var(--border)] pt-4">
              <Button size="sm" variant="secondary" onClick={() => void clearLock()}>
                <Lock className="h-3.5 w-3.5" /> Clear lock
              </Button>
              <Button
                size="sm"
                variant="destructive"
                loading={deleteGuest.isPending}
                onClick={() => removeGuest()}
              >
                {!deleteGuest.isPending && <Trash2 className="h-3.5 w-3.5" />} Delete guest
              </Button>
            </div>
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
  )
}

/** One labeled chart cell in the guest metrics tab — keeps the grid tidy and
 * gives every chart the same syncId so hover crosshairs align. */
function MetricChart({
  title,
  rows,
  series,
  yTickFormatter,
  yDomain,
  valueKind,
  showLegend,
}: {
  title: string
  rows: ChartRow[]
  series: SeriesSpec[]
  yTickFormatter?: (v: number) => string
  yDomain?: [number | "auto" | "dataMin", number | "auto" | "dataMax"]
  valueKind?: "bytes" | "rate"
  showLegend?: boolean
}) {
  return (
    <div>
      <p className="mb-1 text-xs font-medium text-[var(--text-muted)]">{title}</p>
      <ResourceAreaChart data={rows} series={series} yTickFormatter={yTickFormatter} yDomain={yDomain} valueKind={valueKind} showLegend={showLegend} syncId="guest-metrics" height={170} />
    </div>
  )
}

function LiveChip({ icon: Icon, label, value }: { icon: typeof Network; label: string; value: string }) {
  return (
    <div className="flex items-center gap-2 rounded-md border border-[var(--border)] bg-[var(--bg-muted)] px-2.5 py-1.5">
      <Icon className="h-3.5 w-3.5 shrink-0 text-[var(--text-muted)]" />
      <div className="min-w-0">
        <p className="text-[10px] leading-tight text-[var(--text-muted)]">{label}</p>
        <p className="truncate text-sm font-medium tabular">{value}</p>
      </div>
    </div>
  )
}
