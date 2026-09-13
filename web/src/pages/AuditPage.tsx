import { useQuery, useQueryClient } from "@tanstack/react-query"
import type { ColumnDef } from "@tanstack/react-table"
import { Bot, CheckCircle2, ClipboardList, XCircle } from "lucide-react"
import { useMemo, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent } from "@/components/ui/card"
import { DataTable } from "@/components/ui/data-table"
import { ErrorState } from "@/components/ui/error-state"
import { MultiSelect } from "@/components/ui/multi-select"
import { PageHeader } from "@/components/ui/page-header"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Timestamp } from "@/components/ui/timestamp"
import { api, type ToolCallRecord } from "@/lib/api"

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
  const queryClient = useQueryClient()
  return (
    <div className="space-y-4">
      <PageHeader
        title="Audit Log"
        description="Every action taken through this Ferrum instance."
        icon={ClipboardList}
        onRefresh={() => {
          void queryClient.invalidateQueries({ queryKey: ["audit"] })
          void queryClient.invalidateQueries({ queryKey: ["admin", "ai", "activity"] })
        }}
      />
      <Tabs defaultValue="audit">
        <TabsList>
          <TabsTrigger value="audit">General audit log</TabsTrigger>
          <TabsTrigger value="ai">AI &amp; MCP activity</TabsTrigger>
        </TabsList>
        <TabsContent value="audit"><GeneralAuditLog /></TabsContent>
        <TabsContent value="ai"><AIActivityLog /></TabsContent>
      </Tabs>
    </div>
  )
}

function GeneralAuditLog() {
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
        // Forensic surface: absolute first, relative age on hover.
        cell: (c) => <Timestamp iso={c.getValue<string>()} mode="absolute" className="text-xs text-[var(--text-muted)]" />,
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

  if (isError) return <ErrorState title="Couldn't load the audit log" onRetry={refetch} />

  return (
    <Card>
      <CardContent className="pt-4">
        <DataTable
          columns={columns}
          data={filtered}
          loading={isLoading}
          searchPlaceholder="Search audit log..."
          emptyMessage="No activity recorded yet."
          toolbar={
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
      </CardContent>
    </Card>
  )
}

/** Every tool call any user's AI Assistant session or MCP client has made,
 * across the whole instance — what state-changing REST actions look like in
 * the general audit log above, this is for the read+write agentic surface:
 * "who let an LLM do what, and did it succeed." */
function AIActivityLog() {
  const { data, isLoading, isError, refetch } = useQuery({ queryKey: ["admin", "ai", "activity"], queryFn: () => api.get<ToolCallRecord[]>("/admin/ai/activity") })

  const columns = useMemo<ColumnDef<ToolCallRecord>[]>(
    () => [
      {
        accessorKey: "createdAt",
        header: "Time",
        cell: (c) => <Timestamp iso={c.getValue<string>()} mode="absolute" className="text-xs text-[var(--text-muted)]" />,
      },
      { accessorKey: "username", header: "User" },
      {
        accessorKey: "source",
        header: "Source",
        cell: (c) => <Badge variant={c.getValue<string>() === "mcp" ? "brand" : "outline"}>{c.getValue<string>() === "mcp" ? "MCP" : "Chat"}</Badge>,
      },
      { accessorKey: "tool", header: "Tool", cell: (c) => <span className="font-mono text-xs">{c.getValue<string>()}</span> },
      {
        accessorKey: "ok",
        header: "Result",
        cell: (c) =>
          c.getValue<boolean>() ? (
            <span className="inline-flex items-center gap-1 text-xs text-[var(--status-ok)]"><CheckCircle2 className="h-3.5 w-3.5" /> OK</span>
          ) : (
            // The provider's error text renders right in the cell — a
            // title-only tooltip is unreachable by keyboard and easy to miss.
            <span className="inline-flex max-w-56 flex-col items-start text-xs text-[var(--status-error)]">
              <span className="inline-flex items-center gap-1"><XCircle className="h-3.5 w-3.5" /> Failed</span>
              {c.row.original.error && (
                <span className="w-full truncate text-[10px] text-[var(--text-muted)]" title={c.row.original.error}>
                  {c.row.original.error}
                </span>
              )}
            </span>
          ),
      },
    ],
    [],
  )

  if (isError) return <ErrorState title="Couldn't load AI/MCP activity" onRetry={refetch} />

  return (
    <Card>
      <CardContent className="pt-4">
        <DataTable
          columns={columns}
          data={data ?? []}
          loading={isLoading}
          searchPlaceholder="Search tool calls..."
          emptyMessage="No AI or MCP tool calls recorded yet."
        />
        {!isLoading && (data?.length ?? 0) === 0 && (
          <p className="mt-3 flex items-center gap-1.5 text-xs text-[var(--text-muted)]">
            <Bot className="h-3.5 w-3.5" /> This fills in once someone uses the AI Assistant or connects an MCP client.
          </p>
        )}
      </CardContent>
    </Card>
  )
}
