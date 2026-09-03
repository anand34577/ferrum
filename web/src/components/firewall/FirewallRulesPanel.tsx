import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import type { ColumnDef } from "@tanstack/react-table"
import { Plus, Trash2 } from "lucide-react"
import { useMemo, useState } from "react"
import { toast } from "sonner"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { DataTable } from "@/components/ui/data-table"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Hint } from "@/components/ui/tooltip"
import { api, ApiError, type FirewallRule } from "@/lib/api"

const emptyForm = { type: "in", action: "ACCEPT", source: "", dest: "", proto: "", dport: "", macro: "", comment: "" }

/** Add/list/delete firewall rules for one scope (cluster, a node, or a
 * guest) — the rule shape and API surface are identical at every scope, so
 * this is written once and reused for all three instead of copy-pasted. */
export function FirewallRulesPanel({ basePath, queryKey }: { basePath: string; queryKey: unknown[] }) {
  const queryClient = useQueryClient()
  const confirm = useConfirm()
  const rulesQuery = useQuery({
    queryKey,
    queryFn: () => api.get<FirewallRule[]>(`${basePath}/rules`),
    retry: false,
  })

  const [form, setForm] = useState(emptyForm)
  const [showForm, setShowForm] = useState(false)
  const [selected, setSelected] = useState<Set<string>>(new Set())

  function invalidate() {
    queryClient.invalidateQueries({ queryKey })
  }

  const addRule = useMutation({
    mutationFn: () => api.post(`${basePath}/rules`, { ...form, enable: true }),
    onSuccess: () => {
      toast.success("Rule added")
      setForm(emptyForm)
      setShowForm(false)
      invalidate()
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to add rule"),
  })

  const deleteRule = useMutation({
    mutationFn: (pos: number) => api.delete(`${basePath}/rules/${pos}`),
    onSuccess: () => {
      toast.success("Rule deleted")
      invalidate()
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to delete rule"),
  })

  const bulkDelete = useMutation({
    mutationFn: async (positions: string[]) => {
      // Positions shift down after each delete — go highest-first so a
      // batch delete doesn't remove the wrong rules partway through.
      const sorted = positions.map(Number).sort((a, b) => b - a)
      for (const pos of sorted) await api.delete(`${basePath}/rules/${pos}`)
      return sorted.length
    },
    onSuccess: (count) => {
      toast.success(`${count} rule${count === 1 ? "" : "s"} deleted`)
      setSelected(new Set())
      invalidate()
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to delete some rules"),
  })

  async function removeRule(rule: FirewallRule) {
    const ok = await confirm({
      title: `Delete rule #${rule.pos}?`,
      description: rule.action === "ACCEPT"
        ? "Traffic this rule allowed falls through to the next matching rule or the default policy."
        : `Traffic that was ${rule.action === "DROP" ? "blocked" : "rejected"} by this rule may get through.`,
      confirmLabel: "Delete rule",
    })
    if (ok) deleteRule.mutate(rule.pos)
  }

  async function bulkRemove(ids: string[]) {
    const ok = await confirm({
      title: `Delete ${ids.length} rule${ids.length === 1 ? "" : "s"}?`,
      description: "All selected rules are removed at once. Traffic they matched falls through to the remaining rules and the default policy.",
      confirmLabel: "Delete rules",
    })
    if (ok) bulkDelete.mutate(ids)
  }

  const columns = useMemo<ColumnDef<FirewallRule>[]>(
    () => [
      { accessorKey: "pos", header: "#", cell: (c) => <span className="font-mono text-xs tabular">{c.getValue<number>()}</span> },
      { accessorKey: "type", header: "Type" },
      {
        accessorKey: "action",
        header: "Action",
        cell: (c) => {
          const v = c.getValue<string>()
          return <Badge variant={v === "ACCEPT" ? "ok" : v === "DROP" || v === "REJECT" ? "error" : "default"}>{v}</Badge>
        },
      },
      {
        accessorKey: "macro",
        header: "Macro",
        cell: (c) => (c.getValue<string>() ? <Badge>{c.getValue<string>()}</Badge> : <span className="text-[var(--text-muted)]">-</span>),
      },
      { accessorKey: "source", header: "Source", cell: (c) => <span className="font-mono text-xs">{c.getValue<string>() ?? "-"}</span> },
      { accessorKey: "dest", header: "Dest", cell: (c) => <span className="font-mono text-xs">{c.getValue<string>() ?? "-"}</span> },
      {
        id: "protoPort",
        header: "Proto/Port",
        cell: (c) => <span className="font-mono text-xs">{[c.row.original.proto, c.row.original.dport].filter(Boolean).join(":") || "-"}</span>,
      },
      { accessorKey: "enable", header: "Enabled", cell: (c) => (c.getValue<number>() === 0 ? "No" : "Yes") },
      { accessorKey: "comment", header: "Comment", cell: (c) => <span className="text-xs text-[var(--text-muted)]">{c.getValue<string>()}</span> },
      {
        id: "actions",
        header: "",
        cell: (c) => (
          <Hint label="Delete rule">
            <Button
              size="icon"
              variant="ghost"
              aria-label={`Delete rule ${c.row.original.pos}`}
              className="hover:bg-[color-mix(in_oklab,var(--status-error)_12%,transparent)] hover:text-[var(--status-error)]"
              onClick={() => removeRule(c.row.original)}
            >
              <Trash2 className="h-3.5 w-3.5" />
            </Button>
          </Hint>
        ),
      },
    ],
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [deleteRule],
  )

  return (
    <div className="space-y-3">
      <Button size="sm" variant="secondary" onClick={() => setShowForm((s) => !s)}>
        <Plus className="h-3.5 w-3.5" /> Add rule
      </Button>
      {showForm && (
        <div className="grid grid-cols-2 gap-3 rounded-md border border-[var(--border)] p-3 sm:grid-cols-4">
          <div className="space-y-1.5">
            <Label>Direction</Label>
            <Select value={form.type} onValueChange={(v) => setForm({ ...form, type: v })}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="in">Inbound</SelectItem>
                <SelectItem value="out">Outbound</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-1.5">
            <Label>Action</Label>
            <Select value={form.action} onValueChange={(v) => setForm({ ...form, action: v })}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="ACCEPT">Accept</SelectItem>
                <SelectItem value="DROP">Drop</SelectItem>
                <SelectItem value="REJECT">Reject</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-1.5">
            <Label>Source</Label>
            <Input value={form.source} onChange={(e) => setForm({ ...form, source: e.target.value })} placeholder="10.0.0.0/24" />
          </div>
          <div className="space-y-1.5">
            <Label>Dest</Label>
            <Input value={form.dest} onChange={(e) => setForm({ ...form, dest: e.target.value })} />
          </div>
          <div className="space-y-1.5">
            <Label>Macro (optional)</Label>
            <Input value={form.macro} onChange={(e) => setForm({ ...form, macro: e.target.value })} placeholder="SSH" />
          </div>
          <div className="space-y-1.5">
            <Label>Proto</Label>
            <Input value={form.proto} onChange={(e) => setForm({ ...form, proto: e.target.value })} placeholder="tcp" />
          </div>
          <div className="space-y-1.5">
            <Label>Port</Label>
            <Input value={form.dport} onChange={(e) => setForm({ ...form, dport: e.target.value })} placeholder="443" />
          </div>
          <div className="col-span-2 space-y-1.5">
            <Label>Comment</Label>
            <Input value={form.comment} onChange={(e) => setForm({ ...form, comment: e.target.value })} />
          </div>
          <div className="col-span-2 flex items-end">
            <Button size="sm" loading={addRule.isPending} onClick={() => addRule.mutate()}>
              Add rule
            </Button>
          </div>
        </div>
      )}
      {rulesQuery.isError ? (
        <p className="text-sm text-[var(--text-muted)]">Could not load firewall rules.</p>
      ) : (
        <DataTable
          columns={columns}
          data={rulesQuery.data ?? []}
          loading={rulesQuery.isLoading}
          searchPlaceholder="Search rules..."
          emptyMessage="No rules configured — add one to start filtering traffic."
          selection={{
            rowId: (r) => String(r.pos),
            selected,
            onSelectedChange: setSelected,
            bulkActions: (ids) => (
              <Button
                size="sm"
                variant="destructive"
                loading={bulkDelete.isPending}
                onClick={() => bulkRemove(ids)}
              >
                <Trash2 className="h-3.5 w-3.5" /> Delete selected
              </Button>
            ),
          }}
        />
      )}
    </div>
  )
}
