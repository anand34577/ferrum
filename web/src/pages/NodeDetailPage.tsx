import { useMutation, useQueries, useQuery, useQueryClient } from "@tanstack/react-query"
import { Activity, Cpu, HardDrive, MemoryStick, Network, Play, Power, RefreshCw, Server, SquareTerminal, Square, Terminal, Thermometer, Upload, Zap } from "lucide-react"
import { useMemo, useState } from "react"
import { useParams } from "react-router-dom"
import { toast } from "sonner"
import { KpiCard } from "@/components/charts/KpiCard"
import { ResourceAreaChart } from "@/components/charts/ResourceAreaChart"
import { DonutChart, DonutLegend } from "@/components/charts/DonutChart"
import { NodeSystemPanel } from "@/components/system/NodeSystemPanel"
import { ProvisionDiskDialog } from "@/components/system/ProvisionDiskDialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { ErrorState } from "@/components/ui/error-state"
import { Meter } from "@/components/ui/meter"
import { PageHeader } from "@/components/ui/page-header"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  api,
  ApiError,
  type AptUpdate,
  type Disk,
  type NetworkInterface,
  type JournalEntry,
  type NodeStatus,
  type RRDPoint,
  type SmartData,
  type Storage,
} from "@/lib/api"
import { buildShellUrl } from "@/lib/console"
import { FORMATTERS, NODE_SERIES, buildRRDRows, hasAnySeries, rowNum, type ChartRow, type SeriesSpec } from "@/lib/metrics"
import { cn, formatBytes, formatPercentFine, formatRate, formatUptime } from "@/lib/utils"

const TIMEFRAMES = ["hour", "day", "week", "month", "year"] as const

const CHART_SYNC = "node-metrics"

/** Proxmox's disk SMART data has no dedicated temperature field — it's just
 * another attribute row, named differently across ATA ("Temperature_Celsius")
 * and NVMe ("Temperature", "42 Celsius") reports. Extracted here as the
 * leading integer of whichever attribute name contains "temperature". */
function smartTemperatureC(smart: SmartData | undefined): number | undefined {
  const attr = smart?.attributes?.find((a) => a.name.toLowerCase().includes("temperature"))
  if (!attr?.value && !attr?.raw) return undefined
  const match = (attr.raw || attr.value || "").match(/-?\d+/)
  return match ? parseInt(match[0], 10) : undefined
}

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
  const disksQuery = useQuery({ queryKey: ["node-disks", connId, node], queryFn: () => api.get<Disk[]>(`${base}/disks`) })
  // Temperature isn't in the disk list itself (only SMART carries it), so
  // it's fetched per-disk in the background to show inline — the SMART
  // dialog covers everything else, this is purely for the at-a-glance read.
  const diskTempQueries = useQueries({
    queries: (disksQuery.data ?? []).map((d) => ({
      queryKey: ["node-disk-smart", connId, node, d.devpath],
      queryFn: () => api.get<SmartData>(`${base}/disks/smart?disk=${encodeURIComponent(d.devpath)}`),
      staleTime: 60_000,
      retry: false,
    })),
  })
  const diskTempByPath = new Map(
    (disksQuery.data ?? []).map((d, i) => [d.devpath, smartTemperatureC(diskTempQueries[i]?.data)]),
  )
  const [smartDisk, setSmartDisk] = useState<Disk | null>(null)
  const [provisionDisk, setProvisionDisk] = useState<Disk | null>(null)
  const smartQuery = useQuery({
    queryKey: ["node-disk-smart", connId, node, smartDisk?.devpath],
    queryFn: () => api.get<SmartData>(`${base}/disks/smart?disk=${encodeURIComponent(smartDisk!.devpath)}`),
    enabled: smartDisk !== null,
  })
  const networkQuery = useQuery({ queryKey: ["node-network", connId, node], queryFn: () => api.get<NetworkInterface[]>(`${base}/network`) })
  const aptQuery = useQuery({ queryKey: ["node-apt", connId, node], queryFn: () => api.get<AptUpdate[]>(`${base}/apt/updates`) })
  const journalQuery = useQuery({ queryKey: ["node-journal", connId, node], queryFn: () => api.get<JournalEntry[]>(`${base}/journal`) })

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
  const wakeOnLanMutation = useMutation({
    mutationFn: () => api.post(`${base}/wakeonlan`),
    onSuccess: () => toast.success("Wake-on-LAN packet sent"),
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Wake-on-LAN failed"),
  })
  const startAllMutation = useMutation({
    mutationFn: () => api.post(`${base}/startall`),
    onSuccess: () => toast.success("Starting all guests on this node"),
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Bulk start failed"),
  })
  const stopAllMutation = useMutation({
    mutationFn: () => api.post(`${base}/stopall`),
    onSuccess: () => toast.success("Stopping all guests on this node"),
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Bulk stop failed"),
  })
  const openShellMutation = useMutation({
    mutationFn: () => api.post<{ wsPath: string }>(`${base}/shell`),
    onSuccess: ({ wsPath }) => window.open(buildShellUrl(connId, node, wsPath, node), "_blank", "width=900,height=600"),
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to open shell"),
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
  async function stopAllGuests() {
    const ok = await confirm({
      title: `Stop all guests on ${node}?`,
      description: "Every running VM and container on this node shuts down.",
      confirmLabel: "Stop all",
    })
    if (ok) stopAllMutation.mutate()
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
            <Button variant="secondary" size="sm" onClick={() => openShellMutation.mutate()} loading={openShellMutation.isPending}>
              {!openShellMutation.isPending && <SquareTerminal className="h-3.5 w-3.5" />} Shell
            </Button>
            <Button variant="secondary" size="sm" onClick={() => wakeOnLanMutation.mutate()} loading={wakeOnLanMutation.isPending}>
              {!wakeOnLanMutation.isPending && <Zap className="h-3.5 w-3.5" />} Wake-on-LAN
            </Button>
            <Button variant="secondary" size="sm" onClick={() => startAllMutation.mutate()} loading={startAllMutation.isPending}>
              {!startAllMutation.isPending && <Play className="h-3.5 w-3.5" />} Start all
            </Button>
            <Button variant="secondary" size="sm" onClick={() => stopAllGuests()} loading={stopAllMutation.isPending}>
              {!stopAllMutation.isPending && <Square className="h-3.5 w-3.5" />} Stop all
            </Button>
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
                  valueKind="bytes"
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
                  valueKind="rate"
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
          <TabsTrigger value="disks">Disks {disksQuery.data?.length ? `(${disksQuery.data.length})` : ""}</TabsTrigger>
          <TabsTrigger value="network">Network</TabsTrigger>
          <TabsTrigger value="updates">Updates {aptQuery.data?.length ? `(${aptQuery.data.length})` : ""}</TabsTrigger>
          <TabsTrigger value="syslog">Journal</TabsTrigger>
          <TabsTrigger value="system">System</TabsTrigger>
        </TabsList>

        <TabsContent value="storage">
          <Card>
            <CardContent className="space-y-4 pt-4">
              {storageQuery.isError ? (
                <ErrorState title="Couldn't load storage" onRetry={storageQuery.refetch} />
              ) : storageQuery.isLoading ? (
                <div className="space-y-2" aria-busy>
                  <Skeleton className="h-8 w-full" />
                  <Skeleton className="h-8 w-full" />
                </div>
              ) : (
                <>
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
                        <div key={s.storage} className="flex flex-wrap items-center gap-3 text-sm">
                          <span className="w-32 truncate font-medium">{s.storage}</span>
                          <Badge>{s.type}</Badge>
                          {s.shared === 1 && <Badge variant="ok">Shared</Badge>}
                          <Badge variant={s.active === 0 ? "error" : "default"}>{s.active === 0 ? "Inactive" : "Active"}</Badge>
                          <Meter value={pct} label={`${s.storage} usage`} className="min-w-16 flex-1" />
                          <span className="shrink-0 whitespace-nowrap text-right text-xs text-[var(--text-muted)] tabular">{formatBytes(s.used ?? 0)} / {formatBytes(s.total ?? 0)}</span>
                        </div>
                      )
                    })}
                    {storageQuery.data?.length === 0 && <p className="text-sm text-[var(--text-muted)]">No storage found.</p>}
                  </div>
                </>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="disks">
          <Card>
            <CardContent className="space-y-2 pt-4">
              {disksQuery.isError ? (
                <ErrorState title="Couldn't load disks" onRetry={disksQuery.refetch} />
              ) : disksQuery.isLoading ? (
                <div className="space-y-2" aria-busy>
                  <Skeleton className="h-12 w-full" />
                  <Skeleton className="h-12 w-full" />
                </div>
              ) : disksQuery.data?.length === 0 ? (
                <p className="text-sm text-[var(--text-muted)]">No disks reported.</p>
              ) : (
                disksQuery.data?.map((d) => (
                  <div key={d.devpath} className="flex flex-wrap items-center gap-3 rounded-md border border-[var(--border)] px-3 py-2 text-sm">
                    <HardDrive className="h-4 w-4 shrink-0 text-[var(--text-muted)]" />
                    <div className="min-w-0 flex-1">
                      <p className="flex items-center gap-1.5">
                        <span className="truncate font-medium" title={d.devpath}>{d.model || d.devpath}</span>
                        <Badge>{d.type.toUpperCase()}</Badge>
                        {d.health && (
                          <Badge variant={d.health === "PASSED" ? "ok" : d.health === "FAILED" ? "error" : "default"}>{d.health}</Badge>
                        )}
                      </p>
                      <p className="truncate font-mono text-[10px] text-[var(--text-muted)]">
                        {d.devpath}{d.serial ? ` · S/N ${d.serial}` : ""}{d.used ? ` · used by ${d.used}` : " · unused"}
                      </p>
                    </div>
                    {typeof d.wearout === "number" && (
                      <div className="flex w-28 shrink-0 items-center gap-1.5" title="Estimated life remaining">
                        <div
                          className="h-1.5 flex-1 overflow-hidden rounded-none bg-[var(--track)]"
                          role="progressbar"
                          aria-valuenow={Math.round(d.wearout)}
                          aria-valuemin={0}
                          aria-valuemax={100}
                          aria-label="Estimated life remaining"
                        >
                          <div
                            className={d.wearout <= 10 ? "h-full bg-[var(--status-error)]" : d.wearout <= 25 ? "h-full bg-[var(--status-warn)]" : "h-full bg-brand-500"}
                            style={{ width: `${Math.max(0, Math.min(100, d.wearout))}%` }}
                          />
                        </div>
                        <span className="w-8 shrink-0 whitespace-nowrap text-right text-[10px] text-[var(--text-muted)] tabular">{d.wearout}%</span>
                      </div>
                    )}
                    {(() => {
                      const temp = diskTempByPath.get(d.devpath)
                      if (temp === undefined) return null
                      const hot = temp >= 60
                      const warm = temp >= 50
                      return (
                        <span
                          className={cn(
                            "flex shrink-0 items-center gap-1 text-xs tabular",
                            hot ? "text-[var(--status-error)]" : warm ? "text-[var(--status-warn)]" : "text-[var(--text-muted)]",
                          )}
                          title="Drive temperature (from SMART)"
                        >
                          <Thermometer className="h-3.5 w-3.5" /> {temp}°C
                        </span>
                      )
                    })()}
                    <span className="shrink-0 text-xs text-[var(--text-muted)] tabular">{formatBytes(d.size)}</span>
                    {!d.used && (
                      <Button size="sm" variant="outline" onClick={() => setProvisionDisk(d)}>Provision</Button>
                    )}
                    <Button size="sm" variant="outline" onClick={() => setSmartDisk(d)}>View SMART</Button>
                  </div>
                ))
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="network">
          <Card>
            <CardContent className="space-y-2 pt-4">
              {networkQuery.isError ? (
                <ErrorState title="Couldn't load network interfaces" onRetry={networkQuery.refetch} />
              ) : networkQuery.isLoading ? (
                <div className="space-y-2" aria-busy>
                  <Skeleton className="h-12 w-full" />
                  <Skeleton className="h-12 w-full" />
                </div>
              ) : (
                <>
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
                </>
              )}
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
              {aptQuery.isError ? (
                <ErrorState title="Couldn't load pending updates" onRetry={aptQuery.refetch} />
              ) : aptQuery.isLoading ? (
                <div className="space-y-1.5" aria-busy>
                  <Skeleton className="h-6 w-full" />
                  <Skeleton className="h-6 w-full" />
                </div>
              ) : (
                <>
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
                </>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="syslog">
          <Card>
            <CardContent className="pt-4">
              {journalQuery.isError ? (
                <ErrorState title="Couldn't load the journal" onRetry={journalQuery.refetch} />
              ) : journalQuery.isLoading ? (
                <Skeleton className="h-96 w-full" />
              ) : (
                <div className="max-h-96 space-y-0.5 overflow-y-auto rounded-md bg-[var(--bg-muted)] p-3 font-mono text-xs">
                  {journalQuery.data?.map((entry, i) => (
                    // index, not entry.n: some PVE versions return a line
                    // with no line number at all (see JournalEntry's custom
                    // UnmarshalJSON server-side), which would otherwise
                    // collide every such line onto the same n:0 React key.
                    <div key={i} className="flex gap-2">
                      <Terminal className="mt-0.5 h-3 w-3 shrink-0 text-[var(--text-muted)]" />
                      <span className="whitespace-pre-wrap">{entry.t}</span>
                    </div>
                  ))}
                  {journalQuery.data?.length === 0 && <p className="text-[var(--text-muted)]">No log entries.</p>}
                </div>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="system">
          <NodeSystemPanel connId={connId} node={node} />
        </TabsContent>
      </Tabs>

      <Dialog open={smartDisk !== null} onOpenChange={(open) => !open && setSmartDisk(null)}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>SMART — {smartDisk?.model || smartDisk?.devpath}</DialogTitle>
            <DialogDescription>{smartDisk?.devpath}{smartDisk?.serial ? ` · S/N ${smartDisk.serial}` : ""}</DialogDescription>
          </DialogHeader>
          {smartQuery.isError ? (
            <ErrorState title="Couldn't load SMART data" onRetry={smartQuery.refetch} />
          ) : smartQuery.isLoading ? (
            <Skeleton className="h-48 w-full" />
          ) : smartQuery.data?.attributes && smartQuery.data.attributes.length > 0 ? (
            <div className="max-h-[60vh] overflow-auto">
              <table className="w-full text-left text-xs">
                <thead className="font-mono text-[11px] font-semibold uppercase tracking-wider text-[var(--text-muted)]">
                  <tr>
                    <th className="py-1 pr-2">Attribute</th>
                    <th className="py-1 pr-2">Value</th>
                    <th className="py-1 pr-2">Worst</th>
                    <th className="py-1 pr-2">Threshold</th>
                    <th className="py-1">Raw</th>
                  </tr>
                </thead>
                <tbody>
                  {smartQuery.data.attributes.map((a, i) => (
                    <tr key={a.id ?? a.name ?? i} className="border-t border-[var(--border)]">
                      <td className="py-1 pr-2 tabular">{a.name}</td>
                      <td className="py-1 pr-2 tabular">{a.value ?? "-"}</td>
                      <td className="py-1 pr-2 tabular">{a.worst ?? "-"}</td>
                      <td className="py-1 pr-2 tabular">{a.threshold ?? "-"}</td>
                      <td className="py-1 font-mono tabular">{a.raw ?? "-"}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : smartQuery.data?.text ? (
            <pre className="max-h-[60vh] overflow-auto whitespace-pre-wrap rounded-md bg-[var(--bg-muted)] p-3 font-mono text-[11px]">{smartQuery.data.text}</pre>
          ) : (
            <p className="text-sm text-[var(--text-muted)]">No SMART data reported for this disk.</p>
          )}
        </DialogContent>
      </Dialog>

      <ProvisionDiskDialog
        connId={connId}
        node={node}
        disk={provisionDisk}
        onOpenChange={(open) => !open && setProvisionDisk(null)}
      />
    </div>
  )
}
