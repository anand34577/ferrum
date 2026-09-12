import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { AlertTriangle, ChevronRight, ExternalLink, Settings as SettingsIcon } from "lucide-react"
import { useEffect, useState } from "react"
import { Link } from "react-router-dom"
import { toast } from "sonner"
import { AgentSettingsCard } from "@/components/settings/AgentSettingsCard"
import { AIProvidersCard } from "@/components/settings/AIProvidersCard"
import { AppearanceCard } from "@/components/settings/AppearanceCard"
import { DefaultPreferencesCard } from "@/components/settings/DefaultPreferencesCard"
import { DigestSettingsCard } from "@/components/settings/DigestSettingsCard"
import { LifecycleSettingsCard } from "@/components/settings/LifecycleSettingsCard"
import { NotificationsSettingsCard } from "@/components/settings/NotificationsSettingsCard"
import { OIDCSettingsCard } from "@/components/settings/OIDCSettingsCard"
import { SecuritySettingsCard } from "@/components/settings/SecuritySettingsCard"
import { SystemSettingsCard } from "@/components/settings/SystemSettingsCard"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { ErrorState } from "@/components/ui/error-state"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { PageHeader } from "@/components/ui/page-header"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { Textarea } from "@/components/ui/textarea"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { api, ApiError, type ClusterResource, type ConnectionInventory, type DatacenterOptions, type Subscription } from "@/lib/api"
import { dangerousExtraKeys, parseExtraLines } from "@/lib/utils"

// Options DatacenterOptionsForm already has a named field for — sent via
// the typed fields, not the raw "extra" editor, so they don't show up twice.
const NAMED_OPTION_KEYS = new Set(["keyboard", "console", "http_proxy", "email_from", "mac_prefix", "description"])

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
      <DefaultPreferencesCard />
      <SecuritySettingsCard />
      <SystemSettingsCard />
      <NotificationsSettingsCard />
      <DigestSettingsCard />
      <LifecycleSettingsCard />

      <Link
        to="/alerts"
        className="flex items-center justify-between gap-2 rounded-xl border border-[var(--border)] bg-[var(--bg-surface)] px-4 py-3 text-sm shadow-card transition-colors hover:border-[var(--border-strong)] hover:bg-[var(--bg-surface-hover)]"
      >
        <span className="flex items-center gap-2">
          <AlertTriangle className="h-4 w-4 text-[var(--text-muted)]" />
          <span>
            <span className="font-medium">Alert thresholds &amp; rules</span>
            <span className="ml-2 text-[var(--text-muted)]">— create, edit, and delete threshold rules on the Alerts page</span>
          </span>
        </span>
        <ChevronRight className="h-4 w-4 shrink-0 text-[var(--text-muted)]" />
      </Link>

      <OIDCSettingsCard />
      <AIProvidersCard />
      <AgentSettingsCard />

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
          <DatacenterOptionsSection connId={connId} />
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

function DatacenterOptionsForm({
  connId,
  initial,
}: {
  connId: string
  initial: DatacenterOptions
}) {
  const queryClient = useQueryClient()
  const confirm = useConfirm()
  const [form, setForm] = useState<DatacenterOptions>(initial)
  const [extraText, setExtraText] = useState("")

  const save = useMutation({
    mutationFn: () => api.put(`/connections/${connId}/cluster/options`, { ...form, raw: undefined, extra: parseExtraLines(extraText) }),
    onSuccess: () => {
      toast.success("Datacenter options saved")
      setExtraText("")
      queryClient.invalidateQueries({ queryKey: ["datacenter-options", connId] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to save datacenter options"),
  })

  async function submitSave() {
    const keys = dangerousExtraKeys(parseExtraLines(extraText))
    if (keys.length > 0) {
      const ok = await confirm({
        title: "Advanced option runs on the host",
        description: `"${keys.join(", ")}" ${keys.length > 1 ? "are" : "is"} not sandboxed like normal datacenter config — it can run arbitrary code on cluster nodes. Only continue if you trust this configuration.`,
        confirmLabel: "Save anyway",
      })
      if (!ok) return
    }
    save.mutate()
  }

  // Every option PVE reports that this form doesn't already have a field
  // for — shown read-only so nothing silently disappears from view, with
  // the editor below to change or clear one (PVE deletes an option when
  // it's sent as an empty string).
  const otherOptions = Object.entries(initial.raw ?? {}).filter(([k]) => !NAMED_OPTION_KEYS.has(k))

  return (
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
        <Textarea className="font-sans" rows={3} value={form.description ?? ""} onChange={(e) => setForm({ ...form, description: e.target.value })} />
      </div>
      {otherOptions.length > 0 && (
        <div className="space-y-1.5">
          <Label>Other options set on this cluster</Label>
          <div className="flex flex-wrap gap-1.5">
            {otherOptions.map(([k, v]) => (
              <Badge key={k}>{k}: {String(v)}</Badge>
            ))}
          </div>
        </div>
      )}
      <div className="space-y-1.5">
        <Label>Advanced: additional options (one key=value per line)</Label>
        <Textarea
          className="text-xs"
          rows={2}
          placeholder={"bwlimit=default=10240\nu2f=appid=https://example.com"}
          value={extraText}
          onChange={(e) => setExtraText(e.target.value)}
        />
        <p className="text-xs text-[var(--text-muted)]">
          Sets any datacenter option PVE supports beyond the fields above. Send a key with an empty value (e.g. "bwlimit=") to clear it.
        </p>
      </div>
      <Button size="sm" loading={save.isPending} onClick={() => void submitSave()}>
        Save options
      </Button>
    </>
  )
}

function DatacenterOptionsSection({ connId }: { connId: string }) {
  const optionsQuery = useQuery({
    queryKey: ["datacenter-options", connId],
    queryFn: () => api.get<DatacenterOptions>(`/connections/${connId}/cluster/options`),
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
          <DatacenterOptionsForm
            key={connId + JSON.stringify(optionsQuery.data)}
            connId={connId}
            initial={optionsQuery.data ?? {}}
          />
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
