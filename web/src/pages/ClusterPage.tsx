import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { GitBranch, Plus, RefreshCw, Trash2 } from "lucide-react"
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
import { ListSearch } from "@/components/ui/list-search"
import { PageHeader } from "@/components/ui/page-header"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Textarea } from "@/components/ui/textarea"
import {
  api,
  ApiError,
  type AccessACLEntry,
  type AccessDomain,
  type AccessRole,
  type AccessUser,
  type ClusterConfigNode,
  type ClusterJoinInfo,
  type ConnectionInventory,
  type SDNController,
  type SDNIPAM,
  type SDNSubnet,
  type SDNVnet,
  type SDNZone,
} from "@/lib/api"
import { cn } from "@/lib/utils"

// parseExtraFields turns "key=value" lines (as typed into a free-form extra
// fields textarea) into an options map, e.g. for controller/ipam-type-specific
// properties this UI doesn't have a dedicated field for.
function parseExtraFields(text: string): Record<string, string> {
  const out: Record<string, string> = {}
  for (const line of text.split("\n")) {
    const idx = line.indexOf("=")
    if (idx <= 0) continue
    const key = line.slice(0, idx).trim()
    const value = line.slice(idx + 1).trim()
    if (key) out[key] = value
  }
  return out
}

export function ClusterPage() {
  const queryClient = useQueryClient()
  const { data: inventory, isLoading, isError, refetch, isRefetching } = useQuery({
    queryKey: ["inventory"],
    queryFn: () => api.get<ConnectionInventory[]>("/inventory/"),
    refetchInterval: 30_000,
  })
  const connections = inventory ?? []
  const [connId, setConnId] = useState("")
  const activeConnId = connId || connections[0]?.connectionId || ""
  const base = activeConnId ? `/connections/${activeConnId}` : ""

  return (
    <div className="space-y-4">
      <PageHeader
        title="Cluster & SDN"
        description="Cluster membership and software-defined networking, per connection."
        onRefresh={() => void queryClient.invalidateQueries()}
        refreshing={isRefetching}
      />

      {isError ? (
        <ErrorState title="Couldn't load connections" onRetry={refetch} />
      ) : isLoading ? (
        <Skeleton className="h-72" />
      ) : (
        <>
          <div className="flex flex-wrap items-center gap-2">
            <Label className="text-xs text-[var(--text-muted)]">Connection</Label>
            <Select value={activeConnId} onValueChange={setConnId}>
              <SelectTrigger className="w-full sm:w-64"><SelectValue placeholder="Select a connection" /></SelectTrigger>
              <SelectContent>
                {connections.map((c) => (
                  <SelectItem key={c.connectionId} value={c.connectionId}>{c.name}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {!activeConnId ? (
            <EmptyState icon={GitBranch} title="No connections yet" description="Add a Proxmox connection to manage its cluster membership and SDN config." />
          ) : (
            <Tabs defaultValue="sdn">
              <TabsList>
                <TabsTrigger value="sdn">SDN</TabsTrigger>
                <TabsTrigger value="nodes">Cluster Nodes</TabsTrigger>
                <TabsTrigger value="access">Access</TabsTrigger>
              </TabsList>
              <TabsContent value="sdn">
                <SDNPanel base={base} connId={activeConnId} />
              </TabsContent>
              <TabsContent value="nodes">
                <ClusterNodesPanel base={base} connId={activeConnId} />
              </TabsContent>
              <TabsContent value="access">
                <AccessPanel base={base} connId={activeConnId} />
              </TabsContent>
            </Tabs>
          )}
        </>
      )}
    </div>
  )
}

function SDNPanel({ base, connId }: { base: string; connId: string }) {
  const queryClient = useQueryClient()
  const confirm = useConfirm()

  const zonesQuery = useQuery({ queryKey: ["sdn-zones", connId], queryFn: () => api.get<SDNZone[]>(`${base}/cluster/sdn/zones`) })
  const vnetsQuery = useQuery({ queryKey: ["sdn-vnets", connId], queryFn: () => api.get<SDNVnet[]>(`${base}/cluster/sdn/vnets`) })

  const [selectedVnet, setSelectedVnet] = useState<string | null>(null)
  const subnetsQuery = useQuery({
    queryKey: ["sdn-subnets", connId, selectedVnet],
    queryFn: () => api.get<SDNSubnet[]>(`${base}/cluster/sdn/vnets/${selectedVnet}/subnets`),
    enabled: !!selectedVnet,
  })

  function invalidate() {
    queryClient.invalidateQueries({ queryKey: ["sdn-zones", connId] })
    queryClient.invalidateQueries({ queryKey: ["sdn-vnets", connId] })
  }

  const [zoneForm, setZoneForm] = useState({ zone: "", type: "simple", bridge: "" })
  const createZone = useMutation({
    mutationFn: () =>
      api.post(`${base}/cluster/sdn/zones`, {
        zone: zoneForm.zone,
        options: { type: zoneForm.type, ...(zoneForm.bridge ? { bridge: zoneForm.bridge } : {}) },
      }),
    onSuccess: () => {
      toast.success(`Zone "${zoneForm.zone}" created — Apply to activate it`)
      setZoneForm({ zone: "", type: "simple", bridge: "" })
      invalidate()
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to create zone"),
  })
  const deleteZone = useMutation({
    mutationFn: (zone: string) => api.delete(`${base}/cluster/sdn/zones/${encodeURIComponent(zone)}`),
    onSuccess: () => {
      toast.success("Zone deleted")
      invalidate()
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to delete zone"),
  })

  const [vnetForm, setVnetForm] = useState({ vnet: "", zone: "", tag: "" })
  const createVnet = useMutation({
    mutationFn: () => api.post(`${base}/cluster/sdn/vnets`, { vnet: vnetForm.vnet, zone: vnetForm.zone, tag: vnetForm.tag ? Number(vnetForm.tag) : undefined }),
    onSuccess: () => {
      toast.success(`Vnet "${vnetForm.vnet}" created — Apply to activate it`)
      setVnetForm({ vnet: "", zone: "", tag: "" })
      invalidate()
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to create vnet"),
  })
  const deleteVnet = useMutation({
    mutationFn: (vnet: string) => api.delete(`${base}/cluster/sdn/vnets/${encodeURIComponent(vnet)}`),
    onSuccess: () => {
      toast.success("Vnet deleted")
      invalidate()
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to delete vnet"),
  })

  const [subnetForm, setSubnetForm] = useState({ cidr: "", gateway: "" })
  const createSubnet = useMutation({
    mutationFn: () => api.post(`${base}/cluster/sdn/vnets/${selectedVnet}/subnets`, { cidr: subnetForm.cidr, gateway: subnetForm.gateway || undefined }),
    onSuccess: () => {
      toast.success("Subnet created — Apply to activate it")
      setSubnetForm({ cidr: "", gateway: "" })
      queryClient.invalidateQueries({ queryKey: ["sdn-subnets", connId, selectedVnet] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to create subnet"),
  })
  const deleteSubnet = useMutation({
    mutationFn: (subnet: string) => api.delete(`${base}/cluster/sdn/vnets/${selectedVnet}/subnets?subnet=${encodeURIComponent(subnet)}`),
    onSuccess: () => {
      toast.success("Subnet deleted")
      queryClient.invalidateQueries({ queryKey: ["sdn-subnets", connId, selectedVnet] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to delete subnet"),
  })

  const applyConfig = useMutation({
    mutationFn: () => api.post(`${base}/cluster/sdn/apply`),
    onSuccess: () => toast.success("SDN configuration applied"),
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Apply failed"),
  })

  const controllersQuery = useQuery({ queryKey: ["sdn-controllers", connId], queryFn: () => api.get<SDNController[]>(`${base}/cluster/sdn/controllers`) })
  const [controllerForm, setControllerForm] = useState({ controller: "", type: "evpn", extra: "" })
  const createController = useMutation({
    mutationFn: () =>
      api.post(`${base}/cluster/sdn/controllers`, {
        controller: controllerForm.controller,
        options: { type: controllerForm.type, ...parseExtraFields(controllerForm.extra) },
      }),
    onSuccess: () => {
      toast.success(`Controller "${controllerForm.controller}" created — Apply to activate it`)
      setControllerForm({ controller: "", type: "evpn", extra: "" })
      queryClient.invalidateQueries({ queryKey: ["sdn-controllers", connId] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to create controller"),
  })
  const deleteController = useMutation({
    mutationFn: (controller: string) => api.delete(`${base}/cluster/sdn/controllers/${encodeURIComponent(controller)}`),
    onSuccess: () => {
      toast.success("Controller deleted")
      queryClient.invalidateQueries({ queryKey: ["sdn-controllers", connId] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to delete controller"),
  })
  async function removeController(controller: string) {
    const ok = await confirm({ title: `Delete controller "${controller}"?`, description: "Zones referencing it lose route exchange.", confirmLabel: "Delete controller" })
    if (ok) deleteController.mutate(controller)
  }

  const ipamsQuery = useQuery({ queryKey: ["sdn-ipams", connId], queryFn: () => api.get<SDNIPAM[]>(`${base}/cluster/sdn/ipams`) })
  const [ipamForm, setIpamForm] = useState({ ipam: "", type: "netbox", extra: "" })
  const createIpam = useMutation({
    mutationFn: () =>
      api.post(`${base}/cluster/sdn/ipams`, {
        ipam: ipamForm.ipam,
        options: { type: ipamForm.type, ...parseExtraFields(ipamForm.extra) },
      }),
    onSuccess: () => {
      toast.success(`IPAM "${ipamForm.ipam}" created`)
      setIpamForm({ ipam: "", type: "netbox", extra: "" })
      queryClient.invalidateQueries({ queryKey: ["sdn-ipams", connId] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to create IPAM"),
  })
  const deleteIpam = useMutation({
    mutationFn: (ipam: string) => api.delete(`${base}/cluster/sdn/ipams/${encodeURIComponent(ipam)}`),
    onSuccess: () => {
      toast.success("IPAM deleted")
      queryClient.invalidateQueries({ queryKey: ["sdn-ipams", connId] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to delete IPAM"),
  })
  async function removeIpam(ipam: string) {
    const ok = await confirm({ title: `Delete IPAM "${ipam}"?`, description: "Subnets using it for allocation lose that source.", confirmLabel: "Delete IPAM" })
    if (ok) deleteIpam.mutate(ipam)
  }

  async function removeZone(zone: string) {
    const ok = await confirm({ title: `Delete zone "${zone}"?`, description: "Every vnet in this zone must be removed first.", confirmLabel: "Delete zone" })
    if (ok) deleteZone.mutate(zone)
  }
  async function removeVnet(vnet: string) {
    const ok = await confirm({ title: `Delete vnet "${vnet}"?`, description: "Guests attached to it lose network connectivity until reattached.", confirmLabel: "Delete vnet" })
    if (ok) deleteVnet.mutate(vnet)
  }
  async function removeSubnet(subnet: string) {
    const ok = await confirm({ title: `Delete subnet "${subnet}"?`, description: "Guests on this subnet keep their static addresses, but new allocations from it stop.", confirmLabel: "Delete subnet" })
    if (ok) deleteSubnet.mutate(subnet)
  }

  // Cluster-wide activation deserves the same guard as the single-resource
  // deletes beside it — one misclick used to reconfigure live networking on
  // every node with no confirmation.
  async function applySdn() {
    const ok = await confirm({
      title: "Apply pending SDN changes?",
      description:
        "This pushes the pending network configuration (zones, vnets, subnets, controllers) out to every node in the cluster and makes it live. A mistake here can disrupt running guests' networking.",
      confirmLabel: "Apply to cluster",
    })
    if (ok) applyConfig.mutate()
  }

  return (
    <div className="space-y-4">
      <div className="flex justify-end">
        <Button size="sm" loading={applyConfig.isPending} onClick={() => void applySdn()}>
          {!applyConfig.isPending && <RefreshCw className="h-3.5 w-3.5" />} Apply pending changes
        </Button>
      </div>

      <Card>
        <CardHeader><CardTitle className="text-sm">Zones</CardTitle></CardHeader>
        <CardContent className="space-y-3">
          {zonesQuery.isError ? (
            <ErrorState onRetry={zonesQuery.refetch} />
          ) : zonesQuery.isLoading ? (
            <Skeleton className="h-16" />
          ) : (
            <div className="space-y-1.5">
              {(zonesQuery.data ?? []).map((z) => (
                <div key={z.zone} className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-[var(--border)] px-3 py-2 text-sm transition-colors animate-in fade-in slide-in-from-top-1 duration-200 hover:bg-[var(--bg-surface-hover)]">
                  <div className="flex items-center gap-2">
                    <span className="font-medium">{z.zone}</span>
                    <Badge>{z.type}</Badge>
                    {z.pending ? <Badge variant="warn">Pending</Badge> : null}
                  </div>
                  <Button size="icon" variant="ghost-danger" aria-label={`Delete zone ${z.zone}`} onClick={() => removeZone(z.zone)}>
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                </div>
              ))}
              {zonesQuery.data?.length === 0 && <p className="text-sm text-[var(--text-muted)]">No SDN zones configured.</p>}
            </div>
          )}
          <div className="flex flex-wrap items-end gap-2 border-t border-[var(--border)] pt-3">
            <div className="space-y-1.5">
              <Label>Zone name</Label>
              <Input value={zoneForm.zone} onChange={(e) => setZoneForm((f) => ({ ...f, zone: e.target.value }))} className="w-32" />
            </div>
            <div className="space-y-1.5">
              <Label>Type</Label>
              <Select value={zoneForm.type} onValueChange={(v) => setZoneForm((f) => ({ ...f, type: v }))}>
                <SelectTrigger className="w-32"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="simple">Simple</SelectItem>
                  <SelectItem value="vlan">VLAN</SelectItem>
                  <SelectItem value="qinq">QinQ</SelectItem>
                  <SelectItem value="vxlan">VXLAN</SelectItem>
                  <SelectItem value="evpn">EVPN</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label>Bridge (vlan/qinq)</Label>
              <Input placeholder="vmbr0" value={zoneForm.bridge} onChange={(e) => setZoneForm((f) => ({ ...f, bridge: e.target.value }))} className="w-28" />
            </div>
            {/* Default (h-9) size, not sm (h-8) — matches the Input/Select
                beside it exactly so items-end bottom-alignment lands the
                button's edge flush with theirs instead of 4px short. */}
            <Button loading={createZone.isPending} disabled={!zoneForm.zone} onClick={() => createZone.mutate()}>
              <Plus className="h-3.5 w-3.5" /> Add zone
            </Button>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle className="text-sm">Vnets</CardTitle></CardHeader>
        <CardContent className="space-y-3">
          {vnetsQuery.isError ? (
            <ErrorState onRetry={vnetsQuery.refetch} />
          ) : vnetsQuery.isLoading ? (
            <Skeleton className="h-16" />
          ) : (
            <div className="space-y-1.5">
              {(vnetsQuery.data ?? []).map((v) => (
                <div
                  key={v.vnet}
                  className={cn(
                    "flex items-center justify-between gap-2 rounded-md border px-3 py-2 text-sm transition-colors",
                    v.vnet === selectedVnet
                      ? "border-[color-mix(in_oklab,var(--color-brand-500)_35%,var(--border))] bg-[color-mix(in_oklab,var(--color-brand-500)_6%,var(--bg-surface))]"
                      : "border-[var(--border)] hover:bg-[var(--bg-surface-hover)]",
                  )}
                >
                  <button className="flex flex-1 items-center gap-2 text-left" onClick={() => setSelectedVnet(v.vnet === selectedVnet ? null : v.vnet)}>
                    <span className="font-medium">{v.vnet}</span>
                    <Badge>zone: {v.zone}</Badge>
                    {v.tag ? <Badge variant="default">VLAN {v.tag}</Badge> : null}
                    {v.pending ? <Badge variant="warn">Pending</Badge> : null}
                  </button>
                  <Button size="icon" variant="ghost-danger" aria-label={`Delete vnet ${v.vnet}`} onClick={() => removeVnet(v.vnet)}>
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                </div>
              ))}
              {vnetsQuery.data?.length === 0 && <p className="text-sm text-[var(--text-muted)]">No vnets configured.</p>}
            </div>
          )}
          <div className="flex flex-wrap items-end gap-2 border-t border-[var(--border)] pt-3">
            <div className="space-y-1.5">
              <Label>Vnet name</Label>
              <Input value={vnetForm.vnet} onChange={(e) => setVnetForm((f) => ({ ...f, vnet: e.target.value }))} className="w-32" />
            </div>
            <div className="space-y-1.5">
              <Label>Zone</Label>
              <Select value={vnetForm.zone} onValueChange={(v) => setVnetForm((f) => ({ ...f, zone: v }))}>
                <SelectTrigger className="w-32"><SelectValue placeholder="zone" /></SelectTrigger>
                <SelectContent>
                  {(zonesQuery.data ?? []).map((z) => (
                    <SelectItem key={z.zone} value={z.zone}>{z.zone}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label>VLAN tag</Label>
              <Input type="number" placeholder="optional" value={vnetForm.tag} onChange={(e) => setVnetForm((f) => ({ ...f, tag: e.target.value }))} className="w-24" />
            </div>
            <Button loading={createVnet.isPending} disabled={!vnetForm.vnet || !vnetForm.zone} onClick={() => createVnet.mutate()}>
              <Plus className="h-3.5 w-3.5" /> Add vnet
            </Button>
          </div>
        </CardContent>
      </Card>

      {selectedVnet && (
        <Card>
          <CardHeader><CardTitle className="text-sm">Subnets — {selectedVnet}</CardTitle></CardHeader>
          <CardContent className="space-y-3">
            {subnetsQuery.isError ? (
              <ErrorState onRetry={subnetsQuery.refetch} />
            ) : subnetsQuery.isLoading ? (
              <Skeleton className="h-12" />
            ) : (
              <div className="space-y-1.5">
                {(subnetsQuery.data ?? []).map((s) => (
                  <div key={s.subnet} className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-[var(--border)] px-3 py-2 text-sm transition-colors animate-in fade-in slide-in-from-top-1 duration-200 hover:bg-[var(--bg-surface-hover)]">
                    <span className="font-mono">{s.subnet}</span>
                    {s.gateway && <Badge>gw: {s.gateway}</Badge>}
                    <Button size="icon" variant="ghost-danger" aria-label={`Delete subnet ${s.subnet}`} onClick={() => removeSubnet(s.subnet)}>
                      <Trash2 className="h-3.5 w-3.5" />
                    </Button>
                  </div>
                ))}
                {subnetsQuery.data?.length === 0 && <p className="text-sm text-[var(--text-muted)]">No subnets on this vnet.</p>}
              </div>
            )}
            <div className="flex flex-wrap items-end gap-2 border-t border-[var(--border)] pt-3">
              <div className="space-y-1.5">
                <Label>CIDR</Label>
                <Input placeholder="10.0.0.0/24" value={subnetForm.cidr} onChange={(e) => setSubnetForm((f) => ({ ...f, cidr: e.target.value }))} className="w-40" />
              </div>
              <div className="space-y-1.5">
                <Label>Gateway</Label>
                <Input placeholder="10.0.0.1" value={subnetForm.gateway} onChange={(e) => setSubnetForm((f) => ({ ...f, gateway: e.target.value }))} className="w-32" />
              </div>
              <Button loading={createSubnet.isPending} disabled={!subnetForm.cidr} onClick={() => createSubnet.mutate()}>
                <Plus className="h-3.5 w-3.5" /> Add subnet
              </Button>
            </div>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader><CardTitle className="text-sm">Controllers</CardTitle></CardHeader>
        <CardContent className="space-y-3">
          <p className="text-xs text-[var(--text-muted)]">A controller is what makes an EVPN/BGP zone actually exchange routes — a zone without one is configured but inert.</p>
          {controllersQuery.isError ? (
            <ErrorState onRetry={controllersQuery.refetch} />
          ) : controllersQuery.isLoading ? (
            <Skeleton className="h-16" />
          ) : (
            <div className="space-y-1.5">
              {(controllersQuery.data ?? []).map((c) => (
                <div key={c.controller} className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-[var(--border)] px-3 py-2 text-sm transition-colors animate-in fade-in slide-in-from-top-1 duration-200 hover:bg-[var(--bg-surface-hover)]">
                  <div className="flex items-center gap-2">
                    <span className="font-medium">{c.controller}</span>
                    <Badge>{c.type}</Badge>
                  </div>
                  <Button size="icon" variant="ghost-danger" aria-label={`Delete controller ${c.controller}`} onClick={() => removeController(c.controller)}>
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                </div>
              ))}
              {controllersQuery.data?.length === 0 && <p className="text-sm text-[var(--text-muted)]">No SDN controllers configured.</p>}
            </div>
          )}
          <div className="flex flex-wrap items-start gap-2 border-t border-[var(--border)] pt-3">
            <div className="space-y-1.5">
              <Label>Controller name</Label>
              <Input value={controllerForm.controller} onChange={(e) => setControllerForm((f) => ({ ...f, controller: e.target.value }))} className="w-32" />
            </div>
            <div className="space-y-1.5">
              <Label>Type</Label>
              <Select value={controllerForm.type} onValueChange={(v) => setControllerForm((f) => ({ ...f, type: v }))}>
                <SelectTrigger className="w-32"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="evpn">EVPN</SelectItem>
                  <SelectItem value="bgp">BGP</SelectItem>
                  <SelectItem value="faucet">Faucet</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label>Extra fields (key=value per line)</Label>
              <Textarea
                placeholder={"asn=65000\npeers=10.0.0.1,10.0.0.2"}
                value={controllerForm.extra}
                onChange={(e) => setControllerForm((f) => ({ ...f, extra: e.target.value }))}
                className="w-64"
                rows={2}
              />
            </div>
            {/* Invisible label spacer — matches every other column's Label-then-control
                stack so the button sits at the same top edge as the inputs beside it,
                instead of jumping to the row's vertical center once the textarea makes
                this row taller than a single-line field. */}
            <div className="space-y-1.5">
              <Label className="invisible" aria-hidden>Add</Label>
              <Button loading={createController.isPending} disabled={!controllerForm.controller} onClick={() => createController.mutate()}>
                <Plus className="h-3.5 w-3.5" /> Add controller
              </Button>
            </div>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle className="text-sm">IPAM</CardTitle></CardHeader>
        <CardContent className="space-y-3">
          <p className="text-xs text-[var(--text-muted)]">Where PVE tracks subnet/IP allocations — its own tracking ("pve"), or an external system.</p>
          {ipamsQuery.isError ? (
            <ErrorState onRetry={ipamsQuery.refetch} />
          ) : ipamsQuery.isLoading ? (
            <Skeleton className="h-16" />
          ) : (
            <div className="space-y-1.5">
              {(ipamsQuery.data ?? []).map((i) => (
                <div key={i.ipam} className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-[var(--border)] px-3 py-2 text-sm transition-colors animate-in fade-in slide-in-from-top-1 duration-200 hover:bg-[var(--bg-surface-hover)]">
                  <div className="flex items-center gap-2">
                    <span className="font-medium">{i.ipam}</span>
                    <Badge>{i.type}</Badge>
                  </div>
                  <Button size="icon" variant="ghost-danger" aria-label={`Delete IPAM ${i.ipam}`} onClick={() => removeIpam(i.ipam)}>
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                </div>
              ))}
              {ipamsQuery.data?.length === 0 && <p className="text-sm text-[var(--text-muted)]">No IPAM plugins configured.</p>}
            </div>
          )}
          <div className="flex flex-wrap items-start gap-2 border-t border-[var(--border)] pt-3">
            <div className="space-y-1.5">
              <Label>IPAM name</Label>
              <Input value={ipamForm.ipam} onChange={(e) => setIpamForm((f) => ({ ...f, ipam: e.target.value }))} className="w-32" />
            </div>
            <div className="space-y-1.5">
              <Label>Type</Label>
              <Select value={ipamForm.type} onValueChange={(v) => setIpamForm((f) => ({ ...f, type: v }))}>
                <SelectTrigger className="w-32"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="netbox">Netbox</SelectItem>
                  <SelectItem value="phpipam">phpIPAM</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label>Extra fields (key=value per line)</Label>
              <Textarea
                placeholder={"url=https://netbox.example.com\ntoken=..."}
                value={ipamForm.extra}
                onChange={(e) => setIpamForm((f) => ({ ...f, extra: e.target.value }))}
                className="w-64"
                rows={2}
              />
            </div>
            <div className="space-y-1.5">
              <Label className="invisible" aria-hidden>Add</Label>
              <Button loading={createIpam.isPending} disabled={!ipamForm.ipam} onClick={() => createIpam.mutate()}>
                <Plus className="h-3.5 w-3.5" /> Add IPAM
              </Button>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

function ClusterNodesPanel({ base, connId }: { base: string; connId: string }) {
  const queryClient = useQueryClient()
  const confirm = useConfirm()
  const nodesQuery = useQuery({ queryKey: ["cluster-config-nodes", connId], queryFn: () => api.get<ClusterConfigNode[]>(`${base}/cluster/config/nodes`) })
  const [joinInfo, setJoinInfo] = useState<ClusterJoinInfo | null>(null)
  const fetchJoinInfo = useMutation({
    mutationFn: () => api.get<ClusterJoinInfo>(`${base}/cluster/config/join`),
    onSuccess: (info) => setJoinInfo(info),
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "This connection isn't part of a cluster yet"),
  })

  const [clusterName, setClusterName] = useState("")
  const createCluster = useMutation({
    mutationFn: () => api.post(`${base}/cluster/config`, { clusterName }),
    onSuccess: () => {
      toast.success(`Cluster "${clusterName}" created`)
      queryClient.invalidateQueries({ queryKey: ["cluster-config-nodes", connId] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to create cluster"),
  })

  const [joinForm, setJoinForm] = useState({ hostname: "", fingerprint: "", password: "" })
  const joinCluster = useMutation({
    mutationFn: () => api.post(`${base}/cluster/config/join`, joinForm),
    onSuccess: () => {
      toast.success("Join started — this node restarts its cluster services")
      setJoinForm({ hostname: "", fingerprint: "", password: "" })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Join failed"),
  })

  const removeNode = useMutation({
    mutationFn: (node: string) => api.delete(`${base}/nodes/${encodeURIComponent(node)}/cluster-membership`),
    onSuccess: () => {
      toast.success("Node removed from cluster")
      queryClient.invalidateQueries({ queryKey: ["cluster-config-nodes", connId] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to remove node — it must be offline first"),
  })

  async function confirmRemoveNode(node: string) {
    const ok = await confirm({
      title: `Remove "${node}" from the cluster?`,
      description: "Only safe for an already-offline/decommissioned node. PVE refuses this for a live member.",
      confirmLabel: "Remove node",
    })
    if (ok) removeNode.mutate(node)
  }

  async function confirmJoinCluster() {
    const ok = await confirm({
      title: "Join this cluster?",
      description: "This connection restarts its cluster services and merges into the target cluster's corosync config. Only do this on a fresh, standalone node.",
      confirmLabel: "Join cluster",
    })
    if (ok) joinCluster.mutate()
  }

  return (
    <div className="space-y-4">
      <Card>
        <CardHeader><CardTitle className="text-sm">Members</CardTitle></CardHeader>
        <CardContent className="space-y-1.5">
          {nodesQuery.isLoading ? (
            <Skeleton className="h-12" />
          ) : nodesQuery.isError || (nodesQuery.data ?? []).length === 0 ? (
            // PVE errors this same call on a standalone node (no
            // /etc/pve/corosync.conf to read) — indistinguishable, from here,
            // from the connection just being unreachable, so both read as
            // this one message rather than a scary "couldn't load" card with
            // a retry that can't fix "there's no cluster to list".
            <p className="text-sm text-[var(--text-muted)]">Not part of a cluster (standalone node), or this connection can't be reached.</p>
          ) : (
            (nodesQuery.data ?? []).map((n) => (
              <div key={n.name} className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-[var(--border)] px-3 py-2 text-sm transition-colors animate-in fade-in slide-in-from-top-1 duration-200 hover:bg-[var(--bg-surface-hover)]">
                <span className="font-medium">{n.name}</span>
                <div className="flex items-center gap-2">
                  {n.nodeid !== undefined && <Badge>nodeid {n.nodeid}</Badge>}
                  <Button size="icon" variant="ghost-danger" aria-label={`Remove ${n.name}`} onClick={() => confirmRemoveNode(n.name)}>
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                </div>
              </div>
            ))
          )}
        </CardContent>
      </Card>

      <div className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader><CardTitle className="text-sm">Create a cluster</CardTitle></CardHeader>
          <CardContent className="space-y-3">
            <p className="text-xs text-[var(--text-muted)]">Turns this standalone connection into a one-node cluster other nodes can join.</p>
            <div className="flex gap-2">
              <Input placeholder="cluster name" value={clusterName} onChange={(e) => setClusterName(e.target.value)} />
              <Button disabled={!clusterName || createCluster.isPending} onClick={() => createCluster.mutate()}>
                Create cluster
              </Button>
            </div>
            <div className="border-t border-[var(--border)] pt-3">
              <Button size="sm" variant="secondary" disabled={fetchJoinInfo.isPending} onClick={() => fetchJoinInfo.mutate()}>
                Get join info for this cluster
              </Button>
              {joinInfo && (
                <div className="mt-2 space-y-1 rounded-md border border-[var(--border)] bg-[var(--bg-muted)] p-2 text-xs">
                  <p className="break-all"><span className="text-[var(--text-muted)]">Fingerprint:</span> <span className="font-mono">{joinInfo.fingerprint}</span></p>
                  <p className="text-[var(--text-muted)]">Use this cluster's host/IP + the fingerprint above from the joining node's "Join a cluster" form.</p>
                </div>
              )}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader><CardTitle className="text-sm">Join a cluster</CardTitle></CardHeader>
          <CardContent className="space-y-3">
            <p className="text-xs text-[var(--text-muted)]">Makes this (standalone) connection join an existing cluster reachable at the address below.</p>
            <div className="space-y-1.5">
              <Label>Cluster host/IP</Label>
              <Input value={joinForm.hostname} onChange={(e) => setJoinForm((f) => ({ ...f, hostname: e.target.value }))} />
            </div>
            <div className="space-y-1.5">
              <Label>Fingerprint</Label>
              <Input className="font-mono text-xs" value={joinForm.fingerprint} onChange={(e) => setJoinForm((f) => ({ ...f, fingerprint: e.target.value }))} />
            </div>
            <div className="space-y-1.5">
              <Label>Cluster root@pam password</Label>
              <Input type="password" value={joinForm.password} onChange={(e) => setJoinForm((f) => ({ ...f, password: e.target.value }))} />
            </div>
            <Button
              size="sm"
              variant="destructive"
              disabled={!joinForm.hostname || !joinForm.fingerprint || !joinForm.password || joinCluster.isPending}
              onClick={() => void confirmJoinCluster()}
            >
              Join cluster
            </Button>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}

// AccessPanel is read-only: it shows Proxmox's own users/roles/ACLs/realms
// for visibility, separate from this app's local accounts (Settings ->
// Users). Creating/editing PVE users, tokens, or ACLs stays in Proxmox's UI.
function AccessPanel({ base, connId }: { base: string; connId: string }) {
  const usersQuery = useQuery({ queryKey: ["pve-access-users", connId], queryFn: () => api.get<AccessUser[]>(`${base}/access/users`) })
  const rolesQuery = useQuery({ queryKey: ["pve-access-roles", connId], queryFn: () => api.get<AccessRole[]>(`${base}/access/roles`) })
  const aclQuery = useQuery({ queryKey: ["pve-access-acl", connId], queryFn: () => api.get<AccessACLEntry[]>(`${base}/access/acl`) })
  const domainsQuery = useQuery({ queryKey: ["pve-access-domains", connId], queryFn: () => api.get<AccessDomain[]>(`${base}/access/domains`) })

  // Users and ACL are the two lists here that scale with the org, not with
  // how much an admin has manually configured (an LDAP/AD realm can hand PVE
  // hundreds of users; a per-path ACL is often one row per user per pool).
  // Both paginate progressively ("show more") instead of scraping a fixed
  // max-height scroller, and the search box is always visible once a list is
  // big enough to need it — the old >8-rows reveal hid the escape hatch
  // until the wall was already on screen.
  const ACCESS_PAGE = 20
  const [userQuery, setUserQuery] = useState("")
  const [usersLimit, setUsersLimit] = useState(ACCESS_PAGE)
  const users = (usersQuery.data ?? []).filter(
    (u) => !userQuery || u.userid.toLowerCase().includes(userQuery.toLowerCase()) || u.email?.toLowerCase().includes(userQuery.toLowerCase()),
  )
  const [aclFilter, setAclFilter] = useState("")
  const [aclLimit, setAclLimit] = useState(ACCESS_PAGE)
  const acl = (aclQuery.data ?? []).filter(
    (e) =>
      !aclFilter ||
      [e.path, e.ugid, e.roleid, e.type].some((v) => v?.toLowerCase().includes(aclFilter.toLowerCase())),
  )

  return (
    <div className="space-y-4">
      <Card>
        <CardHeader><CardTitle className="text-sm">Realms</CardTitle></CardHeader>
        <CardContent className="space-y-1.5">
          {domainsQuery.isError ? (
            <ErrorState onRetry={domainsQuery.refetch} />
          ) : domainsQuery.isLoading ? (
            <Skeleton className="h-12" />
          ) : (
            (domainsQuery.data ?? []).map((d) => (
              <div key={d.realm} className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-[var(--border)] px-3 py-2 text-sm transition-colors animate-in fade-in slide-in-from-top-1 duration-200 hover:bg-[var(--bg-surface-hover)]">
                <div className="flex items-center gap-2">
                  <span className="font-medium">{d.realm}</span>
                  <Badge variant="outline">{d.type}</Badge>
                  {d.default === 1 && <Badge variant="info">default</Badge>}
                </div>
                {d.comment && <span className="text-xs text-[var(--text-muted)]">{d.comment}</span>}
              </div>
            ))
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle className="text-sm">Users</CardTitle></CardHeader>
        <CardContent className="space-y-1.5">
          {(usersQuery.data?.length ?? 0) > 8 && (
            <ListSearch value={userQuery} onChange={setUserQuery} placeholder="Search users..." className="mb-2" />
          )}
          {usersQuery.isError ? (
            <ErrorState onRetry={usersQuery.refetch} />
          ) : usersQuery.isLoading ? (
            <Skeleton className="h-12" />
          ) : (usersQuery.data ?? []).length === 0 ? (
            <p className="text-sm text-[var(--text-muted)]">No users found.</p>
          ) : users.length === 0 ? (
            <p className="text-sm text-[var(--text-muted)]">No users match "{userQuery}".</p>
          ) : (
            <div className="space-y-1.5">
              {users.slice(0, usersLimit).map((u) => (
                <div key={u.userid} className="rounded-md border border-[var(--border)] px-3 py-2 text-sm">
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <div className="flex items-center gap-2">
                      <span className="font-medium">{u.userid}</span>
                      {u.enable === 0 && <Badge variant="error">disabled</Badge>}
                    </div>
                    {u.email && <span className="text-xs text-[var(--text-muted)]">{u.email}</span>}
                  </div>
                  {u.tokens && u.tokens.length > 0 && (
                    <div className="mt-1.5 flex flex-wrap gap-1">
                      {u.tokens.map((t) => (
                        <Badge key={t.tokenid} variant="outline" className="font-mono text-[10px]">{t.tokenid}</Badge>
                      ))}
                    </div>
                  )}
                </div>
              ))}
              {users.length > usersLimit && (
                <button
                  className="w-full rounded-md py-1.5 text-center text-xs text-[var(--text-muted)] hover:bg-[var(--bg-surface-hover)] hover:text-[var(--text)]"
                  onClick={() => setUsersLimit((l) => l + 50)}
                >
                  Show {Math.min(50, users.length - usersLimit)} more of {users.length} users
                </button>
              )}
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle className="text-sm">Roles</CardTitle></CardHeader>
        <CardContent>
          {rolesQuery.isError ? (
            <ErrorState onRetry={rolesQuery.refetch} />
          ) : rolesQuery.isLoading ? (
            <Skeleton className="h-12" />
          ) : (
            <div className="flex flex-wrap gap-1.5">
              {(rolesQuery.data ?? []).map((r) => <Badge key={r.roleid} variant="outline">{r.roleid}</Badge>)}
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle className="text-sm">ACL</CardTitle></CardHeader>
        <CardContent className="space-y-1.5">
          {(aclQuery.data?.length ?? 0) > 8 && (
            <ListSearch value={aclFilter} onChange={setAclFilter} placeholder="Search ACL entries..." className="mb-2" />
          )}
          {aclQuery.isError ? (
            <ErrorState onRetry={aclQuery.refetch} />
          ) : aclQuery.isLoading ? (
            <Skeleton className="h-12" />
          ) : (aclQuery.data ?? []).length === 0 ? (
            <p className="text-sm text-[var(--text-muted)]">No ACL entries.</p>
          ) : acl.length === 0 ? (
            <p className="text-sm text-[var(--text-muted)]">No ACL entries match "{aclFilter}".</p>
          ) : (
            <div className="space-y-1.5">
              {acl.slice(0, aclLimit).map((e, i) => (
                <div key={`${e.path}-${e.ugid}-${e.roleid}-${i}`} className="flex flex-wrap items-center gap-2 rounded-md border border-[var(--border)] px-3 py-2 text-sm">
                  <Badge variant="outline">{e.type}</Badge>
                  <span className="font-mono text-xs">{e.ugid}</span>
                  <span className="text-[var(--text-muted)]">on</span>
                  <span className="font-mono text-xs">{e.path}</span>
                  <span className="text-[var(--text-muted)]">→</span>
                  <Badge variant="info">{e.roleid}</Badge>
                </div>
              ))}
              {acl.length > aclLimit && (
                <button
                  className="w-full rounded-md py-1.5 text-center text-xs text-[var(--text-muted)] hover:bg-[var(--bg-surface-hover)] hover:text-[var(--text)]"
                  onClick={() => setAclLimit((l) => l + 50)}
                >
                  Show {Math.min(50, acl.length - aclLimit)} more of {acl.length} entries
                </button>
              )}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
