import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Activity, Cpu, HardDrive, MemoryStick, Network, Power, RefreshCw, Server, Terminal, Upload } from "lucide-react"
import { useMemo, useState } from "react"
import { useParams } from "react-router-dom"
import { toast } from "sonner"
import { KpiCard } from "@/components/charts/KpiCard"
import { ResourceAreaChart } from "@/components/charts/ResourceAreaChart"
import { DonutChart, DonutLegend } from "@/components/charts/DonutChart"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { ErrorState } from "@/components/ui/error-state"
import { PageHeader } from "@/components/ui/page-header"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  api,
  ApiError,
  type AptUpdate,
  type NetworkInterface,
  type NodeStatus,
  type RRDPoint,
  type Storage,
  type SyslogEntry,
} from "@/lib/api"
import { FORMATTERS, NODE_SERIES, buildRRDRows, hasAnySeries, rowNum, type ChartRow, type SeriesSpec } from "@/lib/metrics"
import { formatBytes, formatPercentFine, formatRate, formatUptime } from "@/lib/utils"

const TIMEFRAMES = ["hour", "day", "week", "month", "year"] as const

const CHART_SYNC = "node-metrics"

export function NodeDetailPage() {
  const { connId = "", node = "" } = useParams()
  const queryClient = useQueryClient()
  const confirm = useConfirm()
  const base = `/connections/${connId}/nodes/${node}`
  const [timeframe, setTimeframe] = useState<(typeof TIMEFRAMES)[number]>("hour")
  const [peaks, setPeaks] = useState(true)

  const statusQuery = useQuery({ queryKey: ["node-status", connId, node], queryFn: () => api.get<NodeStatus>(`${base}/status`), refetchInterval: 15_000 })
  const rrdAvgQuery = useQuery({
    queryKey: ["node-rrd", connId, node, timeframe, "avg"],
    queryFn: () => api.get<RRDPoint[]>(`${base}/rrddata?timeframe=${timeframe}`),
  })
  // Peak envelope (cf=MAX) — fetched only when the toggle is on.
  const rrdMaxQuery = useQuery({
    queryKey: ["node-rrd", connId, node, timeframe, "max"],
    queryFn: () => api.get<RRDPoint[]>(`${base}/rrddata?timeframe=${timeframe}&cf=MAX`),
    enabled: peaks,
    staleTime: 60_000,
  })
  const storageQuery = useQuery({ queryKey: ["node-storage", connId, node], queryFn: () => api.get<Storage[]>(`${base}/storage`) })
  const networkQuery = useQuery({ queryKey: ["node-network", connId, node], queryFn: () => api.get<NetworkInterface[]>(`${base}/network`) })
  const aptQuery = useQuery({ queryKey: ["node-apt", connId, node], queryFn: () => api.get<AptUpdate[]>(`${base}/apt/updates`) })
  const syslogQuery = useQuery({ queryKey: ["node-syslog", connId, node], queryFn: () => api.get<SyslogEntry[]>(`${base}/syslog`) })

  const rebootMutation = useMutation({
    mutationFn: () => api.post(`${base}/reboot`),
    onSuccess: () => toast.success("Reboot scheduled"),
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Reboot failed"),
  })
  const shutdownMutation = useMutation({
    mutationFn: () => api.post(`${base}/shutdown`),
    onSuccess: () => toast.success("Shutdown scheduled"),
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Shutdown failed"),
  })
  const upgradeMutation = useMutation({
    mutationFn: () => api.post(`${base}/apt/upgrade`),
    onSuccess: () => {
      toast.success("Upgrade started")
      queryClient.invalidateQueries({ queryKey: ["node-apt", connId, node] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Upgrade failed"),
  })

  // Both node power actions take the whole host down — always confirm.
  async function rebootNode() {
    const ok = await confirm({
      title: `Reboot ${node}?`,
      description: "All guests on this node restart with it (HA-managed guests migrate away if possible).",
      confirmLabel: "Reboot",
    })
    if (ok) rebootMutation.mutate()
  }
  async function shutdownNode() {
    const ok = await confirm({
      title: `Shut down ${node}?`,
      description: "All guests on this node shut down with it. The host stays off until someone powers it back on.",
      confirmLabel: "Shut down",
    })
    if (ok) shutdownMutation.mutate()
  }

  const status = statusQuery.data

  // --- Metric rows: one AVERAGE series, optionally enveloped by cf=MAX ---
  const allSpecs: SeriesSpec[] = useMemo(
    () => [
      ...NODE_SERIES.cpu(peaks),
      ...NODE_SERIES.memory(peaks),
      ...NODE_SERIES.network(peaks),
      ...NODE_SERIES.loadavg(peaks),
      ...NODE_SERIES.ioWait(peaks),
      ...NODE_SERIES.pressureIO(),
      ...NODE_SERIES.pressureMemory(),
    ],
    [peaks],
  )
  const rows: ChartRow[] = useMemo(
    () => buildRRDRows(rrdAvgQuery.data, peaks ? rrdMaxQuery.data : undefined, allSpecs),
    [rrdAvgQuery.data, rrdMaxQuery.data, allSpecs, peaks],
  )

  const lastOf = (key: string): number | undefined => {
    for (let i = rows.length - 1; i >= 0; i--) {
      const v = rowNum(rows[i], key)
      if (v !== undefined) return v
    }
    return undefined
  }
  const sparkOf = (key: string): (number | undefined)[] => rows.map((r) => rowNum(r, key))
  const netSpark = rows.map((r) => {
    const a = rowNum(r, "netin")
    const b = rowNum(r, "netout")
    return a !== undefined || b !== undefined ? (a ?? 0) + (b ?? 0) : undefined
  })

  const psiIO = hasAnySeries(rows, ["pressureiosome", "pressureiofull"])
  const psiMem = hasAnySeries(rows, ["pressurememorysome", "pressurememoryfull"])
  const hasIOWait = hasAnySeries(rows, ["iowait"])

  // KPI headline values: live status first (15s polling), RRD as fallback.
  const cpuPct = status?.cpu !== undefined ? status.cpu * 100 : lastOf("cpu")
  const memPct = status ? (status.memory.used / Math.max(1, status.memory.total)) * 100 : undefined
  const swapPct = status && status.swap.total > 0 ? (status.swap.used / status.swap.total) * 100 : undefined
  const rootPct = status ? (status.rootfs.used / Math.max(1, status.rootfs.total)) * 100 : undefined
  const loadNow = status ? Number.parseFloat(status.loadavg[0] ?? "0") : lastOf("loadavg")
  const netNow = (lastOf("netin") ?? 0) + (lastOf("netout") ?? 0)

  const toneFor = (pct: number | undefined): "default" | "warn" | "error" =>
    pct === undefined ? "default" : pct > 90 ? "error" : pct > 75 ? "warn" : "default"

  // Storage grouped by plugin type for the composition donut.
  const byType = new Map<string, number>()
  for (const s of storageQuery.data ?? []) {
    byType.set(s.type, (byType.get(s.type) ?? 0) + (s.used ?? 0))
  }
  const typeSlices = Array.from(byType.entries())
    .filter(([, v]) => v > 0)
    .sort((a, b) => b[1] - a[1])
    .map(([name, value], i) => ({ name, value, color: `var(--chart-${(i % 6) + 1})` }))

  return (
    <div className="space-y-4">
      <PageHeader
        title={node}
        description={status ? `${status.pveversion ?? "Proxmox VE"} · up ${formatUptime(status.uptime)}` : undefined}
        icon={Server}
        back={{ to: "/inventory", label: "Inventory" }}
        actions={
          <>
            <Button variant="secondary" size="sm" onClick={() => rebootNode()} loading={rebootMutation.isPending}>
              {!rebootMutation.isPending && <RefreshCw className="h-3.5 w-3.5" />} Reboot
            </Button>
            <Button variant="destructive" size="sm" onClick={() => shutdownNode()} loading={shutdownMutation.isPending}>
              {!shutdownMutation.isPending && <Power className="h-3.5 w-3.5" />} Shutdown
            </Button>
          </>
        }
      />

      {statusQuery.isError && (
        <ErrorState title="Couldn't load node status" onRetry={statusQuery.refetch} />
      )}

      {/* --- KPI strip: immediate-read metrics, each with its own trend --- */}
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-6">
        {statusQuery.isLoading ? (
          Array.from({ length: 6 }, (_, i) => <Skeleton key={i} className="h-[104px]" />)
        ) : (
          <>
            <KpiCard label="CPU" value={cpuPct !== undefined ? formatPercentFine(cpuPct) : "-"} sub={status ? `${status.cpuinfo.cores} cores` : undefined} progress={cpuPct} tone={toneFor(cpuPct)} spark={sparkOf("cpu")} icon={Cpu} sparkColor="var(--chart-1)" />
            <KpiCard label="Memory" value={memPct !== undefined ? formatPercentFine(memPct) : "-"} sub={status ? `${formatBytes(status.memory.used)} / ${formatBytes(status.memory.total)}` : undefined} progress={memPct} tone={toneFor(memPct)} spark={sparkOf("mem")} icon={MemoryStick} sparkColor="var(--chart-2)" />
            <KpiCard label="Swap" value={swapPct !== undefined ? formatPercentFine(swapPct) : "none"} sub={status && status.swap.total > 0 ? `${formatBytes(status.swap.used)} / ${formatBytes(status.swap.total)}` : undefined} progress={swapPct} tone={toneFor(swapPct)} spark={sparkOf("swap")} icon={MemoryStick} sparkColor="var(--chart-4)" />
            <KpiCard label="Root disk" value={rootPct !== undefined ? formatPercentFine(rootPct) : "-"} sub={status ? `${formatBytes(status.rootfs.used)} / ${formatBytes(status.rootfs.total)}` : undefined} progress={rootPct} tone={toneFor(rootPct)} icon={HardDrive} />
            <KpiCard label="Load (1m)" value={loadNow !== undefined && !Number.isNaN(loadNow) ? loadNow.toFixed(2) : "-"} sub={status ? `of ${status.cpuinfo.cores}` : undefined} spark={sparkOf("loadavg")} sparkVariant="line" icon={Activity} sparkColor="var(--chart-6)" />
            <KpiCard label="Network I/O" value={formatRate(netNow)} sub={lastOf("netin") !== undefined ? `in ${formatRate(lastOf("netin") ?? 0)}` : undefined} spark={netSpark} icon={Network} sparkColor="var(--chart-3)" />
          </>
        )}
      </div>

      {/* --- Historical charts, one unit family per chart --- */}
      <Card>
        <CardHeader className="flex-row items-center justify-between space-y-0">
          <CardTitle>Resource history</CardTitle>
          <div className="flex items-center gap-3">
            <label className="flex items-center gap-2 text-xs text-[var(--text-muted)]">
              Peaks
              <Switch checked={peaks} onCheckedChange={setPeaks} aria-label="Show peak envelope" />
            </label>
            <Select value={timeframe} onValueChange={(v) => setTimeframe(v as (typeof TIMEFRAMES)[number])}>
              <SelectTrigger className="w-32">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="hour">Last hour</SelectItem>
                <SelectItem value="day">Last day</SelectItem>
                <SelectItem value="week">Last week</SelectItem>
                <SelectItem value="month">Last month</SelectItem>
                <SelectItem value="year">Last year</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </CardHeader>
        <CardContent className="grid gap-6 lg:grid-cols-2">
          {rrdAvgQuery.isLoading && <Skeleton className="h-40 lg:col-span-2" />}
          {!rrdAvgQuery.isLoading && rows.length === 0 && (
            <p className="text-sm text-[var(--text-muted)] lg:col-span-2">No historical data available yet.</p>
          )}
          {rows.length > 0 && (
            <>
              <div>
                <p className="mb-1 text-xs font-medium text-[var(--text-muted)]">CPU utilization</p>
                <ResourceAreaChart
                  data={rows}
                  series={NODE_SERIES.cpu(peaks)}
                  yDomain={[0, 100]}
                  yTickFormatter={FORMATTERS.pct}
                  syncId={CHART_SYNC}
                  height={180}
                  showLegend={peaks}
                />
              </div>
              <div>
                <p className="mb-1 text-xs font-medium text-[var(--text-muted)]">Memory & swap</p>
                <ResourceAreaChart
                  data={rows}
                  series={NODE_SERIES.memory(peaks)}
                  yTickFormatter={FORMATTERS.bytes}
                  syncId={CHART_SYNC}
                  height={180}
                  showLegend
                />
              </div>
              <div>
                <p className="mb-1 text-xs font-medium text-[var(--text-muted)]">Network traffic</p>
                <ResourceAreaChart
                  data={rows}
                  series={NODE_SERIES.network(peaks)}
                  yTickFormatter={FORMATTERS.rate}
                  syncId={CHART_SYNC}
                  height={180}
                  showLegend
                />
              </div>
              <div>
                <p className="mb-1 text-xs font-medium text-[var(--text-muted)]">Load average (1 min)</p>
                <ResourceAreaChart
                  data={rows}
                  series={NODE_SERIES.loadavg(peaks)}
                  syncId={CHART_SYNC}
                  height={180}
                  allowDecimals={false}
                />
              </div>
              {(hasIOWait || psiIO || psiMem) && (
                <div className="lg:col-span-2">
                  <p className="mb-1 text-xs font-medium text-[var(--text-muted)]">
                    IO wait{psiIO ? " · IO pressure" : ""}{psiMem ? " · memory pressure" : ""} <span className="font-normal">(% of time resources are contended)</span>
                  </p>
                  <ResourceAreaChart
                    data={rows}
                    series={[
                      ...NODE_SERIES.ioWait(peaks),
                      ...(psiIO ? NODE_SERIES.pressureIO() : []),
                      ...(psiMem ? NODE_SERIES.pressureMemory() : []),
                    ]}
                    yDomain={[0, 100]}
                    yTickFormatter={FORMATTERS.pct}
                    syncId={CHART_SYNC}
                    height={180}
                    showLegend
                  />
                </div>
              )}
            </>
          )}
        </CardContent>
      </Card>

      {status && (
        <Card>
          <CardContent className="grid gap-x-8 gap-y-1 py-4 text-sm sm:grid-cols-2">
            <span className="text-[var(--text-muted)]">CPU model</span>
            <span className="truncate">
              {status.cpuinfo.model}
              {status.cpuinfo.mhz ? ` @ ${status.cpuinfo.mhz}` : ""}
            </span>
            <span className="text-[var(--text-muted)]">Cores / Sockets</span>
            <span className="tabular">{status.cpuinfo.cores} / {status.cpuinfo.sockets}</span>
            <span className="text-[var(--text-muted)]">IO wait (live)</span>
            <span className="tabular">{status.wait !== undefined ? formatPercentFine(status.wait * 100) : "-"}</span>
            {status.ksm?.shared ? (
              <>
                <span className="text-[var(--text-muted)]">KSM shared</span>
                <span className="tabular">{formatBytes(status.ksm.shared)} saved by page merging</span>
              </>
            ) : null}
            <span className="text-[var(--text-muted)]">Kernel</span>
            <span className="truncate font-mono text-xs">{status.kversion}</span>
          </CardContent>
        </Card>
      )}

      <Tabs defaultValue="storage">
        <TabsList>
          <TabsTrigger value="storage">Storage</TabsTrigger>
          <TabsTrigger value="network">Network</TabsTrigger>
          <TabsTrigger value="updates">Updates {aptQuery.data?.length ? `(${aptQuery.data.length})` : ""}</TabsTrigger>
          <TabsTrigger value="syslog">Syslog</TabsTrigger>
        </TabsList>

        <TabsContent value="storage">
          <Card>
            <CardContent className="space-y-4 pt-4">
              {typeSlices.length > 1 && (
                <div className="flex flex-col items-center gap-2 sm:flex-row sm:gap-6">
                  <div className="w-44 shrink-0 self-center">
                    <DonutChart data={typeSlices} height={130} centerValue={formatBytes(typeSlices.reduce((s, t) => s + t.value, 0))} centerLabel="stored" formatValue={formatBytes} />
                  </div>
                  <DonutLegend data={typeSlices} formatValue={formatBytes} />
                  <p className="text-center text-xs text-[var(--text-faint)] sm:hidden">Usage by storage type</p>
                </div>
              )}
              <div className="space-y-2">
                {storageQuery.data?.map((s) => {
                  const pct = s.total ? ((s.used ?? 0) / s.total) * 100 : 0
                  return (
                    <div key={s.storage} className="flex items-center gap-3 text-sm">
                      <span className="w-32 truncate font-medium">{s.storage}</span>
                      <Badge>{s.type}</Badge>
                      {s.shared === 1 && <Badge variant="ok">Shared</Badge>}
                      <Badge variant={s.active === 0 ? "error" : "default"}>{s.active === 0 ? "Inactive" : "Active"}</Badge>
                      <div className="h-1.5 flex-1 overflow-hidden rounded-sm bg-[var(--track)]">
                        <div className={pct > 85 ? "h-full bg-[var(--status-error)]" : "h-full bg-brand-500"} style={{ width: `${pct}%` }} />
                      </div>
                      <span className="w-32 text-right text-xs text-[var(--text-muted)] tabular">{formatBytes(s.used ?? 0)} / {formatBytes(s.total ?? 0)}</span>
                    </div>
                  )
                })}
                {storageQuery.data?.length === 0 && <p className="text-sm text-[var(--text-muted)]">No storage found.</p>}
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="network">
          <Card>
            <CardContent className="space-y-2 pt-4">
              {networkQuery.data?.map((n) => (
                <div key={n.iface} className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-[var(--border)] px-3 py-2 text-sm">
                  <div className="flex min-w-0 flex-wrap items-center gap-2">
                    <span className="shrink-0 font-mono">{n.iface}</span>
                    <Badge>{n.type}</Badge>
                    {n.active === 1 && <Badge variant="ok">Active</Badge>}
                    {n.autostart === 1 && <span className="shrink-0 text-xs text-[var(--text-muted)]">autostart</span>}
                    {n.bridge_ports && <span className="break-all font-mono text-xs text-[var(--text-muted)]">ports: {n.bridge_ports}</span>}
                  </div>
                  <span className="shrink-0 font-mono text-xs text-[var(--text-muted)]">
                    {n.address ? `${n.address}${n.netmask ? `/${n.netmask}` : ""}` : "no IP"}
                    {n.gateway ? ` via ${n.gateway}` : ""}
                  </span>
                </div>
              ))}
              {networkQuery.data?.length === 0 && <p className="text-sm text-[var(--text-muted)]">No interfaces found.</p>}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="updates">
          <Card>
            <CardHeader className="flex-row items-center justify-between space-y-0">
              <CardTitle>Pending updates</CardTitle>
              {(aptQuery.data?.length ?? 0) > 0 && (
                <Button size="sm" onClick={() => upgradeMutation.mutate()} loading={upgradeMutation.isPending}>
                  {!upgradeMutation.isPending && <Upload className="h-3.5 w-3.5" />}
                  Upgrade all
                </Button>
              )}
            </CardHeader>
            <CardContent className="space-y-1.5">
              {aptQuery.data?.map((u) => (
                <div key={u.Package} className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 text-sm">
                  <span className="flex min-w-0 items-center gap-2">
                    <span className="truncate font-mono text-xs">{u.Package}</span>
                    {u.Priority && <Badge variant={u.Priority === "security" ? "error" : "default"}>{u.Priority}</Badge>}
                  </span>
                  <span className="shrink-0 truncate font-mono text-xs text-[var(--text-muted)]">{u.OldVersion} → {u.Version}</span>
                </div>
              ))}
              {aptQuery.data?.length === 0 && <p className="text-sm text-[var(--text-muted)]">System is up to date.</p>}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="syslog">
          <Card>
            <CardContent className="pt-4">
              <div className="max-h-96 space-y-0.5 overflow-y-auto rounded-md bg-[var(--bg-muted)] p-3 font-mono text-xs">
                {syslogQuery.data?.map((entry) => (
                  <div key={entry.n} className="flex gap-2">
                    <Terminal className="mt-0.5 h-3 w-3 shrink-0 text-[var(--text-muted)]" />
                    <span className="whitespace-pre-wrap">{entry.t}</span>
                  </div>
                ))}
                {syslogQuery.data?.length === 0 && <p className="text-[var(--text-muted)]">No log entries.</p>}
              </div>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  )
}
