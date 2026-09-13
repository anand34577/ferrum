import { useMutation, useQueries, useQuery, useQueryClient } from "@tanstack/react-query"
import { ChevronRight, Layers, Minus, Plus, Trash2 } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { CollapsibleCard } from "@/components/ui/collapsible-card"
import { Combobox } from "@/components/ui/combobox"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { EmptyState } from "@/components/ui/empty-state"
import { ErrorState } from "@/components/ui/error-state"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ListSearch } from "@/components/ui/list-search"
import { PageHeader } from "@/components/ui/page-header"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { Hint } from "@/components/ui/tooltip"
import { api, ApiError, type ClusterResource, type ConnectionInventory, type Pool, type PoolDetail } from "@/lib/api"
import { cn } from "@/lib/utils"

export function PoolsPage() {
  const queryClient = useQueryClient()
  const confirm = useConfirm()
  const [expanded, setExpanded] = useState<Set<string>>(new Set())

  const { data: inventory, isLoading, isError, refetch, isRefetching } = useQuery({
    queryKey: ["inventory"],
    queryFn: () => api.get<ConnectionInventory[]>("/inventory/"),
    refetchInterval: 30_000,
  })
  const connections = inventory ?? []

  const poolQueries = useQueries({
    queries: connections.map((c) => ({
      queryKey: ["pools", c.connectionId],
      queryFn: () => api.get<Pool[]>(`/connections/${c.connectionId}/pools/`),
      retry: false,
    })),
  })

  const [connId, setConnId] = useState("")
  const [poolId, setPoolId] = useState("")
  const [comment, setComment] = useState("")
  const createPool = useMutation({
    mutationFn: () => api.post(`/connections/${connId}/pools/`, { poolId, comment }),
    onSuccess: () => {
      toast.success("Pool created")
      queryClient.invalidateQueries({ queryKey: ["pools"] })
      setPoolId("")
      setComment("")
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to create pool"),
  })

  const deletePool = useMutation({
    mutationFn: ({ conn, id }: { conn: string; id: string }) => api.delete(`/connections/${conn}/pools/${encodeURIComponent(id)}`),
    onSuccess: () => {
      toast.success("Pool deleted")
      queryClient.invalidateQueries({ queryKey: ["pools"] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to delete pool (remove its members first)"),
  })

  async function removePool(connId: string, poolId: string) {
    const ok = await confirm({
      title: `Delete pool ${poolId}?`,
      description: "The pool is removed from Proxmox. Guests and storages themselves stay — they just lose this grouping.",
      confirmLabel: "Delete pool",
    })
    if (ok) deletePool.mutate({ conn: connId, id: poolId })
  }

  function toggle(key: string) {
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  return (
    <div className="space-y-4">
      <PageHeader
        title="Resource Pools"
        description="Group guests and storage by team, project, or tenant — independent of which node or connection they live on."
        icon={Layers}
        onRefresh={() => {
          void queryClient.invalidateQueries({ queryKey: ["inventory"] })
          void queryClient.invalidateQueries({ queryKey: ["pools"] })
          void queryClient.invalidateQueries({ queryKey: ["pool-detail"] })
        }}
        refreshing={isRefetching}
      />

      {isError ? (
        <ErrorState title="Couldn't load connections" onRetry={refetch} />
      ) : isLoading ? (
        <div className="space-y-3" aria-busy>
          <Skeleton className="h-32" />
          <Skeleton className="h-40" />
        </div>
      ) : connections.length === 0 ? (
        <EmptyState
          icon={Layers}
          title="No connections configured yet"
          description="Pools live on your Proxmox clusters — add a connection first."
        />
      ) : (
        <>
          <CollapsibleCard title="New pool">
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
                <div className="space-y-1.5">
                  <Label>Connection</Label>
                  <Select value={connId} onValueChange={setConnId}>
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
                  <Label>Pool ID</Label>
                  <Input value={poolId} onChange={(e) => setPoolId(e.target.value)} placeholder="team-web" />
                </div>
                <div className="space-y-1.5">
                  <Label>Comment (optional)</Label>
                  <Input value={comment} onChange={(e) => setComment(e.target.value)} />
                </div>
              </div>
              <Button className="mt-3" size="sm" loading={createPool.isPending} disabled={!connId || !poolId} onClick={() => createPool.mutate()}>
                {!createPool.isPending && <Plus className="h-3.5 w-3.5" />} Create pool
              </Button>
          </CollapsibleCard>

          {connections.map((c, i) => {
            const q = poolQueries[i]
            const guests = (c.resources ?? []).filter((r) => r.type === "qemu" || r.type === "lxc")
            return (
              <Card key={c.connectionId}>
                <CardHeader>
                  <CardTitle>{c.name}</CardTitle>
                </CardHeader>
                <CardContent className="space-y-1">
                  {q.isError && <ErrorState title="Couldn't load pools" onRetry={q.refetch} />}
                  {q.data?.length === 0 && <p className="text-sm text-[var(--text-muted)]">No resource pools yet — create one above.</p>}
                  {q.data?.map((pool) => {
                    const key = `${c.connectionId}/${pool.poolid}`
                    return (
                      <PoolRow
                        key={key}
                        connId={c.connectionId}
                        pool={pool}
                        guestOptions={guests}
                        expanded={expanded.has(key)}
                        onToggle={() => toggle(key)}
                        onDelete={() => removePool(c.connectionId, pool.poolid)}
                      />
                    )
                  })}
                </CardContent>
              </Card>
            )
          })}
        </>
      )}
    </div>
  )
}

function PoolRow({
  connId,
  pool,
  guestOptions,
  expanded,
  onToggle,
  onDelete,
}: {
  connId: string
  pool: Pool
  guestOptions: ClusterResource[]
  expanded: boolean
  onToggle: () => void
  onDelete: () => void
}) {
  const detailQuery = useQuery({
    queryKey: ["pool-detail", connId, pool.poolid],
    queryFn: () => api.get<PoolDetail>(`/connections/${connId}/pools/${encodeURIComponent(pool.poolid)}`),
    enabled: expanded,
    // Membership can change from the PVE side while a pool sits expanded —
    // the lazy fetch used to go permanently stale until collapsed/re-opened.
    refetchInterval: expanded ? 30_000 : false,
  })
  const queryClient = useQueryClient()
  const confirm = useConfirm()
  const [vmid, setVmid] = useState("")
  const [memberFilter, setMemberFilter] = useState("")
  const members = (detailQuery.data?.members ?? []).filter(
    (m) => !memberFilter || (m.name ?? String(m.id)).toLowerCase().includes(memberFilter.toLowerCase()) || String(m.vmid ?? "").includes(memberFilter),
  )

  const addMember = useMutation({
    mutationFn: () => api.put(`/connections/${connId}/pools/${encodeURIComponent(pool.poolid)}/members`, { vmids: [Number(vmid)], remove: false }),
    onSuccess: () => {
      toast.success("Guest added to pool")
      setVmid("")
      queryClient.invalidateQueries({ queryKey: ["pool-detail", connId, pool.poolid] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to add guest to pool"),
  })

  const removeMember = useMutation({
    mutationFn: (id: number) => api.put(`/connections/${connId}/pools/${encodeURIComponent(pool.poolid)}/members`, { vmids: [id], remove: true }),
    onSuccess: () => {
      toast.success("Guest removed from pool")
      queryClient.invalidateQueries({ queryKey: ["pool-detail", connId, pool.poolid] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to remove guest from pool"),
  })

  return (
    <div>
      <div className="flex items-center gap-2 rounded-md px-2 py-1.5 hover:bg-[var(--bg-surface-hover)]">
        <button onClick={onToggle} className="flex min-w-0 flex-1 items-center gap-2 text-left" aria-expanded={expanded}>
          <ChevronRight className={cn("h-3.5 w-3.5 shrink-0 transition-transform", expanded && "rotate-90")} />
          <Layers className="h-3.5 w-3.5 shrink-0 text-brand-500" />
          <span className="truncate text-sm font-medium">{pool.poolid}</span>
          {pool.comment && <span className="truncate text-xs text-[var(--text-muted)]">{pool.comment}</span>}
        </button>
        <Hint label="Delete pool">
          <Button
            size="icon"
            variant="ghost-danger"
            className="shrink-0"
            aria-label={`Delete pool ${pool.poolid}`}
            onClick={onDelete}
          >
            <Trash2 className="h-3.5 w-3.5" />
          </Button>
        </Hint>
      </div>
      {expanded && (
        <div className="ml-8 space-y-1 border-l border-[var(--border)] py-1 pl-3">
          {detailQuery.isLoading && (
            <div className="space-y-1.5 py-1" aria-busy>
              <Skeleton className="h-5 w-64" />
              <Skeleton className="h-5 w-52" />
            </div>
          )}
          {detailQuery.isError && <ErrorState className="py-6" title="Couldn't load this pool's members" onRetry={detailQuery.refetch} />}
          {detailQuery.data?.members?.length === 0 && <p className="text-xs text-[var(--text-muted)]">No members yet — add a guest below.</p>}
          {(detailQuery.data?.members?.length ?? 0) > 8 && (
            <ListSearch value={memberFilter} onChange={setMemberFilter} placeholder="Search members..." className="mb-1.5" />
          )}
          {(detailQuery.data?.members?.length ?? 0) > 0 && members.length === 0 && (
            <p className="text-xs text-[var(--text-muted)]">No members match "{memberFilter}".</p>
          )}
          {/* Past a screenful, this scrolls internally instead of growing the
              whole page — search narrows the list, but clearing it (or a
              broad match) shouldn't turn the pool card into the entire page. */}
          <div className={members.length > 10 ? "max-h-64 space-y-1 overflow-y-auto pr-1" : "space-y-1"}>
            {members.map((m) => (
              <div key={m.id} className="flex items-center gap-2 text-xs">
                <Badge variant="default">{m.type}</Badge>
                <span>{m.name ?? m.id}</span>
                {m.vmid && <span className="font-mono text-[var(--text-muted)] tabular">#{m.vmid}</span>}
                {m.vmid && (
                  <Hint label="Remove from pool">
                    <Button
                      size="icon-sm"
                      variant="ghost-danger"
                      aria-label={`Remove ${m.name ?? m.vmid} from pool`}
                      onClick={async () => {
                        if (await confirm({ title: `Remove ${m.name ?? m.vmid} from pool?`, destructive: false, confirmLabel: "Remove" })) {
                          removeMember.mutate(m.vmid!)
                        }
                      }}
                    >
                      <Minus className="h-3 w-3" />
                    </Button>
                  </Hint>
                )}
              </div>
            ))}
          </div>
          <div className="flex items-center gap-2 pt-1">
            {/* Guests on this connection, straight from inventory — no more
                typing raw VMIDs and hoping they exist. A Combobox (not
                Select) so a pool on a fleet with hundreds of guests can
                actually search this list instead of blind-scrolling it. */}
            <Combobox
              value={vmid}
              onChange={setVmid}
              placeholder="Pick a guest to add…"
              searchPlaceholder="Search guests..."
              className="h-7 w-56 text-xs"
              options={guestOptions.map((g) => ({ value: String(g.vmid), label: `${g.name} #${g.vmid}` }))}
            />
            <Button size="sm" variant="secondary" loading={addMember.isPending} disabled={!vmid} onClick={() => addMember.mutate()}>
              {!addMember.isPending && <Plus className="h-3 w-3" />} Add
            </Button>
          </div>
        </div>
      )}
    </div>
  )
}
