import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Plus, Shield, Trash2 } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"
import { FirewallRulesPanel } from "@/components/firewall/FirewallRulesPanel"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { EmptyState } from "@/components/ui/empty-state"
import { ErrorState } from "@/components/ui/error-state"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { PageHeader } from "@/components/ui/page-header"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Hint } from "@/components/ui/tooltip"
import {
  api,
  ApiError,
  type ClusterResource,
  type ConnectionInventory,
  type FirewallAlias,
  type FirewallIPSet,
} from "@/lib/api"

function ClusterFirewallSwitch({ connId }: { connId: string }) {
  const queryClient = useQueryClient()
  const confirm = useConfirm()
  const optionsQuery = useQuery({
    queryKey: ["fw-options", connId],
    queryFn: () => api.get<{ enable: number }>(`/connections/${connId}/cluster/firewall/options`),
    retry: false,
  })
  const toggle = useMutation({
    mutationFn: (enable: boolean) => api.put(`/connections/${connId}/cluster/firewall/options`, { enable }),
    onSuccess: (_data, enable) => {
      toast.success(`Cluster firewall ${enable ? "enabled" : "disabled"}`)
      queryClient.invalidateQueries({ queryKey: ["fw-options", connId] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to update firewall options"),
  })

  // Turning the firewall OFF removes a live protection — worth one confirm;
  // enabling needs none.
  async function handleToggle(enable: boolean) {
    if (!enable) {
      const ok = await confirm({
        title: "Disable the cluster firewall?",
        description: "Cluster-wide rules stop being enforced on every node until it's turned back on.",
        confirmLabel: "Disable",
      })
      if (!ok) return
    }
    toggle.mutate(enable)
  }

  if (optionsQuery.isError) {
    return <p className="text-xs text-[var(--text-muted)]">Couldn't load firewall status.</p>
  }
  const enabled = optionsQuery.data?.enable === 1

  return (
    <label className="flex items-center gap-2 text-sm">
      <Switch checked={enabled} onCheckedChange={(v) => void handleToggle(v)} disabled={toggle.isPending} />
      Cluster firewall {enabled ? "enabled" : "disabled"}
    </label>
  )
}

function AliasesAndIPSets({ connId }: { connId: string }) {
  const queryClient = useQueryClient()
  const confirm = useConfirm()
  const base = `/connections/${connId}/cluster/firewall`
  const aliasesQuery = useQuery({ queryKey: ["fw-aliases", connId], queryFn: () => api.get<FirewallAlias[]>(`${base}/aliases`), retry: false })
  const ipsetsQuery = useQuery({ queryKey: ["fw-ipsets", connId], queryFn: () => api.get<FirewallIPSet[]>(`${base}/ipsets`), retry: false })

  const [aliasForm, setAliasForm] = useState({ name: "", cidr: "", comment: "" })
  const [showAliasForm, setShowAliasForm] = useState(false)
  // A malformed CIDR (bad prefix length, non-numeric octet, etc.) otherwise
  // only surfaces after the API round-trip rejects it — this catches the
  // common typo cases locally so the field shows why, not just a toast.
  const cidrInvalid = aliasForm.cidr.trim() !== "" && !/^(\d{1,3}\.){3}\d{1,3}(\/(\d|[12]\d|3[0-2]))?$/.test(aliasForm.cidr.trim())
  const addAlias = useMutation({
    mutationFn: () => api.post(`${base}/aliases`, aliasForm),
    onSuccess: () => {
      toast.success("Alias added")
      queryClient.invalidateQueries({ queryKey: ["fw-aliases", connId] })
      setAliasForm({ name: "", cidr: "", comment: "" })
      setShowAliasForm(false)
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to add alias"),
  })
  const deleteAlias = useMutation({
    mutationFn: (aliasName: string) => api.delete(`${base}/aliases/${encodeURIComponent(aliasName)}`),
    onSuccess: () => {
      toast.success("Alias deleted")
      queryClient.invalidateQueries({ queryKey: ["fw-aliases", connId] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to delete alias"),
  })

  const [ipsetForm, setIpsetForm] = useState({ name: "", comment: "" })
  const [showIPSetForm, setShowIPSetForm] = useState(false)
  const addIPSet = useMutation({
    mutationFn: () => api.post(`${base}/ipsets`, ipsetForm),
    onSuccess: () => {
      toast.success("IP set created")
      queryClient.invalidateQueries({ queryKey: ["fw-ipsets", connId] })
      setIpsetForm({ name: "", comment: "" })
      setShowIPSetForm(false)
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to create IP set"),
  })
  const deleteIPSet = useMutation({
    mutationFn: (setName: string) => api.delete(`${base}/ipsets/${encodeURIComponent(setName)}`),
    onSuccess: () => {
      toast.success("IP set deleted")
      queryClient.invalidateQueries({ queryKey: ["fw-ipsets", connId] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to delete IP set"),
  })

  async function removeAlias(name: string) {
    const ok = await confirm({
      title: `Delete alias "${name}"?`,
      description: "Rules that reference this alias will fail to resolve until you replace it.",
      confirmLabel: "Delete alias",
    })
    if (ok) deleteAlias.mutate(name)
  }

  async function removeIPSet(name: string) {
    const ok = await confirm({
      title: `Delete IP set "${name}"?`,
      description: "Rules that reference this set will fail to resolve until you replace it. Its member addresses are deleted with it.",
      confirmLabel: "Delete IP set",
    })
    if (ok) deleteIPSet.mutate(name)
  }

  return (
    <Tabs defaultValue="aliases">
      <TabsList>
        <TabsTrigger value="aliases">Aliases</TabsTrigger>
        <TabsTrigger value="ipsets">IP Sets</TabsTrigger>
      </TabsList>

      <TabsContent value="aliases" className="space-y-3">
        <Button size="sm" variant="secondary" onClick={() => setShowAliasForm((s) => !s)}>
          <Plus className="h-3.5 w-3.5" /> Add alias
        </Button>
        {showAliasForm && (
          <div className="grid grid-cols-1 gap-3 rounded-md border border-[var(--border)] p-3 sm:grid-cols-3">
            <div className="space-y-1.5">
              <Label htmlFor="alias-name">Name</Label>
              <Input id="alias-name" placeholder="internal-net" value={aliasForm.name} onChange={(e) => setAliasForm({ ...aliasForm, name: e.target.value })} />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="alias-cidr">CIDR or IP</Label>
              <Input id="alias-cidr" placeholder="10.0.0.0/24" value={aliasForm.cidr} onChange={(e) => setAliasForm({ ...aliasForm, cidr: e.target.value })} />
              {cidrInvalid && <p className="text-xs text-[var(--status-error)]">Enter an IPv4 address, optionally with a /0–/32 prefix.</p>}
            </div>
            <div className="flex items-end">
              <Button size="sm" disabled={!aliasForm.name || !aliasForm.cidr || cidrInvalid || addAlias.isPending} loading={addAlias.isPending} onClick={() => addAlias.mutate()}>
                Add alias
              </Button>
            </div>
          </div>
        )}
        <div className="space-y-1.5">
          {aliasesQuery.isLoading && <Skeleton className="h-9" />}
          {aliasesQuery.isError && <ErrorState title="Couldn't load aliases" onRetry={() => void aliasesQuery.refetch()} className="py-6" />}
          {aliasesQuery.data?.map((a) => (
            <div key={a.name} className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-[var(--border)] px-3 py-2 text-sm">
              <span className="min-w-0 break-all">
                <span className="font-medium">{a.name}</span>{" "}
                <span className="font-mono text-xs text-[var(--text-muted)]">{a.cidr}</span>
              </span>
              <Hint label="Delete alias">
                <Button
                  size="icon"
                  variant="ghost-danger"
                  className="shrink-0"
                  aria-label={`Delete alias ${a.name}`}
                  onClick={() => removeAlias(a.name)}
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </Button>
              </Hint>
            </div>
          ))}
          {aliasesQuery.data?.length === 0 && <p className="text-sm text-[var(--text-muted)]">No aliases defined.</p>}
        </div>
      </TabsContent>

      <TabsContent value="ipsets" className="space-y-3">
        <Button size="sm" variant="secondary" onClick={() => setShowIPSetForm((s) => !s)}>
          <Plus className="h-3.5 w-3.5" /> Create IP set
        </Button>
        {showIPSetForm && (
          <div className="grid grid-cols-1 gap-3 rounded-md border border-[var(--border)] p-3 sm:grid-cols-3">
            <div className="space-y-1.5">
              <Label htmlFor="ipset-name">Name</Label>
              <Input id="ipset-name" placeholder="admin-workstations" value={ipsetForm.name} onChange={(e) => setIpsetForm({ ...ipsetForm, name: e.target.value })} />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="ipset-comment">Comment (optional)</Label>
              <Input id="ipset-comment" value={ipsetForm.comment} onChange={(e) => setIpsetForm({ ...ipsetForm, comment: e.target.value })} />
            </div>
            <div className="flex items-end">
              <Button size="sm" disabled={!ipsetForm.name || addIPSet.isPending} loading={addIPSet.isPending} onClick={() => addIPSet.mutate()}>
                Create IP set
              </Button>
            </div>
          </div>
        )}
        <div className="space-y-1.5">
          {ipsetsQuery.isLoading && <Skeleton className="h-9" />}
          {ipsetsQuery.isError && <ErrorState title="Couldn't load IP sets" onRetry={() => void ipsetsQuery.refetch()} className="py-6" />}
          {ipsetsQuery.data?.map((s) => (
            <div key={s.name} className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-[var(--border)] px-3 py-2 text-sm">
              <span className="min-w-0 break-words">
                <span className="font-medium">{s.name}</span>{" "}
                {s.comment && <span className="text-xs text-[var(--text-muted)]">{s.comment}</span>}
              </span>
              <Hint label="Delete IP set">
                <Button
                  size="icon"
                  variant="ghost-danger"
                  className="shrink-0"
                  aria-label={`Delete IP set ${s.name}`}
                  onClick={() => removeIPSet(s.name)}
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </Button>
              </Hint>
            </div>
          ))}
          {ipsetsQuery.data?.length === 0 && <p className="text-sm text-[var(--text-muted)]">No IP sets defined.</p>}
        </div>
      </TabsContent>
    </Tabs>
  )
}

function ConnectionFirewall({ connId, name, nodes }: { connId: string; name: string; nodes: ClusterResource[] }) {
  const [scope, setScope] = useState<"cluster" | "node">("cluster")
  const [node, setNode] = useState(nodes[0]?.node ?? "")

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between space-y-0">
        <CardTitle className="flex items-center gap-2">
          <Shield className="h-4 w-4" /> {name}
        </CardTitle>
        <ClusterFirewallSwitch connId={connId} />
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="flex flex-wrap items-center gap-2">
          <Select value={scope} onValueChange={(v) => setScope(v as "cluster" | "node")}>
            <SelectTrigger className="w-40" aria-label="Firewall scope"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="cluster">Cluster-wide</SelectItem>
              <SelectItem value="node">Per-node</SelectItem>
            </SelectContent>
          </Select>
          {scope === "node" && nodes.length > 0 && (
            <Select value={node} onValueChange={setNode}>
              <SelectTrigger className="w-40" aria-label="Node"><SelectValue /></SelectTrigger>
              <SelectContent>
                {nodes.map((n) => (
                  <SelectItem key={n.node} value={n.node}>{n.node}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
        </div>

        {scope === "cluster" ? (
          <div className="space-y-6">
            <FirewallRulesPanel basePath={`/connections/${connId}/cluster/firewall`} queryKey={["fw-rules", connId, "cluster"]} />
            <AliasesAndIPSets connId={connId} />
          </div>
        ) : node ? (
          <FirewallRulesPanel basePath={`/connections/${connId}/nodes/${node}/firewall`} queryKey={["fw-rules", connId, "node", node]} />
        ) : (
          <p className="text-sm text-[var(--text-muted)]">No nodes reported for this connection.</p>
        )}
      </CardContent>
    </Card>
  )
}

export function FirewallPage() {
  const queryClient = useQueryClient()
  const { data: inventory, isLoading, isError, refetch, isRefetching } = useQuery({
    queryKey: ["inventory"],
    queryFn: () => api.get<ConnectionInventory[]>("/inventory/"),
    refetchInterval: 30_000,
  })
  const connections = inventory ?? []

  return (
    <div className="space-y-4">
      <PageHeader
        title="Firewall"
        description="Cluster and per-node firewall rules, aliases, and IP sets. Manage a guest's own rules from its detail dialog in Inventory."
        icon={Shield}
        onRefresh={() => void queryClient.invalidateQueries()}
        refreshing={isRefetching}
      />

      {isError ? (
        <ErrorState title="Couldn't load connections" onRetry={refetch} />
      ) : isLoading ? (
        <div className="space-y-3" aria-busy>
          <Skeleton className="h-40" />
          <Skeleton className="h-40" />
        </div>
      ) : connections.length === 0 ? (
        <EmptyState
          icon={Shield}
          title="No connections configured yet"
          description="Firewall management works per Proxmox connection — add one first."
        />
      ) : (
        connections.map((c) => (
          <ConnectionFirewall
            key={c.connectionId}
            connId={c.connectionId}
            name={c.name}
            nodes={(c.resources ?? []).filter((r) => r.type === "node")}
          />
        ))
      )}
    </div>
  )
}
