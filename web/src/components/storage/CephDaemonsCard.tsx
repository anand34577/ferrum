import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Plus, Server, Trash2 } from "lucide-react"
import { toast } from "sonner"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { StatusDot } from "@/components/ui/status-dot"
import { api, ApiError, type CephFilesystem, type CephMgr, type CephMon } from "@/lib/api"

interface CephDaemonsCardProps {
  connId: string
  connName: string
  /** Every node in this connection — mon/mgr can be added on any of them. */
  nodes: string[]
}

/** Ceph monitor/manager daemons and CephFS filesystems for one connection —
 * a companion to the pool/OSD cards already on this page, since PVE reports
 * mon/mgr/fs from separate endpoints (/ceph/mon, /ceph/mgr, /ceph/fs). */
export function CephDaemonsCard({ connId, connName, nodes }: CephDaemonsCardProps) {
  const queryClient = useQueryClient()
  const confirm = useConfirm()
  const node = nodes[0]
  const base = `/connections/${connId}/nodes/${node}`

  const monsQuery = useQuery({ queryKey: ["ceph-mons", connId], queryFn: () => api.get<CephMon[]>(`${base}/ceph/mon`), retry: false })
  const mgrsQuery = useQuery({ queryKey: ["ceph-mgrs", connId], queryFn: () => api.get<CephMgr[]>(`${base}/ceph/mgr`), retry: false })
  const fsQuery = useQuery({ queryKey: ["ceph-fs", connId], queryFn: () => api.get<CephFilesystem[]>(`${base}/ceph/fs`), retry: false })

  const addMon = useMutation({
    mutationFn: () => api.post(`${base}/ceph/mon`),
    onSuccess: () => {
      toast.success(`Adding monitor on ${node}`)
      queryClient.invalidateQueries({ queryKey: ["ceph-mons", connId] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to add monitor"),
  })
  const removeMon = useMutation({
    mutationFn: (name: string) => api.delete(`${base}/ceph/mon/${encodeURIComponent(name)}`),
    onSuccess: () => {
      toast.success("Monitor removed")
      queryClient.invalidateQueries({ queryKey: ["ceph-mons", connId] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to remove monitor"),
  })
  const addMgr = useMutation({
    mutationFn: () => api.post(`${base}/ceph/mgr`),
    onSuccess: () => {
      toast.success(`Adding manager on ${node}`)
      queryClient.invalidateQueries({ queryKey: ["ceph-mgrs", connId] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to add manager"),
  })

  async function confirmRemoveMon(name: string) {
    const ok = await confirm({ title: `Remove monitor "${name}"?`, description: "Ceph needs a majority of monitors up to stay quorate — don't remove one below that threshold.", confirmLabel: "Remove monitor" })
    if (ok) removeMon.mutate(name)
  }

  if (!monsQuery.data && !mgrsQuery.data && !fsQuery.data && !monsQuery.isPending) return null

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Server className="h-4 w-4" /> Ceph Daemons — {connName}
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <div>
          <div className="mb-1.5 flex items-center justify-between">
            <p className="text-xs font-medium text-[var(--text-muted)]">Monitors</p>
            <Button size="sm" variant="ghost" disabled={addMon.isPending} onClick={() => addMon.mutate()}>
              <Plus className="h-3.5 w-3.5" /> Add on {node}
            </Button>
          </div>
          <div className="flex flex-wrap gap-2">
            {(monsQuery.data ?? []).map((m) => (
              <div key={m.name} className="flex items-center gap-2 rounded-md bg-[var(--bg-muted)] px-3 py-1.5 text-sm">
                <StatusDot status={m.quorum ? "ok" : "error"} />
                <span className="font-mono text-xs">{m.name}</span>
                <Button size="icon-sm" variant="ghost" aria-label={`Remove monitor ${m.name}`} onClick={() => confirmRemoveMon(m.name)}>
                  <Trash2 className="h-3 w-3" />
                </Button>
              </div>
            ))}
            {monsQuery.data?.length === 0 && <p className="text-xs text-[var(--text-muted)]">No monitors reported.</p>}
          </div>
        </div>

        <div>
          <div className="mb-1.5 flex items-center justify-between">
            <p className="text-xs font-medium text-[var(--text-muted)]">Managers</p>
            <Button size="sm" variant="ghost" disabled={addMgr.isPending} onClick={() => addMgr.mutate()}>
              <Plus className="h-3.5 w-3.5" /> Add on {node}
            </Button>
          </div>
          <div className="flex flex-wrap gap-2">
            {(mgrsQuery.data ?? []).map((m, i) => (
              <div key={`${m.host}-${i}`} className="flex items-center gap-2 rounded-md bg-[var(--bg-muted)] px-3 py-1.5 text-sm">
                <StatusDot status={m.active ? "ok" : "muted"} />
                <span className="font-mono text-xs">{m.host ?? "unknown"}</span>
                {m.active ? <Badge variant="ok">active</Badge> : <Badge>standby</Badge>}
              </div>
            ))}
            {mgrsQuery.data?.length === 0 && <p className="text-xs text-[var(--text-muted)]">No managers reported.</p>}
          </div>
        </div>

        {(fsQuery.data?.length ?? 0) > 0 && (
          <div>
            <p className="mb-1.5 text-xs font-medium text-[var(--text-muted)]">CephFS filesystems</p>
            <div className="flex flex-wrap gap-2">
              {(fsQuery.data ?? []).map((f) => (
                <Badge key={f.name}>{f.name}</Badge>
              ))}
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
