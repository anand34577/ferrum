import { useQueries, useQuery } from "@tanstack/react-query"
import type { ColumnDef } from "@tanstack/react-table"
import { Database, HardDrive } from "lucide-react"
import { useMemo } from "react"
import { Bar, BarChart, Cell, LabelList, Rectangle, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts"
import type { BarShapeProps } from "recharts"
import { DonutChart, type DonutSlice } from "@/components/charts/DonutChart"
import { chartTooltip } from "@/components/charts/tooltipTheme"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { DataTable } from "@/components/ui/data-table"
import { EmptyState } from "@/components/ui/empty-state"
import { ErrorState } from "@/components/ui/error-state"
import { PageHeader } from "@/components/ui/page-header"
import { Skeleton } from "@/components/ui/skeleton"
import { StatusDot } from "@/components/ui/status-dot"
import { api, type CephOSD, type CephPool, type CephStatus, type ClusterResource, type ConnectionInventory } from "@/lib/api"
import { formatBytes, formatPercentFine } from "@/lib/utils"

const TYPE_COLORS = [
  "var(--chart-1)",
  "var(--chart-2)",
  "var(--chart-3)",
  "var(--chart-4)",
  "var(--chart-5)",
  "var(--chart-6)",
]

export function StoragePage() {
  const { data: inventory, isLoading, isError, refetch } = useQuery({
    queryKey: ["inventory"],
    queryFn: () => api.get<ConnectionInventory[]>("/inventory/"),
  })

  const connections = inventory ?? []
  const firstNodeByConn = connections.map((c) => ({
    connId: c.connectionId,
    connName: c.name,
    node: (c.resources ?? []).find((r) => r.type === "node")?.node,
  }))

  const cephQueries = useQueries({
    queries: firstNodeByConn
      .filter((t) => t.node)
      .map((t) => ({
        queryKey: ["ceph-status", t.connId],
        queryFn: () => api.get<CephStatus>(`/connections/${t.connId}/nodes/${t.node}/ceph/status`),
        retry: false,
      })),
  })

  const cephPoolQueries = useQueries({
    queries: firstNodeByConn
      .filter((t) => t.node)
      .map((t) => ({
        queryKey: ["ceph-pools", t.connId],
        queryFn: () => api.get<CephPool[]>(`/connections/${t.connId}/nodes/${t.node}/ceph/pools`),
        retry: false,
      })),
  })

  const cephOSDQueries = useQueries({
    queries: firstNodeByConn
      .filter((t) => t.node)
      .map((t) => ({
        queryKey: ["ceph-osds", t.connId],
        queryFn: () => api.get<CephOSD[]>(`/connections/${t.connId}/nodes/${t.node}/ceph/osds`),
        retry: false,
      })),
  })

  const storagePools = connections.flatMap((c) =>
    (c.resources ?? [])
      .filter((r) => r.type === "storage")
      .map((r) => ({ ...r, connName: c.name })),
  )

  // Local storage (dir/lvm/zfspool/...) is genuinely separate capacity per
  // node — a fleet total that sums every node's "local-lvm" is correct.
  // Shared storage (nfs/cifs/pbs/cephfs/iscsi/...) is the opposite: PVE's
  // cluster/resources reports the *same* pool once per node it's mounted
  // on, so naively summing it the same way multiplied its capacity by the
  // node count (an 8-node cluster made a 2TB NFS share read as 16TB).
  //
  // PVE's own `shared` flag is not a reliable signal for this split: it
  // only reflects whether the storage config has the "Shared" checkbox
  // ticked, and plenty of real setups instead add the same NFS/CIFS target
  // as a near-identical per-node definition without ever ticking it — which
  // reports shared:0 despite being one physical volume, and (worse) fed
  // several rows sharing one name into a chart's category axis, which
  // recharts doesn't handle sensibly. So the split below leads with
  // plugintype (network-capable types are "shared" regardless of the flag)
  // and, within that group, dedupes by an exact (name, total, used) match:
  // two truly independent volumes are vanishingly unlikely to report
  // byte-for-byte identical total *and* used capacity at the same instant,
  // while duplicate reports of one physical volume always do.
  const LOCAL_ONLY_TYPES = new Set(["dir", "lvm", "lvmthin", "zfspool", "btrfs"])
  const isNetworkStorage = (p: ClusterResource) => !!p.shared || (!!p.plugintype && !LOCAL_ONLY_TYPES.has(p.plugintype))

  const localPools = storagePools.filter((p) => !isNetworkStorage(p))
  const sharedPools = useMemo(() => {
    const seen = new Map<string, ClusterResource & { connName: string }>()
    for (const p of storagePools) {
      if (!isNetworkStorage(p)) continue
      const key = `${p.connName}|${p.storage ?? p.name}|${p.maxdisk ?? 0}|${p.disk ?? 0}`
      if (!seen.has(key)) seen.set(key, p)
    }
    return Array.from(seen.values())
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [storagePools])

  function rollUp(pools: (ClusterResource & { connName: string })[]) {
    // Roll every storage up by pool NAME across connections — both its total
    // capacity and its current usage, so the legend can show used/total and
    // how full each pool actually is.
    const byName = new Map<string, { total: number; used: number }>()
    for (const p of pools) {
      const key = p.storage ?? "unknown"
      const cur = byName.get(key) ?? { total: 0, used: 0 }
      cur.total += p.maxdisk ?? 0
      cur.used += p.disk ?? 0
      byName.set(key, cur)
    }
    const rows = Array.from(byName.entries())
      .map(([name, v]) => ({ name, total: v.total, used: v.used }))
      .sort((a, b) => b.total - a.total)
    const grandTotal = rows.reduce((s, r) => s + r.total, 0)
    const grandUsed = rows.reduce((s, r) => s + r.used, 0)
    // The ring is sized by USED bytes per pool, not total capacity — a
    // capacity-share ring reads as "100% used" the instant there's only one
    // pool (a single slice necessarily fills the whole ring), which is
    // exactly backwards from what "224 GB used of 1.2 TB" means. A trailing
    // "Free" slice (the untouched remainder) makes the ring double as an
    // overall usage gauge; the 7 largest pools get their own slice and
    // everything smaller folds into "other" so it never turns into confetti.
    const top = rows.slice(0, 7)
    const restUsed = rows.slice(7).reduce((s, r) => s + r.used, 0)
    const slices: DonutSlice[] = top.map((r, i) => ({ name: r.name, value: r.used, color: TYPE_COLORS[i % TYPE_COLORS.length] }))
    if (restUsed > 0) slices.push({ name: `other (${rows.length - 7})`, value: restUsed, color: "var(--text-faint)" })
    const free = grandTotal - grandUsed
    if (free > 0) slices.push({ name: "Free", value: free, color: "var(--track)" })
    return { rows, slices, grandTotal, grandUsed }
  }

  // eslint-disable-next-line react-hooks/exhaustive-deps
  const localCapacity = useMemo(() => rollUp(localPools), [localPools])
  const sharedCapacity = useMemo(() => rollUp(sharedPools), [sharedPools])

  // suffixNode disambiguates local pools that legitimately share a name
  // across nodes (e.g. every node's "local-lvm") — a bar chart's category
  // axis needs a unique label per row, or a charting library like recharts
  // has no sane way to lay out same-named rows as distinct bars.
  function usageBars(pools: (ClusterResource & { connName: string })[], suffixNode: boolean) {
    return pools
      .filter((p) => (p.maxdisk ?? 0) > 0)
      .map((p) => {
        const base = p.storage ?? p.name ?? "unknown"
        return { name: suffixNode && p.node ? `${base} (${p.node})` : base, pct: Math.min(100, ((p.disk ?? 0) / (p.maxdisk ?? 1)) * 100) }
      })
      .sort((a, b) => b.pct - a.pct)
      .slice(0, 8)
  }
  // Local pools stay one bar per node (each is genuinely separate capacity),
  // labeled with their node since the same name (e.g. "local-lvm") is
  // expected on every node; shared pools use the deduped list so a pool
  // mounted on every node in the cluster shows up once, not once per node
  // at an identical percentage.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const usageByPool = useMemo(
    () => [...usageBars(localPools, true), ...usageBars(sharedPools, false)].sort((a, b) => b.pct - a.pct).slice(0, 8),
    [localPools, sharedPools],
  )

  const columns = useMemo<ColumnDef<ClusterResource & { connName: string }>[]>(
    () => [
      { accessorKey: "storage", header: "Pool", cell: (c) => <span className="font-medium">{c.getValue<string>() ?? c.row.original.name}</span> },
      { accessorKey: "connName", header: "Connection", meta: { hideBelowMd: true } },
      { accessorKey: "node", header: "Node" },
      {
        accessorKey: "plugintype",
        header: "Type",
        meta: { hideBelowMd: true },
        cell: (c) => (c.getValue<string>() ? <Badge>{c.getValue<string>()}</Badge> : <span className="text-[var(--text-muted)]">-</span>),
      },
      {
        accessorKey: "shared",
        header: "Shared",
        cell: (c) => (c.getValue<number>() ? <Badge variant="ok">Shared</Badge> : <span className="text-xs text-[var(--text-muted)]">Local</span>),
      },
      {
        id: "usage",
        header: "Usage",
        cell: (c) => {
          const p = c.row.original
          const pct = p.maxdisk ? Math.min(100, ((p.disk ?? 0) / p.maxdisk) * 100) : 0
          return (
            <div className="flex items-center gap-2">
              <div className="h-1.5 w-24 overflow-hidden rounded-sm bg-[var(--track)]">
                <div className={pct > 85 ? "h-full bg-[var(--status-error)]" : "h-full bg-brand-500"} style={{ width: `${pct}%` }} />
              </div>
              <span className="whitespace-nowrap text-xs text-[var(--text-muted)]">
                {formatBytes(p.disk ?? 0)} / {formatBytes(p.maxdisk ?? 0)}
              </span>
            </div>
          )
        },
      },
    ],
    [],
  )

  return (
    <div className="space-y-4">
      <PageHeader
        title="Storage"
        description="Storage pools and Ceph health across every connection."
        icon={Database}
      />

      {isError ? (
        <ErrorState title="Couldn't load storage" onRetry={refetch} />
      ) : isLoading ? (
        <div className="space-y-4" aria-busy>
          <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
            <Skeleton className="h-64" />
            <Skeleton className="h-64" />
          </div>
          <Skeleton className="h-72" />
        </div>
      ) : connections.length === 0 ? (
        <EmptyState
          icon={Database}
          title="No connections configured yet"
          description="Add a Proxmox connection to see its storage pools and Ceph health here."
        />
      ) : (
        <>
      {(localCapacity.rows.length > 0 || sharedCapacity.rows.length > 0) && (
        <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
          {localCapacity.rows.length > 0 && <CapacityCard title="Local storage capacity" capacity={localCapacity} />}
          {sharedCapacity.rows.length > 0 && <CapacityCard title="Shared / external storage capacity" capacity={sharedCapacity} />}

          <Card className={localCapacity.rows.length > 0 && sharedCapacity.rows.length > 0 ? "lg:col-span-2" : undefined}>
            <CardHeader>
              <CardTitle>Fullest pools</CardTitle>
              <p className="text-xs text-[var(--text-muted)]">Local and shared, combined — each shared pool counted once regardless of how many nodes mount it.</p>
            </CardHeader>
            <CardContent>
              <ResponsiveContainer width="100%" height={Math.max(160, usageByPool.length * 28)}>
                <BarChart data={usageByPool} layout="vertical" margin={{ top: 0, right: 36, left: 0, bottom: 0 }}>
                  <XAxis type="number" domain={[0, 100]} hide />
                  <YAxis type="category" dataKey="name" width={100} tick={{ fontSize: 11, fill: "var(--text-muted)" }} axisLine={false} tickLine={false} />
                  <Tooltip
                    cursor={false}
                    formatter={(v) => `${Number(v).toFixed(0)}% used`}
                    {...chartTooltip}
                  />
                  {/* background = the full-length track, so the unfilled
                      remainder reads as "space left" instead of fading into
                      the card. Radius matches the track on every corner so
                      the fill never pokes past its rounded ends. Hover
                      feedback is a 1px outline on the hovered row's track. */}
                  <Bar
                    dataKey="pct"
                    radius={[4, 4, 4, 4]}
                    barSize={12}
                    background={(p: BarShapeProps) => (
                      <Rectangle
                        x={p.x}
                        y={p.y}
                        width={p.width}
                        height={p.height}
                        fill="var(--track)"
                        radius={4}
                        stroke={p.isActive ? "var(--text-faint)" : "none"}
                        strokeWidth={1}
                      />
                    )}
                    isAnimationActive={false}
                  >
                    {usageByPool.map((p) => (
                      <Cell key={p.name} fill={p.pct >= 90 ? "var(--status-error)" : p.pct >= 75 ? "var(--status-warn)" : "var(--color-brand-500)"} />
                    ))}
                    <LabelList dataKey="pct" position="right" formatter={(v) => `${Number(v).toFixed(0)}%`} style={{ fill: "var(--text-muted)", fontSize: 11 }} />
                  </Bar>
                </BarChart>
              </ResponsiveContainer>
            </CardContent>
          </Card>
        </div>
      )}

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <HardDrive className="h-4 w-4" /> Local Storage
          </CardTitle>
          <p className="text-xs text-[var(--text-muted)]">Physically attached to one node — dir, LVM, ZFS, and similar. Not shared across the cluster.</p>
        </CardHeader>
        <CardContent>
          <DataTable columns={columns} data={localPools} searchPlaceholder="Search local storage..." emptyMessage="No local storage found." />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Database className="h-4 w-4" /> Shared / External Storage
          </CardTitle>
          <p className="text-xs text-[var(--text-muted)]">NFS, CIFS, PBS, Ceph, iSCSI, and similar — reachable from (and reported by) every node it's mounted on.</p>
        </CardHeader>
        <CardContent>
          <DataTable columns={columns} data={sharedPools} searchPlaceholder="Search shared storage..." emptyMessage="No shared storage found." />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Database className="h-4 w-4" /> Ceph Health
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-2">
          {firstNodeByConn
            .filter((t) => t.node)
            .map((t, i) => {
              const q = cephQueries[i]
              // A failing/absent Ceph endpoint simply means the cluster doesn't
              // run Ceph — say so instead of hiding the row entirely.
              return (
                <div key={t.connId} className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-[var(--border)] px-3 py-2 text-sm">
                  <span className="truncate font-medium">{t.connName}</span>
                  {q.data ? (
                    <div className="flex flex-wrap items-center gap-3">
                      <Badge variant={q.data.health.status === "HEALTH_OK" ? "ok" : "warn"}>{q.data.health.status}</Badge>
                      <span className="text-xs text-[var(--text-muted)] tabular">
                        {q.data.osdmap.num_up_osds}/{q.data.osdmap.num_osds} OSDs up
                      </span>
                      <span className="text-xs text-[var(--text-muted)] tabular">
                        {formatBytes(q.data.pgmap.bytes_used)} / {formatBytes(q.data.pgmap.bytes_total)}
                      </span>
                    </div>
                  ) : (
                    <span className="text-xs text-[var(--text-muted)]">{q.isPending ? "Checking…" : "Not configured"}</span>
                  )}
                </div>
              )
            })}
        </CardContent>
      </Card>

      {firstNodeByConn
        .filter((t) => t.node)
        .map((t, i) => {
          const pools = cephPoolQueries[i]?.data
          if (!pools || pools.length === 0) return null
          return (
            <Card key={`ceph-pools-${t.connId}`}>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <Database className="h-4 w-4" /> Ceph Pools — {t.connName}
                </CardTitle>
              </CardHeader>
              <CardContent className="space-y-1.5">
                {pools.map((p) => (
                  <div key={p.pool_name} className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-[var(--border)] px-3 py-2 text-sm">
                    <div className="min-w-0">
                      <p className="truncate font-medium">{p.pool_name}</p>
                      <p className="text-xs text-[var(--text-muted)]">
                        {p.pg_num} PGs · replication {p.size}/{p.min_size}
                      </p>
                    </div>
                    <div className="flex shrink-0 items-center gap-2">
                      {p.bytes_used !== undefined && <span className="text-xs text-[var(--text-muted)]">{formatBytes(p.bytes_used)}</span>}
                      {p.percent_used !== undefined && (
                        <Badge variant={p.percent_used > 85 ? "error" : p.percent_used > 70 ? "warn" : "ok"}>
                          {p.percent_used.toFixed(0)}% used
                        </Badge>
                      )}
                    </div>
                  </div>
                ))}
              </CardContent>
            </Card>
          )
        })}

      {firstNodeByConn
        .filter((t) => t.node)
        .map((t, i) => {
          const osds = cephOSDQueries[i]?.data
          if (!osds || osds.length === 0) return null
          return (
            <Card key={`ceph-osds-${t.connId}`}>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <HardDrive className="h-4 w-4" /> Ceph OSDs — {t.connName}
                </CardTitle>
              </CardHeader>
              <CardContent className="flex flex-wrap gap-2">
                {osds.map((osd) => (
                  <div key={osd.id} className="flex items-center gap-2 rounded-md bg-[var(--bg-muted)] px-3 py-1.5 text-sm">
                    <StatusDot status={osd.up === 1 ? "ok" : "error"} />
                    <span className="font-mono text-xs">osd.{osd.id}</span>
                    {osd.host && <span className="text-xs text-[var(--text-muted)]">{osd.host}</span>}
                    <span className="text-xs text-[var(--text-muted)]">
                      {osd.up === 1 ? "up" : "down"} · {osd.in === 1 ? "in" : "out"}
                    </span>
                  </div>
                ))}
              </CardContent>
            </Card>
          )
        })}
        </>
      )}
    </div>
  )
}

interface CapacityRollup {
  rows: { name: string; total: number; used: number }[]
  slices: DonutSlice[]
  grandTotal: number
  grandUsed: number
}

/* Fixed-size donut beside a real legend — a centered flex column lets
   ResponsiveContainer collapse to min-content, which wraps the center label
   vertically ("1.1"/"TB"/"total"/…). Shared between the local and shared/
   external capacity cards so the two never drift into different layouts. */
function CapacityCard({ title, capacity }: { title: string; capacity: CapacityRollup }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        <p className="text-xs text-[var(--text-muted)] tabular">
          {formatBytes(capacity.grandUsed)} used of {formatBytes(capacity.grandTotal)} across {capacity.rows.length} pool{capacity.rows.length === 1 ? "" : "s"}
        </p>
      </CardHeader>
      <CardContent className="flex flex-col items-center gap-5 sm:flex-row">
        <div className="w-44 shrink-0 self-center">
          <DonutChart
            data={capacity.slices}
            height={168}
            centerValue={capacity.grandTotal > 0 ? formatPercentFine((capacity.grandUsed / capacity.grandTotal) * 100) : "0%"}
            centerLabel="used"
            formatValue={formatBytes}
          />
        </div>
        <ul className="min-w-0 flex-1 space-y-2.5">
          {capacity.rows.slice(0, 7).map((r, i) => {
            const color = TYPE_COLORS[i % TYPE_COLORS.length]
            // How full THIS pool is — not its share of the fleet's total
            // capacity, which is a near-useless (and, with one pool,
            // always-100%-and-misleading) number to lead with here.
            const pctUsed = r.total > 0 ? (r.used / r.total) * 100 : 0
            return (
              <li key={r.name} className="grid grid-cols-[auto_1fr_auto] items-center gap-2 text-xs">
                <span className="h-2 w-2 shrink-0 rounded-full" style={{ background: color }} aria-hidden />
                <div className="min-w-0">
                  <p className="flex items-baseline justify-between gap-2">
                    <span className="truncate font-medium" title={r.name}>{r.name}</span>
                    <span className="shrink-0 text-[var(--text-muted)] tabular">{pctUsed.toFixed(0)}% used</span>
                  </p>
                  <div className="mt-1 h-1 w-full overflow-hidden rounded-sm bg-[var(--track)]">
                    <div className="h-full rounded-sm" style={{ width: `${Math.min(100, pctUsed)}%`, background: color }} />
                  </div>
                </div>
                <div className="shrink-0 text-right tabular">
                  <p className="font-medium">{formatBytes(r.total)}</p>
                  <p className="text-[10px] text-[var(--text-muted)]">{formatBytes(r.used)} used</p>
                </div>
              </li>
            )
          })}
          {capacity.rows.length > 7 && (
            <li className="pt-0.5 text-[10px] text-[var(--text-faint)]">+{capacity.rows.length - 7} smaller pool{capacity.rows.length - 7 === 1 ? "" : "s"} in “other”</li>
          )}
        </ul>
      </CardContent>
    </Card>
  )
}
