import { useMutation, useQueries, useQuery, useQueryClient } from "@tanstack/react-query"
import { Plus, ShieldCheck, Trash2 } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { EmptyState } from "@/components/ui/empty-state"
import { ErrorState } from "@/components/ui/error-state"
import { Label } from "@/components/ui/label"
import { PageHeader } from "@/components/ui/page-header"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { Hint } from "@/components/ui/tooltip"
import { api, ApiError, type ClusterResource, type ConnectionInventory, type HAGroup, type HAResource, type HAStatus } from "@/lib/api"

function haStateVariant(state?: string): "ok" | "warn" | "error" | "default" {
  if (state === "started") return "ok"
  if (state === "error" || state === "fence") return "error"
  if (!state) return "default"
  return "warn"
}

/** Live HA manager status badge — /cluster/ha/status/current statuses look
 * like "started (pve1)", so compare on the first word. */
function haStatusVariant(status?: string): "ok" | "warn" | "error" | "default" {
  const base = status?.split(" ")[0] ?? ""
  if (base === "started") return "ok"
  if (base === "error" || base === "fence") return "error"
  if (base === "stopped") return "warn"
  return "default"
}

export function HAPage() {
  const queryClient = useQueryClient()
  const confirm = useConfirm()
  const { data: inventory, isLoading, isError, refetch } = useQuery({
    queryKey: ["inventory"],
    queryFn: () => api.get<ConnectionInventory[]>("/inventory/"),
  })
  const connections = inventory ?? []

  const resourceQueries = useQueries({
    queries: connections.map((c) => ({
      queryKey: ["ha-resources", c.connectionId],
      queryFn: () => api.get<HAResource[]>(`/connections/${c.connectionId}/cluster/ha/resources`),
      retry: false,
    })),
  })
  const groupQueries = useQueries({
    queries: connections.map((c) => ({
      queryKey: ["ha-groups", c.connectionId],
      queryFn: () => api.get<HAGroup[]>(`/connections/${c.connectionId}/cluster/ha/groups`),
      retry: false,
    })),
  })
  // Live manager state per resource/node — still served on Proxmox 9 where
  // the legacy groups API is gone.
  const statusQueries = useQueries({
    queries: connections.map((c) => ({
      queryKey: ["ha-status", c.connectionId],
      queryFn: () => api.get<HAStatus[]>(`/connections/${c.connectionId}/cluster/ha/status`),
      retry: false,
    })),
  })

  const [form, setForm] = useState({ connId: "", guestId: "", group: "" })
  const selectedGuest = form.connId
    ? (connections.find((c) => c.connectionId === form.connId)?.resources ?? []).find(
        (r) => (r.type === "qemu" || r.type === "lxc") && String(r.id) === form.guestId,
      )
    : undefined
  // PVE expects a service id like vm:100 or ct:101 — build it from the
  // picked guest instead of asking for the format in a free-text box.
  const sid = selectedGuest ? `${selectedGuest.type === "lxc" ? "ct" : "vm"}:${selectedGuest.vmid}` : ""
  const groupOptions = form.connId ? groupQueries[connections.findIndex((c) => c.connectionId === form.connId)]?.data ?? [] : []

  const addResource = useMutation({
    mutationFn: () => api.post(`/connections/${form.connId}/cluster/ha/resources`, { sid, group: form.group || undefined }),
    onSuccess: () => {
      toast.success("HA resource added")
      queryClient.invalidateQueries({ queryKey: ["ha-resources"] })
      setForm({ ...form, guestId: "", group: "" })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to add HA resource"),
  })

  const removeResource = useMutation({
    mutationFn: ({ connId, sid }: { connId: string; sid: string }) =>
      api.delete(`/connections/${connId}/cluster/ha/resources/${encodeURIComponent(sid)}`),
    onSuccess: () => {
      toast.success("HA resource removed")
      queryClient.invalidateQueries({ queryKey: ["ha-resources"] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to remove HA resource"),
  })

  async function removeHA(connId: string, resSid: string) {
    const ok = await confirm({
      title: `Remove ${resSid} from HA?`,
      description: "The guest leaves HA management — it stays running where it is, but won't be restarted automatically after a node failure.",
      confirmLabel: "Remove from HA",
    })
    if (ok) removeResource.mutate({ connId, sid: resSid })
  }

  const guestOptionsForConn = (c: ConnectionInventory): ClusterResource[] =>
    (c.resources ?? []).filter((r) => r.type === "qemu" || r.type === "lxc")

  return (
    <div className="space-y-4">
      <PageHeader
        title="High Availability"
        description="Proxmox's built-in HA resources and groups."
        icon={ShieldCheck}
      />

      {isError ? (
        <ErrorState title="Couldn't load connections" onRetry={refetch} />
      ) : isLoading ? (
        <div className="space-y-3" aria-busy>
          <Skeleton className="h-32" />
          <Skeleton className="h-48" />
        </div>
      ) : connections.length === 0 ? (
        <EmptyState
          icon={ShieldCheck}
          title="No connections configured yet"
          description="HA is managed per Proxmox cluster — add a connection first."
        />
      ) : (
        <>
          <Card>
            <CardHeader>
              <CardTitle>Add HA resource</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
                <div className="space-y-1.5">
                  <Label>Connection</Label>
                  <Select value={form.connId} onValueChange={(v) => setForm({ connId: v, guestId: "", group: "" })}>
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
                  <Label>Guest</Label>
                  <Select value={form.guestId} onValueChange={(v) => setForm({ ...form, guestId: v })}>
                    <SelectTrigger>
                      <SelectValue placeholder={form.connId ? "Pick a guest…" : "Pick a connection first"} />
                    </SelectTrigger>
                    <SelectContent>
                      {(connections.find((c) => c.connectionId === form.connId)
                        ? guestOptionsForConn(connections.find((c) => c.connectionId === form.connId)!)
                        : []
                      ).map((g) => (
                        <SelectItem key={g.id} value={String(g.id)}>
                          {g.name} <span className="font-mono text-[var(--text-muted)]">#{g.vmid}</span>
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  {sid && <p className="text-xs text-[var(--text-faint)]">Service ID: <span className="font-mono">{sid}</span></p>}
                </div>
                <div className="space-y-1.5">
                  <Label>Group (optional)</Label>
                  <Select value={form.group} onValueChange={(v) => setForm({ ...form, group: v })}>
                    <SelectTrigger>
                      <SelectValue placeholder={groupOptions.length ? "Any group…" : "No groups defined"} />
                    </SelectTrigger>
                    <SelectContent>
                      {groupOptions.map((g) => (
                        <SelectItem key={g.group} value={g.group}>{g.group}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  {form.connId &&
                    !groupOptions.length &&
                    (() => {
                      const gq = groupQueries[connections.findIndex((c) => c.connectionId === form.connId)]
                      return gq && !gq.isPending && !gq.data ? (
                        <p className="text-xs text-[var(--text-faint)]">Group list unavailable (Proxmox 9 uses HA rules) — leaving it empty lets HA place the guest on any node.</p>
                      ) : null
                    })()}
                </div>
              </div>
              <Button className="mt-3" size="sm" loading={addResource.isPending} disabled={!form.connId || !sid} onClick={() => addResource.mutate()}>
                {!addResource.isPending && <Plus className="h-3.5 w-3.5" />} Add resource
              </Button>
            </CardContent>
          </Card>

          {connections.map((c, i) => (
            <Card key={c.connectionId}>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <ShieldCheck className="h-4 w-4" /> {c.name}
                </CardTitle>
              </CardHeader>
              <CardContent className="space-y-4">
                <div>
                  <p className="mb-2 text-xs font-medium text-[var(--text-muted)]">Live status</p>
                  {statusQueries[i].data && (statusQueries[i].data?.length ?? 0) > 0 ? (
                    <div className="flex flex-wrap gap-2">
                      {statusQueries[i].data!.map((s) => (
                        <div key={`${s.id}-${s.node ?? ""}`} className="flex items-center gap-2 rounded-md bg-[var(--bg-muted)] px-3 py-1.5 text-sm">
                          <span className="font-mono text-xs">{s.id}</span>
                          {s.node && <span className="text-xs text-[var(--text-muted)]">{s.node}</span>}
                          <Badge variant={haStatusVariant(s.status)}>{s.status ?? "unknown"}</Badge>
                        </div>
                      ))}
                    </div>
                  ) : statusQueries[i].isPending ? (
                    <p className="text-xs text-[var(--text-muted)]">Checking HA manager state…</p>
                  ) : (
                    <p className="text-xs text-[var(--text-muted)]">HA manager status isn't available for this connection (standalone servers have no HA stack).</p>
                  )}
                </div>
                <div>
                  <p className="mb-2 text-xs font-medium text-[var(--text-muted)]">Resources</p>
                  <div className="space-y-1.5">
                    {resourceQueries[i].data?.length === 0 && <p className="text-sm text-[var(--text-muted)]">No HA-managed guests.</p>}
                    {resourceQueries[i].data?.map((res) => (
                      <div key={res.sid} className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-[var(--border)] px-3 py-2 text-sm">
                        <span className="truncate font-mono text-xs">{res.sid}</span>
                        <div className="flex flex-wrap items-center gap-2">
                          {res.state && <Badge variant={haStateVariant(res.state)}>{res.state}</Badge>}
                          {res.group && <span className="text-xs text-[var(--text-muted)]">group: {res.group}</span>}
                          {(res.max_restart ?? 0) > 0 && (
                            <span className="text-xs text-[var(--text-muted)] tabular">restart×{res.max_restart}</span>
                          )}
                          {(res.max_relocate ?? 0) > 0 && (
                            <span className="text-xs text-[var(--text-muted)] tabular">relocate×{res.max_relocate}</span>
                          )}
                          <Hint label="Remove from HA">
                            <Button
                              size="icon"
                              variant="ghost"
                              className="hover:bg-[color-mix(in_oklab,var(--status-error)_12%,transparent)] hover:text-[var(--status-error)]"
                              aria-label={`Remove ${res.sid} from HA`}
                              onClick={() => removeHA(c.connectionId, res.sid)}
                            >
                              <Trash2 className="h-3.5 w-3.5" />
                            </Button>
                          </Hint>
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
                <div>
                  <p className="mb-2 text-xs font-medium text-[var(--text-muted)]">Groups</p>
                  <div className="space-y-1.5">
                    {groupQueries[i].data?.length === 0 && <p className="text-sm text-[var(--text-muted)]">No HA groups configured.</p>}
                    {groupQueries[i].data?.map((g) => (
                      <div key={g.group} className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-[var(--border)] px-3 py-2 text-sm">
                        <div className="min-w-0">
                          <span className="font-medium">{g.group}</span>
                          {g.comment && <span className="ml-2 text-xs text-[var(--text-muted)]">{g.comment}</span>}
                        </div>
                        <div className="flex flex-wrap items-center gap-2">
                          {g.restricted === 1 && <Badge variant="warn">Restricted</Badge>}
                          {g.nofailback === 1 && <Badge variant="default">No failback</Badge>}
                          <span className="break-all font-mono text-xs text-[var(--text-muted)]">{g.nodes}</span>
                        </div>
                      </div>
                    ))}
                    {!groupQueries[i].data && !groupQueries[i].isPending && (
                      // The old groups API 500s on Proxmox 9 ("ha groups have
                      // been migrated to rules") — say so instead of rendering
                      // a silently empty section.
                      <p className="text-xs text-[var(--text-muted)]">
                        Group list unavailable for this connection — Proxmox 9 retired HA groups in favor of HA rules, so legacy group definitions may no longer be exposed.
                      </p>
                    )}
                  </div>
                </div>
              </CardContent>
            </Card>
          ))}
        </>
      )}
    </div>
  )
}
