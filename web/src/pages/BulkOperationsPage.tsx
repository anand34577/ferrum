import { useMutation, useQuery } from "@tanstack/react-query"
import type { ColumnDef } from "@tanstack/react-table"
import { CheckCircle2, Layers, XCircle } from "lucide-react"
import { useMemo, useState } from "react"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { DataTable } from "@/components/ui/data-table"
import { ErrorState } from "@/components/ui/error-state"
import { Input } from "@/components/ui/input"
import { PageHeader } from "@/components/ui/page-header"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { TypeChip } from "@/components/ui/type-chip"
import { api, ApiError, type ClusterResource, type ConnectionInventory } from "@/lib/api"
import { cn } from "@/lib/utils"

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
 * one action, run it across all of them in a single request — power actions,
 * snapshots, tags, or deletion — with per-guest results. Reachable from the
 * sidebar's Bulk Operations entry (admin only).
 */
export function BulkOperationsPage() {
  const confirm = useConfirm()
  // Selection is a Set of row ids — DataTable's selection API. The GuestRow
  // behind each id is re-derived from `rows` wherever the full object is
  // needed (running the action, naming guests in the confirms).
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [action, setAction] = useState<BulkAction>("start")
  const [snapshotName, setSnapshotName] = useState("")
  const [tagValue, setTagValue] = useState("")
  const [purgeJobs, setPurgeJobs] = useState(false)
  const [results, setResults] = useState<BulkActionResult[] | null>(null)

  const { data: inventory, isLoading, isError, refetch } = useQuery({
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

  function key(r: GuestRow) {
    return `${r.connId}/${r.guest.type}/${r.guest.node}/${r.guest.vmid}`
  }

  const selectedRows = useMemo(() => rows.filter((r) => selected.has(key(r))), [rows, selected])

  const runMutation = useMutation({
    mutationFn: async () => {
      const targets: BulkTarget[] = selectedRows
        .filter((r) => r.guest.vmid !== undefined)
        .map((r) => ({
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

  // The two actions that destroy work or availability confirm first — this is
  // the one screen that can take down dozens of guests (or delete them
  // outright) in a single click, the same guard Inventory's bulk bar and the
  // guest detail dialog put in front of their hard-stop/delete paths.
  // Graceful actions (start/shutdown/reboot/suspend/resume/snapshot/tag)
  // stay one-click, matching PVE's list UX.
  async function run() {
    if (action === "delete") {
      const ok = await confirm({
        title: `Delete ${selected.size} guest${selected.size === 1 ? "" : "s"}?`,
        description: (
          <>
            Each guest and all of its disks is removed from its cluster. This cannot be undone.
            {purgeJobs && " Associated backup jobs are purged too."}
            <br />
            <span className="mt-1.5 block text-[var(--text-muted)]">{selectedNameList()}</span>
          </>
        ),
        confirmLabel: `Delete ${selected.size} guest${selected.size === 1 ? "" : "s"}`,
      })
      if (!ok) return
    }
    if (action === "stop") {
      const ok = await confirm({
        title: `Hard-stop ${selected.size} guest${selected.size === 1 ? "" : "s"}?`,
        description: (
          <>
            Each guest is stopped like pulling the power cord — unsaved data inside the guests is lost.
            <br />
            <span className="mt-1.5 block text-[var(--text-muted)]">{selectedNameList()}</span>
          </>
        ),
        confirmLabel: `Stop ${selected.size} guest${selected.size === 1 ? "" : "s"}`,
      })
      if (!ok) return
    }
    runMutation.mutate()
  }

  // First 5 names + overflow count — enough to catch "wait, that's not who I
  // meant to select" without turning the confirm into a scrollable list.
  function selectedNameList() {
    const names = selectedRows.map((r) => r.guest.name ?? `#${r.guest.vmid}`)
    return names.length <= 5 ? names.join(", ") : `${names.slice(0, 5).join(", ")} and ${names.length - 5} more`
  }

  const resultSummary = useMemo(() => {
    if (!results) return null
    const ok = results.filter((r) => r.success).length
    return { ok, failed: results.length - ok }
  }, [results])

  // Tags ride along in the guest column's searchable value: DataTable's
  // built-in search only sees accessor values and the old ad-hoc filter
  // matched tags too, but there's no tags column to index.
  const columns = useMemo<ColumnDef<GuestRow>[]>(
    () => [
      {
        id: "guest",
        header: "Guest",
        accessorFn: (r) => `${r.guest.name || `#${r.guest.vmid}`} ${r.guest.tags ?? ""}`,
        cell: (c) => (
          <span className="flex items-center gap-2">
            <TypeChip type={c.row.original.guest.type} />
            <span className="truncate">{c.row.original.guest.name || `#${c.row.original.guest.vmid}`}</span>
          </span>
        ),
      },
      {
        id: "vmid",
        header: "VMID",
        accessorFn: (r) => r.guest.vmid ?? 0,
        cell: (c) => <span className="font-mono text-[var(--text-muted)]">{c.getValue<number>()}</span>,
      },
      {
        id: "node",
        header: "Node",
        accessorFn: (r) => r.guest.node,
        cell: (c) => <span className="text-[var(--text-muted)]">{c.getValue<string>()}</span>,
      },
      {
        id: "connection",
        header: "Connection",
        accessorFn: (r) => r.connName,
        cell: (c) => <span className="text-[var(--text-muted)]">{c.getValue<string>()}</span>,
      },
      {
        id: "status",
        header: "Status",
        accessorFn: (r) => r.guest.status ?? "",
        // Shown but not searched, so the search matches exactly what the old
        // ad-hoc filter matched.
        enableGlobalFilter: false,
        cell: (c) => <span className="text-[var(--text-muted)]">{c.getValue<string>() || "-"}</span>,
      },
      {
        id: "result",
        header: "Result",
        enableSorting: false,
        cell: (c) => {
          const r = c.row.original
          const result = results?.find(
            (res) => res.connId === r.connId && res.type === r.guest.type && res.node === r.guest.node && res.vmid === r.guest.vmid,
          )
          if (!result) return null
          return result.success ? (
            <span className="inline-flex items-center gap-1 text-[var(--status-ok)]">
              <CheckCircle2 className="h-3.5 w-3.5" /> OK
            </span>
          ) : (
            // Error text stays inline (title only as a tooltip) — a failure
            // you have to hover for is a failure nobody reads.
            <span className="inline-flex items-center gap-1 text-[var(--status-error)]" title={result.error}>
              <XCircle className="h-3.5 w-3.5" /> {result.error}
            </span>
          )
        },
      },
    ],
    [results],
  )

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        icon={Layers}
        title="Bulk Operations"
        description="Run one action across guests spanning any mix of connections and clusters at once — power actions, snapshots, tags, or deletion — with per-guest results."
      />

      {isError && (
        <ErrorState
          title="Couldn't load your fleet inventory"
          message="Guests and nodes could not be fetched, so there is nothing to select yet. Check your connections and try again."
          onRetry={() => refetch()}
        />
      )}

      {runMutation.isError && (
        <p role="alert" className="text-sm text-[var(--status-error)]">
          {runMutation.error instanceof ApiError ? runMutation.error.message : "The bulk action could not be submitted — nothing was run. Try again."}
        </p>
      )}

      {resultSummary && (
        <p className="text-sm text-[var(--text-muted)]" role="status" aria-live="polite">
          <span className="font-medium text-[var(--status-ok)]">{resultSummary.ok} succeeded</span>
          {" · "}
          <span className={cn("font-medium", resultSummary.failed > 0 && "text-[var(--status-error)]")}>{resultSummary.failed} failed</span>
          {" — per-guest results below"}
        </p>
      )}

      <Card>
        <CardContent className="pt-4">
          <DataTable
            columns={columns}
            data={rows}
            loading={isLoading}
            pageSize={15}
            searchPlaceholder="Filter by name, VMID, node, tag…"
            emptyMessage="No guests match — adjust the filter, or add a connection first."
            selection={{ rowId: key, selected, onSelectedChange: setSelected }}
            toolbar={
              <div className="flex flex-1 flex-wrap items-center justify-end gap-2">
                <span className="text-xs text-[var(--text-muted)]" aria-live="polite">
                  {selected.size} of {rows.length} guest{rows.length === 1 ? "" : "s"} selected
                </span>

                <Select value={action} onValueChange={(v) => setAction(v as BulkAction)}>
                  <SelectTrigger className="w-40" aria-label="Bulk action">
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
                  loading={runMutation.isPending}
                  onClick={() => void run()}
                >
                  Run on {selected.size} guest{selected.size === 1 ? "" : "s"}
                </Button>
              </div>
            }
          />
        </CardContent>
      </Card>
    </div>
  )
}
