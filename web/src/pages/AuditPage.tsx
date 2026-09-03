import { useQuery } from "@tanstack/react-query"
import type { ColumnDef } from "@tanstack/react-table"
import { ClipboardList } from "lucide-react"
import { useMemo, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent } from "@/components/ui/card"
import { DataTable } from "@/components/ui/data-table"
import { ErrorState } from "@/components/ui/error-state"
import { MultiSelect } from "@/components/ui/multi-select"
import { PageHeader } from "@/components/ui/page-header"
import { api } from "@/lib/api"

interface AuditEntry {
  id: string
  username: string
  action: string
  category: string
  target?: string
  ip?: string
  createdAt: string
}

const categoryVariant: Record<string, "ok" | "warn" | "error" | "default"> = {
  admin: "error",
  connections: "warn",
  vm: "ok",
}

export function AuditPage() {
  const { data, isLoading, isError, refetch } = useQuery({ queryKey: ["audit"], queryFn: () => api.get<AuditEntry[]>("/audit") })
  const [category, setCategory] = useState<string[]>([])

  const categories = useMemo(() => Array.from(new Set((data ?? []).map((e) => e.category))).sort(), [data])
  const filtered = useMemo(
    () => (category.length === 0 ? data ?? [] : (data ?? []).filter((e) => category.includes(e.category))),
    [data, category],
  )

  const columns = useMemo<ColumnDef<AuditEntry>[]>(
    () => [
      {
        accessorKey: "createdAt",
        header: "Time",
        cell: (c) => <span className="text-xs text-[var(--text-muted)] tabular">{new Date(c.getValue<string>()).toLocaleString()}</span>,
      },
      { accessorKey: "username", header: "User" },
      {
        accessorKey: "action",
        header: "Action",
        cell: (c) => <span className="font-mono text-xs">{c.getValue<string>()}</span>,
      },
      {
        accessorKey: "category",
        header: "Category",
        cell: (c) => <Badge variant={categoryVariant[c.getValue<string>()] ?? "default"}>{c.getValue<string>()}</Badge>,
      },
      { accessorKey: "target", header: "Target", meta: { hideBelowMd: true }, cell: (c) => <span className="text-xs text-[var(--text-muted)]">{c.getValue<string>()}</span> },
      { accessorKey: "ip", header: "IP", meta: { hideBelowMd: true }, cell: (c) => <span className="font-mono text-xs text-[var(--text-muted)]">{c.getValue<string>()}</span> },
    ],
    [],
  )

  return (
    <div className="space-y-4">
      <PageHeader
        title="Audit Log"
        description="Every action taken through this Ferrum instance."
        icon={ClipboardList}
        actions={
          <MultiSelect
            options={categories.map((c) => ({ value: c, label: c }))}
            selected={category}
            onChange={setCategory}
            allLabel="All categories"
            label="Filter by category"
            className="w-44"
          />
        }
      />

      {isError ? (
        <ErrorState title="Couldn't load the audit log" onRetry={refetch} />
      ) : (
        <Card>
          <CardContent className="pt-4">
            <DataTable
              columns={columns}
              data={filtered}
              loading={isLoading}
              searchPlaceholder="Search audit log..."
              emptyMessage="No activity recorded yet."
            />
          </CardContent>
        </Card>
      )}
    </div>
  )
}
