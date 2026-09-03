import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import {
  ChevronRight,
  Loader2,
  Pause,
  Play,
  Plus,
  Power,
  RotateCcw,
  Server,
  SquareTerminal,
  Workflow,
  X,
} from "lucide-react"
import { useState } from "react"
import { Link } from "react-router-dom"
import { toast } from "sonner"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import { useConfirm } from "@/components/ui/confirm-dialog"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
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
import { TypeChip } from "@/components/ui/type-chip"
import { CreateGuestDialog } from "@/components/inventory/CreateGuestDialog"
import { GuestDetailDialog } from "@/components/inventory/GuestDetailDialog"
import { api, ApiError, type ClusterResource, type ConnectionInventory } from "@/lib/api"
import { buildConsoleUrl } from "@/lib/console"
import { cn, formatBytes, formatPercent, formatUptime, guestDotStatus } from "@/lib/utils"

export function InventoryPage() {
  const queryClient = useQueryClient()
  const confirm = useConfirm()
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [selectedGuest, setSelectedGuest] = useState<{ connId: string; guest: ClusterResource } | null>(null)
  const [createDialogConn, setCreateDialogConn] = useState<{ connId: string; nodes: ClusterResource[] } | null>(null)
  // Multi-select filters: an empty array means "all", so predicates stay a
  // plain Set lookup without a magic "all" value.
  const [statusTypes, setStatusTypes] = useState<string[]>([])
  const [guestTypes, setGuestTypes] = useState<string[]>([])
  const [poolFilter, setPoolFilter] = useState<string[]>([])
  const [search, setSearch] = useState("")
  const [selected, setSelected] = useState<Map<string, { connId: string; guest: ClusterResource }>>(new Map())
  const [migrateDialogOpen, setMigrateDialogOpen] = useState(false)
  const [migrateTarget, setMigrateTarget] = useState("")

  function toggleSelected(connId: string, guest: ClusterResource) {
    setSelected((prev) => {
      const key = `${connId}:${guest.id}`
      const next = new Map(prev)
      if (next.has(key)) next.delete(key)
      else next.set(key, { connId, guest })
      return next
    })
  }

  const { data, isLoading, isError, refetch } = useQuery({
    queryKey: ["inventory"],
    queryFn: () => api.get<ConnectionInventory[]>("/inventory/"),
    refetchInterval: 15_000,
  })

  const pools = Array.from(
    new Set((data ?? []).flatMap((c) => c.resources ?? []).map((r) => r.pool).filter((p): p is string => !!p)),
  ).sort()

  // Hard stop and reset interrupt work without graceful shutdown — they ask
  // first. Graceful start/shutdown/suspend don't (matches PVE's own list UX).
  const powerAction = useMutation({
    mutationFn: ({ connId, guest, action }: { connId: string; guest: ClusterResource; action: string }) =>
      api.post(`/connections/${connId}/guests/${guest.type}/${guest.node}/${guest.vmid}/power/${action}`),
    onSuccess: () => {
      toast.success("Action submitted")
      queryClient.invalidateQueries({ queryKey: ["inventory"] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Action failed"),
  })

  async function guestPower(connId: string, guest: ClusterResource, action: string) {
    if (action === "stop" || action === "reset") {
      const verb = action === "stop" ? "hard-stop" : "hard-reset"
      const ok = await confirm({
        title: `${verb.charAt(0).toUpperCase()}${verb.slice(1)} ${guest.name}?`,
        description:
          action === "stop"
            ? "The guest is killed immediately — unsaved data in the guest is lost. Use Shutdown for a clean stop."
            : "The guest is reset like pressing the hardware reset button — everything not saved is lost.",
        confirmLabel: `Hard-${action}`,
      })
      if (!ok) return
    }
    powerAction.mutate({ connId, guest, action })
  }

  const bulkPowerAction = useMutation({
    mutationFn: async (action: string) => {
      const targets = Array.from(selected.values())
      const results = await Promise.allSettled(
        targets.map(({ connId, guest }) =>
          api.post(`/connections/${connId}/guests/${guest.type}/${guest.node}/${guest.vmid}/power/${action}`),
        ),
      )
      const failed = results.filter((r) => r.status === "rejected").length
      return { total: targets.length, failed }
    },
    onSuccess: ({ total, failed }) => {
      if (failed === 0) toast.success(`${actionLabel(total)} submitted`)
      else toast.error(`${failed} of ${total} actions failed`)
      queryClient.invalidateQueries({ queryKey: ["inventory"] })
      setSelected(new Map())
    },
    onError: () => toast.error("Bulk action failed"),
  })

  async function bulkPower(action: string) {
    if (action === "stop" || action === "reset") {
      const ok = await confirm({
        title: `Bulk ${action} ${selected.size} guests?`,
        description:
          action === "stop"
            ? "Each selected guest is killed immediately — unsaved data inside the guests is lost."
            : "Each selected guest is hard-reset, like pressing the hardware reset button.",
        confirmLabel: action === "stop" ? "Hard-stop all" : "Hard-reset all",
      })
      if (!ok) return
    }
    bulkPowerAction.mutate(action)
  }

  function actionLabel(n: number) {
    return `${n} action${n === 1 ? "" : "s"}`
  }

  // Migrating a mixed-connection selection to one node name doesn't make
  // sense (each cluster has its own node set) — the action only lights up
  // when every selected guest belongs to the same connection.
  const selectedConnIds = new Set(Array.from(selected.values()).map((s) => s.connId))
  const bulkMigrateConnId = selectedConnIds.size === 1 ? [...selectedConnIds][0] : null
  const bulkMigrateNodes = (data ?? []).find((c) => c.connectionId === bulkMigrateConnId)?.resources?.filter((r) => r.type === "node") ?? []

  const bulkMigrateAction = useMutation({
    mutationFn: async (targetNode: string) => {
      const targets = Array.from(selected.values()).filter(({ guest }) => guest.node !== targetNode)
      const results = await Promise.allSettled(
        targets.map(({ connId, guest }) =>
          api.post(`/connections/${connId}/guests/${guest.type}/${guest.node}/${guest.vmid}/migrate`, {
            targetNode,
            online: guest.status === "running",
          }),
        ),
      )
      const failed = results.filter((r) => r.status === "rejected").length
      return { total: targets.length, failed }
    },
    onSuccess: ({ total, failed }) => {
      if (failed === 0) toast.success(`${actionLabel(total)} submitted`)
      else toast.error(`${failed} of ${total} migrations failed`)
      queryClient.invalidateQueries({ queryKey: ["inventory"] })
      setSelected(new Map())
      setMigrateTarget("")
    },
    onError: () => toast.error("Bulk migration failed"),
  })

  async function bulkMigrate() {
    if (!migrateTarget) return
    const ok = await confirm({
      title: `Migrate ${selected.size} guests to ${migrateTarget}?`,
      description: "Each guest already on the target node is skipped. Running guests migrate live; stopped guests migrate offline.",
      confirmLabel: "Migrate all",
    })
    if (!ok) return
    setMigrateDialogOpen(false)
    bulkMigrateAction.mutate(migrateTarget)
  }

  const openConsole = useMutation({
    mutationFn: async ({ connId, guest }: { connId: string; guest: ClusterResource }) => {
      const res = await api.post<{ wsPath: string; password?: string }>(`/connections/${connId}/guests/${guest.type}/${guest.node}/${guest.vmid}/console`)
      return { connId, guest, wsPath: res.wsPath, password: res.password }
    },
    onSuccess: ({ connId, guest, wsPath, password }) => {
      window.open(buildConsoleUrl(connId, guest, wsPath, password), "_blank", "width=1024,height=768")
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to open console"),
  })

  function toggle(key: string) {
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  const allGuests = (data ?? []).flatMap((c) => c.resources ?? []).filter((r) => r.type === "qemu" || r.type === "lxc")

  return (
    <div className="space-y-4">
      <PageHeader
        title="Inventory"
        description="Nodes, VMs, and containers across every connection."
        icon={Server}
      />

      <div className="flex flex-wrap items-center gap-2">
        <Input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Search guests by name or VMID..."
          className="max-w-xs"
        />
        <MultiSelect
          options={[
            { value: "running", label: "Running" },
            { value: "stopped", label: "Stopped" },
            { value: "paused", label: "Paused" },
          ]}
          selected={statusTypes}
          onChange={setStatusTypes}
          allLabel="All statuses"
          label="Filter by status"
          className="w-40"
        />
        <MultiSelect
          options={[
            { value: "qemu", label: "VMs" },
            { value: "lxc", label: "Containers" },
          ]}
          selected={guestTypes}
          onChange={setGuestTypes}
          allLabel="VMs & Containers"
          label="Filter by type"
          className="w-44"
        />
        {pools.length > 0 && (
          <MultiSelect
            options={pools.map((p) => ({ value: p, label: p }))}
            selected={poolFilter}
            onChange={setPoolFilter}
            allLabel="All pools"
            label="Filter by pool"
            className="w-40"
          />
        )}
        <p className="ml-auto text-xs text-[var(--text-faint)] tabular" aria-live="polite">
          {isLoading ? "Loading…" : `${allGuests.length} guest${allGuests.length === 1 ? "" : "s"} on ${(data ?? []).length} connection${(data ?? []).length === 1 ? "" : "s"}`}
        </p>
      </div>

      {selected.size > 0 && (
        <div className="flex flex-wrap items-center gap-2.5 rounded-lg border border-[color-mix(in_oklab,var(--color-brand-500)_35%,var(--border))] bg-[color-mix(in_oklab,var(--color-brand-500)_8%,var(--bg-surface))] px-4 py-2.5">
          <span className="text-sm font-semibold text-brand-600 dark:text-brand-400">{selected.size} selected</span>
          <Button size="sm" variant="secondary" disabled={bulkPowerAction.isPending} onClick={() => bulkPower("start")}>
            <Play className="h-3.5 w-3.5" /> Start
          </Button>
          <Button size="sm" variant="secondary" disabled={bulkPowerAction.isPending} onClick={() => bulkPower("shutdown")}>
            <Power className="h-3.5 w-3.5" /> Shutdown
          </Button>
          <Button size="sm" variant="secondary" disabled={bulkPowerAction.isPending} onClick={() => bulkPower("reset")}>
            <RotateCcw className="h-3.5 w-3.5" /> Reset
          </Button>
          <Hint label={bulkMigrateConnId ? "Move every selected guest to one node" : "Select guests from a single connection to migrate them together"}>
            <span>
              <Button size="sm" variant="secondary" disabled={!bulkMigrateConnId || bulkMigrateAction.isPending} onClick={() => setMigrateDialogOpen(true)}>
                <Workflow className="h-3.5 w-3.5" /> Migrate
              </Button>
            </span>
          </Hint>
          {(bulkPowerAction.isPending || bulkMigrateAction.isPending) && <Loader2 className="h-4 w-4 animate-spin text-[var(--text-muted)]" />}
          <Button size="sm" variant="ghost" className="ml-auto" onClick={() => setSelected(new Map())}>
            <X className="h-3.5 w-3.5" /> Clear
          </Button>
        </div>
      )}

      {isError && (
        <ErrorState
          title="Couldn't load your inventory"
          message="Guests and nodes could not be fetched. Check your connections and try again."
          onRetry={refetch}
        />
      )}

      {!isError && isLoading && (
        <div className="space-y-3" aria-busy>
          {[0, 1].map((i) => (
            <Skeleton key={i} className="h-[4.5rem]" />
          ))}
        </div>
      )}

      {!isError && !isLoading && (data ?? []).length === 0 && (
        <EmptyState
          icon={Server}
          title="No connections configured yet"
          description="Add a Proxmox host or cluster to start managing your fleet from here."
          action={
            <Link
              to="/connections"
              className="inline-flex h-9 items-center justify-center gap-2 rounded-md bg-brand-600 px-4 text-sm font-medium text-white shadow-xs transition-colors hover:bg-brand-700"
            >
              <Plus className="h-4 w-4" /> Add connection
            </Link>
          }
        />
      )}

      <div className="space-y-3">
        {!isError && data?.map((conn) => {
          const nodes = (conn.resources ?? []).filter((r) => r.type === "node")
          const guestsByNode = (conn.resources ?? []).filter((r) => {
            if (r.type !== "qemu" && r.type !== "lxc") return false
            if (statusTypes.length > 0 && !statusTypes.includes(r.status ?? "")) return false
            if (guestTypes.length > 0 && !guestTypes.includes(r.type)) return false
            if (poolFilter.length > 0 && !(r.pool && poolFilter.includes(r.pool))) return false
            if (search && !`${r.name ?? ""} ${r.vmid ?? ""}`.toLowerCase().includes(search.toLowerCase())) return false
            return true
          })
          const connGuestCount = (conn.resources ?? []).filter((r) => r.type === "qemu" || r.type === "lxc").length

          return (
            <Card key={conn.connectionId}>
              <div className="flex w-full items-center gap-2 p-4">
                <button
                  onClick={() => toggle(conn.connectionId)}
                  className="flex min-w-0 flex-1 items-center gap-2 text-left"
                  aria-expanded={expanded.has(conn.connectionId)}
                >
                  <ChevronRight
                    className={cn("h-4 w-4 shrink-0 transition-transform", expanded.has(conn.connectionId) && "rotate-90")}
                  />
                  <Server className="h-4 w-4 shrink-0 text-brand-500" />
                  <span className="truncate font-medium">{conn.name}</span>
                  <span className="shrink-0 text-xs text-[var(--text-faint)] tabular">
                    {nodes.length} node{nodes.length === 1 ? "" : "s"} · {connGuestCount} guest{connGuestCount === 1 ? "" : "s"}
                  </span>
                </button>
                <span className="flex shrink-0 items-center gap-1.5 text-xs" title={conn.online ? "Online" : conn.error ?? "Offline"}>
                  <StatusDot status={conn.online ? "ok" : "error"} />
                  {!conn.online && <span className="max-w-48 truncate text-[var(--status-error)]">{conn.error ?? "Offline"}</span>}
                </span>
                {conn.online && (
                  <Button
                    size="sm"
                    variant="secondary"
                    onClick={() => setCreateDialogConn({ connId: conn.connectionId, nodes })}
                  >
                    <Plus className="h-3.5 w-3.5" /> Create guest
                  </Button>
                )}
              </div>

              {expanded.has(conn.connectionId) && (
                <CardContent className="space-y-1 pt-0">
                  {nodes.map((node) => {
                    const nodeKey = `${conn.connectionId}/${node.node}`
                    const guests = guestsByNode.filter((g) => g.node === node.node)
                    return (
                      <div key={nodeKey}>
                        <div className="flex w-full flex-wrap items-center gap-x-2 gap-y-1 rounded-md px-2 py-1.5 hover:bg-[var(--bg-surface-hover)]">
                          <button onClick={() => toggle(nodeKey)} className="flex min-w-0 flex-1 items-center gap-2 text-left" aria-expanded={expanded.has(nodeKey)}>
                            <ChevronRight
                              className={cn("h-3.5 w-3.5 shrink-0 transition-transform", expanded.has(nodeKey) && "rotate-90")}
                            />
                            <Server className="h-3.5 w-3.5 shrink-0 text-[var(--text-muted)]" />
                            <span className="truncate text-sm font-medium hover:underline">
                              <Link to={`/nodes/${conn.connectionId}/${node.node}`} onClick={(e) => e.stopPropagation()}>
                                {node.node}
                              </Link>
                            </span>
                            <span
                              className="flex shrink-0 items-center"
                              title={node.status === "online" ? "Node online" : node.status === "offline" ? "Node offline" : `Node status: ${node.status ?? "unknown"}`}
                            >
                              <StatusDot status={node.status === "online" ? "ok" : node.status === "offline" ? "error" : "warn"} />
                            </span>
                          </button>
                          <span className="flex shrink-0 flex-wrap gap-3 text-xs text-[var(--text-muted)] tabular">
                            <span>CPU {formatPercent(node.cpu ?? 0)}</span>
                            <span>Mem {formatBytes(node.mem ?? 0)} / {formatBytes(node.maxmem ?? 0)}</span>
                            <span>{formatUptime(node.uptime ?? 0)}</span>
                          </span>
                        </div>

                        {expanded.has(nodeKey) && (
                          <div className="ml-8 space-y-0.5 border-l border-[var(--border)] pl-3">
                            {guests.length === 0 && (
                              <p className="py-1 text-xs text-[var(--text-muted)]">No guests on this node.</p>
                            )}
                            {guests.map((guest) => (
                              <div
                                key={guest.id}
                                className="flex flex-wrap items-center gap-x-2 gap-y-1 rounded-md px-2 py-1.5 hover:bg-[var(--bg-surface-hover)]"
                              >
                                <Checkbox
                                  checked={selected.has(`${conn.connectionId}:${guest.id}`)}
                                  onCheckedChange={() => toggleSelected(conn.connectionId, guest)}
                                  aria-label={`Select ${guest.name}`}
                                />
                                <div className="flex min-w-0 flex-1 basis-40 items-center gap-2">
                                  {/* Status leads the row: a dot, not a text chip —
                                      the type chip says VM vs LXC, the name carries
                                      the identity, and HA/tags stay on the right. */}
                                  <span
                                    className="flex shrink-0 items-center"
                                    title={`Status: ${guest.status ?? "unknown"}`}
                                  >
                                    <StatusDot status={guestDotStatus(guest.status)} />
                                  </span>
                                  <TypeChip type={guest.type} />
                                  <button
                                    className="min-w-0 truncate text-sm hover:underline"
                                    onClick={() => setSelectedGuest({ connId: conn.connectionId, guest })}
                                  >
                                    {guest.name}
                                  </button>
                                  <span className="shrink-0 font-mono text-xs text-[var(--text-muted)] tabular">#{guest.vmid}</span>
                                </div>
                                <div className="flex shrink-0 flex-wrap items-center gap-1">
                                  {guest.hastate && (
                                    <Badge variant={guest.hastate === "started" ? "ok" : guest.hastate === "error" || guest.hastate === "fence" ? "error" : "default"}>
                                      HA: {guest.hastate}
                                    </Badge>
                                  )}
                                  {guest.tags && (
                                    <span className="flex flex-wrap items-center gap-1" aria-label={`Tags: ${guest.tags.split(/[,;]/).filter(Boolean).join(", ")}`}>
                                      {guest.tags.split(/[,;]/).filter(Boolean).map((t) => (
                                        <Badge key={t} variant="default" aria-hidden>{t}</Badge>
                                      ))}
                                    </span>
                                  )}
                                </div>
                                {guest.status === "running" && (
                                  <span className="hidden shrink-0 items-center gap-3 text-xs text-[var(--text-muted)] tabular xl:flex">
                                    <span>CPU {formatPercent(guest.cpu ?? 0)}</span>
                                    <span>Mem {formatBytes(guest.mem ?? 0)} / {formatBytes(guest.maxmem ?? 0)}</span>
                                    {(guest.maxdisk ?? 0) > 0 && (
                                      <span>Disk {formatBytes(guest.disk ?? 0)} / {formatBytes(guest.maxdisk ?? 0)}</span>
                                    )}
                                    <span>{formatUptime(guest.uptime ?? 0)}</span>
                                  </span>
                                )}
                                <span className="ml-auto flex shrink-0 items-center gap-1">
                                  {/* Direct console access — one click, no menu.
                                      Running guests only; a stopped VM/container
                                      has nothing to attach to. */}
                                  {guest.status === "running" && (
                                    <Hint label="Open console">
                                      <Button
                                        size="icon-sm"
                                        variant="ghost"
                                        aria-label={`Open console for ${guest.name}`}
                                        disabled={openConsole.isPending}
                                        onClick={() => openConsole.mutate({ connId: conn.connectionId, guest })}
                                      >
                                        <SquareTerminal className="h-3.5 w-3.5" />
                                      </Button>
                                    </Hint>
                                  )}
                                  <DropdownMenu>
                                    <Hint label="Power & console">
                                      <DropdownMenuTrigger asChild>
                                        <Button
                                          size="icon-sm"
                                          variant="ghost"
                                          aria-label={`Actions for ${guest.name}`}
                                          disabled={powerAction.isPending}
                                        >
                                          <Power className="h-3.5 w-3.5" />
                                        </Button>
                                      </DropdownMenuTrigger>
                                    </Hint>
                                    <DropdownMenuContent align="end" className="w-44">
                                      <DropdownMenuLabel>{guest.name}</DropdownMenuLabel>
                                      <DropdownMenuSeparator />
                                      {guest.status !== "running" ? (
                                        <DropdownMenuItem onSelect={() => guestPower(conn.connectionId, guest, "start")}>
                                          <Play className="h-3.5 w-3.5 text-[var(--status-ok)]" /> Start
                                        </DropdownMenuItem>
                                      ) : (
                                        <>
                                          <DropdownMenuItem onSelect={() => openConsole.mutate({ connId: conn.connectionId, guest })}>
                                            <SquareTerminal className="h-3.5 w-3.5 text-[var(--text-muted)]" /> Console
                                          </DropdownMenuItem>
                                          <DropdownMenuSeparator />
                                          <DropdownMenuItem onSelect={() => guestPower(conn.connectionId, guest, "suspend")}>
                                            <Pause className="h-3.5 w-3.5 text-[var(--text-muted)]" /> Suspend
                                          </DropdownMenuItem>
                                          <DropdownMenuItem onSelect={() => guestPower(conn.connectionId, guest, "shutdown")}>
                                            <Power className="h-3.5 w-3.5 text-[var(--status-warn)]" /> Shutdown
                                          </DropdownMenuItem>
                                          <DropdownMenuItem onSelect={() => guestPower(conn.connectionId, guest, "reset")}>
                                            <RotateCcw className="h-3.5 w-3.5 text-[var(--status-warn)]" /> Reset…
                                          </DropdownMenuItem>
                                          <DropdownMenuItem
                                            onSelect={() => guestPower(conn.connectionId, guest, "stop")}
                                            className="text-[var(--status-error)] data-[highlighted]:bg-[color-mix(in_oklab,var(--status-error)_10%,transparent)]"
                                          >
                                            <X className="h-3.5 w-3.5" /> Hard stop…
                                          </DropdownMenuItem>
                                        </>
                                      )}
                                    </DropdownMenuContent>
                                  </DropdownMenu>
                                </span>
                              </div>
                            ))}
                          </div>
                        )}
                      </div>
                    )
                  })}
                </CardContent>
              )}
            </Card>
          )
        })}
      </div>

      <GuestDetailDialog
        connId={selectedGuest?.connId ?? ""}
        guest={selectedGuest?.guest ?? null}
        onOpenChange={(open) => !open && setSelectedGuest(null)}
      />

      {createDialogConn && (
        <CreateGuestDialog
          connId={createDialogConn.connId}
          nodes={createDialogConn.nodes}
          open={!!createDialogConn}
          onOpenChange={(open) => !open && setCreateDialogConn(null)}
        />
      )}

      <Dialog open={migrateDialogOpen} onOpenChange={setMigrateDialogOpen}>
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle>Migrate {selected.size} guests</DialogTitle>
            <DialogDescription>Every selected guest moves to the node you pick — one already there is left alone.</DialogDescription>
          </DialogHeader>
          <div className="space-y-1.5">
            <Label>Target node</Label>
            <Select value={migrateTarget} onValueChange={setMigrateTarget}>
              <SelectTrigger>
                <SelectValue placeholder="Select a node..." />
              </SelectTrigger>
              <SelectContent>
                {bulkMigrateNodes.map((n) => (
                  <SelectItem key={n.node} value={n.node!}>
                    {n.node}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <DialogFooter>
            <Button disabled={!migrateTarget || bulkMigrateAction.isPending} onClick={() => void bulkMigrate()}>
              Migrate
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
