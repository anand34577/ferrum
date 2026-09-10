import { useMutation, useQuery } from "@tanstack/react-query"
import { CheckCircle2, Layers, Loader2, XCircle } from "lucide-react"
import { useMemo, useState } from "react"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import { EmptyState } from "@/components/ui/empty-state"
import { Input } from "@/components/ui/input"
import { PageHeader } from "@/components/ui/page-header"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { TypeChip } from "@/components/ui/type-chip"
import { api, ApiError, type ClusterResource, type ConnectionInventory } from "@/lib/api"

// Mirrors api.bulkTarget / api.bulkActionResult (internal/api/bulk.go).
interface BulkTarget {
  connId: string
  type: "qemu" | "lxc"
  node: string
  vmid: number
}
interface BulkActionResult extends BulkTarget {
  success: boolean
  upid?: string
  error?: string
}

const ACTIONS = [
  { value: "start", label: "Start" },
  { value: "stop", label: "Stop" },
  { value: "shutdown", label: "Shutdown" },
  { value: "reboot", label: "Reboot" },
  { value: "suspend", label: "Suspend" },
  { value: "resume", label: "Resume" },
  { value: "snapshot", label: "Create snapshot" },
  { value: "tag", label: "Set tags" },
  { value: "delete", label: "Delete" },
] as const
type BulkAction = (typeof ACTIONS)[number]["value"]

interface GuestRow {
  connId: string
  connName: string
  guest: ClusterResource
}

/**
 * Fleet-wide bulk operations: pick guests from any connection/cluster, pick
 * one action, run it across all of them in a single request — the one thing
 * no single-connection Proxmox UI (or PDM) can do. Self-contained: pulls its
 * own guest list from the existing inventory endpoint and is not wired into
 * routing/navigation yet (a follow-up pass does that for everything built
 * this round at once).
 */
export function BulkOperationsPage() {
  const [selected, setSelected] = useState<Map<string, GuestRow>>(new Map())
  const [action, setAction] = useState<BulkAction>("start")
  const [snapshotName, setSnapshotName] = useState("")
  const [tagValue, setTagValue] = useState("")
  const [purgeJobs, setPurgeJobs] = useState(false)
  const [results, setResults] = useState<BulkActionResult[] | null>(null)
  const [filter, setFilter] = useState("")

  const { data: inventory, isLoading, isError } = useQuery({
    queryKey: ["inventory"],
    queryFn: () => api.get<ConnectionInventory[]>("/inventory/"),
  })

  const rows = useMemo<GuestRow[]>(() => {
    const out: GuestRow[] = []
    for (const conn of inventory ?? []) {
      if (!conn.online) continue
      for (const res of conn.resources ?? []) {
        if (res.type !== "qemu" && res.type !== "lxc") continue
        if (res.template) continue
        out.push({ connId: conn.connectionId, connName: conn.name, guest: res })
      }
    }
    return out
  }, [inventory])

  const filtered = useMemo(() => {
    const q = filter.trim().toLowerCase()
    if (!q) return rows
    return rows.filter(
      (r) =>
        (r.guest.name ?? "").toLowerCase().includes(q) ||
        String(r.guest.vmid ?? "").includes(q) ||
        r.guest.node.toLowerCase().includes(q) ||
        r.connName.toLowerCase().includes(q) ||
        (r.guest.tags ?? "").toLowerCase().includes(q),
    )
  }, [rows, filter])

  function key(r: GuestRow) {
    return `${r.connId}/${r.guest.type}/${r.guest.node}/${r.guest.vmid}`
  }

  function toggle(r: GuestRow) {
    setSelected((prev) => {
      const next = new Map(prev)
      const k = key(r)
      if (next.has(k)) next.delete(k)
      else next.set(k, r)
      return next
    })
  }

  function toggleAllFiltered() {
    setSelected((prev) => {
      const next = new Map(prev)
      const allSelected = filtered.length > 0 && filtered.every((r) => next.has(key(r)))
      for (const r of filtered) {
        if (allSelected) next.delete(key(r))
        else next.set(key(r), r)
      }
      return next
    })
  }

  const runMutation = useMutation({
    mutationFn: async () => {
      const targets: BulkTarget[] = Array.from(selected.values()).map((r) => ({
        connId: r.connId,
        type: r.guest.type as "qemu" | "lxc",
        node: r.guest.node,
        vmid: r.guest.vmid!,
      }))
      return api.post<BulkActionResult[]>("/bulk/guests/action", {
        targets,
        action,
        ...(action === "snapshot" ? { snapshotName } : {}),
        ...(action === "tag" ? { tags: tagValue } : {}),
        ...(action === "delete" ? { purgeJobs } : {}),
      })
    },
    onSuccess: (data) => setResults(data),
  })

  const needsSnapshotName = action === "snapshot" && snapshotName.trim() === ""
  const canRun = selected.size > 0 && !needsSnapshotName && !runMutation.isPending

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        icon={Layers}
        title="Bulk Operations"
        description="Run one action across guests spanning any mix of connections and clusters at once — power actions, snapshots, tags, or deletion — with per-guest results."
      />

      {isError && (
        <Card>
          <CardContent className="py-6 text-center text-sm text-[var(--status-error)]">
            Failed to load fleet inventory.
          </CardContent>
        </Card>
      )}

      <Card>
        <CardContent className="flex flex-col gap-3 py-4 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex flex-1 flex-wrap items-center gap-2">
            <Input
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              placeholder="Filter by name, VMID, node, tag…"
              className="max-w-xs"
            />
            <span className="text-xs text-[var(--text-muted)]">
              {selected.size} of {rows.length} guest{rows.length === 1 ? "" : "s"} selected
            </span>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            <Select value={action} onValueChange={(v) => setAction(v as BulkAction)}>
              <SelectTrigger className="w-40">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {ACTIONS.map((a) => (
                  <SelectItem key={a.value} value={a.value}>
                    {a.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>

            {action === "snapshot" && (
              <Input
                value={snapshotName}
                onChange={(e) => setSnapshotName(e.target.value)}
                placeholder="Snapshot name"
                className="w-40"
              />
            )}
            {action === "tag" && (
              <Input
                value={tagValue}
                onChange={(e) => setTagValue(e.target.value)}
                placeholder="tags (comma/semicolon)"
                className="w-48"
              />
            )}
            {action === "delete" && (
              <label className="flex items-center gap-1.5 text-xs text-[var(--text-muted)]">
                <Checkbox checked={purgeJobs} onCheckedChange={(v) => setPurgeJobs(v === true)} />
                Purge backup jobs
              </label>
            )}

            <Button
              variant={action === "delete" ? "destructive" : "default"}
              disabled={!canRun}
              onClick={() => runMutation.mutate()}
            >
              {runMutation.isPending && <Loader2 className="h-4 w-4 animate-spin" />}
              Run on {selected.size} guest{selected.size === 1 ? "" : "s"}
            </Button>
          </div>
        </CardContent>
      </Card>

      {runMutation.isError && (
        <p className="text-sm text-[var(--status-error)]">
          {runMutation.error instanceof ApiError ? runMutation.error.message : "Request failed."}
        </p>
      )}

      <Card className="overflow-hidden">
        {isLoading ? (
          <CardContent className="py-10 text-center text-sm text-[var(--text-muted)]">Loading inventory…</CardContent>
        ) : filtered.length === 0 ? (
          <CardContent>
            <EmptyState icon={Layers} title="No guests match" description="Adjust the filter, or add a connection first." />
          </CardContent>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm">
              <thead className="border-b border-[var(--border)] text-xs text-[var(--text-muted)]">
                <tr>
                  <th className="w-10 px-4 py-2">
                    <Checkbox
                      checked={filtered.length > 0 && filtered.every((r) => selected.has(key(r)))}
                      onCheckedChange={() => toggleAllFiltered()}
                      aria-label="Select all filtered guests"
                    />
                  </th>
                  <th className="px-2 py-2">Guest</th>
                  <th className="px-2 py-2">VMID</th>
                  <th className="px-2 py-2">Node</th>
                  <th className="px-2 py-2">Connection</th>
                  <th className="px-2 py-2">Status</th>
                  <th className="px-2 py-2">Result</th>
                </tr>
              </thead>
              <tbody>
                {filtered.map((r) => {
                  const k = key(r)
                  const checked = selected.has(k)
                  const result = results?.find(
                    (res) => res.connId === r.connId && res.type === r.guest.type && res.node === r.guest.node && res.vmid === r.guest.vmid,
                  )
                  return (
                    <tr key={k} className="border-b border-[var(--border)]/60 last:border-0 hover:bg-[var(--bg-surface-hover)]">
                      <td className="px-4 py-2">
                        <Checkbox checked={checked} onCheckedChange={() => toggle(r)} aria-label={`Select ${r.guest.name ?? r.guest.vmid}`} />
                      </td>
                      <td className="px-2 py-2">
                        <span className="flex items-center gap-2">
                          <TypeChip type={r.guest.type} />
                          <span className="truncate">{r.guest.name || `#${r.guest.vmid}`}</span>
                        </span>
                      </td>
                      <td className="px-2 py-2 font-mono text-xs text-[var(--text-muted)]">{r.guest.vmid}</td>
                      <td className="px-2 py-2 text-xs text-[var(--text-muted)]">{r.guest.node}</td>
                      <td className="px-2 py-2 text-xs text-[var(--text-muted)]">{r.connName}</td>
                      <td className="px-2 py-2 text-xs text-[var(--text-muted)]">{r.guest.status ?? "-"}</td>
                      <td className="px-2 py-2">
                        {result &&
                          (result.success ? (
                            <span className="inline-flex items-center gap-1 text-xs text-[var(--status-ok)]">
                              <CheckCircle2 className="h-3.5 w-3.5" /> OK
                            </span>
                          ) : (
                            <span className="inline-flex items-center gap-1 text-xs text-[var(--status-error)]" title={result.error}>
                              <XCircle className="h-3.5 w-3.5" /> {result.error}
                            </span>
                          ))}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </Card>
    </div>
  )
}
