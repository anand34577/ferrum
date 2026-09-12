import { useQuery } from "@tanstack/react-query"
import {
  Activity,
  AlertTriangle,
  ArrowDown,
  ArrowUp,
  ArrowUpDown,
  Box,
  Boxes,
  ChevronRight,
  Container,
  Cpu,
  Database,
  Flame,
  HardDrive,
  LayoutDashboard,
  MemoryStick,
  Network,
  Server,
  ShieldAlert,
  ShieldCheck,
  Waypoints,
} from "lucide-react"
import { useMemo, useState } from "react"
import { Link } from "react-router-dom"
import { Heatmap, type HeatmapRow } from "@/components/charts/Heatmap"
import { KpiCard } from "@/components/charts/KpiCard"
import { Button } from "@/components/ui/button"
import { EmptyState } from "@/components/ui/empty-state"
import { ErrorState } from "@/components/ui/error-state"
import { Meter, SplitMeter } from "@/components/ui/meter"
import { PageHeader } from "@/components/ui/page-header"
import { Skeleton } from "@/components/ui/skeleton"
import { StatusDot } from "@/components/ui/status-dot"
import { Hint } from "@/components/ui/tooltip"
import { api, type AlertInstance, type FleetOverviewConn } from "@/lib/api"
import { summarizeFleet, useScopedInventory, utilizationTone } from "@/lib/fleet"
import { cn, formatAlertValue, formatBytes, formatPercentFine, formatRelativeTime } from "@/lib/utils"

/**
 * Fleet Overview — the landing page of the centralized manager. Aggregated
 * KPIs across every connected Proxmox cluster/server first, then a sortable
 * per-connection comparison (with poll latency), a health matrix, and a
 * fleet-signals panel (guest mix, HA coverage, storage by type), so an admin
 * sees the whole estate at a glance and can drill into any single host.
 */

type SortKey = "name" | "nodes" | "vms" | "lxcs" | "cpu" | "memory" | "storage" | "alerts" | "latency"

const toneClass = {
  ok: "text-[var(--status-ok)]",
  warn: "text-[var(--status-warn)]",
  error: "text-[var(--status-error)]",
} as const

const CHART_COLORS = [
  "var(--chart-1)",
  "var(--chart-2)",
  "var(--chart-3)",
  "var(--chart-4)",
  "var(--chart-5)",
  "var(--chart-6)",
]

/** Latency tone for the poll round-trip column — generous thresholds because
 * remote sites over WAN links are the normal case here. */
function latencyTone(ms: number): "ok" | "warn" | "error" {
  if (ms >= 800) return "error"
  if (ms >= 300) return "warn"
  return "ok"
}

const ALERT_METRIC_LABELS: Record<string, string> = {
  node_cpu: "Node CPU",
  node_mem: "Node memory",
  node_disk: "Node disk",
  guest_cpu: "Guest CPU",
  guest_mem: "Guest memory",
  storage_usage: "Storage pool",
}

export function OverviewPage() {
  const { data: overview, isLoading, isError, refetch } = useQuery({
    queryKey: ["overview"],
    queryFn: () => api.get<FleetOverviewConn[]>("/overview"),
    refetchInterval: 15_000,
  })
  const totals = useMemo(() => summarizeFleet(overview), [overview])
  const [sortKey, setSortKey] = useState<SortKey>("cpu")
  const [sortDesc, setSortDesc] = useState(true)

  const sorted = useMemo(() => {
    const rows = [...totals.connections]
    const get = (c: FleetOverviewConn): string | number => {
      switch (sortKey) {
        case "name":
          return c.name.toLowerCase()
        case "nodes":
          return c.nodes.total
        case "vms":
          return c.vms.total + c.lxcs.total
        case "lxcs":
          return c.lxcs.total
        case "cpu":
          return c.cpu.pct
        case "memory":
          return c.memory.pct
        case "storage":
          return c.storage.pct
        case "alerts":
          return c.alerts.critical * 100 + c.alerts.warning
        case "latency":
          return c.latencyMs ?? Number.MAX_SAFE_INTEGER
      }
    }
    rows.sort((a, b) => {
      const va = get(a)
      const vb = get(b)
      if (typeof va === "string" || typeof vb === "string") {
        const cmp = String(va).localeCompare(String(vb))
        return sortDesc ? -cmp : cmp
      }
      return sortDesc ? vb - va : va - vb
    })
    return rows
  }, [totals.connections, sortKey, sortDesc])

  function sortButton(key: SortKey, label: string, align: "left" | "right" = "right") {
    const active = sortKey === key
    return (
      <button
        onClick={() => (active ? setSortDesc((d) => !d) : (setSortKey(key), setSortDesc(true)))}
        aria-label={`${label}: activate to sort${active ? (sortDesc ? ", currently descending" : ", currently ascending") : ""}`}
        className={cn(
          "flex w-full items-center gap-1.5 transition-colors",
          align === "right" ? "justify-end" : "justify-start",
          active ? "text-[var(--text)]" : "text-[var(--text-muted)] hover:text-[var(--text)]",
        )}
      >
        {label}
        {active ? sortDesc ? <ArrowDown className="h-3 w-3 text-brand-500" /> : <ArrowUp className="h-3 w-3 text-brand-500" /> : <ArrowUpDown className="h-3 w-3 opacity-35" />}
      </button>
    )
  }

  // Health matrix: one row per connection, columns CPU/Memory/Storage.
  const healthRows: HeatmapRow[] = totals.connections.map((c) =>
    c.online
      ? { label: c.name, values: [c.cpu.pct, c.memory.pct, c.storage.pct] }
      : { label: c.name, values: [100, 100, 100] }, // offline = max alarm
  )

  // Storage-by-type rollup across every online connection, top 4 types shown.
  const storageByType = useMemo(() => {
    const byType = new Map<string, number>()
    for (const c of totals.online) {
      for (const [t, v] of Object.entries(c.storage.byType ?? {})) {
        if (typeof v === "number" && v > 0) byType.set(t, (byType.get(t) ?? 0) + v)
      }
    }
    const entries = [...byType.entries()].sort((a, b) => b[1] - a[1])
    const total = entries.reduce((s, [, v]) => s + v, 0)
    const segments = entries.slice(0, 4).map(([label, value], i) => ({ label, value, color: CHART_COLORS[i % CHART_COLORS.length] }))
    const rest = entries.slice(4).reduce((s, [, v]) => s + v, 0)
    if (rest > 0) segments.push({ label: "other", value: rest, color: "var(--text-faint)" })
    return { segments, total }
  }, [totals.online])

  const guestsTotal = totals.vms + totals.lxcs

  const alertTone = totals.alertsCritical > 0 ? "error" : totals.alertsWarning > 0 ? "warn" : "ok"

  // Hottest guests across the whole estate — reuses the shared 15s inventory
  // poll (same query key as the command palette and dashboard widgets), so
  // this adds no extra request.
  const { connections: invConnections, isLoading: invLoading } = useScopedInventory()
  const hotGuests = useMemo(
    () =>
      invConnections
        .flatMap((c) =>
          (c.resources ?? [])
            .filter((r) => (r.type === "qemu" || r.type === "lxc") && r.status === "running")
            .map((r) => ({
              id: r.id,
              name: r.name ?? `#${r.vmid}`,
              conn: c.name,
              isVm: r.type === "qemu",
              cpuPct: Math.min(100, (r.cpu ?? 0) * 100),
              memPct: r.maxmem ? Math.min(100, ((r.mem ?? 0) / r.maxmem) * 100) : 0,
            })),
        )
        .sort((a, b) => b.cpuPct - a.cpuPct || b.memPct - a.memPct)
        .slice(0, 5),
    [invConnections],
  )

  // Active alert instances — the same query the Alerts page uses, so the
  // overview shows what the alert rules are actually firing right now.
  const activeAlerts = useQuery({
    queryKey: ["alerts", "active"],
    queryFn: () => api.get<AlertInstance[]>("/alerts/?status=active"),
    refetchInterval: 30_000,
    retry: false,
  })
  const alerts = useMemo(
    () =>
      (activeAlerts.data ?? [])
        .slice()
        .sort((a, b) => (a.severity === "critical" ? 0 : 1) - (b.severity === "critical" ? 0 : 1))
        .slice(0, 4),
    [activeAlerts.data],
  )

  return (
    <div className="space-y-4">
      <PageHeader
        title="Fleet Overview"
        description={`${totals.serversOnline}/${totals.serversTotal} Proxmox ${totals.serversTotal === 1 ? "server" : "servers"} online · ${totals.clusters} cluster${totals.clusters === 1 ? "" : "s"} · auto-refreshes every 15s`}
        icon={Waypoints}
        actions={
          <Hint label="Build your own layout from these widgets">
            <Link to="/dashboard">
              <Button variant="secondary" size="sm" className="gap-2 shadow-xs">
                <LayoutDashboard className="h-4 w-4" /> Custom dashboard
              </Button>
            </Link>
          </Hint>
        }
      />

      {isError && <ErrorState title="Couldn't load the fleet overview" onRetry={refetch} />}

      {!isError && (
      <>
      {/* --- Global KPIs: two even rows of four on desktop — eight-across
              left every card too narrow for its own numbers. --- */}
      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        {isLoading ? (
          Array.from({ length: 8 }, (_, i) => <Skeleton key={i} className="h-[92px]" />)
        ) : (
          <>
            <KpiCard label="Proxmox servers" value={`${totals.serversOnline}`} sub={`of ${totals.serversTotal}`} tone={totals.offline.length > 0 ? "warn" : "ok"} icon={Network} progress={totals.serversTotal ? (totals.serversOnline / totals.serversTotal) * 100 : 0} progressInvert />
            <KpiCard label="Nodes" value={`${totals.nodesOnline}`} sub={`of ${totals.nodesTotal} · ${totals.clusters} cluster${totals.clusters === 1 ? "" : "s"}`} tone={totals.nodesOnline < totals.nodesTotal ? "warn" : "ok"} icon={Server} />
            <KpiCard label="Virtual machines" value={`${totals.vms}`} sub={`${totals.vmsRunning} running`} icon={Boxes} />
            <KpiCard label="Containers" value={`${totals.lxcs}`} sub={`${totals.lxcsRunning} running`} icon={Container} />
            <KpiCard label="CPU" value={formatPercentFine(totals.cpuPct)} sub={`${totals.cores} cores`} tone={utilizationTone(totals.cpuPct)} icon={Cpu} progress={totals.cpuPct} />
            <KpiCard label="Memory" value={formatPercentFine(totals.memPct)} sub={`${formatBytes(totals.memUsed)} / ${formatBytes(totals.memTotal)}`} tone={utilizationTone(totals.memPct)} icon={MemoryStick} progress={totals.memPct} />
            <KpiCard label="Storage" value={formatPercentFine(totals.stoPct)} sub={`${formatBytes(totals.stoUsed)} / ${formatBytes(totals.stoTotal)}`} tone={utilizationTone(totals.stoPct)} icon={HardDrive} progress={totals.stoPct} />
            <KpiCard
              label="Alerts"
              value={`${totals.alertsCritical + totals.alertsWarning}`}
              sub={`${totals.alertsCritical} critical · ${totals.alertsWarning} warn`}
              tone={alertTone === "error" ? "error" : alertTone === "warn" ? "warn" : "ok"}
              icon={AlertTriangle}
            />
          </>
        )}
      </div>

      {/* --- Attention banner: anything unreachable, with why and when --- */}
      {!isLoading && totals.offline.length > 0 && (
        <div className="rounded-xl border border-[color-mix(in_oklab,var(--status-error)_35%,var(--border))] bg-[color-mix(in_oklab,var(--status-error)_8%,var(--bg-surface))] px-4.5 py-3.5 shadow-xs">
          <p className="flex items-center gap-2 text-sm font-medium text-[var(--status-error)]">
            <AlertTriangle className="h-4 w-4 shrink-0" />
            {totals.offline.length} connection{totals.offline.length === 1 ? "" : "s"} unreachable — the numbers above only cover what's online
          </p>
          <ul className="mt-2 space-y-1">
            {totals.offline.map((c) => (
              <li key={c.connectionId} className="flex flex-wrap items-baseline gap-x-2 text-xs">
                <span className="font-medium">{c.name}</span>
                <span className="font-mono text-[var(--text-muted)]">{c.host}:{c.port}</span>
                <span className="min-w-0 truncate text-[var(--status-error)]" title={c.error}>{c.error ?? "offline"}</span>
                <span className="ml-auto shrink-0 text-[var(--text-faint)] tabular" title={c.checkedAt}>checked {formatRelativeTime(c.checkedAt)}</span>
              </li>
            ))}
          </ul>
        </div>
      )}

      {/* --- Fleet resource bars: fleet total + per-connection breakdown --- */}
      <div className="grid gap-4 lg:grid-cols-3">
        <ResourceCard icon={Cpu} title="CPU" pct={totals.cpuPct} detail={`${totals.cores} physical cores`} rows={totals.online.map((c) => ({ name: c.name, pct: c.cpu.pct, detail: `${c.cpu.usedCores.toFixed(1)} / ${c.cpu.cores}` }))} />
        <ResourceCard icon={MemoryStick} title="Memory" pct={totals.memPct} detail={`${formatBytes(totals.memUsed)} of ${formatBytes(totals.memTotal)}`} rows={totals.online.map((c) => ({ name: c.name, pct: c.memory.pct, detail: `${formatBytes(c.memory.used)} / ${formatBytes(c.memory.total)}` }))} />
        <ResourceCard icon={HardDrive} title="Storage" pct={totals.stoPct} detail={`${formatBytes(totals.stoUsed)} of ${formatBytes(totals.stoTotal)}`} rows={totals.online.map((c) => ({ name: c.name, pct: c.storage.pct, detail: `${formatBytes(c.storage.used)} / ${formatBytes(c.storage.total)}` }))} />
      </div>

      <div className="grid items-start gap-4 xl:grid-cols-3">
        {/* --- Per-connection comparison + guest/alert detail panels --- */}
        <div className="min-w-0 space-y-4 xl:col-span-2">
          <div className="overflow-hidden rounded-xl border border-[var(--border)] bg-[var(--bg-surface)] shadow-card">
            <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 border-b border-[var(--border)] bg-[var(--bg-muted)]/30 px-4.5 py-3">
              <p className="flex items-center gap-1.5 font-display text-xs font-semibold">
                <Waypoints className="h-3.5 w-3.5 text-brand-500" /> Cluster comparison
              </p>
              <p className="text-[10px] text-[var(--text-faint)]">Click a header to sort · nodes drill through to host detail</p>
            </div>
            {sorted.length === 0 && !isLoading ? (
              <EmptyState
                icon={Waypoints}
                title="No Proxmox connections yet"
                description="Add a host or cluster to see it compared here."
                className="rounded-none border-0"
                action={
                  <Link to="/connections">
                    <Button variant="secondary" size="sm" className="gap-2">
                      <ChevronRight className="h-3.5 w-3.5" /> Add connection
                    </Button>
                  </Link>
                }
              />
            ) : (
            <div className="overflow-x-auto">
              {/* min-w keeps the nine columns readable — below it the table
                  would crush its bar cells instead of scrolling. */}
              <table className="w-full min-w-[880px] text-sm">
                <thead className="border-b border-[var(--border)] bg-[var(--bg-muted)]/50">
                  <tr className="[&>th]:font-mono [&>th]:text-[11px] [&>th]:font-semibold [&>th]:uppercase [&>th]:tracking-wider [&>th]:text-[var(--text-muted)]">
                    <th className="px-4 py-2 text-left">{sortButton("name", "Connection", "left")}</th>
                    <th className="px-3 py-2 text-right">{sortButton("nodes", "Nodes")}</th>
                    <th className="px-3 py-2 text-right">{sortButton("vms", "VMs")}</th>
                    <th className="px-3 py-2 text-right">{sortButton("lxcs", "CTs")}</th>
                    <th className="w-36 px-3 py-2">{sortButton("cpu", "CPU")}</th>
                    <th className="w-36 px-3 py-2">{sortButton("memory", "Memory")}</th>
                    <th className="w-36 px-3 py-2">{sortButton("storage", "Storage")}</th>
                    <th className="px-3 py-2 text-right">{sortButton("alerts", "Alerts")}</th>
                    <th className="px-3 py-2 text-right" title="Round-trip time of the last fleet poll — orange at 300ms, red at 800ms">{sortButton("latency", "Latency")}</th>
                  </tr>
                </thead>
                <tbody>
                  {sorted.map((c) => (
                    <tr key={c.connectionId} className="border-b border-[var(--border)] last:border-0 hover:bg-[var(--bg-surface-hover)]">
                      <td className="max-w-52 px-4 py-2.5">
                        <div className="flex items-center gap-2">
                          <StatusDot status={c.online ? "ok" : "error"} />
                          <div className="min-w-0">
                            <p className="truncate font-medium">{c.name}</p>
                            <p className="truncate font-mono text-[10px] text-[var(--text-muted)]">
                              {c.online
                                ? c.cluster
                                  ? `${c.cluster.name}${c.cluster.quorate ? " · quorate" : " · NO QUORUM"}`
                                  : "standalone"
                                : c.error ?? "offline"}
                            </p>
                          </div>
                          {c.online && (
                            <Link to="/inventory" aria-label={`Inventory for ${c.name}`} className="ml-auto shrink-0 text-[var(--text-muted)] hover:text-[var(--text)]">
                              <ChevronRight className="h-4 w-4" />
                            </Link>
                          )}
                        </div>
                      </td>
                      <td className="px-3 py-2.5 text-right tabular">
                        <span className={c.nodes.online < c.nodes.total ? "text-[var(--status-warn)]" : undefined}>{c.nodes.online}</span>
                        <span className="text-[var(--text-faint)]">/{c.nodes.total}</span>
                      </td>
                      <td className="px-3 py-2.5 text-right tabular">{c.vms.total}</td>
                      <td className="px-3 py-2.5 text-right tabular">{c.lxcs.total}</td>
                      <td className="px-3 py-2.5">{c.online ? <Meter value={c.cpu.pct} size="md" showLabel label="CPU" /> : <span className="text-xs text-[var(--text-faint)]">—</span>}</td>
                      <td className="px-3 py-2.5">{c.online ? <Meter value={c.memory.pct} size="md" showLabel label="Memory" /> : <span className="text-xs text-[var(--text-faint)]">—</span>}</td>
                      <td className="px-3 py-2.5">{c.online ? <Meter value={c.storage.pct} size="md" showLabel label="Storage" /> : <span className="text-xs text-[var(--text-faint)]">—</span>}</td>
                      <td className="px-3 py-2.5 text-right tabular">
                        {c.alerts.critical > 0 ? (
                          <span className="font-medium text-[var(--status-error)]">{c.alerts.critical}</span>
                        ) : c.alerts.warning > 0 ? (
                          <span className="text-[var(--status-warn)]">{c.alerts.warning}</span>
                        ) : (
                          <span className="text-[var(--text-faint)]">0</span>
                        )}
                      </td>
                      <td className="px-3 py-2.5 text-right tabular" title="Round-trip time of the last fleet poll">
                        {c.online && c.latencyMs !== undefined ? (
                          <span className={cn("text-xs", toneClass[latencyTone(c.latencyMs)])}>{c.latencyMs} ms</span>
                        ) : (
                          <span className="text-xs text-[var(--text-faint)]">—</span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            )}
          </div>

          {/* --- Hottest guests: the whole estate's busiest workloads, so the
                  column under the comparison table stays as tall as the
                  right rail instead of dead space. --- */}
          <div className="rounded-xl border border-[var(--border)] bg-[var(--bg-surface)] p-4.5 shadow-card dark:shadow-[inset_0_1px_0_0_rgba(255,255,255,0.05),0_1px_3px_rgba(0,0,0,0.4)]">
            <div className="flex items-center justify-between gap-2">
              <p className="flex items-center gap-1.5 font-display text-xs font-semibold">
                <Flame className="h-3.5 w-3.5 text-brand-500" /> Hottest guests
              </p>
              <p className="text-[10px] text-[var(--text-faint)]">Busiest running guests across the fleet · live</p>
            </div>
            {invLoading ? (
              <Skeleton className="mt-3 h-24" />
            ) : hotGuests.length === 0 ? (
              <p className="py-5 text-center text-sm text-[var(--text-muted)]">No running guests yet.</p>
            ) : (
              <ul className="mt-3 space-y-2.5">
                {hotGuests.map((g) => (
                  <li key={g.id} className="flex items-center gap-3 text-xs">
                    <span className="shrink-0 text-[var(--text-muted)]" title={g.isVm ? "Virtual machine" : "Container"}>
                      {g.isVm ? <Box className="h-3.5 w-3.5" /> : <Container className="h-3.5 w-3.5" />}
                    </span>
                    <span className="w-40 min-w-0 shrink-0 truncate font-medium" title={`${g.name} — ${g.conn}`}>{g.name}</span>
                    <span className="hidden w-28 shrink-0 truncate text-[10px] text-[var(--text-muted)] sm:block">{g.conn}</span>
                    <Meter value={g.cpuPct} size="md" showLabel label="CPU" className="min-w-24 flex-1" />
                    <span className="w-24 shrink-0 text-right text-[10px] text-[var(--text-faint)] tabular" title="Memory usage">
                      mem {g.memPct.toFixed(0)}%
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </div>

          {/* --- Active alerts: what the alert rules are firing right now --- */}
          <div className="rounded-xl border border-[var(--border)] bg-[var(--bg-surface)] p-4.5 shadow-card dark:shadow-[inset_0_1px_0_0_rgba(255,255,255,0.05),0_1px_3px_rgba(0,0,0,0.4)]">
            <div className="flex items-center justify-between gap-2">
              <p className="flex items-center gap-1.5 font-display text-xs font-semibold">
                <ShieldAlert className={cn("h-3.5 w-3.5", alerts.length > 0 ? "text-[var(--status-warn)]" : "text-brand-500")} /> Active alerts
              </p>
              <Link to="/alerts" className="text-[10px] text-[var(--text-muted)] underline-offset-2 hover:text-[var(--text)] hover:underline">
                View all
              </Link>
            </div>
            {activeAlerts.isLoading ? (
              <Skeleton className="mt-3 h-20" />
            ) : alerts.length === 0 ? (
              <p className="py-4 text-center text-sm text-[var(--text-muted)]">
                No active alerts — every threshold is holding.
              </p>
            ) : (
              <ul className="mt-3 space-y-2">
                {alerts.map((a) => (
                  <li key={a.id} className="flex items-center gap-2.5 text-xs">
                    <span
                      role="img"
                      className={cn("h-1.5 w-1.5 shrink-0 rounded-full", a.severity === "critical" ? "bg-[var(--status-error)]" : "bg-[var(--status-warn)]")}
                      aria-label={a.severity}
                    />
                    <span className="w-36 min-w-0 shrink-0 truncate font-medium" title={a.resourceName}>{a.resourceName}</span>
                    <span className="hidden min-w-0 flex-1 truncate text-[10px] text-[var(--text-muted)] md:block">
                      {ALERT_METRIC_LABELS[a.metric] ?? a.metric} · {a.connectionName}
                    </span>
                    <span className="ml-auto shrink-0 tabular text-[var(--text-muted)]" title="Value vs threshold">
                      {formatAlertValue(a.metric, a.value, a.threshold)}
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </div>

        {/* --- Right rail: health matrix + fleet signals --- */}
        <div className="space-y-4">
          <div className="rounded-xl border border-[var(--border)] bg-[var(--bg-surface)] p-4.5 shadow-card dark:shadow-[inset_0_1px_0_0_rgba(255,255,255,0.05),0_1px_3px_rgba(0,0,0,0.4)]">
            <p className="flex items-center gap-1.5 font-display text-xs font-semibold">
              <Activity className="h-3.5 w-3.5 text-brand-500" /> Fleet health matrix
            </p>
            <p className="mb-3 mt-0.5 text-[10px] text-[var(--text-faint)]">Utilization per connection — darker means hotter</p>
            {isLoading ? (
              <Skeleton className="h-32" />
            ) : healthRows.length > 0 ? (
              <Heatmap rows={healthRows} columnLabels={["CPU", "Memory", "Storage"]} valueFormatter={(v) => `${v.toFixed(1)}%`} cellHeight={26} />
            ) : (
              <p className="pt-6 text-center text-sm text-[var(--text-muted)]">Nothing to compare yet.</p>
            )}
          </div>

          <div className="rounded-xl border border-[var(--border)] bg-[var(--bg-surface)] p-4.5 shadow-card dark:shadow-[inset_0_1px_0_0_rgba(255,255,255,0.05),0_1px_3px_rgba(0,0,0,0.4)]">
            <p className="flex items-center gap-1.5 font-display text-xs font-semibold">
              <Boxes className="h-3.5 w-3.5 text-brand-500" /> Fleet signals
            </p>
            <p className="mt-0.5 text-[10px] text-[var(--text-faint)]">Guest mix and storage shape across the whole estate</p>

            <div className="mt-3 space-y-4">
              <div>
                <p className="mb-1.5 flex items-center justify-between text-xs">
                  <span className="text-[var(--text-muted)]">Guests — running vs stopped</span>
                  <span className="tabular text-[var(--text-muted)]">
                    {totals.running} of {guestsTotal} up
                  </span>
                </p>
                <SplitMeter
                  total={guestsTotal}
                  segments={[
                    { label: "running", value: totals.running, color: "var(--status-ok)" },
                    { label: "stopped", value: totals.stopped, color: "var(--status-error)" },
                  ]}
                />
                <p className="mt-1.5 flex flex-wrap gap-x-3 text-[10px] text-[var(--text-muted)]">
                  <span>VM {totals.vmsRunning}/{totals.vms} running</span>
                  <span>CT {totals.lxcsRunning}/{totals.lxcs} running</span>
                  <span>{totals.templates} template{totals.templates === 1 ? "" : "s"}</span>
                </p>
              </div>

              <div>
                <p className="mb-1.5 flex items-center justify-between text-xs">
                  <span className="text-[var(--text-muted)]">Storage by type</span>
                  <span className="tabular text-[var(--text-muted)]">{formatBytes(storageByType.total)}</span>
                </p>
                <SplitMeter total={storageByType.total} segments={storageByType.segments} formatValue={formatBytes} />
              </div>

              <div className="grid grid-cols-2 gap-2 border-t border-[var(--border)] pt-3 text-xs text-[var(--text-muted)]">
                <p className="flex items-center gap-2">
                  <ShieldCheck className={cn("h-3.5 w-3.5", totals.haGuests > 0 ? "text-[var(--status-ok)]" : undefined)} />
                  {totals.haGuests} HA-protected
                </p>
                <p className="flex items-center gap-2">
                  <Server className="h-3.5 w-3.5" /> {totals.cores} cores
                </p>
                <p className="flex items-center gap-2">
                  <Database className="h-3.5 w-3.5" /> {totals.clusters} cluster{totals.clusters === 1 ? "" : "s"}
                </p>
                <p className="flex items-center gap-2">
                  <AlertTriangle className={cn("h-3.5 w-3.5", totals.offline.length > 0 && "text-[var(--status-warn)]")} />
                  {totals.offline.length} unreachable
                </p>
              </div>
            </div>
          </div>
        </div>
      </div>
      </>
      )}
    </div>
  )
}

/** One resource family: fleet total on top, per-connection bars below. */
function ResourceCard({
  icon: Icon,
  title,
  pct,
  detail,
  rows,
}: {
  icon: typeof Cpu
  title: string
  pct: number
  detail: string
  rows: { name: string; pct: number; detail: string }[]
}) {
  const tone = utilizationTone(pct)
  return (
    <div className="rounded-xl border border-[var(--border)] bg-[var(--bg-surface)] p-4.5 transition-colors duration-200 hover:border-[var(--border-strong)] dark:shadow-[inset_0_1px_0_0_rgba(255,255,255,0.05),0_1px_3px_rgba(0,0,0,0.4)]">
      <div className="flex items-center justify-between gap-2">
        <p className="flex items-center gap-1.5 font-display text-xs font-semibold">
          <span className="flex h-5 w-5 items-center justify-center rounded-sm bg-[color-mix(in_oklab,var(--color-brand-500)_12%,transparent)] text-brand-500">
            <Icon className="h-3 w-3" />
          </span>
          {title}
        </p>
        <p className={cn("font-display text-lg font-semibold tabular", toneClass[tone])}>{pct.toFixed(1)}%</p>
      </div>
      <p className="mb-2.5 text-[10px] text-[var(--text-faint)] tabular">{detail}</p>
      <div className="space-y-1.5">
        {rows.length === 0 && <p className="text-xs text-[var(--text-muted)]">No online connections.</p>}
        {rows
          .slice()
          .sort((a, b) => b.pct - a.pct)
          .slice(0, 5)
          .map((r) => (
            <div key={r.name} className="flex items-center gap-2 text-xs">
              <span className="w-24 shrink-0 truncate text-[var(--text-muted)]" title={r.name}>{r.name}</span>
              <Meter value={r.pct} size="md" showLabel label="Utilization" className="flex-1" />
              <span className="hidden w-28 shrink-0 text-right text-[10px] text-[var(--text-faint)] tabular sm:block">{r.detail}</span>
            </div>
          ))}
        {rows.length > 5 && <p className="text-[10px] text-[var(--text-faint)]">+{rows.length - 5} more connections</p>}
      </div>
    </div>
  )
}
