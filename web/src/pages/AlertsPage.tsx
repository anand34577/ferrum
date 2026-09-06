import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import type { ColumnDef } from "@tanstack/react-table"
import { AlertTriangle, BellOff, ChartPie, Plus, ShieldAlert, Trash2, WifiOff } from "lucide-react"
import { useMemo, useState } from "react"
import { toast } from "sonner"
import { DonutChart } from "@/components/charts/DonutChart"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { StatusDot } from "@/components/ui/status-dot"
import { DataTable } from "@/components/ui/data-table"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { ErrorState } from "@/components/ui/error-state"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { PageHeader } from "@/components/ui/page-header"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Hint } from "@/components/ui/tooltip"
import { api, ApiError, type AlertInstance, type AlertRule, type ConnectionHealth, type ConnectionInventory } from "@/lib/api"

/** How long ago `since` was, as a short "3h 12m" style duration — connection
 * downtime is the one alert-adjacent number worth reading at a glance. */
function ageFrom(iso: string): string {
  const ms = Date.now() - new Date(iso).getTime()
  if (ms < 0) return "0m"
  const mins = Math.floor(ms / 60_000)
  if (mins < 60) return `${mins}m`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours}h ${mins % 60}m`
  const days = Math.floor(hours / 24)
  return `${days}d ${hours % 24}h`
}

const METRIC_LABELS: Record<string, string> = {
  node_cpu: "Node CPU %",
  node_mem: "Node Memory %",
  node_disk: "Node Disk %",
  guest_cpu: "Guest CPU %",
  guest_mem: "Guest Memory %",
  storage_usage: "Storage Pool Usage %",
}

export function AlertsPage() {
  const queryClient = useQueryClient()
  const confirm = useConfirm()
  const [showForm, setShowForm] = useState(false)
  const [form, setForm] = useState({ name: "", metric: "node_cpu", connectionId: "", threshold: "90", severity: "warning" })

  const summaryQuery = useQuery({
    queryKey: ["alerts-summary"],
    queryFn: () => api.get<{ warning: number; critical: number }>("/alerts/summary"),
    refetchInterval: 30_000,
  })
  const activeAlertsQuery = useQuery({
    queryKey: ["alerts", "active"],
    queryFn: () => api.get<AlertInstance[]>("/alerts/?status=active"),
    refetchInterval: 30_000,
  })
  const rulesQuery = useQuery({ queryKey: ["alert-rules"], queryFn: () => api.get<AlertRule[]>("/alert-rules/") })
  const healthQuery = useQuery({
    queryKey: ["connection-health"],
    queryFn: () => api.get<ConnectionHealth[]>("/connection-health"),
    refetchInterval: 30_000,
  })
  const downConnections = (healthQuery.data ?? []).filter((h) => h.status === "down")
  const { data: inventory } = useQuery({ queryKey: ["inventory"], queryFn: () => api.get<ConnectionInventory[]>("/inventory/") })

  const silenceMutation = useMutation({
    mutationFn: (id: string) => api.post(`/alerts/${id}/silence`),
    onSuccess: () => {
      toast.success("Alert silenced")
      queryClient.invalidateQueries({ queryKey: ["alerts"] })
      queryClient.invalidateQueries({ queryKey: ["alerts-summary"] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to silence alert"),
  })

  const createRuleMutation = useMutation({
    mutationFn: () =>
      api.post("/alert-rules/", {
        name: form.name,
        metric: form.metric,
        connectionId: form.connectionId || undefined,
        threshold: Number(form.threshold),
        severity: form.severity,
      }),
    onSuccess: () => {
      toast.success("Alert rule created")
      queryClient.invalidateQueries({ queryKey: ["alert-rules"] })
      setForm({ name: "", metric: "node_cpu", connectionId: "", threshold: "90", severity: "warning" })
      setShowForm(false)
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to create rule"),
  })

  const deleteRuleMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/alert-rules/${id}`),
    onSuccess: () => {
      toast.success("Rule deleted")
      queryClient.invalidateQueries({ queryKey: ["alert-rules"] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to delete rule"),
  })

  async function removeRule(rule: AlertRule) {
    const ok = await confirm({
      title: `Delete rule "${rule.name}"?`,
      description: "Ferrum stops evaluating this threshold. Existing alerts stay until they resolve or are silenced.",
      confirmLabel: "Delete rule",
    })
    if (ok) deleteRuleMutation.mutate(rule.id)
  }

  const donutData = useMemo(
    () => [
      { name: "Critical", value: summaryQuery.data?.critical ?? 0, color: "var(--status-error)" },
      { name: "Warning", value: summaryQuery.data?.warning ?? 0, color: "var(--status-warn)" },
    ],
    [summaryQuery.data],
  )
  const totalActive = (summaryQuery.data?.critical ?? 0) + (summaryQuery.data?.warning ?? 0)

  const alertColumns = useMemo<ColumnDef<AlertInstance>[]>(
    () => [
      {
        accessorKey: "severity",
        header: "Severity",
        cell: (c) => <Badge variant={c.getValue<string>() === "critical" ? "error" : "warn"}>{c.getValue<string>()}</Badge>,
      },
      { accessorKey: "resourceName", header: "Resource" },
      { accessorKey: "connectionName", header: "Connection", meta: { hideBelowMd: true } },
      { accessorKey: "metric", header: "Metric", cell: (c) => METRIC_LABELS[c.getValue<string>()] ?? c.getValue<string>() },
      {
        id: "value",
        header: "Value",
        meta: { hideBelowMd: true },
        cell: (c) => (
          <span className="font-mono text-xs tabular">
            {c.row.original.value.toFixed(1)}% ≥ {c.row.original.threshold}%
          </span>
        ),
      },
      {
        accessorKey: "triggeredAt",
        header: "Triggered",
        cell: (c) => <span className="text-xs text-[var(--text-muted)] tabular">{new Date(c.getValue<string>()).toLocaleString()}</span>,
      },
      {
        id: "actions",
        header: "",
        cell: (c) => (
          <Button size="sm" variant="ghost" loading={silenceMutation.isPending} onClick={() => silenceMutation.mutate(c.row.original.id)}>
            {!silenceMutation.isPending && <BellOff className="h-3.5 w-3.5" />} Silence
          </Button>
        ),
      },
    ],
    [silenceMutation],
  )

  const ruleColumns = useMemo<ColumnDef<AlertRule>[]>(
    () => [
      { accessorKey: "name", header: "Name" },
      { accessorKey: "metric", header: "Metric", cell: (c) => METRIC_LABELS[c.getValue<string>()] ?? c.getValue<string>() },
      { accessorKey: "threshold", header: "Threshold", cell: (c) => <span className="tabular">{c.getValue<number>()}%</span> },
      {
        accessorKey: "severity",
        header: "Severity",
        cell: (c) => <Badge variant={c.getValue<string>() === "critical" ? "error" : "warn"}>{c.getValue<string>()}</Badge>,
      },
      {
        id: "scope",
        header: "Scope",
        meta: { hideBelowMd: true },
        cell: (c) => (c.row.original.connectionId ? "One connection" : "All connections"),
      },
      {
        id: "actions",
        header: "",
        cell: (c) => (
          <Hint label="Delete rule">
            <Button
              size="icon"
              variant="ghost"
              className="hover:bg-[color-mix(in_oklab,var(--status-error)_12%,transparent)] hover:text-[var(--status-error)]"
              aria-label={`Delete rule ${c.row.original.name}`}
              onClick={() => removeRule(c.row.original)}
            >
              <Trash2 className="h-4 w-4" />
            </Button>
          </Hint>
        ),
      },
    ],
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [deleteRuleMutation],
  )

  return (
    <div className="space-y-4">
      <PageHeader
        title="Alerts"
        description="Threshold-based monitoring across every connection, evaluated every minute."
        icon={AlertTriangle}
        actions={
          <Button onClick={() => setShowForm((s) => !s)}>
            <Plus className="h-4 w-4" /> New rule
          </Button>
        }
      />

      {downConnections.length > 0 && (
        <Card className="border-[var(--status-error)]/40 bg-[color-mix(in_oklab,var(--status-error)_6%,transparent)]">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-[var(--status-error)]">
              <WifiOff className="h-4 w-4" /> {downConnections.length} connection{downConnections.length === 1 ? "" : "s"} unreachable
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-2">
            <p className="text-xs text-[var(--text-muted)]">
              Ferrum can't reach these Proxmox hosts at all — a notification was already sent to every configured channel.
              This is tracked independently of threshold rules below, since there's no metric to evaluate when a host is fully offline.
            </p>
            {downConnections.map((h) => (
              <div key={h.connectionId} className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-[var(--status-error)]/30 bg-[var(--bg-surface)] px-3 py-2 text-sm">
                <div className="flex min-w-0 items-center gap-2">
                  <Badge variant="error">down</Badge>
                  <span className="font-medium">{h.connectionName}</span>
                  {h.lastError && <span className="truncate text-xs text-[var(--text-muted)]" title={h.lastError}>{h.lastError}</span>}
                </div>
                <span className="shrink-0 text-xs text-[var(--text-muted)] tabular">unreachable for {ageFrom(h.since)}</span>
              </div>
            ))}
          </CardContent>
        </Card>
      )}

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
        <Card className="sm:col-span-1">
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <ChartPie className="h-4 w-4" /> Alert summary
            </CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col items-center gap-2">
            {summaryQuery.isError ? (
              <ErrorState title="Couldn't load summary" onRetry={summaryQuery.refetch} />
            ) : summaryQuery.isLoading ? (
              <div className="py-4" aria-busy>
                <div className="mx-auto h-[150px] w-[150px] animate-pulse rounded-full bg-[var(--bg-muted)]" />
              </div>
            ) : (
              <>
                <DonutChart data={donutData} centerValue={String(totalActive)} centerLabel="active" height={150} />
                <div className="flex gap-4 text-xs">
                  <span className="flex items-center gap-1.5"><StatusDot status="error" /> Critical: <span className="tabular">{summaryQuery.data?.critical ?? 0}</span></span>
                  <span className="flex items-center gap-1.5"><StatusDot status="warn" /> Warning: <span className="tabular">{summaryQuery.data?.warning ?? 0}</span></span>
                </div>
              </>
            )}
          </CardContent>
        </Card>

        <Card className="sm:col-span-2">
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <ShieldAlert className="h-4 w-4" /> Active alerts
            </CardTitle>
          </CardHeader>
          <CardContent>
            {activeAlertsQuery.isError ? (
              <ErrorState title="Couldn't load active alerts" onRetry={activeAlertsQuery.refetch} />
            ) : (
              <DataTable
                columns={alertColumns}
                data={activeAlertsQuery.data ?? []}
                loading={activeAlertsQuery.isLoading}
                searchPlaceholder="Search alerts..."
                emptyMessage="No active alerts — everything's healthy."
                pageSize={5}
              />
            )}
          </CardContent>
        </Card>
      </div>

      {showForm && (
        <Card>
          <CardHeader>
            <CardTitle>New alert rule</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
              <div className="space-y-1.5">
                <Label>Name</Label>
                <Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="e.g. Storage node CPU" />
              </div>
              <div className="space-y-1.5">
                <Label>Metric</Label>
                <Select value={form.metric} onValueChange={(v) => setForm({ ...form, metric: v })}>
                  <SelectTrigger><SelectValue /></SelectTrigger>
                  <SelectContent>
                    {Object.entries(METRIC_LABELS).map(([k, label]) => (
                      <SelectItem key={k} value={k}>{label}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1.5">
                <Label>Threshold (%)</Label>
                <Input type="number" min={1} max={100} value={form.threshold} onChange={(e) => setForm({ ...form, threshold: e.target.value })} />
              </div>
              <div className="space-y-1.5">
                <Label>Severity</Label>
                <Select value={form.severity} onValueChange={(v) => setForm({ ...form, severity: v })}>
                  <SelectTrigger><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="warning">Warning</SelectItem>
                    <SelectItem value="critical">Critical</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1.5 sm:col-span-2">
                <Label>Connection (optional — leave blank to apply everywhere)</Label>
                <Select value={form.connectionId} onValueChange={(v) => setForm({ ...form, connectionId: v })}>
                  <SelectTrigger><SelectValue placeholder="All connections" /></SelectTrigger>
                  <SelectContent>
                    {inventory?.map((c) => (
                      <SelectItem key={c.connectionId} value={c.connectionId}>{c.name}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>
            <div className="flex gap-2">
              <Button disabled={!form.name} loading={createRuleMutation.isPending} onClick={() => createRuleMutation.mutate()}>
                Create rule
              </Button>
              <Button variant="ghost" onClick={() => setShowForm(false)}>Cancel</Button>
            </div>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Alert rules</CardTitle>
        </CardHeader>
        <CardContent>
          {rulesQuery.isError ? (
            <ErrorState title="Couldn't load alert rules" onRetry={rulesQuery.refetch} />
          ) : (
            <DataTable
              columns={ruleColumns}
              data={rulesQuery.data ?? []}
              loading={rulesQuery.isLoading}
              searchPlaceholder="Search rules..."
              emptyMessage="No alert rules configured — create one to start monitoring."
            />
          )}
        </CardContent>
      </Card>
    </div>
  )
}
