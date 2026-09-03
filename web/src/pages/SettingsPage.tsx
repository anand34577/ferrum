import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { ExternalLink, Settings as SettingsIcon } from "lucide-react"
import { useEffect, useState } from "react"
import { toast } from "sonner"
import { AppearanceCard } from "@/components/settings/AppearanceCard"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { ErrorState } from "@/components/ui/error-state"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { PageHeader } from "@/components/ui/page-header"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { api, ApiError, type ClusterResource, type ConnectionInventory, type DatacenterOptions, type Subscription } from "@/lib/api"

export function SettingsPage() {
  const { data: inventory, isLoading, isError, refetch } = useQuery({
    queryKey: ["inventory"],
    queryFn: () => api.get<ConnectionInventory[]>("/inventory/"),
  })
  const connections = inventory ?? []
  const [connId, setConnId] = useState("")

  useEffect(() => {
    if (!connId && connections.length > 0) setConnId(connections[0].connectionId)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connections.length])

  return (
    <div className="space-y-4">
      <PageHeader
        title="Settings"
        description="Appearance, datacenter-wide options and support status, per connection."
        icon={SettingsIcon}
      />

      <AppearanceCard />

      {isError ? (
        <ErrorState title="Couldn't load connections" onRetry={refetch} />
      ) : isLoading ? (
        <Skeleton className="h-28 max-w-xs" />
      ) : (
        <div className="max-w-xs space-y-1.5">
          <Label>Connection</Label>
          <Select value={connId} onValueChange={setConnId}>
            <SelectTrigger>
              <SelectValue placeholder="Select a connection..." />
            </SelectTrigger>
            <SelectContent>
              {connections.map((c) => (
                <SelectItem key={c.connectionId} value={c.connectionId}>{c.name}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      )}

      {connId && (
        <>
          <DatacenterOptionsCard connId={connId} />
          <SubscriptionCard connId={connId} nodes={(connections.find((c) => c.connectionId === connId)?.resources ?? []).filter((r) => r.type === "node")} />
        </>
      )}

      <AboutCard />
    </div>
  )
}

function AboutCard() {
  return (
    <Card>
      <CardHeader>
        <CardTitle>About Ferrum</CardTitle>
        <CardDescription>Fleet control for Proxmox VE — open source, self-hosted.</CardDescription>
      </CardHeader>
      <CardContent>
        <a
          href="https://github.com/anand34577/ferrum"
          target="_blank"
          rel="noreferrer"
          className="inline-flex items-center gap-2 rounded-md border border-[var(--border)] bg-[var(--bg-surface)] px-3 py-2 text-sm font-medium transition-colors hover:border-[var(--border-strong)] hover:bg-[var(--bg-surface-hover)]"
        >
          github.com/anand34577/ferrum
          <ExternalLink className="h-3.5 w-3.5 text-[var(--text-muted)]" aria-hidden />
        </a>
      </CardContent>
    </Card>
  )
}

function DatacenterOptionsCard({ connId }: { connId: string }) {
  const queryClient = useQueryClient()
  const optionsQuery = useQuery({
    queryKey: ["datacenter-options", connId],
    queryFn: () => api.get<DatacenterOptions>(`/connections/${connId}/cluster/options`),
  })
  const [form, setForm] = useState<DatacenterOptions>({})

  useEffect(() => {
    setForm(optionsQuery.data ?? {})
  }, [optionsQuery.data])

  const save = useMutation({
    mutationFn: () => api.put(`/connections/${connId}/cluster/options`, form),
    onSuccess: () => {
      toast.success("Datacenter options saved")
      queryClient.invalidateQueries({ queryKey: ["datacenter-options", connId] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to save datacenter options"),
  })

  return (
    <Card>
      <CardHeader>
        <CardTitle>Datacenter options</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        {optionsQuery.isError ? (
          <ErrorState title="Couldn't load datacenter options" onRetry={optionsQuery.refetch} />
        ) : optionsQuery.isLoading ? (
          <div className="space-y-3" aria-busy>
            <Skeleton className="h-9 w-full" />
            <Skeleton className="h-9 w-full" />
            <Skeleton className="h-9 w-2/3" />
          </div>
        ) : (
          <>
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label>Keyboard layout</Label>
                <Input value={form.keyboard ?? ""} onChange={(e) => setForm({ ...form, keyboard: e.target.value })} placeholder="en-us" />
              </div>
              <div className="space-y-1.5">
                <Label>Console viewer</Label>
                <Select value={form.console ?? ""} onValueChange={(v) => setForm({ ...form, console: v })}>
                  <SelectTrigger>
                    <SelectValue placeholder="Default" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="html5">HTML5 (noVNC)</SelectItem>
                    <SelectItem value="xtermjs">xterm.js</SelectItem>
                    <SelectItem value="vv">SPICE</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1.5">
                <Label>HTTP proxy</Label>
                <Input value={form.http_proxy ?? ""} onChange={(e) => setForm({ ...form, http_proxy: e.target.value })} placeholder="http://proxy:8080" />
              </div>
              <div className="space-y-1.5">
                <Label>Notification email from</Label>
                <Input value={form.email_from ?? ""} onChange={(e) => setForm({ ...form, email_from: e.target.value })} placeholder="proxmox@example.com" />
              </div>
              <div className="space-y-1.5">
                <Label>MAC address prefix</Label>
                <Input value={form.mac_prefix ?? ""} onChange={(e) => setForm({ ...form, mac_prefix: e.target.value })} placeholder="BC:24:11" />
              </div>
            </div>
            <div className="space-y-1.5">
              <Label>Description (cluster-wide MOTD)</Label>
              <textarea
                className="w-full rounded-md border border-[var(--border)] bg-[var(--bg-surface)] px-3 py-2 text-sm"
                rows={3}
                value={form.description ?? ""}
                onChange={(e) => setForm({ ...form, description: e.target.value })}
              />
            </div>
            <Button size="sm" loading={save.isPending} onClick={() => save.mutate()}>
              Save options
            </Button>
          </>
        )}
      </CardContent>
    </Card>
  )
}

function SubscriptionCard({ connId, nodes }: { connId: string; nodes: ClusterResource[] }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Support status</CardTitle>
      </CardHeader>
      <CardContent className="space-y-2">
        {nodes.length === 0 && <p className="text-sm text-[var(--text-muted)]">No nodes reported for this connection.</p>}
        {nodes.map((n) => (
          <NodeSubscriptionRow key={n.node} connId={connId} node={n.node} />
        ))}
      </CardContent>
    </Card>
  )
}

function NodeSubscriptionRow({ connId, node }: { connId: string; node: string }) {
  const subQuery = useQuery({
    queryKey: ["subscription", connId, node],
    queryFn: () => api.get<Subscription>(`/connections/${connId}/nodes/${node}/subscription`),
    retry: false,
  })

  return (
    <div className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-[var(--border)] px-3 py-2 text-sm">
      <div className="min-w-0">
        <p className="truncate font-medium">{node}</p>
        {subQuery.data?.productname && <p className="text-xs text-[var(--text-muted)]">{subQuery.data.productname}</p>}
      </div>
      {subQuery.isLoading ? (
        <span className="text-xs text-[var(--text-muted)]">Checking...</span>
      ) : subQuery.data ? (
        <Badge variant={subQuery.data.status === "Active" ? "ok" : "default"}>{subQuery.data.status}</Badge>
      ) : (
        <span className="text-xs text-[var(--text-muted)]">Unavailable</span>
      )}
    </div>
  )
}
