import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import {
  ChevronRight,
  Loader2,
  Pause,
  Play,
  Plus,
  Power,
  RefreshCw,
  RotateCcw,
  Server,
  SquareTerminal,
  Workflow,
  X,
} from "lucide-react"
import { useDeferredValue, useEffect, useMemo, useRef, useState } from "react"
import { Link, useLocation, useSearchParams } from "react-router-dom"
import { toast } from "sonner"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import { Combobox } from "@/components/ui/combobox"
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
import { Label } from "@/components/ui/label"
import { ListSearch } from "@/components/ui/list-search"
import { MultiSelect } from "@/components/ui/multi-select"
import { PageHeader } from "@/components/ui/page-header"
import { Skeleton } from "@/components/ui/skeleton"
import { StatusDot } from "@/components/ui/status-dot"
import { Hint } from "@/components/ui/tooltip"
import { TypeChip } from "@/components/ui/type-chip"
import { CreateGuestDialog } from "@/components/inventory/CreateGuestDialog"
import { GuestDetailDialog } from "@/components/inventory/GuestDetailDialog"
import { api, ApiError, type ClusterResource, type ConnectionInventory } from "@/lib/api"
import { buildConsoleUrl, openConsolePopup } from "@/lib/console"
import { cn, formatBytes, formatPercent, formatUptime, guestDotStatus } from "@/lib/utils"

const EXPANDED_KEY = "ferrum:inventory-expanded"

export function InventoryPage() {
  const queryClient = useQueryClient()
  const confirm = useConfirm()
  const location = useLocation()
  const focusState = location.state as { focusGuestId?: string; focusGuestName?: string } | null
  // Which connection/node groups are open. Persisted so the tree comes back
  // the way the operator left it instead of fully collapsed on every visit;
  // with nothing stored yet, the first data load expands everything for a
  // small fleet (see the effect below) so guests are visible without clicks.
  const [expanded, setExpanded] = useState<Set<string> | null>(() => {
    try {
      const raw = localStorage.getItem(EXPANDED_KEY)
      return raw ? new Set<string>(JSON.parse(raw) as string[]) : null
    } catch {
      return null
    }
  })
  // A node with hundreds of guests renders every row unvirtualized once
  // expanded — fine for the common case, sluggish at the tail. Cap the
  // unfiltered render and let the operator opt into the rest per node
  // instead of adding a virtualization dependency for a rare case; the
  // moment a filter is active the list is already narrowed, so the cap
  // only applies when nothing is filtering it down.
  const [revealedNodes, setRevealedNodes] = useState<Set<string>>(new Set())
  const GUEST_RENDER_CAP = 150
  const [selectedGuest, setSelectedGuest] = useState<{ connId: string; guest: ClusterResource } | null>(null)
  const [createDialogConn, setCreateDialogConn] = useState<{ connId: string; nodes: ClusterResource[] } | null>(null)
  // Filters live in the URL (not just component state) so a search/filter
  // stays put across a drill-in-and-back navigation, and so a filtered view
  // is shareable/bookmarkable — useful for pointing a teammate at "these
  // guests" during an incident.
  const [searchParams, setSearchParams] = useSearchParams()
  // Multi-select filters: an empty array means "all", so predicates stay a
  // plain Set lookup without a magic "all" value.
  const [statusTypes, setStatusTypes] = useState<string[]>(() => searchParams.get("status")?.split(",").filter(Boolean) ?? [])
  const [guestTypes, setGuestTypes] = useState<string[]>(() => searchParams.get("type")?.split(",").filter(Boolean) ?? [])
  const [poolFilter, setPoolFilter] = useState<string[]>(() => searchParams.get("pool")?.split(",").filter(Boolean) ?? [])
  const [search, setSearch] = useState(focusState?.focusGuestName ?? searchParams.get("q") ?? "")

  useEffect(() => {
    const next = new URLSearchParams(searchParams)
    if (search) next.set("q", search)
    else next.delete("q")
    if (statusTypes.length) next.set("status", statusTypes.join(","))
    else next.delete("status")
    if (guestTypes.length) next.set("type", guestTypes.join(","))
    else next.delete("type")
    if (poolFilter.length) next.set("pool", poolFilter.join(","))
    else next.delete("pool")
    setSearchParams(next, { replace: true })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [search, statusTypes, guestTypes, poolFilter])
  const [selected, setSelected] = useState<Map<string, { connId: string; guest: ClusterResource }>>(new Map())
  const [migrateDialogOpen, setMigrateDialogOpen] = useState(false)
  const [migrateTarget, setMigrateTarget] = useState("")
  const [highlightId, setHighlightId] = useState<string | null>(focusState?.focusGuestId ?? null)
  const guestRefs = useRef<Map<string, HTMLDivElement>>(new Map())

  // Deep-link scopes (set by guestUrl() — Overview hot-guests, Task Center,
  // alerts, topology): `conn` narrows the tree to one connection, and
  // `focusGuest` (a VMID or name) auto-opens that guest's detail dialog on
  // arrival — the shareable "show me this VM" URL.
  const connFilter = searchParams.get("conn") ?? ""
  const focusGuestParam = searchParams.get("focusGuest")
  const autoOpenedRef = useRef(false)

  function toggleSelected(connId: string, guest: ClusterResource) {
    setSelected((prev) => {
      const key = `${connId}:${guest.id}`
      const next = new Map(prev)
      if (next.has(key)) next.delete(key)
      else next.set(key, { connId, guest })
      return next
    })
  }

  const { data, isLoading, isError, refetch, isRefetching } = useQuery({
    queryKey: ["inventory"],
    queryFn: () => api.get<ConnectionInventory[]>("/inventory/"),
    refetchInterval: 15_000,
  })

  // The `conn` deep-link scope narrows the tree to one connection (matched
  // by id or name) without touching the shared inventory cache. A scope that
  // matches nothing (connection deleted, or link predates a re-add) falls
  // back to the whole fleet — an empty page helps no one.
  const connections = useMemo(() => {
    const all = data ?? []
    if (!connFilter) return all
    const matched = all.filter((c) => c.connectionId === connFilter || c.name === connFilter)
    return matched.length > 0 ? matched : all
  }, [data, connFilter])
  const scopedConn = connFilter
    ? (data ?? []).find((c) => c.connectionId === connFilter || c.name === connFilter)
    : undefined

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

  // First 5 names + overflow count — enough to catch "wait, that's not who I
  // meant to select" without turning the confirm into a scrollable list.
  function selectedNameList() {
    const names = Array.from(selected.values()).map(({ guest }) => guest.name)
    return names.length <= 5 ? names.join(", ") : `${names.slice(0, 5).join(", ")} and ${names.length - 5} more`
  }

  async function bulkPower(action: string) {
    if (action === "stop" || action === "reset") {
      const ok = await confirm({
        title: `Bulk ${action} ${selected.size} guests?`,
        description: (
          <>
            {action === "stop"
              ? "Each selected guest is killed immediately — unsaved data inside the guests is lost."
              : "Each selected guest is hard-reset, like pressing the hardware reset button."}
            <br />
            <span className="mt-1.5 block text-[var(--text-muted)]">{selectedNameList()}</span>
          </>
        ),
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
      // The single-use ticket travels over postMessage, not the popup URL.
      openConsolePopup(buildConsoleUrl(connId, guest), "width=1024,height=768", { wsPath, password })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to open console"),
  })

  function toggle(key: string) {
    setExpanded((prev) => {
      const next = new Set(prev ?? [])
      if (next.has(key)) next.delete(key)
      else next.add(key)
      try {
        localStorage.setItem(EXPANDED_KEY, JSON.stringify([...next]))
      } catch {
        // storage blocked — the toggle still works for this visit
      }
      return next
    })
  }

  const allGuests = connections.flatMap((c) => c.resources ?? []).filter((r) => r.type === "qemu" || r.type === "lxc")

  // First visit (nothing persisted): open every connection, and every node
  // too while the fleet is small enough to read in one screen. Larger fleets
  // get connections only, so the page isn't a wall of hundreds of rows.
  useEffect(() => {
    if (expanded !== null || !data) return
    const keys = new Set<string>()
    for (const conn of data) {
      keys.add(conn.connectionId)
      if (allGuests.length <= GUEST_RENDER_CAP) {
        for (const r of conn.resources ?? []) if (r.type === "node") keys.add(`${conn.connectionId}/${r.node}`)
      }
    }
    setExpanded(keys)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [data, expanded])

  // Typing into the search box re-filters every guest on every keystroke;
  // deferring just the value used for matching (not the input's own value,
  // which must stay immediate) lets React keep the input responsive on a
  // large fleet instead of blocking a keystroke behind a full re-filter.
  const deferredSearch = useDeferredValue(search)

  // A search/filter match hidden inside a collapsed connection or node group
  // is effectively invisible — the classic "search doesn't find what's
  // right there" complaint. Whenever a filter is active, force-expand any
  // group that contains at least one match; clearing the filter restores
  // whatever the user had manually expanded/collapsed.
  const filtersActive = !!(search || statusTypes.length || guestTypes.length || poolFilter.length)
  function guestMatches(r: ClusterResource) {
    if (statusTypes.length > 0 && !statusTypes.includes(r.status ?? "")) return false
    if (guestTypes.length > 0 && !guestTypes.includes(r.type)) return false
    if (poolFilter.length > 0 && !(r.pool && poolFilter.includes(r.pool))) return false
    if (deferredSearch && !`${r.name ?? ""} ${r.vmid ?? ""}`.toLowerCase().includes(deferredSearch.toLowerCase())) return false
    return true
  }

  // Jump-to-guest from the command palette: expand the guest's connection
  // and node, scroll it into view, and pulse-highlight it briefly so it's
  // unmistakable even in a long list.
  useEffect(() => {
    if (!highlightId || !data) return
    for (const conn of data) {
      const guest = (conn.resources ?? []).find((r) => r.id === highlightId)
      if (guest) {
        setExpanded((prev) => new Set(prev ?? []).add(conn.connectionId).add(`${conn.connectionId}/${guest.node}`))
        const el = guestRefs.current.get(highlightId)
        el?.scrollIntoView({ behavior: "smooth", block: "center" })
        break
      }
    }
    const t = setTimeout(() => setHighlightId(null), 2500)
    return () => clearTimeout(t)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [highlightId, data])

  // Deep-link arrival (?conn=…&focusGuest=…): once the inventory is here,
  // find the guest, expand its groups, highlight it, and open its detail
  // dialog. Runs once per mount — the param is stripped afterwards so a
  // refresh doesn't fight the user closing the dialog.
  useEffect(() => {
    if (!focusGuestParam || !data || autoOpenedRef.current) return
    for (const conn of data) {
      if (connFilter && conn.connectionId !== connFilter && conn.name !== connFilter) continue
      const guest = (conn.resources ?? []).find(
        (r) => (r.type === "qemu" || r.type === "lxc") && (String(r.vmid) === focusGuestParam || r.name === focusGuestParam),
      )
      if (guest) {
        autoOpenedRef.current = true
        setExpanded((prev) => new Set(prev ?? []).add(conn.connectionId).add(`${conn.connectionId}/${guest.node}`))
        setHighlightId(guest.id)
        setSelectedGuest({ connId: conn.connectionId, guest })
        const next = new URLSearchParams(searchParams)
        next.delete("focusGuest")
        setSearchParams(next, { replace: true })
        break
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [data, focusGuestParam, connFilter])

  return (
    <div className="space-y-4">
      <PageHeader
        title="Inventory"
        description="Nodes, VMs, and containers across every connection."
        icon={Server}
        onRefresh={() => void queryClient.invalidateQueries({ queryKey: ["inventory"] })}
        refreshing={isRefetching}
      />

      <div className="flex flex-wrap items-center gap-2">
        {/* ListSearch, not a bare Input: same leading glyph + clear button as
            every other list search in the app (its own doc lists Inventory as
            a consumer — this makes that true). */}
        <ListSearch
          value={search}
          onChange={setSearch}
          placeholder="Search guests by name or VMID..."
          containerClassName="w-full sm:max-w-xs"
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
        {scopedConn && (
          <button
            onClick={() => {
              const next = new URLSearchParams(searchParams)
              next.delete("conn")
              setSearchParams(next, { replace: true })
            }}
            className="flex h-8 items-center gap-1.5 rounded-md border border-[color-mix(in_oklab,var(--color-brand-500)_40%,var(--border))] bg-[color-mix(in_oklab,var(--color-brand-500)_10%,var(--bg-surface))] px-2.5 text-xs font-medium text-brand-600 dark:text-brand-400"
            aria-label={`Show only ${scopedConn.name} — click to clear`}
          >
            <Server className="h-3 w-3" /> {scopedConn.name} <X className="h-3 w-3" aria-hidden />
          </button>
        )}
        <p className="ml-auto text-xs text-[var(--text-faint)] tabular" aria-live="polite">
          {isLoading
            ? "Loading…"
            : `${allGuests.length} guest${allGuests.length === 1 ? "" : "s"} on ${connections.length} connection${connections.length === 1 ? "" : "s"}`}
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

      {!isError && !isLoading && connections.length === 0 && (
        <EmptyState
          icon={Server}
          title="No connections configured yet"
          description="Add a Proxmox host or cluster to start managing your fleet from here."
          action={
            <Link to="/connections">
              <Button>
                <Plus className="h-4 w-4" /> Add connection
              </Button>
            </Link>
          }
        />
      )}

      <div className="space-y-3">
        {!isError && connections.map((conn) => {
          const nodes = (conn.resources ?? []).filter((r) => r.type === "node")
          const guestsByNode = (conn.resources ?? []).filter((r) => (r.type === "qemu" || r.type === "lxc") && guestMatches(r))
          const connGuestCount = (conn.resources ?? []).filter((r) => r.type === "qemu" || r.type === "lxc").length
          const connHasMatch = filtersActive && guestsByNode.length > 0
          const connExpanded = (expanded?.has(conn.connectionId) ?? false) || connHasMatch

          return (
            <Card key={conn.connectionId}>
              <div className="flex w-full items-center gap-2 p-4">
                <button
                  onClick={() => toggle(conn.connectionId)}
                  className="flex min-w-0 flex-1 items-center gap-2 text-left"
                  aria-expanded={connExpanded}
                >
                  <ChevronRight
                    className={cn("h-4 w-4 shrink-0 transition-transform", connExpanded && "rotate-90")}
                  />
                  <Server className="h-4 w-4 shrink-0 text-brand-500" />
                  <span className="truncate font-medium">{conn.name}</span>
                  {/* Secondary on phones: the fixed-width count used to win the
                      space fight and squeeze the connection name to nothing. */}
                  <span className="hidden shrink-0 text-xs text-[var(--text-faint)] tabular sm:inline">
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
                    aria-label={`Create guest on ${conn.name}`}
                  >
                    <Plus className="h-3.5 w-3.5" /> <span className="hidden sm:inline">Create guest</span>
                  </Button>
                )}
              </div>

              {connExpanded && (
                <CardContent className="space-y-1 pt-0">
                  {nodes.map((node) => {
                    const nodeKey = `${conn.connectionId}/${node.node}`
                    const guests = guestsByNode.filter((g) => g.node === node.node)
                    const nodeExpanded = (expanded?.has(nodeKey) ?? false) || (filtersActive && guests.length > 0)
                    const guestsCapped = !filtersActive && !revealedNodes.has(nodeKey) && guests.length > GUEST_RENDER_CAP
                    const visibleGuests = guestsCapped ? guests.slice(0, GUEST_RENDER_CAP) : guests
                    return (
                      <div key={nodeKey}>
                        <div className="inv-row flex w-full flex-wrap items-center gap-x-2 gap-y-1 rounded-md px-2 py-1.5 hover:bg-[var(--bg-surface-hover)]">
                          {/* Two separate controls, not a link inside a button
                              (invalid nesting that screen readers announce as one
                              ambiguous control): the toggle owns the chevron/icon
                              and the empty space, the link owns the name. */}
                          <div className="flex min-w-0 flex-1 items-center gap-2">
                            <button
                              onClick={() => toggle(nodeKey)}
                              className="flex shrink-0 items-center gap-2 rounded-sm"
                              aria-expanded={nodeExpanded}
                              aria-label={`${nodeExpanded ? "Collapse" : "Expand"} ${node.node}`}
                            >
                              <ChevronRight
                                className={cn("h-3.5 w-3.5 shrink-0 transition-transform", nodeExpanded && "rotate-90")}
                              />
                              <Server className="h-3.5 w-3.5 shrink-0 text-[var(--text-muted)]" />
                            </button>
                            <Link
                              to={`/nodes/${conn.connectionId}/${node.node}`}
                              className="min-w-0 truncate text-sm font-medium underline-offset-2 hover:underline"
                              title={`Open ${node.node}`}
                            >
                              {node.node}
                            </Link>
                            <span
                              className="flex shrink-0 items-center"
                              title={node.status === "online" ? "Node online" : node.status === "offline" ? "Node offline" : `Node status: ${node.status ?? "unknown"}`}
                            >
                              <StatusDot status={node.status === "online" ? "ok" : node.status === "offline" ? "error" : "warn"} />
                            </span>
                            <button onClick={() => toggle(nodeKey)} className="h-5 min-w-0 flex-1 cursor-pointer" tabIndex={-1} aria-hidden />
                          </div>
                          {/* On phones the stats drop to their own line under
                              the name rather than crushing it to a sliver. */}
                          <span className="flex basis-full shrink-0 flex-wrap gap-3 pl-7 text-xs text-[var(--text-muted)] tabular sm:basis-auto sm:pl-0">
                            <span>CPU {formatPercent(node.cpu ?? 0)}</span>
                            <span>Mem {formatBytes(node.mem ?? 0)} / {formatBytes(node.maxmem ?? 0)}</span>
                            <span>{formatUptime(node.uptime ?? 0)}</span>
                          </span>
                        </div>

                        {nodeExpanded && (
                          <div className="ml-8 space-y-0.5 border-l border-[var(--border)] pl-3">
                            {guests.length === 0 && (
                              <p className="py-1 text-xs text-[var(--text-muted)]">No guests on this node.</p>
                            )}
                            {visibleGuests.map((guest) => (
                              <div
                                key={guest.id}
                                ref={(el) => {
                                  if (el) guestRefs.current.set(guest.id, el)
                                  else guestRefs.current.delete(guest.id)
                                }}
                                className={cn(
                                  "inv-row flex flex-wrap items-center gap-x-2 gap-y-1 rounded-md px-2 py-1.5 transition-colors duration-500 hover:bg-[var(--bg-surface-hover)]",
                                  highlightId === guest.id && "bg-[color-mix(in_oklab,var(--color-brand-500)_16%,transparent)] ring-1 ring-[var(--ring)]",
                                )}
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
                                  {guest.tags && (() => {
                                    const tags = guest.tags.split(/[,;]/).filter(Boolean)
                                    const shown = tags.slice(0, 3)
                                    const overflow = tags.length - shown.length
                                    return (
                                      <span role="group" className="flex flex-wrap items-center gap-1" aria-label={`Tags: ${tags.join(", ")}`}>
                                        {shown.map((t) => (
                                          <Badge key={t} variant="outline" aria-hidden>{t}</Badge>
                                        ))}
                                        {overflow > 0 && <Badge variant="outline" aria-hidden>+{overflow}</Badge>}
                                      </span>
                                    )
                                  })()}
                                </div>
                                {guest.status === "running" && (
                                  <span className="hidden shrink-0 items-center gap-3 text-xs text-[var(--text-muted)] tabular md:flex">
                                    <span>CPU {formatPercent(guest.cpu ?? 0)}</span>
                                    <span>Mem {formatBytes(guest.mem ?? 0)} / {formatBytes(guest.maxmem ?? 0)}</span>
                                    {(guest.maxdisk ?? 0) > 0 && (
                                      <span className="hidden xl:inline">Disk {formatBytes(guest.disk ?? 0)} / {formatBytes(guest.maxdisk ?? 0)}</span>
                                    )}
                                    <span className="hidden xl:inline">{formatUptime(guest.uptime ?? 0)}</span>
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
                                          <DropdownMenuItem onSelect={() => guestPower(conn.connectionId, guest, "reboot")}>
                                            <RefreshCw className="h-3.5 w-3.5 text-[var(--text-muted)]" /> Reboot
                                          </DropdownMenuItem>
                                          <DropdownMenuItem onSelect={() => guestPower(conn.connectionId, guest, "shutdown")}>
                                            <Power className="h-3.5 w-3.5 text-[var(--status-warn)]" /> Shutdown
                                          </DropdownMenuItem>
                                          <DropdownMenuItem onSelect={() => guestPower(conn.connectionId, guest, "reset")}>
                                            <RotateCcw className="h-3.5 w-3.5 text-[var(--status-warn)]" /> Reset
                                          </DropdownMenuItem>
                                          <DropdownMenuItem
                                            onSelect={() => guestPower(conn.connectionId, guest, "stop")}
                                            className="text-[var(--status-error)] data-[highlighted]:bg-[color-mix(in_oklab,var(--status-error)_10%,transparent)]"
                                          >
                                            <X className="h-3.5 w-3.5" /> Hard stop
                                          </DropdownMenuItem>
                                        </>
                                      )}
                                    </DropdownMenuContent>
                                  </DropdownMenu>
                                </span>
                              </div>
                            ))}
                            {guestsCapped && (
                              <button
                                className="w-full rounded-md py-1.5 text-center text-xs text-[var(--text-muted)] hover:bg-[var(--bg-surface-hover)] hover:text-[var(--text)]"
                                onClick={() => setRevealedNodes((s) => new Set(s).add(nodeKey))}
                              >
                                Show all {guests.length} guests (search above narrows this list too)
                              </button>
                            )}
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
            <Combobox
              value={migrateTarget}
              onChange={setMigrateTarget}
              placeholder="Select a node..."
              searchPlaceholder="Search nodes..."
              options={bulkMigrateNodes.map((n) => ({ value: n.node!, label: n.node! }))}
            />
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
