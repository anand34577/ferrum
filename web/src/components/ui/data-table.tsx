import {
  type ColumnDef,
  flexRender,
  getCoreRowModel,
  getFilteredRowModel,
  getPaginationRowModel,
  getSortedRowModel,
  type SortingState,
  useReactTable,
} from "@tanstack/react-table"
import { ArrowDown, ArrowUp, ArrowUpDown, ChevronLeft, ChevronRight, X } from "lucide-react"
import { type ReactNode, useEffect, useState } from "react"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { ListSearch } from "@/components/ui/list-search"
import { Skeleton } from "@/components/ui/skeleton"
import { cn } from "@/lib/utils"

interface SelectionProps<T> {
  /** Stable string id for a row — required to make selection work across sort/filter/paging. */
  rowId: (row: T) => string
  selected: Set<string>
  onSelectedChange: (next: Set<string>) => void
  /** Rendered in a toolbar above the table whenever at least one row is selected. */
  bulkActions?: (selectedIds: string[]) => ReactNode
}

interface DataTableProps<T> {
  columns: ColumnDef<T, any>[]
  data: T[]
  searchPlaceholder?: string
  emptyMessage?: string
  pageSize?: number
  /** Renders skeleton rows instead of the body while the data is in flight. */
  loading?: boolean
  /** Hides the search box for small, fixed datasets. */
  searchable?: boolean
  /** Adds a checkbox column with select-all — pass this instead of building bulk-select per page. */
  selection?: SelectionProps<T>
  /** Extra controls rendered next to the search input (filters, actions). */
  toolbar?: ReactNode
}

export function DataTable<T>({
  columns,
  data,
  searchPlaceholder = "Search...",
  emptyMessage = "No results.",
  pageSize = 15,
  loading = false,
  searchable = true,
  selection,
  toolbar,
}: DataTableProps<T>) {
  const [sorting, setSorting] = useState<SortingState>([])
  const [globalFilter, setGlobalFilter] = useState("")

  const table = useReactTable({
    data,
    columns,
    state: { sorting, globalFilter },
    onSortingChange: setSorting,
    onGlobalFilterChange: setGlobalFilter,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    initialState: { pagination: { pageSize } },
  })

  // A match on page 3 is invisible while pagination stays parked on page 1 —
  // the same "search doesn't lead you to the actual result" gap fixed on
  // Inventory, generalized to every DataTable (Tasks, Alerts, Backups,
  // Users, Connections, Audit, ...): jump back to the first page whenever
  // the search term changes so a new result is never hidden a page away.
  useEffect(() => {
    table.setPageIndex(0)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [globalFilter])

  const rows = table.getRowModel().rows
  // "Select all" acts on every row matching the current search/filter, not
  // just the current page — the intuitive meaning when someone is about to
  // bulk-delete a filtered-down list.
  const filteredIds = selection ? table.getFilteredRowModel().rows.map((r) => selection.rowId(r.original)) : []
  const allFilteredSelected = selection ? filteredIds.length > 0 && filteredIds.every((id) => selection.selected.has(id)) : false

  function toggleAll() {
    if (!selection) return
    const next = new Set(selection.selected)
    if (allFilteredSelected) {
      for (const id of filteredIds) next.delete(id)
    } else {
      for (const id of filteredIds) next.add(id)
    }
    selection.onSelectedChange(next)
  }

  function toggleRow(id: string) {
    if (!selection) return
    const next = new Set(selection.selected)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    selection.onSelectedChange(next)
  }

  return (
    <div className="space-y-3">
      {(searchable || toolbar || (selection && selection.selected.size > 0)) && (
        <div className="flex flex-wrap items-center gap-2">
          {searchable && (
            <ListSearch
              value={globalFilter}
              onChange={setGlobalFilter}
              placeholder={searchPlaceholder}
              size="sm"
              containerClassName="w-full sm:w-64"
            />
          )}
          {toolbar}
          {selection && selection.selected.size > 0 && (
            <div className="flex flex-1 flex-wrap items-center gap-2 rounded-md border border-[color-mix(in_oklab,var(--color-brand-500)_35%,var(--border))] bg-[color-mix(in_oklab,var(--color-brand-500)_8%,var(--bg-surface))] px-3 py-1.5">
              <span className="text-xs font-semibold text-brand-600 dark:text-brand-400">{selection.selected.size} selected</span>
              {selection.bulkActions?.(Array.from(selection.selected))}
              <Button size="sm" variant="ghost" className="ml-auto text-xs" onClick={() => selection.onSelectedChange(new Set())}>
                <X className="h-3.5 w-3.5" /> Clear
              </Button>
            </div>
          )}
        </div>
      )}

      <div className="overflow-x-auto rounded-lg border border-[var(--border)] bg-[var(--bg-surface)]">
        <table className="w-full text-sm">
          <thead>
            {table.getHeaderGroups().map((hg) => (
              <tr key={hg.id} className="border-b border-[var(--border)] bg-[var(--bg-muted)]/50 backdrop-blur-xs">
                {selection && (
                  <th className="dt-cell w-8 px-3.5 py-2.5">
                    <Checkbox checked={allFilteredSelected} onCheckedChange={toggleAll} aria-label="Select all rows" />
                  </th>
                )}
                {hg.headers.map((header) => {
                  const sortable = header.column.getCanSort()
                  const sorted = header.column.getIsSorted()
                  const ariaSort = sorted === "asc" ? "ascending" : sorted === "desc" ? "descending" : undefined
                  // Lower-priority columns (timestamps, secondary ids) collapse below
                  // md instead of forcing the whole table into horizontal scroll on
                  // phones — set via `meta: { hideBelowMd: true }` on the column def.
                  const hideBelowMd = (header.column.columnDef.meta as { hideBelowMd?: boolean } | undefined)?.hideBelowMd
                  return (
                    <th
                      key={header.id}
                      scope="col"
                      aria-sort={ariaSort}
                      className={cn(
                        "dt-cell px-3.5 py-2.5 text-left font-mono text-[11px] font-semibold uppercase tracking-wider text-[var(--text-muted)]",
                        hideBelowMd && "hidden md:table-cell",
                      )}
                    >
                      {header.isPlaceholder ? null : sortable ? (
                        <button
                          className="flex items-center gap-1.5 transition-colors hover:text-[var(--text)]"
                          onClick={header.column.getToggleSortingHandler()}
                          aria-label={`${String(header.column.columnDef.header)}: activate to sort${
                            sorted === "asc" ? ", currently ascending" : sorted === "desc" ? ", currently descending" : ""
                          }`}
                        >
                          {flexRender(header.column.columnDef.header, header.getContext())}
                          {sorted === "asc" && <ArrowUp className="h-3 w-3 text-brand-500" />}
                          {sorted === "desc" && <ArrowDown className="h-3 w-3 text-brand-500" />}
                          {!sorted && <ArrowUpDown className="h-3 w-3 opacity-35" />}
                        </button>
                      ) : (
                        flexRender(header.column.columnDef.header, header.getContext())
                      )}
                    </th>
                  )
                })}
              </tr>
            ))}
          </thead>
          <tbody>
            {loading &&
              Array.from({ length: 6 }).map((_, i) => (
                <tr key={`skeleton-${i}`} className="border-b border-[var(--border)] last:border-0">
                  {selection && (
                    <td className="dt-cell px-3.5 py-2.5">
                      <Skeleton className="h-4 w-4" />
                    </td>
                  )}
                  {columns.map((col, c) => (
                    <td key={c} className={cn("dt-cell px-3.5 py-2.5", (col.meta as { hideBelowMd?: boolean } | undefined)?.hideBelowMd && "hidden md:table-cell")}>
                      <Skeleton className="h-4 max-w-32" style={{ width: `${40 + ((i * 13 + c * 29) % 45)}%`, opacity: 1 - i * 0.1 }} />
                    </td>
                  ))}
                </tr>
              ))}
            {!loading &&
              rows.map((row) => {
                const id = selection?.rowId(row.original)
                const checked = id ? selection?.selected.has(id) : false
                return (
                  <tr
                    key={row.id}
                    className={cn(
                      "border-b border-[var(--border)] transition-colors duration-150 last:border-0 hover:bg-[var(--bg-surface-hover)]/70",
                      checked && "bg-[color-mix(in_oklab,var(--color-brand-500)_7%,transparent)]",
                    )}
                  >
                    {selection && id && (
                      <td className="dt-cell px-3.5 py-2.5">
                        <Checkbox checked={checked} onCheckedChange={() => toggleRow(id)} aria-label="Select row" />
                      </td>
                    )}
                    {row.getVisibleCells().map((cell) => (
                      <td
                        key={cell.id}
                        className={cn(
                          "dt-cell px-3.5 py-2.5 align-middle text-xs",
                          (cell.column.columnDef.meta as { hideBelowMd?: boolean } | undefined)?.hideBelowMd && "hidden md:table-cell",
                        )}
                      >
                        {flexRender(cell.column.columnDef.cell, cell.getContext())}
                      </td>
                    ))}
                  </tr>
                )
              })}
            {!loading && rows.length === 0 && (
              <tr>
                <td colSpan={columns.length + (selection ? 1 : 0)} className="px-3.5 py-12 text-center text-sm text-[var(--text-muted)]">
                  {emptyMessage}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {!loading && table.getPageCount() > 1 && (
        <div className="flex items-center justify-between rounded-lg border border-[var(--border)] bg-[var(--bg-surface)] px-3 py-2 text-xs text-[var(--text-muted)]">
          <span className="tabular">
            Page {table.getState().pagination.pageIndex + 1} of {table.getPageCount()} · {data.length} rows
          </span>
          <div className="flex gap-1">
            <Button
              size="icon-sm"
              variant="ghost"
              aria-label="Previous page"
              className={cn(!table.getCanPreviousPage() && "opacity-40")}
              disabled={!table.getCanPreviousPage()}
              onClick={() => table.previousPage()}
            >
              <ChevronLeft className="h-4 w-4" />
            </Button>
            <Button
              size="icon-sm"
              variant="ghost"
              aria-label="Next page"
              className={cn(!table.getCanNextPage() && "opacity-40")}
              disabled={!table.getCanNextPage()}
              onClick={() => table.nextPage()}
            >
              <ChevronRight className="h-4 w-4" />
            </Button>
          </div>
        </div>
      )}
    </div>
  )
}
